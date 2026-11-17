// Conformance test for the resolved projects (docket M, decision 1) as
// `jhon status --json` exposes them: the live discovery of `jhon init` with
// `test_command_override` from openspec/config.yaml applied. The resolution
// itself is unit-tested in internal/discover.
package ldd

import "testing"

func TestStatusResolvedProjects(t *testing.T) {
	t.Parallel()
	// contracts/vocabulary.yaml: testability_state and command_source.
	const testable, exempt, discovered, override = "testable", "structurally_exempt", "discovered", "override"
	// Three projects: `go` testable by discovery, `py` unknown (source, no
	// command), and an exempt uv workspace root.
	root := tree(t, map[string]string{
		"pyproject.toml":       "[project]\nname = \"root\"\n\n[tool.uv.workspace]\nmembers = [\"go\", \"py\"]\n",
		"go/go.mod":            "module example.com/go\n",
		"py/pyproject.toml":    "[project]\nname = \"py\"\n",
		"py/app.py":            "print('x')\n",
		"openspec/config.yaml": "test_command_override:\n  py: exit 0\n",
	})
	r := jhonPlain(t, root, "status", "--json")
	var got [][]any
	for _, p := range object(t, r.Stdout)["projects"].([]any) {
		m := p.(map[string]any)
		got = append(got, []any{m["path"], m["testability"], m["command"], m["command_source"]})
	}
	equal(t, got, [][]any{
		{".", exempt, nil, nil},
		{"go", testable, "go test ./...", discovered},
		{"py", testable, "exit 0", override},
	}, r.Stderr)
}
