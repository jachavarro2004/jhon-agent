package audit

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The audit's hot spots, rewritten for speed (S14): each rewrite returns
// what the code it replaced returned, over every tool result and command of
// the audit fixtures and the cases that exercise its edges.

// fixtureSessions are every session under test/testdata/audit that loads.
func fixtureSessions(t *testing.T) []*Session {
	t.Helper()
	dirs, err := os.ReadDir("../../../test/testdata/audit")
	must(t, err)
	var out []*Session
	for _, d := range dirs {
		files, _ := filepath.Glob(filepath.Join("../../../test/testdata/audit", d.Name(), "*.jsonl"))
		for _, f := range files {
			if s, errs := LoadSession(f); s != nil && len(errs) == 0 {
				out = append(out, s)
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no fixture session loads")
	}
	return out
}

// fixtureTexts are every tool result text, and every Bash command with its
// directory, of the fixtures.
func fixtureTexts(t *testing.T) (results []string, commands [][2]string) {
	t.Helper()
	for _, s := range fixtureSessions(t) {
		for _, tr := range s.transcripts() {
			for _, e := range tr.Entries {
				for _, r := range e.ToolResults {
					results = append(results, r.Text)
				}
				for _, u := range e.ToolUses {
					if c, ok := strProp(u.Input, "command"); ok {
						commands = append(commands, [2]string{c, or(e.Cwd, testRoot)})
					}
				}
			}
		}
	}
	return results, commands
}

func TestCapRefusedIsTheCapRegex(t *testing.T) {
	results, commands := fixtureTexts(t)
	cases := append(results,
		"you have spent your 40 turns", "You have spent your 40 turns", "you have spent your turns",
		"turn-guard: worker has spent 41 turns, past its cap", "turn-guard: a\nb has spent 41 turns, past its cap",
		"x turn-guard: y turn-guard: z has spent 3 turns, past its cap", "turn-guard: has spent 3 turns",
		"reached maxTurns", "Reached maxTurns", "reached maxturns", "", strings.Repeat("turn-guard: ", 200))
	for _, c := range commands {
		cases = append(cases, c[0])
	}
	for _, text := range cases {
		if got, want := capRefused(text), capRefusal.MatchString(text); got != want {
			t.Errorf("capRefused(%q) = %v, the regex says %v", short(text, 200), got, want)
		}
	}
}

// withoutHeredocsByRegex is WithoutHeredocs as it was: every line through
// unquoted and the heredoc regexes.
func withoutHeredocsByRegex(command string) string {
	var out []string
	end, open := "", false
	for _, l := range strings.Split(command, "\n") {
		if open {
			if js.Trim(l) == end {
				open = false
			}
			continue
		}
		out = append(out, l)
		m := heredocOpen.FindStringSubmatch(unquoted(l))
		if m == nil {
			m = heredocQuoted.FindStringSubmatch(l)
		}
		if m != nil {
			end, open = m[1], true
		}
	}
	return strings.Join(out, "\n")
}

func TestWithoutHeredocsIsTheLineRegex(t *testing.T) {
	_, commands := fixtureTexts(t)
	cases := []string{
		"cat > a <<EOF\nx\nEOF\nls", "cat <<-'END'\n\tbody\n\tEND\necho done", `cat <<"Q"` + "\nq\nQ\nrest",
		`echo "<<EOF"` + "\nls", `echo '<<'EOF` + "\nls\nEOF\nafter", `echo "<"<EOF` + "\nls", "a <'x'< b\nc",
		"x << EOF\nbody\n  EOF  \ny", "<<EOF", "no heredoc\nat all", "", `echo "a\"<<EOF"` + "\nls", "cat <<A <<B\na\nA\nb\nB",
	}
	for _, c := range commands {
		cases = append(cases, c[0])
	}
	for _, c := range cases {
		if got, want := WithoutHeredocs(c), withoutHeredocsByRegex(c); got != want {
			t.Errorf("WithoutHeredocs(%q) = %q, the line regex gives %q", c, got, want)
		}
	}
}

func TestWritesOfIsWrittenFilesAndWritesOutside(t *testing.T) {
	_, commands := fixtureTexts(t)
	commands = append(commands,
		[2]string{"echo a > out.txt && cp x.txt y.txt", "/work/p"},
		[2]string{"cd sub && sed -i s/a/b/ f.go g.go > /tmp/log", "/work/p"},
		[2]string{`python3 -c "open('/work/p/src/a.py','w').write('x')"`, "/work/p"},
		[2]string{`node -e "require('fs').writeFileSync('/elsewhere/b.js','x')"`, "/work/p"},
		[2]string{"tee a b c < in", "/work/p"},
		[2]string{"echo a > src/x && echo b > src/x", "/work/p"})
	in := func(abs string) bool { return strings.HasPrefix(abs, "/work/p/src/") }
	for _, c := range commands {
		written, outside := writesOf(c[0], c[1], in)
		if want := guard.WrittenFiles(c[0], c[1]); !slices.Equal(written, want) {
			t.Errorf("writesOf(%q) writes %q, guard.WrittenFiles %q", c[0], written, want)
		}
		if want := guard.WritesOutsideIn(c[0], c[1], in); !slices.Equal(outside, want) {
			t.Errorf("writesOf(%q) outside %q, guard.WritesOutsideIn %q", c[0], outside, want)
		}
	}
}
