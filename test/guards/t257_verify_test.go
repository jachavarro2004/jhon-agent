package guards

import (
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// B1 (S item 5), B2 (items 1-2), B3 (item 3): refused to a sub-agent.
func TestT257VerifyClassEscapes(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	support.Write(t, s.scratch, map[string]string{
		"e.cfg": "[alias]\n\tx = !rm -rf .git\n",
		"c.cfg": "[includeIf \"gitdir:/\"]\n\tpath = e.cfg\n",
	})
	e, c := filepath.Join(s.scratch, "e.cfg"), filepath.Join(s.scratch, "c.cfg")
	for _, l := range []string{
		"GIT_CONFIG_GLOBAL=" + c + " git x",
		"git -c include.path=" + c + " x",
		"GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=include.path GIT_CONFIG_VALUE_0=" + e + " git x",
		"GIT_CONFIG_PARAMETERS=\"'include.path'='" + e + "'\" git x",
		"git -c include.path=/dev/stdin x < " + e,
		"git -c include.path=/dev/fd/3 x 3< " + e,
		"bash /dev/fd/3 3< <(echo 'rm -rf .git')",
		"exec 3< <(echo 'rm -rf .git'); bash <&3",
		"bash <(cat <(echo 'rm -rf .git'))",
		"cat <(echo 'rm -rf .git') | bash",
		"awk -f <(echo 'BEGIN{system(\"rm -rf .git\")}')",
		"awk -f - <<< 'BEGIN{system(\"rm -rf .git\")}'",
		"sed -f - notes.txt <<< '1e rm -rf .git'",
		"exec 3< <(echo '.shell rm -rf .git'); sqlite3 x.db <&3",
		"perl -e 'system qw|rm -rf .git|'",
		"perl -e 'system qw!rm -rf .git!'",
		"ruby -e 'system(*%w<rm -rf .git>)'",
	} {
		code(t, s.bash(t, l, sgOpts{}), 2, l)
	}
}

// B4 (item 6): a name defined in eval, a shell string, a sourced <(…) or by hash -p is not trusted.
func TestT257VerifyRouteRedefinedName(t *testing.T) {
	t.Parallel()
	m := newMG(t)
	for _, l := range []string{
		`eval 'ls() { "$@"; }'; ls env -u JHON_AGENT git status`,
		`bash -c 'ls() { "$@"; }; ls env -u JHON_AGENT git status'`,
		`source <(echo 'ls() { "$@"; }'); ls env -u JHON_AGENT git status`,
		"hash -p /usr/bin/env ls; ls -u JHON_AGENT git status",
	} {
		code(t, m.bash(t, m.p, l), 2, l)
	}
}
