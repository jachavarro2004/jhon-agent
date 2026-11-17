package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// TestTarballIsReproducible: the same tree packed twice is the same bytes,
// whatever the files' mtimes, owners or group-write bits on disk (T52).
func TestTarballIsReproducible(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "build"), 0o755))
	must(t, os.MkdirAll(filepath.Join(dir, "shell", "lib"), 0o775))
	must(t, os.WriteFile(filepath.Join(dir, "build", "jhon"), []byte("#!/bin/sh\n"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "shell", "lib", "a.ts"), []byte("a\n"), 0o664))
	must(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("r\n"), 0o644))
	must(t, os.Symlink("lib/a.ts", filepath.Join(dir, "shell", "link.ts")))
	epoch := time.Date(2026, 11, 6, 23, 40, 39, 0, time.UTC)

	pack := func(name string) []byte {
		t.Helper()
		dst := filepath.Join(t.TempDir(), name)
		must(t, tarball(dir, dst, epoch))
		b, err := os.ReadFile(dst)
		must(t, err)
		return b
	}
	first := pack("a.tar.gz")
	// Another day's mtimes and a looser mode on disk: nothing in the archive changes.
	later := time.Now().Add(time.Hour)
	for _, p := range []string{"build", "build/jhon", "shell", "shell/lib", "shell/lib/a.ts", "README.md", "."} {
		must(t, os.Chtimes(filepath.Join(dir, p), later, later))
	}
	must(t, os.Chmod(filepath.Join(dir, "README.md"), 0o664))
	if second := pack("b.tar.gz"); !bytes.Equal(first, second) {
		t.Fatal("packing the same tree twice gave different bytes")
	}

	zr, err := gzip.NewReader(bytes.NewReader(first))
	must(t, err)
	if zr.Name != "" || !zr.ModTime.IsZero() {
		t.Errorf("gzip header carries name %q, mtime %v; want none", zr.Name, zr.ModTime)
	}
	tr := tar.NewReader(zr)
	var names []string
	modes := map[string]int64{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		must(t, err)
		names = append(names, h.Name)
		modes[h.Name] = h.Mode
		if h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" {
			t.Errorf("%s: owner %d:%d %q:%q; want 0:0 and no names", h.Name, h.Uid, h.Gid, h.Uname, h.Gname)
		}
		if !h.ModTime.Equal(epoch) {
			t.Errorf("%s: mtime %v; want the commit's %v", h.Name, h.ModTime, epoch)
		}
		if h.Format != tar.FormatGNU || len(h.PAXRecords) != 0 {
			t.Errorf("%s: format %v with PAX records %v; want GNU and no extended headers", h.Name, h.Format, h.PAXRecords)
		}
	}
	want := []string{"README.md", "build/", "build/jhon", "shell/", "shell/lib/", "shell/lib/a.ts", "shell/link.ts"}
	if !slices.Equal(names, want) {
		t.Errorf("entries %q; want %q, sorted", names, want)
	}
	for name, mode := range map[string]int64{"README.md": 0o644, "build/": 0o755, "build/jhon": 0o755, "shell/lib/": 0o755, "shell/lib/a.ts": 0o644, "shell/link.ts": 0o777} {
		if modes[name] != mode {
			t.Errorf("%s: mode %o; want %o", name, modes[name], mode)
		}
	}
}

func TestBuildArgsAreReproducible(t *testing.T) {
	got := buildArgs("out", "-s -w")
	want := []string{"build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w", "-o", "out", "./engine/cmd/jhon"}
	if !slices.Equal(got, want) {
		t.Errorf("buildArgs = %q; want %q", got, want)
	}
}

// TestTarballUnpacksWithSystemTar: the archive InstallArtifact unpacks with
// the system tar keeps symlinks, empty directories and executables; modes
// are normalised to 0755 and 0644 (wave 1 verify).
func TestTarballUnpacksWithSystemTar(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("no system tar")
	}
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "sub", "empty"), 0o700))
	must(t, os.WriteFile(filepath.Join(dir, "sub", "run"), []byte("#!/bin/sh\necho ran\n"), 0o700))
	must(t, os.WriteFile(filepath.Join(dir, "secret"), []byte("s\n"), 0o600))
	must(t, os.Symlink("sub/run", filepath.Join(dir, "filelink")))
	must(t, os.Symlink("sub", filepath.Join(dir, "dirlink")))
	dst := filepath.Join(t.TempDir(), "a.tar.gz")
	must(t, tarball(dir, dst, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)))
	un := t.TempDir()
	if out, err := exec.Command("tar", "-xzf", dst, "-C", un).CombinedOutput(); err != nil {
		t.Fatalf("tar -xzf: %v\n%s", err, out)
	}
	mode := func(p string) os.FileMode {
		t.Helper()
		fi, err := os.Lstat(filepath.Join(un, p))
		must(t, err)
		return fi.Mode()
	}
	for p, want := range map[string]os.FileMode{"sub": os.ModeDir | 0o755, "sub/empty": os.ModeDir | 0o755, "sub/run": 0o755, "secret": 0o644} {
		if got := mode(p); got != want {
			t.Errorf("%s: mode %v, want %v", p, got, want)
		}
	}
	for link, target := range map[string]string{"filelink": "sub/run", "dirlink": "sub"} {
		if got, err := os.Readlink(filepath.Join(un, link)); err != nil || got != target {
			t.Errorf("%s -> %q (%v), want %q", link, got, err, target)
		}
	}
	if out, err := exec.Command(filepath.Join(un, "filelink")).Output(); err != nil || string(out) != "ran\n" {
		t.Errorf("the unpacked executable: %q %v", out, err)
	}
}
