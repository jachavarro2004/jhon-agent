package render

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
)

// The README's "What each runtime guarantees" table counts agents.yaml's
// `guarantees` per runtime and level (T56): a guarantee that moves moves the table.
func TestReadmeGuaranteeCountsMatchAgentsYaml(t *testing.T) {
	m := manifest.MustLoad(repo).Matrix
	for _, file := range []string{"README.md"} {
		b, err := os.ReadFile(filepath.Join(repo, file))
		if err != nil {
			t.Fatal(err)
		}
		guaranteeRows(t, m, file, string(b))
	}
}

func guaranteeRows(t *testing.T, m manifest.Matrix, file, readme string) {
	t.Helper()
	for rt, name := range map[string]string{"claude-code": "Claude Code", "pi": "pi"} {
		n := map[string]int{}
		for _, anchor := range m.Guarantees.Keys() {
			entry, _ := m.Guarantees.Get(anchor)
			level, _ := entry.Get(rt)
			n[level]++
		}
		row := fmt.Sprintf("| %s | %d | %d | %d |", name, n["enforced"], n["behavioural"], n["unsupported"])
		if !strings.Contains(readme, row+"\n") {
			t.Errorf("%s lacks the row %q", file, row)
		}
	}
}
