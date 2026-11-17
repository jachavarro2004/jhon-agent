package release

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// The public README is the user's manual until the source opens (T36, L351):
// it names the commands a user runs, and each one it names is one `jhon help`
// states.
func TestPublicReadmeNamesOnlyCommandsJhonHas(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join(support.Root(), "dist", "public", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	named := readmeCommands(string(b))

	t.Run("it names every command a user runs", func(t *testing.T) {
		t.Parallel()
		for _, want := range []string{
			"setup", "start", "start pi", "doctor", "status", "risk",
			"executions run", "executions verify", "audit", "dashboard",
			"settings list", "settings get", "settings set", "settings migrate",
			"profile", "update", "help",
		} {
			if !slices.ContainsFunc(named, func(n string) bool { return n == want || strings.HasPrefix(n, want+" ") }) {
				t.Errorf("dist/public/README.md does not name `jhon %s`", want)
			}
		}
	})

	t.Run("jhon help states every command it names", func(t *testing.T) {
		t.Parallel()
		r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "help"}, Dir: temp(t), Env: support.Env("JHON_ROOT=" + support.Root())})
		equal(t, r.Code, 0, r.Stderr)
		var rows [][]string
		for _, l := range strings.Split(r.Stdout, "\n") {
			if strings.HasPrefix(l, "  jhon ") {
				rows = append(rows, strings.Fields(strings.NewReplacer("[", " ", "]", " ", "|", " ").Replace(l))[1:])
			}
		}
		for _, n := range named {
			words := strings.Fields(n)
			if words[0] == "help" {
				continue // `jhon help` is the usage itself, run above
			}
			cmd := slices.ContainsFunc(rows, func(row []string) bool { return row[0] == words[0] })
			if !cmd {
				t.Errorf("dist/public/README.md names `jhon %s`, which jhon help does not state", words[0])
				continue
			}
			if len(words) < 2 {
				continue
			}
			// The second word is a subcommand when a row of the command has it;
			// otherwise it is an argument (a change's name), which is fine only
			// when no row of the command takes a subcommand in its place.
			sub := slices.ContainsFunc(rows, func(row []string) bool { return row[0] == words[0] && slices.Contains(row[1:], words[1]) })
			argument := slices.ContainsFunc(rows, func(row []string) bool {
				return row[0] == words[0] && (len(row) == 1 || strings.HasPrefix(row[1], "<") || strings.HasPrefix(row[1], "-"))
			})
			if !sub && !argument {
				t.Errorf("dist/public/README.md names `jhon %s %s`, which jhon help does not state", words[0], words[1])
			}
		}
	})
}

// readmeCommands is every `jhon <command> [<word>]` the README names in a code
// span or a code block, as "command" or "command word".
func readmeCommands(md string) []string {
	var code []string
	inBlock := false
	for _, l := range strings.Split(md, "\n") {
		if strings.HasPrefix(l, "```") {
			inBlock = !inBlock
			continue
		}
		if inBlock {
			code = append(code, l)
			continue
		}
		for _, m := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(l, -1) {
			code = append(code, m[1])
		}
	}
	re := regexp.MustCompile(`(?:^|\s)jhon((?: [a-z][a-z-]*){1,2})`)
	var out []string
	for _, c := range code {
		for _, m := range re.FindAllStringSubmatch(c, -1) {
			if n := strings.TrimSpace(m[1]); !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	return out
}
