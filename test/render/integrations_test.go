package render

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T58a M4 (L1150): `jhon validate` refuses a malformed integration manifest
// (integrations/<name>/manifest.yaml, schema jhon.integration/v1), naming the
// file and the field.
func TestRenderValidateIntegrationManifests(t *testing.T) {
	t.Parallel()
	engram := filepath.Join("integrations", "engram", "manifest.yaml")
	for _, c := range []struct {
		name string
		edit func(t *testing.T, repo string)
		want string
	}{
		{"a missing field", func(t *testing.T, repo string) {
			editFile(t, filepath.Join(repo, engram), "display: Engram\n", "")
		}, `integration\s+integrations/engram/manifest.yaml  display: missing`},
		{"an unknown schema", func(t *testing.T, repo string) {
			editFile(t, filepath.Join(repo, engram), "schema: jhon.integration/v1", "schema: jhon.integration/v2")
		}, `integration\s+integrations/engram/manifest.yaml  schema: jhon.integration/v2 is not jhon.integration/v1`},
		{"a duplicate name", func(t *testing.T, repo string) {
			b, err := os.ReadFile(filepath.Join(repo, engram))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(repo, "integrations", "memory"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo, "integrations", "memory", "manifest.yaml"), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}, `integration\s+integrations/memory/manifest.yaml  name: engram is also the name of integrations/engram/manifest.yaml`},
		{"a capability agents.yaml does not declare", func(t *testing.T, repo string) {
			editFile(t, filepath.Join(repo, engram), "capabilities: [memory, memory-read, memory-session]", "capabilities: [memory, recall]")
		}, `integration\s+integrations/engram/manifest.yaml  capabilities: recall is not a capability of agents.yaml`},
		{"a capability holding another server's tool", func(t *testing.T, repo string) {
			editFile(t, filepath.Join(repo, "integrations", "codegraph", "manifest.yaml"), "  servers: [codegraph]", "  servers: [codemap]")
		}, `integration\s+integrations/codegraph/manifest.yaml  capabilities: codegraph holds mcp__codegraph__codegraph_explore, which is not a tool of claude-code.servers`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			repo := support.CopyRepo(t)
			c.edit(t, repo)
			r := validate(t, repo)
			wantCode(t, r, 1)
			match(t, r.Out(), c.want)
			if !strings.Contains(r.Out(), "validation failed") {
				t.Errorf("no verdict:\n%s", r.Out())
			}
		})
	}
	t.Run("the tree's own manifests pass", func(t *testing.T) {
		t.Parallel()
		r := validate(t, repoRoot)
		wantCode(t, r, 0)
		if regexp.MustCompile(`integration\s+integrations/`).MatchString(r.Out()) {
			t.Errorf("a problem in the tree's manifests:\n%s", r.Out())
		}
	})
}
