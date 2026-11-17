package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// F4 (L1159): a manifest set that breaks what the engine still assumes is
// refused by `jhon validate`, naming the file and the field.
func TestRenderValidateIntegrationShape(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		edit func(t *testing.T, repo string)
		want string
	}{
		{"a core integration no longer required", func(t *testing.T, repo string) {
			editFile(t, filepath.Join(repo, "integrations", "codegraph", "manifest.yaml"), "required: true\nwhen_off: refused", "required: false\nwhen_off: render")
		}, `integration\s+integrations/codegraph/manifest.yaml  required: false, but setup shows codegraph as a locked row and pins its binary`},
		{"the remote integration probed locally", func(t *testing.T, repo string) {
			editFile(t, filepath.Join(repo, "integrations", "context7", "manifest.yaml"), "  probe: remote", "  probe: --version")
		}, `integration\s+integrations/context7/manifest.yaml  doctor.probe: --version is not remote; doctor probes context7 at doctor.url`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			repo := support.CopyRepo(t)
			c.edit(t, repo)
			r := validate(t, repo)
			wantCode(t, r, 1)
			match(t, r.Out(), c.want)
		})
	}
}

// F5 (L1159): a tree without integrations/ validates the manifests compiled
// into jhon and says so on one line, as before v0.3.3.
func TestRenderValidateWithoutIntegrationsFolder(t *testing.T) {
	t.Parallel()
	repo := support.CopyRepo(t)
	if err := os.RemoveAll(filepath.Join(repo, "integrations")); err != nil {
		t.Fatal(err)
	}
	r := validate(t, repo)
	wantCode(t, r, 0)
	const note = "validation: no integrations/ in this tree; validated the integration manifests compiled into jhon\n"
	if strings.Count(r.Stdout, note) != 1 || strings.Contains(r.Stderr, "integration") {
		t.Errorf("want the one line %q:\n%s", note, r.Out())
	}
	match(t, r.Stdout, `(?m)^validation: ok — \d+ agents`)
}

// F4 (L1159): a binary built from a reshaped manifest set (the remote
// integration gone, a core one no longer required) still starts, and its
// validate refuses the set, naming the files and fields; nothing panics.
func TestReshapedEmbeddedManifestsStart(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds jhon from a copy of the tree")
	}
	repo := support.CopyRepo(t)
	if err := os.RemoveAll(filepath.Join(repo, "integrations", "context7")); err != nil {
		t.Fatal(err)
	}
	editFile(t, filepath.Join(repo, "integrations", "codegraph", "manifest.yaml"), "required: true\nwhen_off: refused", "required: false\nwhen_off: render")
	bin := filepath.Join(t.TempDir(), "jhon")
	build := exec.Command("go", "build", "-trimpath", "-o", bin, "./engine/cmd/jhon")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	run := func(args ...string) support.Result {
		return support.Run(t, support.Cmd{Args: append([]string{bin}, args...),
			Env: support.Env("HOME="+t.TempDir(), "JHON_ROOT="+repo, support.RuntimesPath(t))})
	}
	v := run("version")
	wantCode(t, v, 0)
	r := run("render-engine", "--validate")
	wantCode(t, r, 1)
	for _, want := range []string{
		`integration\s+integrations/codegraph/manifest.yaml  required: false, but setup shows codegraph as a locked row and pins its binary`,
		`integration\s+integrations/context7/manifest.yaml  name: no manifest is named context7; doctor and the render read its url and setting by name`,
	} {
		match(t, r.Out(), want)
	}
	for _, res := range []support.Result{v, r} {
		if strings.Contains(res.Out(), "panic") || strings.Contains(res.Out(), "goroutine ") {
			t.Errorf("a panic:\n%s", res.Out())
		}
	}
}
