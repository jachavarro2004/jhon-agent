package sandbox

// Verifier probes for sandbox-deploy-tools S10 (T5, verify mwquuljq-c-arku):
// every folder the mount-point exemption keeps is a read-only mount point in
// the real run, and no rename, exchange or nested namespace moves it; the
// cases main left open refuse, the safe ones run.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestAKeptFolderIsAReadOnlyMountPointNoCommandMoves(t *testing.T) {
	b := newBox(t)
	aw := func(dir, allowed string) {
		support.Write(t, dir, map[string]string{"settings.toml": "schema = 1\n\n[sandbox]\nallow_write = [\"" + allowed + "\"]\n"})
	}
	for _, c := range []struct {
		name, kept, parent string
		env                []string
	}{
		{"settings under allow_write ~/.config", filepath.Join(b.home, ".config", "jhon"), filepath.Join(b.home, ".config"), nil},
		{"XDG_CONFIG_HOME is $TMPDIR", filepath.Join(b.tmp, "jhon"), b.tmp, []string{"XDG_CONFIG_HOME=" + b.tmp}},
		{"XDG_STATE_HOME is the project", filepath.Join(b.proj, "jhon"), b.proj, []string{"XDG_STATE_HOME=" + b.proj}},
	} {
		t.Run(c.name, func(t *testing.T) {
			os.RemoveAll(filepath.Join(b.home, ".config"))
			if err := os.MkdirAll(c.kept, 0o755); err != nil {
				t.Fatal(err)
			}
			aw(filepath.Join(b.home, ".config", "jhon"), filepath.Join(b.home, ".config"))
			env := append(append([]string{}, b.env...), c.env...)
			r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "cat", "/proc/self/mountinfo")
			if r.Code != 0 || !strings.Contains(r.Stdout, " "+c.kept+" ro,") {
				t.Fatalf("mountinfo: exit %d, stderr %q; want %s mounted read-only", r.Code, r.Stderr, c.kept)
			}
			script := `mkdir -p "$2/new" && { mv "$1" "$1.old"; mv -T "$2/new" "$1"; rmdir "$1"; ` +
				`python3 -I -c 'import ctypes,sys; l=ctypes.CDLL(None); sys.exit(0 if l.renameat2(-100,sys.argv[1].encode(),-100,sys.argv[2].encode(),2)==0 else 1)' "$2/new" "$1"; ` +
				`python3 -I -c 'import ctypes,os,sys; os.unshare(os.CLONE_NEWUSER|os.CLONE_NEWNS); l=ctypes.CDLL(None); l.umount2(sys.argv[1].encode(),2); os.rename(sys.argv[1], sys.argv[1]+".n")' "$1"; true; }`
			support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", script, "sh", c.kept, c.parent)
			for _, moved := range []string{c.kept + ".old", c.kept + ".n"} {
				if exists(moved) {
					t.Errorf("%s was moved to %s", c.kept, moved)
				}
			}
			if exists(filepath.Join(c.kept, "new")) || !exists(c.kept) {
				t.Errorf("%s was replaced or removed", c.kept)
			}
		})
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Log("python3 missing: the renameat2 and nested-namespace probes did not run")
	}
}

func TestTheDefaultsAWritableFolderCouldMoveStopTheRun(t *testing.T) {
	for _, c := range []struct {
		name, allowed string
		link, stops   bool
	}{
		{"allow_write ~/.local: the state's plain parent", ".local", false, true},
		{"allow_write ~/.local/state: the state folder a mount point", ".local/state", false, false},
		{"allow_write ~/.config, ~/.config/jhon a link", ".config", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newBox(t)
			dir := filepath.Join(b.home, ".config", "jhon")
			if c.link {
				dir = filepath.Join(b.home, "dotfiles", "jhon")
			}
			support.Write(t, dir, map[string]string{"settings.toml": "schema = 1\n\n[sandbox]\nallow_write = [\"" + filepath.Join(b.home, c.allowed) + "\"]\n"})
			if c.link {
				if err := os.MkdirAll(filepath.Join(b.home, ".config"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(dir, filepath.Join(b.home, ".config", "jhon")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Join(b.home, ".local", "state", "jhon"), 0o755); err != nil {
				t.Fatal(err)
			}
			r := b.sh(t, b.proj, "", "echo done")
			if c.stops && (r.Code != 126 || r.Stdout != "") || !c.stops && (r.Code != 0 || r.Stdout != "done\n") {
				t.Errorf("exit %d, stdout %q, stderr %q; want a refusal: %v", r.Code, r.Stdout, r.Stderr, c.stops)
			}
		})
	}
}

func TestAJhonSettingsFileInTheKeptFolderRuns(t *testing.T) {
	b := newBox(t)
	file := b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~/.config\"]\n")
	env := append(append([]string{}, b.env...), "JHON_SETTINGS="+file)
	r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `echo x > "$JHON_SETTINGS" || mv "$(dirname "$JHON_SETTINGS")" "$HOME/.config/moved"`)
	if r.Code == 0 || r.Code == 126 || exists(filepath.Join(b.home, ".config", "moved")) {
		t.Errorf("exit %d, stderr %q; want the program run and failing", r.Code, r.Stderr)
	}
	if g := support.Jhon(t, b.proj, env, "settings", "get", "sandbox.mode"); g.Stdout != "on\n" {
		t.Errorf("sandbox.mode %q, want on", g.Stdout)
	}
}

func TestXDGThroughProcOrDevShmStopsTheRun(t *testing.T) {
	b := newBox(t)
	link := filepath.Join(b.home, "plink")
	if err := os.Symlink("/proc/self/cwd", link); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"XDG_CONFIG_HOME=" + link, "XDG_STATE_HOME=" + link, "XDG_CONFIG_HOME=/proc/self/cwd", "XDG_STATE_HOME=/dev/shm/jhon-verify-missing"} {
		env := append(append([]string{}, b.env...), v)
		if r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "true"); r.Code != 126 || !strings.HasPrefix(r.Stderr, "jhon sandbox: ") {
			t.Errorf("%s: exit %d, stderr %q; want 126 and the refusal", v, r.Code, r.Stderr)
		}
	}
}
