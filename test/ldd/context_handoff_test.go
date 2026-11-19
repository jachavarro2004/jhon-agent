// T163 (L1421, L1430): `jhon context <change> --handoff` names why Engram
// could not be asked, and an Engram that finds nothing is no failure.
package ldd

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// engramScript is a fake engram that runs body.
func engramScript(t *testing.T, body string) string {
	t.Helper()
	bin := filepath.Join(temp(t), "engram")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestContextHandoffNamesEngramFailure(t *testing.T) {
	t.Parallel()
	root := support.GitRepo(t, temp(t))
	for _, tc := range []struct{ name, engram, note string }{
		{"exit 1", engramScript(t, "echo boom >&2; exit 1\n"), `^engram unavailable \(engram exited 1: boom\): search Engram`},
		{"missing", filepath.Join(root, "no-such-engram"), `^engram unavailable \(engram not found\): search Engram`},
	} {
		r := handoff(t, root, tc.engram, "c1", "--json")
		equal(t, r.Code, 0, tc.name, r.Stderr)
		equal(t, keys(t, []byte(r.Stdout)), append(slices.Clone(contextKeys), "risk", "decisions", "decisions_note"), tc.name)
		c := object(t, r.Stdout)
		v, ok := c["decisions"]
		equal(t, ok && v == nil, true, tc.name, "decisions is null")
		match(t, c["decisions_note"].(string), tc.note, tc.name)
	}
}

func TestContextHandoffNoMemories(t *testing.T) {
	t.Parallel()
	root := support.GitRepo(t, temp(t))
	r := handoff(t, root, engramScript(t, "echo 'No memories found for: \"c1\"'\n"), "c1", "--json")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, keys(t, []byte(r.Stdout)), append(slices.Clone(contextKeys), "risk", "decisions"))
	equal(t, object(t, r.Stdout)["decisions"], []any{})
}
