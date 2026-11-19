// T128f slice 5: what lies outside the project is the sandbox's. A shell
// write there, and a credentials file there, pass safety-guard; a secret
// inside the project (L1354), a protected path and a target the guard
// cannot resolve keep today's verdicts.

package guards

import "testing"

func TestOutsideTheProjectIsTheSandboxs(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	for _, c := range []string{"echo x > ~/notes.txt", "cp i18n/en.json <OTHER>/x.json", "mkdir -p /srv/x", "sed -i s/a/b/ /etc/hosts",
		"dd if=i18n/en.json of=<OTHER>/x", "chmod -R 755 <OTHER>",
		"cat <OTHER>/.env", "cat ~/.aws/credentials", "cat ~/.ssh/*", "cd <OTHER> && cat .env",
		`python3 -c "open('/srv/keys/id_rsa').read()"`, `python3 -c "import os; open(os.path.expanduser('~/.netrc'))"`} {
		for _, o := range []sgOpts{{}, top()} {
			r := s.bash(t, s.expand(c), o)
			code(t, r, 0, c)
			noDecision(t, r.Stdout, c)
		}
	}
	for _, c := range []string{"cat .env", "cat <P>/.env", "cat ./i18n/../.env", "cat $PWD/.env", "cat .env* | head", "head -n 1 config/.env.local",
		"git show HEAD:.env", "cat $X/.ssh/*", `python3 -c "open('<P>/.env').read()"`, `python3 -c "open('.env').read()"`,
		`node -e "require('fs').readFileSync(process.cwd() + '/.env')"`, `python3 -c "import os; open(os.path.join(os.getcwd(), '/.env'))"`,
		"echo x > .git/config", "cp i18n/en.json .claude/settings.json", "echo x | tee ~/.ssh/authorized_keys", "chmod -R 755 ..",
		"dd if=i18n/en.json of=.git/config"} {
		s.code(t, s.expand(c), sgOpts{}, 2, "still refused: "+c)
	}
	for _, c := range []string{`echo x > "$(printf .git)/config"`, `echo x > "$OUT"`} {
		askedWhy(t, s.bash(t, c, top()).Stdout, "still asked: "+c)
	}
}
