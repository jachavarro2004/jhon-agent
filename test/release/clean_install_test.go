// The clean install of a release (T30 D): pack an artifact from a scratch
// checkout, install it in a fresh HOME the way install.sh does (it hands the
// tarball to the release's own `jhon release install`; install.sh itself only
// downloads from GitHub, so the test calls that step directly), then check
// what a new user sees first.
package release

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// cleanInstallTimeout bounds every command of the clean install.
const cleanInstallTimeout = 10 * time.Minute

func TestCleanInstall(t *testing.T) {
	for _, tool := range []string{"git", "go", "tar", "gzip"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("the clean install needs %s on PATH: %v", tool, err)
		}
	}
	parallel(t)
	src := scratchSource(t)
	out := temp(t)
	env := scratchHome(t)
	// pi is found by its directory, as on a machine that has it.
	support.Write(t, env.home, map[string]string{".pi/.keep": ""})
	xdg := []string{
		"XDG_CONFIG_HOME=" + filepath.Join(env.home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(env.home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(env.home, ".local", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(env.home, ".cache"),
		"TMPDIR=" + temp(t),
	}
	bounded := func(t *testing.T, dir, cmd string, args ...string) support.Result {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), cleanInstallTimeout)
		defer cancel()
		overrides := append([]string{"HOME=" + env.home, "PATH=" + env.bin + ":" + os.Getenv("PATH"), "JHON_SYNC_GO_TEST=0"}, offlineGo(t)...)
		c := exec.CommandContext(ctx, cmd, args...)
		c.Dir = dir
		// A user's shell names no checkout or settings file of the developer's.
		for _, e := range support.Env(merge(overrides, xdg)...) {
			if k, _, _ := strings.Cut(e, "="); k != "JHON_ROOT" && k != "JHON_SETTINGS" {
				c.Env = append(c.Env, e)
			}
		}
		var stdout, stderr bytes.Buffer
		c.Stdout, c.Stderr = &stdout, &stderr
		err := c.Run()
		if ctx.Err() != nil {
			t.Fatalf("%s %v: no answer in %s", cmd, args, cleanInstallTimeout)
		}
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			t.Fatalf("run %s %v: %v", cmd, args, err)
		}
		code := 0
		if exit != nil {
			code = exit.ExitCode()
		}
		return support.Result{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}
	}

	wantCode(t, bounded(t, src, cliOf(src), "release", "pack", "--version", "0.2.0", "--out", out), 0)
	artifact := filepath.Join(out, artifactName("0.2.0"))
	unpacked := temp(t)
	sh(t, unpacked, "tar", "-xzf", artifact, "build/jhon")
	wantCode(t, bounded(t, env.home, filepath.Join(unpacked, "build", "jhon"), "release", "install", artifact), 0)
	jhon := filepath.Join(env.home, ".local", "bin", "jhon")

	t.Run("jhon --version says 0.2.0", func(t *testing.T) {
		v := bounded(t, env.home, jhon, "--version")
		wantCode(t, v, 0)
		match(t, v.Stdout, `^jhon 0\.2\.0 \(`)
	})

	t.Run("jhon doctor is healthy and its validation passes", func(t *testing.T) {
		d := bounded(t, env.home, jhon, "doctor")
		wantCode(t, d, 0)
		match(t, d.Stdout, `(?m)^  ok    validation: ok`)
		match(t, d.Stdout, `(?m)^healthy$`)
	})

	t.Run("jhon settings list names no herdr or vim key", func(t *testing.T) {
		s := bounded(t, env.home, jhon, "settings", "list")
		wantCode(t, s, 0)
		noMatch(t, s.Out(), `(?i)herdr|vim`)
	})

	// validate is the maintainer's and needs the checkout; a user's release
	// is validated by doctor (above), and validate points to jhon update.
	t.Run("jhon validate on a user install points to jhon update", func(t *testing.T) {
		v := bounded(t, env.home, jhon, "validate")
		wantCode(t, v, 1)
		match(t, v.Stderr, `validate needs the Jhon source checkout.*run 'jhon update'`)
	})

	t.Run("the pi profile is rendered under the scratch HOME", func(t *testing.T) {
		for _, f := range []string{"settings.json", "jhon.json"} {
			equal(t, exists(filepath.Join(env.home, ".pi", "jhon", f)), true, f)
		}
		if strings.Contains(support.Read(t, filepath.Join(env.home, ".pi", "jhon", "jhon.json")), src) {
			t.Fatal("jhon.json names the source checkout")
		}
	})
}
