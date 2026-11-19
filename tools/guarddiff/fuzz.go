package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// The fuzz set (test/guards/corpus/fuzz.tsv), in the spirit of the WU3
// verifies' fuzz lines (L830, L834): commands composed from the forms the
// guards judge (redirects, mktemp, rm, cp and their targets inside the
// project, outside it and in tmp, credentials and framework paths, git and
// gh, bash -c, xargs, eval, quoting), from a fixed seed so the file is the
// same on every regeneration (`go run ./tools/guarddiff gen-fuzz`).
const (
	fuzzSeed  = 128
	fuzzLines = 3600
)

var (
	fuzzTargets = [][]string{
		{"src/app.py", "out.txt", "<P>/src/new.py", "tests/test_x.py", "src/a.ts", "svc/src/x.py", "src/parse/p.ts", "src/util/helpers.ts", "package.json", "openspec/changes/c1/tasks.md", "./build/out"},
		{".git/config", ".git/hooks/pre-commit", ".claude/settings.json", ".claude/settings.local.json"},
		{"<OTHER>/x", "../x", "/etc/jhon.conf", "<HOME>/x", "~/notes.txt", "$HOME/.bashrc", "<SCRATCH>/x", "/usr/local/bin/x"},
		{"/tmp/jhon-fz", "<TMP>/x", "$TMPDIR/x", `"$(mktemp)"`, `"$d/x"`, "/var/tmp/x"},
		{"<FW>/orchestrator.md", "<HOME>/.claude/CLAUDE.md", "<HOME>/.local/bin/jhon", "<HOME>/.agents/skills/ldd/SKILL.md", "<HOME>/.claude/agents/general.md"},
		{".env", ".env.local", ".env.example", "deep/sub/id_rsa", "credentials.json", "<HOME>/.ssh/id_rsa", "<HOME>/.aws/credentials", "<OTHER>/id_rsa", "keys/id_rsa.old", "~/.netrc"},
	}
	fuzzWrites = []string{"echo x > {T}", "printf 'y\\n' >> {T}", "cat src/app.py > {T}", "echo x | tee {T}", "echo x | tee -a {T}", "cp src/app.py {T}",
		"cp -r src {T}", "mv out.txt {T}", "rm -f {T}", "rm -rf {T}", "touch {T}", "sed -i 's/a/b/' {T}", "mkdir -p {T}", "ln -sf src/app.py {T}",
		"dd if=src/app.py of={T}", "curl -sSo {T} http://example.invalid/a", "install -m 644 src/app.py {T}", "truncate -s 0 {T}", "go build -o {T} ./..."}
	fuzzReads = []string{"cat {T}", "head -n 5 {T}", "grep -n x {T}", "jq . {T}", "source {T}", ". {T}", "wc -l {T}", "cp {T} out.txt", "base64 {T}"}
	fuzzXargs = []string{"echo {T} | xargs rm -rf", "echo {T} | xargs -I{} cp src/app.py {}", "find . -name '*.py' | xargs cat", "printf '%s\\n' {T} | xargs cat"}
	fuzzPlain = []string{"git status", "git diff", "git log --oneline -5", "git add -A", "git commit -q -m x", "git push", "git push origin main", "git push --force",
		"git push -f origin HEAD", "git reset --hard", "git reset --hard HEAD~1", "git clean -fdx", "git checkout -- .", "git restore .", "git stash",
		"git branch -D topic", "git rebase -i HEAD~2", "git config core.hooksPath /tmp/h", "git config user.email x@y", "git -C <OTHER> status",
		"git -C <OTHER> commit -am x", "git tag v9", "git push --tags", "gh pr list", "gh pr view 1", "gh pr create --title x --body y",
		"gh pr merge 1 --squash", "gh pr merge 1 --rebase", "gh api repos/o/r", "gh api -X DELETE repos/o/r", "gh release create v9",
		"gh issue comment 1 -b x", "npm publish", "npm test", "go test ./...", "uv run pytest -q", "kubectl apply -f x.yaml", "ssh host",
		"docker rm -f c", "make", "ls -la", "jhon mode yolo", "jhon grant push", "$x push", "git frobnicate", "sleep 1", "while true; do sleep 1; done"}
	fuzzWraps = []string{"bash -c {Q}", "sh -c {Q}", "eval {Q}", "env X=1 {C}", "timeout 5 {C}", "( {C} )", "{ {C}; }", "{C} && echo ok", "{C}; git status",
		"true || {C}", "if [ -f x ]; then {C}; fi", "for f in a b; do {C}; done", "{C} 2>&1 | tail -5", "cd <OTHER> && {C}", "cd /tmp && {C}", "nohup {C} &"}
	fuzzPlaces = []string{"ldd", "repo", "folder", "corpus"}
)

// fuzzRand is splitmix64: a stream fixed by its seed on every Go release.
type fuzzRand uint64

func (r *fuzzRand) n(k int) int {
	*r += 0x9e3779b97f4a7c15
	z := uint64(*r)
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	return int((z ^ z>>31) % uint64(k))
}

func (r *fuzzRand) pick(s []string) string { return s[r.n(len(s))] }

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// target is a path of a random class, plain or quoted.
func (r *fuzzRand) target() string {
	t := r.pick(fuzzTargets[r.n(len(fuzzTargets))])
	if strings.ContainsAny(t, `"'`) {
		return t
	}
	switch r.n(4) {
	case 2:
		if !strings.Contains(t, "$") {
			return shQuote(t)
		}
	case 3:
		return `"` + t + `"`
	}
	return t
}

func (r *fuzzRand) command() string {
	var c string
	switch k := r.n(10); {
	case k < 5:
		c = strings.ReplaceAll(r.pick(fuzzWrites), "{T}", r.target())
	case k < 7:
		c = strings.ReplaceAll(r.pick(fuzzReads), "{T}", r.target())
	case k < 9:
		c = r.pick(fuzzPlain)
	default:
		c = strings.ReplaceAll(r.pick(fuzzXargs), "{T}", r.target())
	}
	for depth := 0; depth < 2 && r.n(2) == 0; depth++ {
		c = strings.NewReplacer("{Q}", shQuote(c), "{C}", c).Replace(r.pick(fuzzWraps))
	}
	if strings.Contains(c, "$d") {
		c = "d=$(mktemp -d); " + c
	}
	return c
}

// fileInput is a Write, Edit, Read or Grep call on a target with no shell
// in it, by absolute path; "" for a target with one.
func (r *fuzzRand) fileInput() (string, string) {
	t := r.pick(fuzzTargets[r.n(len(fuzzTargets))])
	if strings.ContainsAny(t, `"'$`) {
		return "", ""
	}
	if after, ok := strings.CutPrefix(t, "~/"); ok {
		t = "<HOME>/" + after
	} else if !strings.HasPrefix(t, "/") && !strings.HasPrefix(t, "<") {
		t = "<P>/" + strings.TrimPrefix(t, "./")
	}
	tool := r.pick([]string{"Write", "Edit", "Read", "Grep"})
	in := map[string]string{"file_path": t}
	switch tool {
	case "Write":
		in["content"] = "x"
	case "Edit":
		in["old_string"], in["new_string"] = "a", "b"
	case "Grep":
		in = map[string]string{"pattern": "x", "path": t}
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // a placeholder's <> stay as written
	enc.Encode(in)
	return tool, strings.TrimSuffix(b.String(), "\n")
}

// fuzzTSV is the fuzz set's file.
func fuzzTSV() string {
	r := fuzzRand(fuzzSeed)
	var b strings.Builder
	fmt.Fprintf(&b, "# Generated by `go run ./tools/guarddiff gen-fuzz` (seed %d): do not edit. Columns: tools/guarddiff/README.md.\n", fuzzSeed)
	b.WriteString("# place\tfixture\tcaller\ttool\tinput\tsource\n")
	seen := map[string]bool{}
	for len(seen) < fuzzLines {
		place, fixture := r.pick(fuzzPlaces), "none"
		if place == "corpus" {
			fixture = r.pick([]string{"none", "cred-near", "cred-far"})
		}
		tool, input := "Bash", ""
		if r.n(7) == 0 {
			tool, input = r.fileInput()
		} else {
			input = r.command()
		}
		row := strings.Join([]string{place, fixture, r.pick(callers), tool, strconv.Quote(input), "fuzz"}, "\t")
		if input == "" || seen[row] {
			continue
		}
		seen[row] = true
		b.WriteString(row + "\n")
	}
	return b.String()
}
