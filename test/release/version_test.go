// Tests for `jhon version`: the version stamped into the binary at build
// time (-ldflags -X on engine/internal/version), the one `jhon update` and
// the release CI compare against. A plain build is 0.0.0-dev, unstamped.
package release

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const versionPkg = "github.com/jachavarro2004/jhon-agent/engine/internal/version"

// stamped builds the binary with Version, Commit and Date set, as the
// release CI does.
func stamped(t *testing.T, version, commit, date string) string {
	t.Helper()
	bin := filepath.Join(temp(t), "jhon")
	flags := "-X " + versionPkg + ".Version=" + version + " -X " + versionPkg + ".Commit=" + commit + " -X " + versionPkg + ".Date=" + date
	c := exec.Command("go", "build", "-trimpath", "-ldflags", flags, "-o", bin, "./engine/cmd/jhon")
	c.Dir = support.Root()
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	return bin
}

func TestVersion(t *testing.T) {
	t.Parallel()
	const commit = "0123456789abcdef0123456789abcdef01234567"
	const date = "2026-11-06T23:40:39-05:00"

	t.Run("jhon version prints the stamped version, its commit's first 12 characters, and its date; --json all three", func(t *testing.T) {
		t.Parallel()
		bin := stamped(t, "0.1.0", commit, date)
		for _, args := range [][]string{{"version"}, {"--version"}} {
			r := support.Run(t, support.Cmd{Args: append([]string{bin}, args...), Dir: temp(t), Env: support.Env()})
			equal(t, r.Code, 0)
			equal(t, r.Stdout, "jhon 0.1.0 (0123456789ab, "+date+")\n")
			equal(t, r.Stderr, "")
		}
		r := support.Run(t, support.Cmd{Args: []string{bin, "version", "--json"}, Dir: temp(t), Env: support.Env()})
		equal(t, r.Code, 0)
		var out map[string]any
		if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
			t.Fatal(err, r.Stdout)
		}
		equal(t, out, map[string]any{"version": "0.1.0", "commit": commit, "date": date})
	})

	t.Run("an unstamped build is jhon 0.0.0-dev, with no commit and no date", func(t *testing.T) {
		t.Parallel()
		r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "version"}, Dir: temp(t), Env: support.Env()})
		equal(t, r.Code, 0)
		equal(t, r.Stdout, "jhon 0.0.0-dev\n")
		j := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "version", "--json"}, Dir: temp(t), Env: support.Env()})
		var out map[string]any
		if err := json.Unmarshal([]byte(j.Stdout), &out); err != nil {
			t.Fatal(err, j.Stdout)
		}
		equal(t, out, map[string]any{"version": "0.0.0-dev", "commit": "", "date": ""})
	})

	t.Run("jhon version refuses any other argument with exit 2 and its usage", func(t *testing.T) {
		t.Parallel()
		r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "version", "--bogus"}, Dir: temp(t), Env: support.Env()})
		equal(t, r.Code, 2)
		equal(t, r.Stdout, "")
		equal(t, r.Stderr, "jhon version: unknown argument: --bogus\nusage: jhon version [--json]\n")
	})

	t.Run("jhon version extra exits 2 with exactly its usage", func(t *testing.T) {
		t.Parallel()
		r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "version", "extra"}, Dir: temp(t), Env: support.Env()})
		equal(t, r.Code, 2)
		equal(t, r.Stdout, "")
		equal(t, r.Stderr, "jhon version: unknown argument: extra\nusage: jhon version [--json]\n")
	})

	t.Run("jhon --help lists jhon version", func(t *testing.T) {
		t.Parallel()
		r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "--help"}, Dir: temp(t), Env: support.Env()})
		match(t, r.Stdout, `jhon version \[--json\]`)
	})
}
