// T109 WU4 (L784, L820): a word built from an IFS expansion with an
// operation (${IFS%??}, ${IFS:0:1}, ${IFS/ /x}) is not worked out by the
// reader: the command is unreadable, asked as such, never silently allowed.

package guards

import (
	"strings"
	"testing"
)

// ifsActs are runnerActs plus a credential read: each is refused wherever it is read.
var ifsActs = append(append([]string{}, runnerActs...), "cat ~/.ssh/id_rsa")

// ifsSliced are act with its spaces spelled as IFS slices.
func ifsSliced(act string) []string {
	return []string{
		"sh -c '" + strings.ReplaceAll(act, " ", "${IFS%??}") + "'",
		strings.ReplaceAll(act, " ", "${IFS%??}"),
		strings.ReplaceAll(act, " ", "${IFS:0:1}"),
		strings.ReplaceAll(act, " ", "${IFS%%[^ ]*}"),
		"IFS=x; " + strings.ReplaceAll(act, " ", "${IFS%?}"),
	}
}

// An IFS-sliced act is refused, or asked as unreadable (passed
// with the unreadable note in super-yolo, L464).
func TestIFSSlicedActsFailClosed(t *testing.T) {
	t.Parallel()
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo, _, env := ownerFixture(t, mode)
			for _, act := range ifsActs {
				for _, c := range ifsSliced(act) {
					for _, caller := range callers {
						r := ownerCall(t, env, repo, caller, c)
						label := mode + "/" + caller + ": " + c
						if r.Code == 2 {
							continue
						}
						switch {
						case mode == "super-yolo":
							code(t, r, 0, label)
							matches(t, modeNote(t, r.Stdout), `allowed by the owner's super-yolo mode: unknown: `, label)
						case caller == "top-default":
							code(t, r, 0, label)
							matches(t, askedWhy(t, r.Stdout, label), `(^|\n)unknown: `, label)
						default:
							code(t, r, 2, label)
						}
					}
				}
			}
		})
	}
}

// A benign command built from an IFS slice is asked as unreadable.
func TestIFSSlicedBenignIsAsked(t *testing.T) {
	t.Parallel()
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo, _, env := ownerFixture(t, mode)
			for _, c := range []string{"ls${IFS/ /x}-la", "ls${IFS%??}-la", "IFS=x; ls${IFS%?}-la"} {
				for _, caller := range callers {
					r := ownerCall(t, env, repo, caller, c)
					label := mode + "/" + caller + ": " + c
					switch {
					case mode == "super-yolo":
						code(t, r, 0, label)
						matches(t, modeNote(t, r.Stdout), `allowed by the owner's super-yolo mode: unknown: `, label)
					case caller == "top-default":
						code(t, r, 0, label)
						matches(t, askedWhy(t, r.Stdout, label), `(^|\n)unknown: `, label)
					default:
						code(t, r, 2, label)
					}
				}
			}
		})
	}
}

// A plain $IFS / ${IFS} keeps its reading as a word break (L486).
func TestIFSPlainKeepsItsDecision(t *testing.T) {
	t.Parallel()
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo, _, env := ownerFixture(t, mode)
			for _, c := range []string{"echo a${IFS}b", "echo a$IFS b"} {
				for _, caller := range callers {
					r := ownerCall(t, env, repo, caller, c)
					code(t, r, 0, mode+"/"+caller+": "+c)
					noDecision(t, r.Stdout, mode+"/"+caller+": "+c)
				}
			}
		})
	}
}

// A hard refusal stays refused when a word of the command is an IFS
// expansion with an operation (T109 WU4 verify): the unknown note adds to
// the reading, never replaces it.
func TestIFSOpWordKeepsHardRefusal(t *testing.T) {
	t.Parallel()
	hard := []string{"git push " + "--force", "/usr/bin/git push " + "--force", "chmod -R 777 /etc",
		"claude --dangerously-skip-permissions", "bash -c 'jhon grant push'", "echo 'jhon ldd off' | xargs herdr pane run p1"}
	ops := []string{"${IFS%??}", "${IFS:-x}", "${IFS:0:1}", "${!IFS}", "${IFS%%[^ ]*}", "${IFS/ /x}", "${IFS#?}"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo, _, env := ownerFixture(t, mode)
			for _, h := range hard {
				for _, op := range ops {
					c := h + " " + op
					for _, caller := range callers {
						code(t, ownerCall(t, env, repo, caller, c), 2, mode+"/"+caller+": "+c)
					}
				}
			}
		})
	}
	s := newSG(t)
	for _, h := range hard {
		c := h + " ${IFS%??}"
		s.code(t, c, top(), 2, "Claude Code top")
		code(t, s.piCall(t, c, false), 2, "pi top: "+c)
		code(t, s.piCall(t, c, true), 2, "pi child: "+c)
	}
	c := "sudo jhon${IFS%??}mode${IFS%??}yolo"
	s.code(t, c, top(), 2, "Claude Code top")
	code(t, s.piCall(t, c, false), 2, "pi top: "+c)
}

// A plain $IFS / ${IFS} word break keeps a hard refusal (WU4 re-verify, L855).
func TestIFSPlainWordBreakKeepsHardRefusal(t *testing.T) {
	t.Parallel()
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo, _, env := ownerFixture(t, mode)
			for _, c := range []string{"git${IFS}push${IFS}--" + "force", "git$IFS push$IFS --" + "force",
				"chmod${IFS}-R${IFS}777${IFS}..", "claude${IFS}--dangerously-skip-permissions"} {
				for _, caller := range callers {
					code(t, ownerCall(t, env, repo, caller, c), 2, mode+"/"+caller+": "+c)
				}
			}
		})
	}
}
