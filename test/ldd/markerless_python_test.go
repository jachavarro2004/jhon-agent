package ldd

import (
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T64 B (L501): a Python repository with unittest files and no marker
// (no pyproject.toml) is the root project, `unknown` until /jhon-ldd init
// writes the command it verified as test_command_override for "."; then
// `jhon executions` runs it. A repository where discovery already finds a
// project, OpenSpec or not, keeps its projects as they are.

// markerless is a committed Python repository with no marker, its tests
// at the root, and config as its layout's config when not empty.
func markerless(t *testing.T, config map[string]string) string {
	t.Helper()
	root := support.GitRepo(t, temp(t))
	files := map[string]string{
		"accounts.py":      "def load():\n    return []\n",
		"test_accounts.py": "import unittest\n",
	}
	for k, v := range config {
		files[k] = v
	}
	support.Write(t, root, files)
	commit(t, root, "base")
	return root
}

func rootProject(t *testing.T, dir string) map[string]any {
	t.Helper()
	r := jhonGit(t, dir, "init", "--json")
	equal(t, r.Code, 0, r.Stderr)
	projects, _ := object(t, r.Stdout)["projects"].([]any)
	if len(projects) != 1 {
		t.Fatalf("projects %v, want the root alone:\n%s", projects, r.Stdout)
	}
	return projects[0].(map[string]any)
}

func TestMarkerlessPythonIsTheRootProject(t *testing.T) {
	t.Parallel()
	p := rootProject(t, markerless(t, nil))
	equal(t, p["path"], ".")
	equal(t, p["stacks"], []any{"Python"})
	equal(t, p["markers"], []any{})
	equal(t, p["test_command"], nil, "no command is guessed")
	equal(t, p["testability"], "unknown")
	equal(t, p["testability_reason"], "no test command, but accounts.py is Python source (*.py)")
}

func TestMarkerlessPythonRunsTheInitOverride(t *testing.T) {
	t.Parallel()
	root := markerless(t, map[string]string{"ldd/config.yaml": "test_command_override:\n  \".\": python3 -m unittest\n"})
	support.Git(t, root, "checkout", "-q", "-b", "fix/rows")
	support.Write(t, root, map[string]string{"accounts.py": "def load():\n    return [1]\n"})
	p := rootProject(t, root)
	equal(t, p["test_command"], nil)
	r := jhonGit(t, root, "status", "--json")
	equal(t, r.Code, 0, r.Stderr)
	projects, _ := object(t, r.Stdout)["projects"].([]any)
	if len(projects) != 1 {
		t.Fatalf("status projects %v", projects)
	}
	resolved := projects[0].(map[string]any)
	equal(t, resolved["testability"], "testable")
	equal(t, resolved["command"], "python3 -m unittest")
	dry := jhonGit(t, root, "executions", "run", "fix-rows", "--dry-run")
	equal(t, dry.Code, 0, dry.Stderr)
	match(t, dry.Stdout, `python3 -m unittest`)
	equal(t, exists(filepath.Join(root, ".git/jhon/changes/fix-rows/execution-record.json")), false, "nothing was written")
}

// Owner (T64): the marker-less path never changes a project discovery
// already finds, and an OpenSpec repository keeps openspec/ as its layout.
func TestMarkerlessPythonLeavesFoundProjects(t *testing.T) {
	t.Parallel()
	root := markerless(t, map[string]string{
		"svc/go.mod":           "module example.com/svc\n",
		"openspec/config.yaml": "test_command_override:\n  svc: go test ./...\n",
	})
	r := jhonGit(t, root, "init", "--json")
	equal(t, r.Code, 0, r.Stderr)
	d := object(t, r.Stdout)
	projects, _ := d["projects"].([]any)
	if len(projects) != 1 || projects[0].(map[string]any)["path"] != "svc" {
		t.Fatalf("projects %v, want svc alone", projects)
	}
	equal(t, d["openspec_config"], map[string]any{"path": "openspec/config.yaml", "exists": true})
	equal(t, exists(filepath.Join(root, "ldd")), false, "jhon init created ldd/ beside openspec/")
}

func TestMarkerlessPythonOpenSpecKeepsItsLayout(t *testing.T) {
	t.Parallel()
	root := markerless(t, map[string]string{"openspec/config.yaml": "test_command_override:\n  \".\": python3 -m unittest\n"})
	r := jhonGit(t, root, "init", "--json")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, object(t, r.Stdout)["openspec_config"], map[string]any{"path": "openspec/config.yaml", "exists": true})
	equal(t, exists(filepath.Join(root, "ldd")), false, "jhon init created ldd/ beside openspec/")
}
