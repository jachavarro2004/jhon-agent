package hook

import "testing"

// L14: what a line executes that changes the merge gate's environment is
// found however its words are spelled; a name handed to a reader is not.
func TestEnvChangeReadsWhatTheLineRuns(t *testing.T) {
	for _, line := range []string{
		"unset JHON_AGENT", `unset "JHON_AGENT"`, `unset JHON\_AGENT`, "unset JHON_'AGENT'", "n=JHON_AGENT; unset $n", "unset ${!JHON_@}", "unset ${!GIT_C*}",
		"export -n JHON_AGENT", "export JHON_AGENT=0", "JHON_AGENT= git status", "declare -x GIT_CONFIG_{COUNT=1,KEY_0=x}",
		"printf -v GIT_CONFIG_COUNT 1", "read GIT_CONFIG_COUNT <<< 1", "read -r -a GIT_CONFIG_KEY_0 <<< x", "mapfile -t GIT_CONFIG_KEY_0 < f",
		"GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=a GIT_CONFIG_VALUE_0=b git status", "SHELLOPTS=noexec git status",
		"export -f read", "declare -fx printf", "env -i git status", "/usr/bin/env -i git status", "command env -i git status",
		"env --ignore-env git status", "env --i git status", "env -iv git status", "env - git status", "env -u JHON_AGENT git status",
		"env -uJHON_AGENT git status", "env --unset=JHON_AGENT git status", "env --un JHON_AGENT git status", "env -S 'x y'",
		"env FOO=1 JHON_AGENT=0 git status", "exec -c git status", "(exec -c git status)", "nohup env -i git status",
		"sh -c 'unset JHON_AGENT; git status'", "eval 'unset JHON_AGENT'",
	} {
		if envChange(line) == "" {
			t.Errorf("%s: not found", line)
		}
	}
	for _, line := range []string{
		"printenv JHON_AGENT", "echo $JHON_AGENT", "env | grep JHON", "env", "grep -rn GIT_CONFIG_COUNT docs", "rg GIT_CONFIG_PARAMETERS",
		"rg -n 'GIT_CONFIG_PARAMETERS' .", "cat notes.md | grep JHON_AGENT", "env FOO=1 git status", "export PATH=$PATH:/x", "read -r line",
		"unset FOO", "declare -f", "ENV=production npm run build", "export ENV=staging", "BASH_ENV=x bash -c true", "command -v env", "exec git status", "printf '%s\\n' GIT_CONFIG_COUNT",
	} {
		if why := envChange(line); why != "" {
			t.Errorf("%s: refused: %s", line, why)
		}
	}
}

// T228 L2: zsh's precommand modifiers, repeat, external runners and the
// strings a builtin runs later are read through the shared reader (sh.Unwrap),
// so env -i, env -u and unset behind them are found as bare.
func TestEnvChangeReadsBehindWrappersAndRunners(t *testing.T) {
	for _, line := range []string{
		"noglob env -i git status", "- env -u JHON_AGENT git status", "nocorrect unset JHON_AGENT", "repeat 1 env -i git status",
		"repeat 2 { unset JHON_AGENT }", "builtin command env -i git status", "builtin - env -i git status", "setsid -f env -i git status",
		"chrt -f 1 env -i git status", "taskset -c 0 env -i git status", "ionice -c3 env -i git status", "strace -f -o x env -i git status",
		"watch -n1 'env -i git status'", "watch -x env -i git status", "nice -n 5 env -i git status", "timeout 5 env -i git status",
		"stdbuf -o0 env -i git status", "unshare -r env -i git status", "systemd-run --user env -i git status", "gosu u env -i git status",
		"sg wheel -c 'env -i git status'", "trap 'unset JHON_AGENT' EXIT", "sched +1 unset JHON_AGENT", "alias g='env -i git'",
		"zsh -c 'noglob unset JHON_AGENT'", "eval 'repeat 1 unset JHON_AGENT'", "setsid chrt 1 noglob env -i git status",
		"strace --no-such-option env -i git status",
	} {
		if envChange(line) == "" {
			t.Errorf("%s: not found", line)
		}
	}
	for _, line := range []string{
		"noglob git status", "- git status", "repeat 3 echo hi", "setsid -f git status", "watch -n1 'git status'", "timeout 5 git status",
		"chrt 1 printenv JHON_AGENT", "trap 'echo done' EXIT", "nice ls", "strace --no-such-option ls", "taskset -p 1234",
		"setsid env FOO=1 git status", "noglob export PATH=$PATH:/x",
	} {
		if why := envChange(line); why != "" {
			t.Errorf("%s: refused: %s", line, why)
		}
	}
}
