package settings

import (
	"os"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// deployToolsAccepted is what sandbox.deploy_tools accepts, as a refusal
// names it.
const deployToolsAccepted = `a list of program names, each a bare name with no / and no whitespace, such as aws,cdk,sam, and none of the shells, wrappers, terminals, editors, interpreters, remote shells or script runners Jhon knows (bash, env, sudo, xargs, python, node, ssh, make, npx and the like, in any case and with any version suffix); jhon settings set takes them separated by commas, and "" for none`

// odd/tasks/sandbox-deploy-tools.md S6: `jhon settings` lists, sets and
// resets sandbox.deploy_tools, comma-separated, as it does
// sandbox.allow_write; a name with a slash or whitespace is refused with
// exit 2 and the settings stay as they were.
func TestSettingsListsSetsAndResetsDeployTools(t *testing.T) {
	h := newHome(t)
	if got, ok := find(list(t, h, h.dir), "sandbox.deploy_tools"); !ok || !same(got.Value, []any{}) || got.Source != "default" {
		t.Errorf("sandbox.deploy_tools by default = %#v; want an empty list from the default", got)
	}
	r := h.jhon(t, "set", "sandbox.deploy_tools", "aws,cdk")
	if r.Code != 0 || r.Stdout != "sandbox.deploy_tools = aws; cdk in ~/xdg-config/jhon/settings.toml\n" || r.Stderr != "" {
		t.Fatalf("set sandbox.deploy_tools aws,cdk: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	if got, ok := find(list(t, h, h.dir), "sandbox.deploy_tools"); !ok || !same(got.Value, []any{"aws", "cdk"}) || got.Source != "file" {
		t.Errorf("list --json after set: %#v; want [aws cdk] from the file", got)
	}
	if r := h.jhon(t); r.Code != 0 || !containsRow(r.Stdout, "sandbox.deploy_tools", "aws; cdk", "file") {
		t.Errorf("jhon settings: exit %d; want a row sandbox.deploy_tools aws; cdk file\n%s", r.Code, r.Out())
	}
	saved, err := os.ReadFile(h.file())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"./aws", "/usr/bin/aws", "a b", "aws,,cdk"} {
		r := h.jhon(t, "set", "sandbox.deploy_tools", bad)
		shown := `"` + bad + `"`
		if bad == "aws,,cdk" {
			shown = `""`
		}
		want := "jhon settings: sandbox.deploy_tools: " + shown + " is not accepted (accepted: " + deployToolsAccepted + ")\n"
		if r.Code != 2 || r.Stdout != "" || r.Stderr != want {
			t.Errorf("set sandbox.deploy_tools %q: exit %d, stdout %q, stderr %q; want 2 and %q", bad, r.Code, r.Stdout, r.Stderr, want)
		}
		if now, _ := os.ReadFile(h.file()); string(now) != string(saved) {
			t.Errorf("set %q changed the settings:\n%s", bad, now)
		}
	}
	r = h.jhon(t, "set", "sandbox.deploy_tools", "")
	if r.Code != 0 || r.Stderr != "" {
		t.Fatalf("reset sandbox.deploy_tools: exit %d, stderr %q", r.Code, r.Stderr)
	}
	if got, ok := find(list(t, h, h.dir), "sandbox.deploy_tools"); !ok || !same(got.Value, []any{}) {
		t.Errorf("list --json after the reset: %#v; want an empty list", got)
	}
	if got := support.Read(t, h.file()); !strings.HasSuffix(got, "[sandbox]\ndeploy_tools = []\n") {
		t.Errorf("the settings hold %q", got)
	}
}

// S8: a shell, wrapper or interpreter in the list is refused with exit 2,
// the refusal naming it, and the settings stay as they were.
func TestSettingsRefusesAShellAsADeployTool(t *testing.T) {
	h := newHome(t)
	if r := h.jhon(t, "set", "sandbox.deploy_tools", "aws"); r.Code != 0 {
		t.Fatalf("set aws: exit %d\n%s", r.Code, r.Out())
	}
	saved := support.Read(t, h.file())
	for _, bad := range []string{"bash", "env", "sudo", "python3", "node", "ssh"} {
		r := h.jhon(t, "set", "sandbox.deploy_tools", "aws,"+bad)
		want := "jhon settings: sandbox.deploy_tools: \"" + bad + "\" is not accepted (accepted: " + deployToolsAccepted + ")\n"
		if r.Code != 2 || r.Stdout != "" || r.Stderr != want {
			t.Errorf("set sandbox.deploy_tools aws,%s: exit %d, stdout %q, stderr %q; want 2 and %q", bad, r.Code, r.Stdout, r.Stderr, want)
		}
		if now := support.Read(t, h.file()); now != saved {
			t.Errorf("set aws,%s changed the settings:\n%s", bad, now)
		}
	}
}

// containsRow is whether the plain listing has a row of key, value and
// source, as `jhon settings` aligns them.
func containsRow(out, key, value, source string) bool {
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 0 && f[0] == key {
			return strings.Join(f[1:], " ") == value+" "+source
		}
	}
	return false
}

// same is whether two decoded JSON values are equal.
func same(a, b any) bool {
	x, ok1 := a.([]any)
	y, ok2 := b.([]any)
	if !ok1 || !ok2 || len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
