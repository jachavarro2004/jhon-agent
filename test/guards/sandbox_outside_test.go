// T128f slice 5: what lies outside the project is the sandbox's. A shell
// write there passes safety-guard. Slice 6 (L1366, L1368): an unresolved
// write passes in the project too; a write or delete of a credentials file
// and a protected path stay refused. T271 (S10): a listed reader printing a
// credentials file is refused wherever it lies; code the guard does not
// read as a reader (python's open) is not judged.

package guards

import "testing"

func TestOutsideTheProjectIsTheSandboxs(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	for _, c := range []string{"echo x > ~/notes.txt", "cp i18n/en.json <OTHER>/x.json", "mkdir -p /srv/x", "sed -i s/a/b/ /etc/hosts",
		"dd if=i18n/en.json of=<OTHER>/x", "chmod -R 755 <OTHER>",
		`python3 -c "open('/srv/keys/id_rsa').read()"`, `python3 -c "import os; open(os.path.expanduser('~/.netrc'))"`} {
		for _, o := range []sgOpts{{}, top()} {
			r := s.bash(t, s.expand(c), o)
			code(t, r, 0, c)
			noDecision(t, r.Stdout, c)
		}
	}
	// Unresolved writes pass inside the project too (L1366).
	for _, c := range []string{`python3 -c "open('.env').read()"`, `echo x > "$(printf .git)/config"`, `echo x > "$OUT"`} {
		s.code(t, s.expand(c), sgOpts{}, 0, "passes: "+c)
	}
	for _, c := range []string{"cat <OTHER>/.env", "cat ~/.aws/credentials", "cat ~/.ssh/*", "cd <OTHER> && cat .env", "cat .env", "cat $PWD/.env", "git show HEAD:.env"} {
		for _, o := range []sgOpts{{}, top()} {
			code(t, s.bash(t, s.expand(c), o), 2, "a credentials read (T271): "+c)
		}
	}
	for _, c := range []string{"rm .env", "echo x > .env", "echo x > .git/config", "cp i18n/en.json .claude/settings.json", "echo x | tee ~/.ssh/authorized_keys", "chmod -R 755 ..",
		"dd if=i18n/en.json of=.git/config"} {
		s.code(t, s.expand(c), sgOpts{}, 2, "still refused: "+c)
	}
}
