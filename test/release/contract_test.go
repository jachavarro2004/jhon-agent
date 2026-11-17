// Tests for `jhon contract`: the version of the surface jhon-shell (shell/)
// consumes, which the shell checks before it lets a guard judge a call. The
// binary and the shell ship in one release, so they must name the same one.
package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestContract(t *testing.T) {
	t.Parallel()

	var pkg struct {
		Jhon struct {
			Contract string `json:"contract"`
		} `json:"jhon"`
	}
	b, err := os.ReadFile(filepath.Join(support.Root(), "shell", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		t.Fatal(err)
	}
	if pkg.Jhon.Contract == "" {
		t.Fatal("shell/package.json names no jhon.contract")
	}

	t.Run("jhon contract --json names the shell contract jhon-shell's package.json speaks", func(t *testing.T) {
		t.Parallel()
		r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "contract", "--json"}, Dir: temp(t), Env: support.Env("JHON_ROOT=" + support.Root())})
		if r.Code != 0 {
			t.Fatal(r.Stderr)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
			t.Fatal(err, r.Stdout)
		}
		equal(t, out, map[string]any{"shell": pkg.Jhon.Contract})
	})

	t.Run("jhon contract prints it as text, and refuses an unknown argument", func(t *testing.T) {
		t.Parallel()
		r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "contract"}, Dir: temp(t), Env: support.Env("JHON_ROOT=" + support.Root())})
		equal(t, r.Code, 0)
		equal(t, r.Stdout, "shell "+pkg.Jhon.Contract+"\n")
		bad := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "contract", "--bogus"}, Dir: temp(t), Env: support.Env("JHON_ROOT=" + support.Root())})
		equal(t, bad.Code, 2)
		match(t, bad.Stderr, `usage: jhon contract`)
	})
}
