package sandbox

import (
	"path/filepath"
	"strconv"
	"strings"
)

// `jhon sandbox enter` (T248, the owner's option A, L4) is the first
// program of every sandbox `jhon sandbox run` starts, and the last step of
// its setup: bubblewrap runs it from a descriptor of jhon's own program
// (/proc/self/fd/<n>), with one --expect for each bind. It checks, inside
// the sandbox, that each destination is a mount whose folder is the one
// Jhon checked, and only then replaces itself with the program, which gets
// its argv and environment as bubblewrap would have given them. It is not
// on the usage: no one runs it but the sandbox, and anyone who does gets
// that check and the program, nothing else.

// enterUsage is enter's usage, for arguments it does not take.
const enterUsage = "usage: jhon sandbox enter [--expect <folder>=<fd>[,ro]]... -- <program> [args...]\n"

// expectation is one bind enter checks: the folder bubblewrap mounted it on
// (absolute, clean), the descriptor of the folder Jhon checked (3 or
// above), and whether the mount is read-only.
type expectation struct {
	dest string
	fd   int
	ro   bool
}

// Command is what follows bubblewrap's options (Args) for program: `--`,
// enter run from jhon's own program, an --expect for each bind, `--` and
// the program.
func (b *Bound) Command(program []string) []string {
	out := []string{"--", "/proc/self/fd/" + strconv.Itoa(int(b.self.Fd())), "sandbox", "enter"}
	for _, e := range b.expect {
		out = append(out, "--expect", e)
	}
	return append(append(out, "--"), program...)
}

// parseEnter reads enter's arguments: each --expect, then `--` and the
// program; ok is false for anything else.
func parseEnter(args []string) (expects []expectation, program []string, ok bool) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--":
			if i+1 >= len(args) {
				return nil, nil, false
			}
			return expects, args[i+1:], true
		case "--expect":
			if i+1 >= len(args) {
				return nil, nil, false
			}
			i++
			v, ro := strings.CutSuffix(args[i], ",ro")
			k := strings.LastIndexByte(v, '=')
			if k < 0 {
				return nil, nil, false
			}
			dest := v[:k]
			fd, err := strconv.Atoi(v[k+1:])
			if err != nil || fd < 3 || !filepath.IsAbs(dest) || filepath.Clean(dest) != dest || dest == "/" {
				return nil, nil, false
			}
			expects = append(expects, expectation{dest: dest, fd: fd, ro: ro})
		default:
			return nil, nil, false
		}
	}
	return nil, nil, false
}

// enterRefused is enter's note for dest, a bind that is not the folder
// checked.
func enterRefused(dest string) string {
	return "jhon sandbox: " + dest + " is not the folder Jhon checked (it changed while bubblewrap mounted it), so nothing was run; fix: run the command again\n"
}
