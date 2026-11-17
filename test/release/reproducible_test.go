// Tests for reproducible releases (T52): the same commit packs to the same
// bytes, and tools/release/repro.sh, the release workflow's check, says so
// and fails when an artifact differs.
package release

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestReleasePackIsReproducible(t *testing.T) {
	parallel(t)
	src := scratchSource(t)
	out := temp(t)
	packer := scratchHome(t)
	wantCode(t, run(t, packer, cliOf(src), []string{"release", "pack", "--version", "0.1.0", "--out", out}), 0)
	script := filepath.Join(support.Root(), "tools", "release", "repro.sh")
	repro := func() support.Result {
		t.Helper()
		env := []string{"HOME=" + packer.home, "PATH=" + packer.bin + ":" + os.Getenv("PATH"), "XDG_STATE_HOME=" + filepath.Join(packer.home, ".local", "state")}
		return support.Run(t, support.Cmd{Args: []string{script, cliOf(src), "0.1.0", out}, Dir: src, Env: support.Env(merge(env, offlineGo(t))...)})
	}

	r := repro()
	wantCode(t, r, 0)
	match(t, r.Stdout, `(?m)^reproducible: `+regexp.QuoteMeta(artifactName("0.1.0"))+` [0-9a-f]{64}$`)

	// A reference that differs by one byte is reported, and the check fails.
	tarball := filepath.Join(out, artifactName("0.1.0"))
	b, err := os.ReadFile(tarball)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-1] ^= 0xff
	if err := os.WriteFile(tarball, b, 0o644); err != nil {
		t.Fatal(err)
	}
	r = repro()
	wantFail(t, r)
	match(t, r.Stdout, `(?m)^::error::`+regexp.QuoteMeta(artifactName("0.1.0"))+` is not reproducible: `)
}
