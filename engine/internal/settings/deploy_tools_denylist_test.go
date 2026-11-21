package settings

import (
	"errors"
	"strings"
	"testing"
)

// odd/tasks/sandbox-deploy-tools.md S8: "jhon settings set rechaza bash, sh,
// zsh, fish, env, sudo, doas, xargs, python, node, ssh, etc.: con uno de
// ellos todo correría sin sandbox." A shell, a wrapper that runs another
// program, an interpreter or a remote shell is refused, by set and in the
// file, and a refused set writes nothing; a deploy tool is still taken.
func TestDeployToolsRefusesShellsWrappersAndInterpreters(t *testing.T) {
	refused := []string{
		"bash", "sh", "zsh", "fish", "dash", "ksh", "csh", "tcsh", "busybox",
		"env", "sudo", "doas", "su", "pkexec", "xargs", "nohup", "nice", "timeout", "setsid", "unshare", "chroot",
		"python", "python3", "node", "deno", "bun", "perl", "ruby", "php", "lua", "pwsh",
		"ssh", "scp", "sftp",
		"make", "npm", "npx", "pnpm", "yarn",
	}
	path := write(t, "schema = 1\n")
	if _, err := Set(path, "sandbox.deploy_tools", "aws,cdk"); err != nil {
		t.Fatal(err)
	}
	saved := read(t, path)
	for _, name := range refused {
		var inv *InvalidError
		if _, err := Set(path, "sandbox.deploy_tools", "aws,"+name); !errors.As(err, &inv) {
			t.Errorf("Set(aws,%s) = %v, want it refused", name, err)
		}
		if _, err := Load(write(t, "[sandbox]\ndeploy_tools = [\""+name+"\"]\n"), noEnv); err == nil || !strings.Contains(err.Error(), `sandbox.deploy_tools: "`+name+`" is not accepted`) {
			t.Errorf("a file listing %s: %v, want it refused", name, err)
		}
	}
	if got := read(t, path); got != saved {
		t.Errorf("a refused value changed the file:\n%s", got)
	}
	for _, name := range []string{"aws", "cdk", "sam", "terraform", "tofu", "pulumi", "serverless", "gcloud", "az", "kubectl", "helm", "flyctl", "vercel", "wrangler"} {
		if _, err := Set(path, "sandbox.deploy_tools", name); err != nil {
			t.Errorf("Set(%s) = %v, want a deploy tool taken", name, err)
		}
	}
}

// S8 (T3 verify mwqi6pf3-7-ew46): another spelling of a refused program (a
// version suffix, a case variant), the POSIX synonym of source, and the
// builtins, wrappers, terminals, editors, REPLs and runners that run
// whatever they are given are refused too.
func TestDeployToolsRefusesVariantsAndOtherEscapes(t *testing.T) {
	refused := []string{
		"python3.14", "python3.12", "pypy3.10", "node22", "nodejs22", "ruby3.3", "perl5.38",
		"lua5.4", "luajit-2.1", "php8.3", "php83", "tclsh8.6", "bash5", "ksh93", "Bash", "PYTHON",
		".", "trap", "fc", "enable", "sched", "zmodload", "rbash", "osh", "ysh",
		"sg", "newgrp", "run0", "systemd-run", "machinectl", "flatpak-spawn", "distrobox", "toolbox",
		"firejail", "bwrap", "dbus-launch", "sshpass", "ssh-agent", "at", "crontab", "find", "fd", "rsync",
		"mosh", "autossh", "tmux", "screen", "kitty", "xterm", "vim", "nvim", "vi", "ed", "emacs",
		"ipython", "irb", "jshell", "java", "R", "guile", "ghci", "tcc",
		"cargo", "go", "gradle", "mvn", "rake", "bundle", "poetry", "hatch", "tox", "nox", "pip", "pip3", "nix-shell", "conda", "bunx",
		"Rscript", "rscript", "python3.14t", "yarnpkg", "jupyter", "zx",
		"mise", "asdf", "pyenv", "rbenv", "nodenv", "volta", "fnm", "direnv",
		"taskset", "chrt", "prlimit", "setpriv", "numactl", "setarch", "faketime", "proxychains", "torsocks",
		"unbuffer", "rlwrap", "valgrind", "perf", "hyperfine", "systemd-inhibit",
	}
	path := write(t, "schema = 1\n")
	if _, err := Set(path, "sandbox.deploy_tools", "aws"); err != nil {
		t.Fatal(err)
	}
	saved := read(t, path)
	for _, name := range refused {
		var inv *InvalidError
		if _, err := Set(path, "sandbox.deploy_tools", "aws,"+name); !errors.As(err, &inv) {
			t.Errorf("Set(aws,%s) = %v, want it refused", name, err)
		}
		if _, err := Load(write(t, "[sandbox]\ndeploy_tools = [\""+name+"\"]\n"), noEnv); err == nil {
			t.Errorf("a file listing %s loaded, want it refused", name)
		}
	}
	if got := read(t, path); got != saved {
		t.Errorf("a refused value changed the file:\n%s", got)
	}
	for _, name := range []string{"aws", "cdk", "sam", "terraform", "tofu", "pulumi", "serverless", "sls", "gcloud", "az", "kubectl", "helm", "flyctl", "vercel", "wrangler", "eksctl", "doctl", "s3cmd"} {
		if _, err := Set(path, "sandbox.deploy_tools", name); err != nil {
			t.Errorf("Set(%s) = %v, want a deploy tool taken", name, err)
		}
	}
}
