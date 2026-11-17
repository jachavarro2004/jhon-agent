package integrations

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	embedded "github.com/jachavarro2004/jhon-agent/integrations"
)

// embeddedEdited is the compiled-in manifests, each edit replacing old with
// new in a file ("" new and old drops the file).
func embeddedEdited(t *testing.T, edits ...[3]string) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	paths, err := fs.Glob(embedded.FS, "*/manifest.yaml")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no embedded manifests: %v", err)
	}
	for _, p := range paths {
		b, err := fs.ReadFile(embedded.FS, p)
		if err != nil {
			t.Fatal(err)
		}
		out[p] = &fstest.MapFile{Data: b}
	}
	for _, e := range edits {
		f, ok := out[e[0]]
		if !ok {
			t.Fatalf("no %s", e[0])
		}
		if e[1] == "" && e[2] == "" {
			delete(out, e[0])
			continue
		}
		if !strings.Contains(string(f.Data), e[1]) {
			t.Fatalf("%s holds no %q", e[0], e[1])
		}
		out[e[0]] = &fstest.MapFile{Data: []byte(strings.Replace(string(f.Data), e[1], e[2], 1))}
	}
	return out
}

// The compiled-in set has no problem, of form or of shape.
func TestEmbeddedHasNoProblems(t *testing.T) {
	ms, probs := Embedded()
	if len(probs) > 0 || len(ms) == 0 {
		t.Fatalf("%d manifests, problems %v", len(ms), probs)
	}
	if len(ms) != len(All()) || !slices.EqualFunc(ms, All(), func(a, b Manifest) bool { return a.File == b.File && a.Name == b.Name }) {
		t.Error("Embedded's manifests are not All's")
	}
}

// F4 (L1159): what the engine still assumes of the set, by name (setup's
// locked rows and pins, doctor's remote probe), is checked: a set that
// breaks it is refused naming the file and the field, never a panic.
func TestAssumedRefusesAReshapedSet(t *testing.T) {
	const cg, c7, en, cn = "codegraph/manifest.yaml", "context7/manifest.yaml", "engram/manifest.yaml", "m365/manifest.yaml"
	for _, c := range []struct {
		name  string
		edits [][3]string
		want  string
	}{
		{"a core integration no longer required", [][3]string{{cg, "required: true\nwhen_off: refused", "required: false\nwhen_off: render"}},
			"integrations/codegraph/manifest.yaml: required: false, but setup shows codegraph as a locked row and pins its binary"},
		{"a core integration gone", [][3]string{{en, "", ""}},
			"integrations/engram/manifest.yaml: name: no manifest is named engram; setup's locked rows and pins read it by name"},
		{"a core integration without a pin", [][3]string{{cg, "  binary: codegraph\n  version: 1.6.2\n", ""}},
			"integrations/codegraph/manifest.yaml: doctor.version: missing; setup installs codegraph's binary at it"},
		{"a third required integration", [][3]string{{cn, "required: false\nwhen_off: launch", "required: true\nwhen_off: refused"}},
			"integrations/m365/manifest.yaml: required: setup shows a locked row only for engram and codegraph"},
		{"the remote integration gone", [][3]string{{c7, "", ""}},
			"integrations/context7/manifest.yaml: name: no manifest is named context7; doctor and the render read its url and setting by name"},
		{"the remote integration probed locally", [][3]string{{c7, "  probe: remote", "  probe: --version"}},
			"integrations/context7/manifest.yaml: doctor.probe: --version is not remote; doctor probes context7 at doctor.url"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("panic: %v", r)
					}
				}()
				ms, probs := Load(embeddedEdited(t, c.edits...))
				for _, p := range append(probs, Assumed(ms)...) {
					got = append(got, p.String())
				}
			}()
			if !slices.Contains(got, c.want) {
				t.Errorf("want %q among %q", c.want, got)
			}
		})
	}
	ms, probs := Load(embeddedEdited(t))
	if a := Assumed(ms); len(probs) > 0 || len(a) > 0 {
		t.Errorf("the compiled-in set: %v %v", probs, a)
	}
}
