// A credentials glob is judged by what it reaches in the project now
// (T64, L508-L509), in a line as in a lone command: a reader's glob beside
// stderr sent to /dev/null, and a find that only lists names by -name or
// -iname, piped into a filter, are expanded like `cat data/*.json`. A glob
// or name test that reaches a credentials file is refused in every mode,
// with the message it always had.

package guards

import (
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// dataTree is the bench's shape: data/c.json and a.py, no credentials file.
func dataTree(t *testing.T) sgEnv {
	t.Helper()
	s := newSG(t)
	support.Write(t, s.p, map[string]string{"data/c.json": "{}\n", "a.py": "x = 1\n"})
	return s
}

// piCall is the payload pi's judge() sends, through `jhon hook pre-tool`.
func (s sgEnv) piCall(t *testing.T, command string, child bool) support.Result {
	t.Helper()
	payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": cmd(command), "cwd": s.p}
	if child {
		payload["agent_id"] = "pi-child"
	} else {
		payload["permission_mode"] = "default"
	}
	return hook(t, s.env, "pre-tool", []string{"--agent", "*", "safety-guard", "vcs-guard", ".git", "secrets-guard", "route-guard"}, payload)
}

var lineForms = []string{"cat *.py data/*.json 2>/dev/null", `find . -name "*.json" | head`, "cat data/*.json", "ls data/*.json",
	"cat data/*.json 2>&1", "cat data/*.json >/dev/null", `find . -name '*.json'`, `find data -iname '*.JSON' -type f | sort | head -n 5`,
	`find . -maxdepth 2 -name '*.json' -print | wc -l`, "cat data/*.json &>/dev/null", "cat data/*.json 2>&1 >/dev/null"}

func TestCredentialGlobInALineIsJudgedByItsExpansion(t *testing.T) {
	t.Parallel()
	s := dataTree(t)
	for _, c := range lineForms {
		s.code(t, c, sgOpts{}, 0, "a sub-agent, no credentials file in the project")
		s.code(t, c, top(), 0, "the top level, no credentials file in the project")
		for _, child := range []bool{false, true} {
			if r := s.piCall(t, c, child); r.Code != 0 {
				t.Errorf("pi (child %v): %q exits %d: %s%s", child, c, r.Code, r.Stderr, r.Stdout)
			}
		}
	}
}

// hardLine is a credentials file of each kind the list names, and a command
// reaching it with the refusal's first words.
type hardLine struct{ file, command, refused string }

func globRefusal(g string) string {
	return "refused: " + g + ": `" + g + "` matches a credentials file, which the owner handles"
}

func fileRefusal(f string) string {
	return "refused: " + f + ": " + f + " holds credentials, which the owner handles"
}

var hardLines = []hardLine{
	{"cert.pem", "cat *.pem", globRefusal("*.pem")},
	{".env", "cat .env*", globRefusal(".env*")},
	{"keys/server.key", `find . -name "*.key"`, globRefusal("*.key")},
	{"keys/server.key", `find . -name "*.key" | head`, globRefusal("*.key")},
	{".env", `find . -name ".env"`, fileRefusal(".env")},
	{"config/credentials.json", "cat config/*.json", globRefusal("config/*.json")},
	{"config/credentials.json", "cat *.py config/*.json 2>/dev/null", globRefusal("config/*.json")},
	{"config/credentials.json", `find . -name "*.json" | head`, globRefusal("*.json")},
	{"config/credentials.json", `find config -iname '*.JSON'`, globRefusal("*.JSON")},
	{".env.production", "cat .env.*", globRefusal(".env.*")},
	{".envrc", "cat .env* 2>/dev/null", globRefusal(".env*")},
	{"home/id_ed25519", "cat home/id_*", globRefusal("home/id_*")},
	{"home/id_ed25519", `find . -name 'id_*' | head`, globRefusal("id_*")},
	{"store/a.p12", "cat store/*.p12", globRefusal("store/*.p12")},
	{"store/b.pfx", `find store -name '*.pfx'`, globRefusal("*.pfx")},
	{"store/c.jks", "cat store/*.jks 2>&1", globRefusal("store/*.jks")},
	{"store/d.keystore", "cat store/*.keystore", globRefusal("store/*.keystore")},
	{"aws/credentials", "cat aws/cred*", globRefusal("aws/cred*")},
	{".netrc", "cat .netrc", fileRefusal(".netrc")},
	{".pgpass", "cat .pgpass 2>/dev/null", fileRefusal(".pgpass")},
	{".pypirc", "cat .pypirc", fileRefusal(".pypirc")},
}

func TestCredentialGlobHardLineHoldsInEveryMode(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"normal", "yolo", "super-yolo"} {
		for _, h := range hardLines {
			s := dataTree(t)
			s.setMode(t, s.p, mode)
			support.Write(t, s.p, map[string]string{h.file: "dummy\n"})
			for _, o := range []sgOpts{{}, top()} {
				r := s.bash(t, h.command, o)
				code(t, r, 2, mode+": "+h.command+" beside "+h.file)
				if !strings.Contains(r.Stderr, h.refused) {
					t.Errorf("%s: %q beside %s: stderr %q lacks %q", mode, h.command, h.file, r.Stderr, h.refused)
				}
			}
			if r := s.piCall(t, h.command, false); r.Code != 2 {
				t.Errorf("%s: pi: %q beside %s exits %d", mode, h.command, h.file, r.Code)
			}
		}
	}
}

// What a listing find can do beyond naming files keeps today's rule.
func TestCredentialFindBeyondAListingKeepsTheNameRule(t *testing.T) {
	t.Parallel()
	s := dataTree(t)
	for _, c := range []string{`find . -name '*.json' -exec cat {} +`, `find . -name '*.json' | xargs cat`, `find . -name '*.json' -print0 | xargs -0 cat`,
		`find -L . -name '*.json'`, `find .. -name '*.json'`, `find /tmp -name '*.json'`, `find . -name '*.json' -delete`,
		`find . -name '*.json' | head > out.txt`, `find . -name '*.json' | sh`, `find ~ -name '*.json'`,
		`find . -name '*.json' -fprint out`, `find . -name '*.json' 2>/dev/null | tee out`,
		`find . -name '*.json' | sort -o out`, `find . -name '*.json' | uniq - out`,
		`find .//.. -name '*.json'`, `find -H . -name '*.json'`} {
		s.code(t, c, sgOpts{}, 2, "a find the allow-list cannot prove")
	}
}

// findWalked were refused by the name rule until T87b (L630-L631): a find
// that only names files, on a line that writes nothing, is walked.
var findWalked = []string{`find . -path '*.json'`, `find . -name '*.path' -path '*.json'`, `find . -not -path '*.json'`, `find . -name '*.json' | head x.json`,
	`find . -name '*.json' -print0 | wc --files0-from=-`}

func TestCredentialFindListingIsWalked(t *testing.T) {
	t.Parallel()
	s := dataTree(t)
	for _, c := range findWalked {
		s.code(t, c, sgOpts{}, 0, "a find that names files, no credentials file")
	}
	support.Write(t, s.p, map[string]string{"data/deep/credentials.json": "{}\n"})
	for _, c := range findWalked {
		s.code(t, c, sgOpts{}, 2, "a find that names files beside a credentials file")
	}
}

// A list of read-only commands is judged element by element (T64c, L519):
// the contacts-migration run's survey of its repository.
// invoiceSurvey is quick-f772832e/invoice-slices-plain-jhon-1's line, as run.
const invoiceSurvey = `git status --short && ls -la && cat *.py | head -200 && ls tests 2>/dev/null; find . -name "*.json" -not -path "./.git/*" | head`

const contactsSurvey = `git status --short && git log --oneline | head -5 && ls -R | grep -v '^\.git' | head -50 && cat *.py; cat data/*.json; ls tests 2>/dev/null`

func contactsTree(t *testing.T) sgEnv {
	t.Helper()
	s := newSG(t)
	support.Write(t, s.p, map[string]string{"migrate.py": "x = 1\n", "store.py": "y = 2\n", "test_store.py": "z = 3\n",
		"data/contacts.json": "[]\n", "docs/DATA.md": "# data\n", "README.md": "# r\n"})
	return s
}

func TestCredentialGlobInAReadOnlyListIsJudgedByItsExpansion(t *testing.T) {
	t.Parallel()
	s := contactsTree(t)
	for _, c := range []string{contactsSurvey,
		`git status --short && ls -R | grep -v pycache | head -50 && cat *.py; cat data/*.json; ls tests 2>/dev/null && cat tests/*.py`,
		invoiceSurvey, `find . -name '*.json'; cat x`,
		"pwd; echo x; cat data/*.json || git diff --stat", "git branch --show-current && jq . data/*.json"} {
		s.code(t, c, sgOpts{}, 0, "a read-only list, no credentials file")
		s.code(t, c, top(), 0, "a read-only list at the top level, no credentials file")
		for _, child := range []bool{false, true} {
			if r := s.piCall(t, c, child); r.Code != 0 {
				t.Errorf("pi (child %v): %q exits %d: %s%s", child, c, r.Code, r.Stderr, r.Stdout)
			}
		}
	}
	support.Write(t, s.p, map[string]string{"data/credentials.json": "dummy\n"})
	for c, g := range map[string]string{contactsSurvey: "data/*.json", invoiceSurvey: "*.json", `find . -name '*.json'; cat x`: "*.json"} {
		r := s.bash(t, c, sgOpts{})
		code(t, r, 2, c+" beside data/credentials.json")
		if !strings.Contains(r.Stderr, globRefusal(g)) {
			t.Errorf("%q: the refusal lacks today's message: %s", c, r.Stderr)
		}
	}
}

func TestCredentialGlobInAListThatMovesOrWritesKeepsTheNameRule(t *testing.T) {
	t.Parallel()
	s := contactsTree(t)
	for _, c := range []string{"cp a.py b.py; cat data/*.json", "touch c; cat data/*.json", "ln -s data l; cat data/*.json",
		"cat data/*.json; ls > out", "cat data/*.json; sort -o out x", "cat data/*.json; uniq x out",
		"cat data/*.json; git log --output=out", "cat data/*.json; git branch new",
		"cat data/*.json; eval ls", "cat data/*.json; source x.sh", "cat data/*.json; git show HEAD -- '*.json'",
		"cat data/*.json; node x.mjs", "git -C .. status; cat data/*.json"} {
		s.code(t, c, sgOpts{}, 2, "a list the allow-list cannot prove")
	}
	// T87b (L630-L631): a list that writes nothing walks its extension globs.
	walked := []string{"cd docs; cat data/*.json", "cat data/*.json; cd docs", "cat data/*.json & ls", "cat data/*.json; grep -r x .", "cat data/*.json; echo $(ls)",
		"cat data/*.json; grep x *.json", "cat data/*.json | head; ls", "cat data/*.json\nls"}
	for _, c := range walked {
		s.code(t, c, sgOpts{}, 0, "a list that writes nothing, no credentials file")
	}
	support.Write(t, s.p, map[string]string{"data/x/credentials.json": "{}\n", "docs/data/credentials.json": "{}\n"})
	for _, c := range walked {
		s.code(t, c, sgOpts{}, 2, "a list that writes nothing, beside a credentials file")
	}
	support.Write(t, s.p, map[string]string{".env": "dummy\n"})
	r := s.bash(t, "cat data/*.json; cat .env", sgOpts{})
	code(t, r, 2, "a list that reads .env")
	if !strings.Contains(r.Stderr, strings.TrimPrefix(fileRefusal(".env"), "refused: ")) {
		t.Errorf("the .env refusal lacks today's message: %s", r.Stderr)
	}
}
