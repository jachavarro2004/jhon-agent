// odd/tasks/sandbox-deploy-tools.md: the programs the owner lists in
// sandbox.deploy_tools run outside the sandbox as docker does, solas en su
// línea: `jhon sandbox profile` answers unsandboxed for such a line under
// docker's conditions, and the runtimes run it with --profile unsandboxed,
// the owner's environment kept. These tests never run a real aws, cdk,
// sam or terraform: profile only reads the line, and a fake aws writes
// what it was given outside every folder a sandboxed command may write.
package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// deployHome is a scratch home and project, with no inherited setting, and
// its environment; with tools it sets sandbox.deploy_tools through `jhon
// settings set`, as the owner would.
func deployHome(t *testing.T, tools string) (proj string, env []string) {
	t.Helper()
	home := t.TempDir()
	proj = filepath.Join(home, "project")
	if err := os.MkdirAll(filepath.Join(proj, "infra"), 0o755); err != nil {
		t.Fatal(err)
	}
	support.GitRepo(t, proj)
	for _, kv := range support.Env() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "JHON_") || strings.HasPrefix(k, "XDG_") || k == "HOME" {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+home, "JHON_TELEMETRY=off", "JHON_ROOT="+support.Root())
	if tools != "" {
		if r := support.Jhon(t, proj, env, "settings", "set", "sandbox.deploy_tools", tools); r.Code != 0 {
			t.Fatalf("settings set sandbox.deploy_tools %s: exit %d\n%s", tools, r.Code, r.Out())
		}
	}
	return proj, env
}

// profileIs checks `jhon sandbox profile -- <line>` for each line.
func profileIs(t *testing.T, proj string, env []string, want map[string]string) {
	t.Helper()
	for line, w := range want {
		r := support.Jhon(t, proj, env, "sandbox", "profile", "--", line)
		if r.Code != 0 || r.Stdout != w+"\n" || r.Stderr != "" {
			t.Errorf("profile %q: exit %d, stdout %q, stderr %q; want 0 and %s", line, r.Code, r.Stdout, r.Stderr, w)
		}
	}
}

// S3: the owner's examples, with `jhon settings sandbox.deploy_tools
// aws,cdk,sam`.
func TestTheOwnersDeployToolExamples(t *testing.T) {
	proj, env := deployHome(t, "aws,cdk,sam")
	profileIs(t, proj, env, map[string]string{
		"aws s3 sync dist s3://x": "unsandboxed",
		"cdk deploy":              "unsandboxed",
		"aws s3 ls | grep x":      "work",
		"terraform apply":         "work",
	})
}

// S4: with sandbox.deploy_tools unset every line keeps today's profile.
func TestNoDeployToolsKeepsTodaysProfiles(t *testing.T) {
	proj, env := deployHome(t, "")
	profileIs(t, proj, env, map[string]string{
		"aws sts get-caller-identity": "work",
		"aws s3 sync dist s3://x":     "work",
		"cdk deploy":                  "work",
		"sam deploy":                  "work",
		"terraform apply":             "work",
		"docker compose up -d":        "unsandboxed",
	})
}

// S5: a listed tool's line loses unsandboxed where docker's does, and keeps
// it after a leading cd into the project.
func TestADeployToolLineGetsDockersConditions(t *testing.T) {
	proj, env := deployHome(t, "aws,cdk,sam")
	profileIs(t, proj, env, map[string]string{
		"aws s3 ls | grep x":     "work",
		"aws s3 ls && make":      "work",
		"aws s3 ls > out.txt":    "work",
		"aws s3 cp $FILE s3://x": "work",
		"cd " + filepath.Join(proj, "infra") + " && cdk deploy": "unsandboxed",
		"cd /tmp && cdk deploy":                                 "work",
	})
}

// fakeAWS puts an aws in dir, first on PATH: it appends its words and
// AWS_PROFILE to note.
func fakeAWS(t *testing.T, b *box, dir, note string) {
	t.Helper()
	support.Write(t, dir, map[string]string{"aws": "#!/bin/sh\necho \"$* profile=$AWS_PROFILE\" >> " + note + "\n"})
	if err := os.Chmod(filepath.Join(dir, "aws"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i, kv := range b.env {
		if path, ok := strings.CutPrefix(kv, "PATH="); ok {
			b.env[i] = "PATH=" + dir + ":" + path
		}
	}
	b.env = append(b.env, "AWS_PROFILE=deploy")
}

// S5: a listed tool's line runs outside the sandbox with the owner's
// environment; not listed, or planted where a sandboxed command may write,
// it runs in the work profile, its write outside refused.
func TestADeployToolLineRunsOutsideTheSandbox(t *testing.T) {
	b := newBox(t)
	note := filepath.Join(b.base, "aws-ran")
	fakeAWS(t, &b, filepath.Join(b.base, "aws-bin"), note)
	r := b.unsandboxed(t, b.env, "aws s3 sync dist s3://x")
	if r.Code == 0 || exists(note) {
		t.Errorf("aws not listed: exit %d, stderr %q, wrote outside %v; want it refused in the work profile", r.Code, r.Stderr, exists(note))
	}
	b.settings(t, "schema = 1\n\n[sandbox]\ndeploy_tools = [\"aws\"]\n")
	r = b.unsandboxed(t, b.env, "aws s3 sync dist s3://x")
	got, _ := os.ReadFile(note)
	if want := "s3 sync dist s3://x profile=deploy\n"; r.Code != 0 || string(got) != want {
		t.Fatalf("a listed aws: exit %d, stderr %q, note %q; want 0 and %q", r.Code, r.Stderr, got, want)
	}
	os.Remove(note)
	// The pipe's exit code is cat's: only the refused write tells.
	if r := b.unsandboxed(t, b.env, "aws s3 ls | cat"); exists(note) {
		t.Errorf("a pipe: exit %d, stderr %q, wrote outside; want it refused in the work profile", r.Code, r.Stderr)
	}
	planted := newBox(t)
	planted.settings(t, "schema = 1\n\n[sandbox]\ndeploy_tools = [\"aws\"]\n")
	pnote := filepath.Join(planted.base, "aws-ran")
	fakeAWS(t, &planted, filepath.Join(planted.proj, "bin"), pnote)
	if r := planted.unsandboxed(t, planted.env, "aws s3 ls"); r.Code == 0 || exists(pnote) {
		t.Errorf("a planted aws: exit %d, stderr %q, wrote outside %v; want it run in the work profile, its write refused", r.Code, r.Stderr, exists(pnote))
	}
}

// S3 for Claude Code: the wrap hands a listed tool's line back as the
// unsandboxed run, and a tool not listed keeps the work wrap.
func TestClaudeCodeRunsADeployToolLineUnsandboxed(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	b.settings(t, "schema = 1\n\n[sandbox]\nmode = \"on\"\ndeploy_tools = [\"aws\", \"cdk\", \"sam\"]\n")
	r, a := preTool(t, b, env, bashGuards, "Bash", `{"command":"aws sts get-caller-identity"}`, false)
	if r.Code != 0 || a.Out.Decision != "allow" || a.command(t) != unwrapped(t, b.proj, bash, "aws sts get-caller-identity") {
		t.Errorf("aws sts get-caller-identity: exit %d, stderr %q, stdout %q; want the unsandboxed run", r.Code, r.Stderr, r.Stdout)
	}
	_, a = preTool(t, b, env, bashGuards, "Bash", `{"command":"terraform version"}`, false)
	if got := a.command(t); got != expected(t, "Bash", got, b.proj, bash, "terraform version") {
		t.Errorf("terraform version: the command is %s; want the work wrap", got)
	}
}
