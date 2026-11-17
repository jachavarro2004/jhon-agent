package discover

import (
	"reflect"
	"strings"
	"testing"
)

// workspace holds three projects: `go` testable by discovery, `py` unknown
// (source, no command), and an exempt uv workspace root.
var workspace = map[string]string{
	"pyproject.toml":    "[project]\nname = \"root\"\n\n[tool.uv.workspace]\nmembers = [\"go\", \"py\"]\n",
	"go/go.mod":         "module example.com/go\n",
	"py/pyproject.toml": "[project]\nname = \"py\"\n",
	"py/app.py":         "print('x')\n",
}

func resolved(t *testing.T, config string) ResolvedProjects {
	files := workspace
	if config != "" {
		files = with(workspace, map[string]string{"openspec/config.yaml": config})
	}
	return ResolveProjects(Discover(tree(t, files)), config)
}

func by(r ResolvedProjects, path string) ResolvedProject {
	for _, p := range r.Projects {
		if p.Path == path {
			return p
		}
	}
	return ResolvedProject{}
}

func row(p ResolvedProject) [3]any {
	return [3]any{p.Testability, deref(p.Command), deref(p.CommandSource)}
}

func TestResolveWithoutOverrides(t *testing.T) {
	r := resolved(t, "")
	var states [][2]string
	for _, p := range r.Projects {
		states = append(states, [2]string{p.Path, p.Testability})
	}
	if !reflect.DeepEqual(states, [][2]string{{".", Exempt()}, {"go", Testable()}, {"py", Unknown()}}) {
		t.Errorf("testability %q", states)
	}
	if row(by(r, "go")) != [3]any{Testable(), "go test ./...", Discovered()} || row(by(r, "py")) != [3]any{Unknown(), nil, nil} {
		t.Errorf("go %v, py %v", row(by(r, "go")), row(by(r, "py")))
	}
	if r.Notes == nil || len(r.Notes) != 0 {
		t.Errorf("notes %#v", r.Notes)
	}
}

func TestResolveOverrides(t *testing.T) {
	r := resolved(t, "test_command_override:\n  py: make test-py\n")
	if row(by(r, "py")) != [3]any{Testable(), "make test-py", Override()} || by(r, "py").TestabilityReason != "test_command_override make test-py" {
		t.Errorf("an override on an unknown project: %+v", by(r, "py"))
	}
	r = resolved(t, "test_command_override:\n  \"go\": 'go test -race ./...'  # stated by the user\n")
	if row(by(r, "go")) != [3]any{Testable(), "go test -race ./...", Override()} {
		t.Errorf("an override replaces a discovered command: %+v", by(r, "go"))
	}
	r = resolved(t, "test_command_override:\n  ghost: true\n  go: echo ok\n")
	if deref(by(r, "go").Command) != "echo ok" || len(r.Notes) != 1 ||
		r.Notes[0] != "openspec/config.yaml sets test_command_override for 'ghost', which is not a discovered project; the entry is ignored" {
		t.Errorf("an override for a path discovery did not find: %q", r.Notes)
	}
	r = resolved(t, "test_command_override:\n  \".\": true\n")
	if row(by(r, ".")) != [3]any{Exempt(), nil, nil} || !strings.Contains(strings.Join(r.Notes, "\n"), "'.', which is structurally_exempt") {
		t.Errorf("a structurally exempt project stays exempt: %+v %q", by(r, "."), r.Notes)
	}
}

func TestProjectsFor(t *testing.T) {
	root := tree(t, with(workspace, map[string]string{"openspec/config.yaml": "test_command_override:\n  py: exit 0\n"}))
	r, err := ProjectsFor(root)
	if err != nil || deref(by(r, "py").Command) != "exit 0" {
		t.Errorf("ProjectsFor reads openspec/config.yaml: %+v, %v", by(r, "py"), err)
	}
}

func TestConfigMap(t *testing.T) {
	config := "schema: spec-driven\r\ntest_command_override:  # per project\r\n  # a comment\r\n\r\n  \".\": \"npm test\"\r\n  'a b': x: y  # note\r\n  c:no-space\r\n  a b: second\r\n  \".\": last\r\nnext: 1\r\n  d: after\r\n"
	m := ConfigMap(config, "test_command_override")
	if m == nil || !reflect.DeepEqual(m.Keys, []string{".", "a b"}) || m.Values["."] != "last" || m.Values["a b"] != "second" {
		t.Errorf("ConfigMap = %+v", m)
	}
	if ConfigMap(config, "absent") != nil || ConfigMap("", "test_command_override") != nil {
		t.Error("an absent key is nil")
	}
	if m := ConfigMap("k:\n  x: 'it''s'\n", "k"); m.Values["x"] != "it''s" {
		t.Errorf("only one pair of surrounding quotes is removed: %q", m.Values["x"])
	}
}
