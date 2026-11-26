package release

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The release artifact (T3): the two halves `jhon sync` composes, apart, so
// an end user installs a release with no checkout and no toolchain.
//
// Pack builds, from the committed HEAD of a checkout, what a synced release
// directory holds (the archived HEAD and build/jhon stamped with the
// version) into jhon-<version>-linux-<arch>.tar.gz with a sha256sum-compatible .sha256
// and its SBOM (sbom.go) beside it. InstallArtifact verifies one, unpacks it into
// releases/<commit> (the commit its binary carries: the one naming scheme,
// which the renderer and publish read), and activates it as sync does.

// VersionPattern is what a packed version may be: it names the artifact.
var VersionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]*$`)

var commitID = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ArtifactName is the artifact of version for arch.
func ArtifactName(version, arch string) string {
	return fmt.Sprintf("jhon-%s-linux-%s.tar.gz", version, arch)
}

// Pack is `jhon release pack`: the artifact of the source's committed HEAD,
// at ver (DevVersion when empty), in outDir (ArtifactsDir when empty: never
// the checkout, which the next pack needs clean), its Go tests passed. It
// writes nothing under the share directory.
func Pack(r Roots, o *Out, ver, outDir string) int {
	if code, ok := r.NeedRepo(o, "release pack"); !ok {
		return code
	}
	if dirty(r.Source, false) {
		return o.Die("working tree is dirty; commit first (pack builds the committed HEAD)")
	}
	commit, ok := git(r.Source, false, "rev-parse", "--verify", "HEAD")
	if !ok {
		return o.Die("cannot resolve HEAD in " + r.Source)
	}
	epoch, ok := commitTime(r.Source, commit)
	if !ok {
		return o.Die("cannot read the commit time of " + commit)
	}
	if ver == "" {
		ver = DevVersion(commit)
	}
	arch, ok := goCmd(r.Source, "env", "GOARCH")
	if !ok {
		return o.Die("go env GOARCH failed: " + arch)
	}
	stage, err := os.MkdirTemp("", "jhon-pack-")
	if err != nil {
		return o.Die("cannot create a staging directory: " + err.Error())
	}
	defer os.RemoveAll(stage)
	if !archive(r.Source, commit, stage) {
		return o.Die(fmt.Sprintf("git archive of %s failed; nothing was packed", commit))
	}
	if out, ok := buildBinary(stage, r.Source, commit, ver); !ok {
		fmt.Fprintf(o.E, "%s\n", out)
		return o.Die(fmt.Sprintf("release %s failed to build %s; nothing was packed", commit, Binary))
	}
	if out, ok := SourceChecks(stage, r.Source, commit); !ok {
		fmt.Fprintf(o.E, "%s\n", out)
		return o.Die(fmt.Sprintf("release %s failed its Go tests or validation; nothing was packed", commit))
	}
	if outDir == "" {
		outDir = ArtifactsDir()
	}
	if err := os.MkdirAll(outDir, 0o777); err != nil {
		return o.Die("cannot create " + outDir + ": " + err.Error())
	}
	dst, _ := filepath.Abs(filepath.Join(outDir, ArtifactName(ver, arch)))
	if err := tarball(stage, dst, epoch); err != nil {
		return o.Die("cannot write " + dst + ": " + err.Error())
	}
	sum, err := sha256File(dst)
	if err == nil {
		err = os.WriteFile(dst+".sha256", []byte(sum+"  "+filepath.Base(dst)+"\n"), 0o644)
	}
	if err != nil {
		os.Remove(dst)
		return o.Die("cannot write " + dst + ".sha256: " + err.Error())
	}
	bom := filepath.Join(filepath.Dir(dst), SBOMName(ver, arch))
	if err := writeSBOM(stage, bom, filepath.Base(dst), ver, sum, epoch); err != nil {
		os.Remove(dst)
		os.Remove(dst + ".sha256")
		return o.Die("cannot write " + bom + ": " + err.Error())
	}
	o.OK(Tilde(dst))
	o.OK(Tilde(dst + ".sha256"))
	o.OK(Tilde(bom))
	return 0
}

// writeSBOM writes the SBOM of the artifact packed from stage into dst: the
// Go modules stage's binary was built from and the shell's lockfile.
func writeSBOM(stage, dst, artifact, ver, sum string, created time.Time) error {
	info, err := buildinfo.ReadFile(filepath.Join(stage, Binary))
	if err != nil {
		return err
	}
	lock, err := os.ReadFile(filepath.Join(stage, "shell", "package-lock.json"))
	if errors.Is(err, fs.ErrNotExist) {
		lock, err = nil, nil
	}
	if err != nil {
		return err
	}
	b, err := sbom(info, lock, artifact, ver, sum, created)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

// ArtifactsDir is where pack writes without --out: under the user's cache.
func ArtifactsDir() string { return filepath.Join(cacheDir(), "jhon", "artifacts") }

// commitTime is the committer time of commit: every timestamp in the
// artifact, so the same commit packs to the same bytes (T52).
func commitTime(source, commit string) (time.Time, bool) {
	out, ok := git(source, true, "log", "-1", "--format=%ct", commit)
	sec, err := strconv.ParseInt(out, 10, 64)
	if !ok || err != nil {
		return time.Time{}, false
	}
	return time.Unix(sec, 0).UTC(), true
}

// tarball writes dir's entries, relative to it, into the gzipped tar dst,
// through a file beside it renamed into place. It is reproducible (T52):
// entries sorted by name, every mtime the given one, owned by 0:0 with no
// user or group names, modes 0755 or 0644 (0777 for a symlink), GNU
// headers with no PAX records, and a gzip header with no name or mtime.
func tarball(dir, dst string, mtime time.Time) error {
	tmp := fmt.Sprintf("%s.%d.tmp", dst, os.Getpid())
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	err = writeTarball(dir, f, mtime)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func writeTarball(dir string, w io.Writer, mtime time.Time) error {
	zw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(zw)
	// WalkDir visits each directory's entries in lexical order.
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		h := &tar.Header{Name: filepath.ToSlash(rel), ModTime: mtime.Truncate(time.Second), Format: tar.FormatGNU}
		switch {
		case d.IsDir():
			h.Typeflag, h.Name, h.Mode = tar.TypeDir, h.Name+"/", 0o755
		case info.Mode()&os.ModeSymlink != 0:
			if h.Linkname, err = os.Readlink(p); err != nil {
				return err
			}
			h.Typeflag, h.Mode = tar.TypeSymlink, 0o777
		case info.Mode().IsRegular():
			h.Typeflag, h.Mode, h.Size = tar.TypeReg, 0o644, info.Size()
			if info.Mode()&0o111 != 0 {
				h.Mode = 0o755
			}
		default:
			return fmt.Errorf("%s: not a file, directory or symlink", rel)
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			return nil
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(tw, src)
		return err
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return zw.Close()
}

func sha256File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// InstallArtifact is `jhon release install <artifact>`: verify the artifact
// against the .sha256 beside it, unpack it into releases/<commit> unless
// that release exists (re-stamped from the artifact when its binary reports
// another version, as a synced dev build of the same commit does), and activate it as sync does (the renderer's
// preflight, current, the runtime projections). A failed check or unpack
// changes nothing; earlier releases stay for a rollback.
func InstallArtifact(r Roots, o *Out, artifact string) int {
	name := filepath.Base(artifact)
	b, err := os.ReadFile(artifact + ".sha256")
	if err != nil {
		return o.Die("no checksum beside the artifact: " + Tilde(artifact) + ".sha256; nothing was changed")
	}
	want, _, _ := strings.Cut(strings.TrimSpace(string(b)), " ")
	got, err := sha256File(artifact)
	if err != nil {
		return o.Die("cannot read " + Tilde(artifact) + ": " + err.Error() + "; nothing was changed")
	}
	if !strings.EqualFold(want, got) {
		return o.Die("checksum mismatch for " + name + "; nothing was changed")
	}
	releases := filepath.Join(r.Share, "releases")
	if err := os.MkdirAll(releases, 0o777); err != nil {
		return o.Die("cannot create " + releases + ": " + err.Error())
	}
	tmp := filepath.Join(releases, fmt.Sprintf(".unpacking-%d", os.Getpid()))
	os.RemoveAll(tmp)
	if err := os.Mkdir(tmp, 0o777); err != nil {
		return o.Die("cannot create " + tmp + ": " + err.Error())
	}
	if b, err := exec.Command("tar", "-xzf", artifact, "-C", tmp).CombinedOutput(); err != nil {
		fmt.Fprintf(o.E, "%s\n", Substituted(b))
		os.RemoveAll(tmp)
		return o.Die("cannot unpack " + name + "; nothing was changed")
	}
	commit, ver := stamp(tmp)
	if !commitID.MatchString(commit) {
		os.RemoveAll(tmp)
		return o.Die(name + " holds no " + Binary + " stamped with its commit; nothing was changed")
	}
	release := filepath.Join(releases, commit)
	built := false
	if IsDir(release) {
		err := restamp(tmp, release, ver)
		os.RemoveAll(tmp)
		if err != nil {
			return o.Die("cannot re-stamp " + release + ": " + err.Error())
		}
	} else if err := os.Rename(tmp, release); err != nil {
		os.RemoveAll(tmp)
		return o.Die("cannot place the release at " + release + ": " + err.Error())
	} else {
		built = true
	}
	if code, ok := activate(r, o, commit, release, built, false); !ok {
		return code
	}
	sessions(o)
	_, active := stamp(release)
	fmt.Fprintf(o.W, "\nactivated jhon %s from %s\n", active, name)
	return 0
}

// restamp replaces the existing release's build/jhon with the unpacked
// artifact's, written beside and renamed into place, when the existing
// binary is not stamped ver.
func restamp(unpacked, release, ver string) error {
	if _, have := stamp(release); have == ver {
		return nil
	}
	src := filepath.Join(unpacked, Binary)
	if !IsFile(src) {
		return nil
	}
	return installFile(src, filepath.Join(release, Binary), 0o755)
}

// stamp is the commit and version the release's binary carries.
func stamp(release string) (commit, ver string) {
	b, err := exec.Command(filepath.Join(release, Binary), "version", "--json").Output()
	if err != nil {
		return "", ""
	}
	var v struct{ Version, Commit string }
	if json.Unmarshal(b, &v) != nil {
		return "", ""
	}
	return v.Commit, v.Version
}
