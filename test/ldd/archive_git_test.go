package ldd

// T250 B4 (the verify of 1459cc1, odd/probes250v2/tests.md): `jhon archive`
// moves a tracked change with git mv, which cleans the moved path through
// the index; jhon's own git runs no program the repository's configuration
// names (project.ReadGitCmd), whatever a sandboxed command wrote there.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestArchiveRunsNoProgramTheRepositoryConfigurationNames(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, hook string
		moves      bool
	}{
		// A configured hook as git takes one: it is turned off for the
		// event, and the archive moves the change.
		{"a named hook", "p", true},
		// The verify's plant, a hook named as its event: with jhon's switch
		// for the event git refuses the configuration (the name collides
		// with an event), so nothing runs and nothing moves.
		{"a hook named as its event", "post-index-change", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := archiveProject(t)
			commitSource(t, root, archiveSource)
			markers := t.TempDir()
			plant := func(name string) string {
				p := filepath.Join(markers, name+".sh")
				if err := os.WriteFile(p, []byte("#!/bin/sh\ntouch '"+filepath.Join(markers, name)+"'\ncat\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				return p
			}
			support.Write(t, root, map[string]string{".gitattributes": "*.md filter=x\n"})
			for _, kv := range [][2]string{
				{"core.fsmonitor", plant("fsmonitor")},
				{"filter.x.clean", plant("clean")},
				{"filter.x.smudge", plant("smudge")},
				{"hook." + c.hook + ".command", plant("hook")},
				{"hook." + c.hook + ".event", "post-index-change"},
			} {
				support.Git(t, root, "config", kv[0], kv[1])
			}
			r := archiveMove(t, root, archiveChange, archiveDate)
			for _, m := range []string{"fsmonitor", "clean", "smudge", "hook"} {
				if exists(filepath.Join(markers, m)) {
					t.Errorf("jhon archive ran the repository's %s (exit %d, %s)", m, r.Code, r.Out())
				}
			}
			if c.moves {
				equal(t, r.Code, 0, r.Out())
				match(t, r.Stdout, `(?m)^method: git-mv$`)
				assertArchived(t, root)
			}
		})
	}
}
