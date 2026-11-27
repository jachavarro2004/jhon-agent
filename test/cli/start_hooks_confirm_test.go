package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T266 confirmation (L1769), B1: the unquoted, whole-quoted and interpreter forms.
func TestStartCountsUnquotedAndInterpreterForms(t *testing.T) {
	t.Parallel()
	for _, command := range []string{`$CLAUDE_PROJECT_DIR/tools/guard.sh`, `${CLAUDE_PROJECT_DIR}/tools/guard.sh`,
		`"$CLAUDE_PROJECT_DIR/tools/guard.sh"`, `node "$CLAUDE_PROJECT_DIR/tools/guard.sh"`} {
		m := newMachine(t)
		q, _ := jsonString(command)
		support.Write(t, m.proj, map[string]string{
			".claude/settings.json": `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":` + q + `}]}]}}`,
			"tools/guard.sh":        "#!/bin/sh\nexit 0\n"})
		commit(t, m.proj, "hook")
		approved(t, command, m.try(t, m.proj, "--approve-hooks"))
		support.Write(t, m.proj, map[string]string{"tools/guard.sh": "#!/bin/sh\necho x\n"})
		asked(t, command, m.try(t, m.proj), "changed tools/guard.sh")
	}
}

// sameSize rewrites path's first byte and puts its mtime back.
func sameSize(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("z"), 0); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := os.Chtimes(path, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
}

// B3: past the count cap and the 16 MiB total, a same-size edit with its mtime restored asks.
func TestStartCatchesASameSizeEditPastTheCountAndTotalCaps(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	files := map[string]string{}
	for i := 0; i < 1050; i++ {
		files[fmt.Sprintf(".pi/extensions/many/f%05d.js", i)] = "aaaa\n"
	}
	support.Write(t, m.proj, files)
	approved(t, "count cap", m.try(t, m.proj, "--approve-hooks"))
	sameSize(t, filepath.Join(m.proj, ".pi/extensions/many/f01040.js"))
	asked(t, "past the count cap", m.try(t, m.proj), "changed (files past the first 1000)")

	n := newMachine(t)
	big := strings.Repeat("a", 900_000)
	files = map[string]string{}
	for i := 0; i < 20; i++ {
		files[fmt.Sprintf(".claude/hooks/f%02d.js", i)] = big
	}
	support.Write(t, n.proj, files)
	approved(t, "total cap", n.try(t, n.proj, "--approve-hooks"))
	sameSize(t, filepath.Join(n.proj, ".claude/hooks/f19.js"))
	asked(t, "past the total cap", n.try(t, n.proj), "changed .claude/hooks/f19.js")
}
