// Tests for the release artifact (T3): `jhon release pack` builds, from the
// committed HEAD of a checkout, the tarball a synced release directory
// holds, with a .sha256 beside it; `jhon release install <artifact>`
// verifies it, unpacks it into ~/.local/share/jhon/releases/<commit>/,
// switches current, and installs the runtime projections from it, with no
// checkout and no toolchain. `jhon sync` composes both halves.
package release

import (
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func artifactName(version string) string {
	return "jhon-" + version + "-linux-" + runtime.GOARCH + ".tar.gz"
}

func sbomName(version string) string {
	return "jhon-" + version + "-linux-" + runtime.GOARCH + ".spdx.json"
}

// topLevel is the first component of every entry tar lists, sorted, unique.
func topLevel(listing string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(listing), "\n") {
		first, _, _ := strings.Cut(strings.TrimPrefix(l, "./"), "/")
		if first != "" && first != "." && !slices.Contains(out, first) {
			out = append(out, first)
		}
	}
	slices.Sort(out)
	return out
}

func sh(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	c := exec.Command(name, args...)
	c.Dir = dir
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatal(name, args, err, string(b))
	}
	return string(b)
}

func TestReleaseArtifact(t *testing.T) {
	parallel(t)
	src := scratchSource(t)
	first := head(t, src)
	out := temp(t)
	packer := scratchHome(t)
	r := run(t, packer, cliOf(src), []string{"release", "pack", "--version", "0.1.0", "--out", out})
	wantCode(t, r, 0)
	artifact := filepath.Join(out, artifactName("0.1.0"))

	t.Run("pack writes the tarball and a sha256sum -c .sha256; it holds the archived HEAD and build/, and its build/jhon is the given version", func(t *testing.T) {
		match(t, r.Stdout, `(?m)^  ok    `+regexp.QuoteMeta(artifact)+`$`)
		equal(t, names(t, out), []string{sbomName("0.1.0"), artifactName("0.1.0"), artifactName("0.1.0") + ".sha256"})
		match(t, sh(t, out, "sha256sum", "-c", artifactName("0.1.0")+".sha256"), `: OK`)
		archived := sh(t, src, "sh", "-c", "git archive --format=tar HEAD | tar -t")
		want := append(topLevel(archived), "build")
		slices.Sort(want)
		listing := sh(t, out, "tar", "-tzf", artifact)
		equal(t, topLevel(listing), want)
		// T24b: herdr is no longer part of Jhon; an artifact carries none of it.
		for _, l := range strings.Split(listing, "\n") {
			if strings.HasPrefix(l, "build/herdr") || strings.HasPrefix(l, "herdr/") {
				t.Errorf("the artifact carries %s", l)
			}
		}
		unpacked := temp(t)
		sh(t, unpacked, "tar", "-xzf", artifact)
		v := run(t, packer, filepath.Join(unpacked, "build", "jhon"), []string{"version"})
		match(t, v.Stdout, `^jhon 0\.1\.0 \(`+first[:12]+`, `)
		// Static (T7b): no interpreter and no dynamic section, so no glibc floor (WSL's Ubuntu 22.04).
		f, err := elf.Open(filepath.Join(unpacked, "build", "jhon"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, p := range f.Progs {
			if p.Type == elf.PT_INTERP || p.Type == elf.PT_DYNAMIC {
				t.Errorf("the packed build/jhon is dynamically linked: it has %v", p.Type)
			}
		}
		// Packing writes nothing under the home.
		equal(t, exists(share(packer.home)), false)
	})

	t.Run("pack writes an SPDX SBOM of the tarball: its sha256, the binary's Go modules and the Go standard library (T52)", func(t *testing.T) {
		match(t, r.Stdout, `(?m)^  ok    `+regexp.QuoteMeta(filepath.Join(out, sbomName("0.1.0")))+`$`)
		bom := support.Read(t, filepath.Join(out, sbomName("0.1.0")))
		sum, _, _ := strings.Cut(support.Read(t, artifact+".sha256"), " ")
		for _, want := range []string{
			`"spdxVersion": "SPDX-2.3"`,
			`"packageFileName": "` + artifactName("0.1.0") + `"`,
			`"checksumValue": "` + sum + `"`,
			`"referenceLocator": "pkg:golang/stdlib@go`,
			`"referenceLocator": "pkg:golang/golang.org/x/`,
		} {
			if !strings.Contains(bom, want) {
				t.Errorf("the SBOM lacks %s", want)
			}
		}
	})

	t.Run("pack without --version is 0.0.0-dev+<short commit>; an unsafe --version is a usage error", func(t *testing.T) {
		dev := temp(t)
		wantCode(t, run(t, packer, cliOf(src), []string{"release", "pack", "--out", dev}), 0)
		equal(t, names(t, dev), []string{sbomName("0.0.0-dev+" + first[:7]), artifactName("0.0.0-dev+" + first[:7]), artifactName("0.0.0-dev+"+first[:7]) + ".sha256"})
		bad := run(t, packer, cliOf(src), []string{"release", "pack", "--version", "../x", "--out", dev})
		wantCode(t, bad, 2)
		match(t, bad.Stderr, `--version`)
	})

	env := scratchHome(t)
	installed := run(t, env, cliOf(src), []string{"release", "install", artifact})

	t.Run("install verifies and activates the artifact: current, the CLI link at its version, and the projections", func(t *testing.T) {
		wantCode(t, installed, 0)
		release := filepath.Join(share(env.home), "releases", first)
		equal(t, realpath(t, filepath.Join(share(env.home), "current")), release)
		v := run(t, env, filepath.Join(env.home, ".local", "bin", "jhon"), []string{"version"})
		match(t, v.Stdout, `^jhon 0\.1\.0 \(`)
		match(t, installed.Stdout, `(?m)^  ok    current -> ~/\.local/share/jhon/releases/`+first+`$`)
		if !exists(filepath.Join(env.home, ".claude", "agents", "jhon-orchestrator.md")) {
			t.Fatal("the agents were not projected\n" + installed.Out())
		}
		match(t, support.Read(t, filepath.Join(env.home, ".cache", "jhon", "install.json")), first)
		noMatch(t, installed.Out(), `herdr`)
		equal(t, exists(filepath.Join(env.home, ".local", "bin", "herdr-jhon")), false)
	})

	t.Run("a tampered artifact or a missing .sha256 is refused and changes nothing", func(t *testing.T) {
		before := tree(t, share(env.home))
		link, _ := os.Readlink(filepath.Join(share(env.home), "current"))
		bad := temp(t)
		b, err := os.ReadFile(artifact)
		if err != nil {
			t.Fatal(err)
		}
		b[len(b)/2] ^= 0xff
		tampered := filepath.Join(bad, artifactName("0.1.0"))
		support.Write(t, bad, map[string]string{artifactName("0.1.0"): string(b), artifactName("0.1.0") + ".sha256": support.Read(t, artifact+".sha256")})
		r := run(t, env, cliOf(src), []string{"release", "install", tampered})
		wantFail(t, r)
		match(t, r.Stderr, `jhon: checksum mismatch for `+regexp.QuoteMeta(artifactName("0.1.0"))+`; nothing was changed`)

		lone := temp(t)
		support.Write(t, lone, map[string]string{artifactName("0.1.0"): support.Read(t, artifact)})
		m := run(t, env, cliOf(src), []string{"release", "install", filepath.Join(lone, artifactName("0.1.0"))})
		wantFail(t, m)
		match(t, m.Stderr, `jhon: no checksum beside the artifact: .*\.sha256; nothing was changed`)

		equal(t, tree(t, share(env.home)), before)
		after, _ := os.Readlink(filepath.Join(share(env.home), "current"))
		equal(t, after, link)
	})

	t.Run("installing a newer artifact keeps the previous release", func(t *testing.T) {
		appendTo(t, filepath.Join(src, "prose", "skills", "ldd", "references", "apply.md"), "\nsecond release\n")
		support.Git(t, src, "commit", "-q", "--no-verify", "-am", "second")
		second := head(t, src)
		wantCode(t, run(t, packer, cliOf(src), []string{"release", "pack", "--version", "0.1.1", "--out", out}), 0)
		r := run(t, env, cliOf(src), []string{"release", "install", filepath.Join(out, artifactName("0.1.1"))})
		wantCode(t, r, 0)
		equal(t, realpath(t, filepath.Join(share(env.home), "current")), filepath.Join(share(env.home), "releases", second))
		if !exists(filepath.Join(share(env.home), "releases", first, "build", "jhon")) {
			t.Fatal("the previous release was not kept")
		}
	})
}

// T3c (L12): the verify's advisories on the artifact, and its probes.
func TestReleaseArtifactProbes(t *testing.T) {
	parallel(t)
	src := scratchSource(t)
	first := head(t, src)
	packer := scratchHome(t)
	noXDG := []string{"XDG_CACHE_HOME=", "XDG_CONFIG_HOME="}

	t.Run("pack without --out writes into the user's cache, outside the checkout, so a second pack is not refused", func(t *testing.T) {
		one := run(t, packer, cliOf(src), []string{"release", "pack"}, noXDG...)
		wantCode(t, one, 0)
		two := run(t, packer, cliOf(src), []string{"release", "pack"}, noXDG...)
		wantCode(t, two, 0)
		name := artifactName("0.0.0-dev+" + first[:7])
		match(t, two.Stdout, `(?m)^  ok    ~/\.cache/jhon/artifacts/`+regexp.QuoteMeta(name)+`$`)
		equal(t, names(t, filepath.Join(packer.home, ".cache", "jhon", "artifacts")), []string{sbomName("0.0.0-dev+" + first[:7]), name, name + ".sha256"})
		equal(t, strings.TrimSpace(support.Git(t, src, "status", "--porcelain")), "")
	})

	t.Run("pack runs go with telemetry off: nothing under ~/.config/go", func(t *testing.T) {
		home := scratchHome(t)
		wantCode(t, run(t, home, cliOf(src), []string{"release", "pack", "--out", temp(t)}, noXDG...), 0)
		equal(t, exists(filepath.Join(home.home, ".config", "go")), false)
	})

	t.Run("sync runs go with its config aside too: nothing under ~/.config/go", func(t *testing.T) {
		home := scratchHome(t)
		wantCode(t, run(t, home, cliOf(src), []string{"sync"}, noXDG...), 0)
		equal(t, exists(filepath.Join(home.home, ".config", "go")), false)
	})

	t.Run("a release's binary is stripped: no symbol table, no DWARF", func(t *testing.T) {
		home := scratchHome(t)
		wantCode(t, syncOf(t, home, src), 0)
		f, err := elf.Open(filepath.Join(share(home.home), "releases", first, "build", "jhon"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, s := range []string{".symtab", ".debug_info"} {
			if f.Section(s) != nil {
				t.Errorf("the binary has %s", s)
			}
		}
	})

	out := temp(t)
	wantCode(t, run(t, packer, cliOf(src), []string{"release", "pack", "--version", "0.1.0", "--out", out}), 0)
	artifact := filepath.Join(out, artifactName("0.1.0"))

	t.Run("installing an artifact of a commit synced as a dev build re-stamps the binary: jhon version is the artifact's", func(t *testing.T) {
		env := scratchHome(t)
		wantCode(t, syncOf(t, env, src), 0)
		r := run(t, env, cliOf(src), []string{"release", "install", artifact})
		wantCode(t, r, 0)
		v := run(t, env, filepath.Join(env.home, ".local", "bin", "jhon"), []string{"version"})
		match(t, v.Stdout, `^jhon 0\.1\.0 \(`)
		match(t, r.Stdout, `(?m)^activated jhon 0\.1\.0 from `+regexp.QuoteMeta(artifactName("0.1.0"))+`$`)
	})

	t.Run("a truncated artifact with a matching .sha256 cannot be unpacked and changes nothing", func(t *testing.T) {
		env := scratchHome(t)
		wantCode(t, run(t, env, cliOf(src), []string{"release", "install", artifact}), 0)
		before := tree(t, share(env.home))
		link, _ := os.Readlink(filepath.Join(share(env.home), "current"))
		b := []byte(support.Read(t, artifact))
		dir := temp(t)
		cut := filepath.Join(dir, artifactName("0.1.0"))
		if err := os.WriteFile(cut, b[:len(b)/2], 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sh(t, dir, "sha256sum", artifactName("0.1.0"))
		support.Write(t, dir, map[string]string{artifactName("0.1.0") + ".sha256": sum})
		r := run(t, env, cliOf(src), []string{"release", "install", cut})
		equal(t, r.Code, 1)
		match(t, r.Stderr, `cannot unpack `+regexp.QuoteMeta(artifactName("0.1.0"))+`; nothing was changed`)
		equal(t, tree(t, share(env.home)), before)
		after, _ := os.Readlink(filepath.Join(share(env.home), "current"))
		equal(t, after, link)
	})

	t.Run("install with runtimes.pi = false skips pi and writes no ~/.pi/jhon", func(t *testing.T) {
		env := scratchHome(t)
		settings := filepath.Join(temp(t), "settings.toml")
		support.Write(t, filepath.Dir(settings), map[string]string{"settings.toml": "schema = 1\n\n[runtimes]\npi = false\n"})
		r := run(t, env, cliOf(src), []string{"release", "install", artifact}, "JHON_SETTINGS="+settings)
		wantCode(t, r, 0)
		match(t, r.Stdout, `skip: pi \(runtimes\.pi = false\)`)
		equal(t, exists(filepath.Join(env.home, ".pi", "jhon")), false)
	})

	t.Run("install needs only tar and gzip on PATH", func(t *testing.T) {
		env := scratchHome(t)
		bin := temp(t)
		for _, tool := range []string{"tar", "gzip"} {
			p, err := exec.LookPath(tool)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
				t.Fatal(err)
			}
		}
		r := run(t, env, cliOf(src), []string{"release", "install", artifact}, "PATH="+bin)
		wantCode(t, r, 0)
		v := run(t, env, filepath.Join(env.home, ".local", "bin", "jhon"), []string{"version"})
		match(t, v.Stdout, `^jhon 0\.1\.0 \(`)
	})
}

// devOnly is what the artifact leaves out (T10f): what only development
// reads. The installed release's validation checks what ships, never the
// repository's own evidence.
var devOnly = []string{"test", "design", "dist", "docs", "openspec", "tools", ".codegraph", ".github", "staticcheck.conf", "shell/test", "dashboard/test"}

func TestReleaseArtifactLeavesOutDevelopment(t *testing.T) {
	parallel(t)
	src := scratchSource(t)
	first := head(t, src)
	packer := scratchHome(t)
	out := temp(t)
	wantCode(t, run(t, packer, cliOf(src), []string{"release", "pack", "--version", "0.1.0", "--out", out}), 0)
	artifact := filepath.Join(out, artifactName("0.1.0"))

	t.Run("the artifact holds none of the development paths and no maintainer's home path", func(t *testing.T) {
		listing := strings.Split(strings.TrimSpace(sh(t, out, "tar", "-tzf", artifact)), "\n")
		for _, l := range listing {
			l = strings.TrimPrefix(l, "./")
			for _, d := range devOnly {
				if l == d || l == d+"/" || strings.HasPrefix(l, d+"/") {
					t.Errorf("the artifact ships %s", l)
				}
			}
		}
		// third_party/ stays: go.mod replaces bubbletea with it, and the
		// release's binary is built from the archive.
		if !slices.Contains(listing, "third_party/bubbletea/go.mod") {
			t.Error("the artifact lost third_party/bubbletea")
		}
		unpacked := temp(t)
		sh(t, unpacked, "tar", "-xzf", artifact)
		g := exec.Command("grep", "-rlF", "/home/"+"jchavarro", ".")
		g.Dir = unpacked
		b, _ := g.Output()
		equal(t, strings.TrimSpace(string(b)), "")
	})

	env := scratchHome(t)
	installed := run(t, env, cliOf(src), []string{"release", "install", artifact})
	jhon := filepath.Join(env.home, ".local", "bin", "jhon")

	t.Run("the trimmed artifact installs, and the installed jhon's version, status --brief and doctor run", func(t *testing.T) {
		wantCode(t, installed, 0)
		match(t, installed.Stdout, `(?m)^  ok    current -> ~/\.local/share/jhon/releases/`+first+`$`)
		v := run(t, env, jhon, []string{"version"})
		wantCode(t, v, 0)
		match(t, v.Stdout, `^jhon 0\.1\.0 \(`)
		wantCode(t, run(t, env, jhon, []string{"status", "--brief"}), 0)
		// doctor on a user's release: TestUserReleaseWithoutCheckout.
		s := run(t, env, jhon, []string{"setup", "--help"})
		wantCode(t, s, 0)
	})

	t.Run("from an installed release with no checkout, sync, pack and test refuse in one line", func(t *testing.T) {
		for _, a := range [][]string{{"sync"}, {"release", "pack"}, {"test"}} {
			r := run(t, env, jhon, a)
			wantCode(t, r, 1)
			match(t, r.Out(), `^jhon: `+regexp.QuoteMeta(strings.Join(a, " "))+` needs the Jhon source checkout, not found at ~/dev/jhon \(run 'jhon sync' from your checkout once, or set JHON_SOURCE\); to update an installed Jhon run 'jhon update'\n$`)
		}
	})

	t.Run("in a checkout a broken README reference still fails validate and sync", func(t *testing.T) {
		broken := scratchSource(t)
		appendTo(t, filepath.Join(broken, "README.md"), "\nSee `test/no-such-file.md`.\n")
		support.Git(t, broken, "commit", "-q", "--no-verify", "-am", "broken ref")
		v := run(t, scratchHome(t), cliOf(broken), []string{"validate"})
		equal(t, v.Code, 1)
		match(t, v.Out(), `broken-ref +README\.md:\d+ +test/no-such-file\.md does not resolve`)
		home := scratchHome(t)
		s := run(t, home, cliOf(broken), []string{"sync"})
		equal(t, s.Code != 0, true, s.Out())
		match(t, s.Out(), `test/no-such-file\.md does not resolve`)
		equal(t, exists(filepath.Join(share(home.home), "current")), false)
	})
}

// TestReleaseInstallRuntimesDetected (T10g, S4): with no setting a runtime is
// on only where it is detected (its home directory or its command on PATH),
// so a fresh HOME gets only the core; an explicit setting always wins.
func TestReleaseInstallRuntimesDetected(t *testing.T) {
	parallel(t)
	src := scratchSource(t)
	packer := scratchHome(t)
	out := temp(t)
	wantCode(t, run(t, packer, cliOf(src), []string{"release", "pack", "--version", "0.1.0", "--out", out}), 0)
	artifact := filepath.Join(out, artifactName("0.1.0"))
	// Only what install needs on PATH: neither claude nor pi is found there.
	bin := temp(t)
	for _, tool := range []string{"tar", "gzip"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	settingsFile := func(text string) string {
		dir := temp(t)
		support.Write(t, dir, map[string]string{"settings.toml": text})
		return filepath.Join(dir, "settings.toml")
	}
	install := func(t *testing.T, home string, extra ...string) support.Result {
		t.Helper()
		return run(t, scratch{home, bin}, cliOf(src), []string{"release", "install", artifact}, append([]string{"PATH=" + bin}, extra...)...)
	}
	orchestrator := func(home string) string { return filepath.Join(home, ".claude", "agents", "jhon-orchestrator.md") }

	t.Run("a fresh HOME with neither runtime gets only the core: no ~/.claude, and the absent line", func(t *testing.T) {
		home := temp(t)
		r := install(t, home)
		wantCode(t, r, 0)
		match(t, r.Stdout, `(?m)^claude-code:\n  skip   ~/\.claude is absent; not created$`)
		match(t, r.Stdout, `(?m)^  skip   ~/\.pi is absent; not created$`)
		equal(t, exists(filepath.Join(home, ".claude")), false)
		equal(t, exists(filepath.Join(share(home), "current")), true)
	})

	t.Run("an existing ~/.claude is detected: the agents are written", func(t *testing.T) {
		home := temp(t)
		support.Write(t, home, map[string]string{".claude/CLAUDE.md": "mine\n"})
		r := install(t, home)
		wantCode(t, r, 0)
		equal(t, exists(orchestrator(home)), true)
		equal(t, support.Read(t, filepath.Join(home, ".claude", "CLAUDE.md")), "mine\n")
	})

	t.Run("settings choosing Claude Code create ~/.claude even when absent", func(t *testing.T) {
		home := temp(t)
		r := install(t, home, "JHON_SETTINGS="+settingsFile("schema = 1\n\n[runtimes]\nclaude_code = true\n"))
		wantCode(t, r, 0)
		equal(t, exists(orchestrator(home)), true)
	})

	t.Run("settings turning Claude Code off write nothing into an existing ~/.claude", func(t *testing.T) {
		home := temp(t)
		support.Write(t, home, map[string]string{".claude/CLAUDE.md": "mine\n"})
		r := install(t, home, "JHON_SETTINGS="+settingsFile("schema = 1\n\n[runtimes]\nclaude_code = false\npi = true\n"))
		wantCode(t, r, 0)
		match(t, r.Stdout, `skip: claude-code \(runtimes\.claude_code = false\)`)
		equal(t, exists(filepath.Join(home, ".claude", "agents")), false)
	})
}

// TestUserReleaseWithoutCheckout (T10h, L51): a user has only the release,
// installed as install.sh does (the artifact's own build/jhon runs
// `release install`), no ~/dev/jhon and no JHON_SOURCE.
func TestUserReleaseWithoutCheckout(t *testing.T) {
	parallel(t)
	src := scratchSource(t)
	out := temp(t)
	wantCode(t, run(t, scratchHome(t), cliOf(src), []string{"release", "pack", "--version", "0.1.0", "--out", out}), 0)
	artifact := filepath.Join(out, artifactName("0.1.0"))
	unpacked := temp(t)
	sh(t, unpacked, "tar", "-xzf", artifact, "build/jhon")
	userInstall := func(t *testing.T, env scratch, extra ...string) support.Result {
		t.Helper()
		return run(t, env, filepath.Join(unpacked, "build", "jhon"), []string{"release", "install", artifact}, extra...)
	}

	env := scratchHome(t)
	support.Write(t, env.home, map[string]string{".pi/.keep": ""})
	installed := userInstall(t, env)
	wantCode(t, installed, 0)
	jhon := filepath.Join(env.home, ".local", "bin", "jhon")

	t.Run("doctor judges the installed release and skips the checkout's checks in one line", func(t *testing.T) {
		d := run(t, env, jhon, []string{"doctor"})
		wantCode(t, d, 0)
		noMatch(t, d.Out(), `no framework source|hook inactive`)
		match(t, d.Stdout, `(?m)^  ok    validation: ok`)
		match(t, d.Stdout, `(?m)^  ok    up to date$`)
		match(t, d.Stdout, `(?m)^integrity\n  skipped: an installed release; the pre-commit hook and the publication checks are the source checkout's$`)
		match(t, d.Stdout, `(?m)^healthy$`)
	})

	t.Run("nothing the install writes names a source path", func(t *testing.T) {
		for _, dir := range []string{".claude", ".pi", ".config", ".cache", ".local/bin"} {
			g := exec.Command("grep", "-rlE", "dev/jhon|"+regexp.QuoteMeta(src)+"|"+regexp.QuoteMeta(unpacked), filepath.Join(env.home, dir))
			b, _ := g.Output()
			equal(t, strings.TrimSpace(string(b)), "", dir)
		}
		g := exec.Command("grep", "-rlF", src, share(env.home))
		b, _ := g.Output()
		equal(t, strings.TrimSpace(string(b)), "")
		match(t, support.Read(t, filepath.Join(env.home, ".claude", "agents", "worker.md")), `'framework-guard' '`+regexp.QuoteMeta(share(env.home))+`/releases/`)
	})

	t.Run("doctor in a home with neither runtime says Claude Code was not found, not that a setting turned it off", func(t *testing.T) {
		bin := temp(t)
		for _, tool := range []string{"tar", "gzip"} {
			p, err := exec.LookPath(tool)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
				t.Fatal(err)
			}
		}
		home := scratch{temp(t), bin}
		in := userInstall(t, home, "PATH="+bin)
		wantCode(t, in, 0)
		// A user is never told to sync (T10i).
		noMatch(t, in.Out(), "jhon sync")
		match(t, in.Stdout, "(?m)no install record \\(~/\\.cache/jhon/install\\.json\\): no runtime is installed yet; run `jhon setup` to choose one$")
		for _, cmd := range []string{"setup", "update"} {
			h := run(t, home, filepath.Join(home.home, ".local", "bin", "jhon"), []string{cmd, "--help"}, "PATH="+bin)
			wantCode(t, h, 0)
			match(t, h.Stdout, `^jhon `+cmd+` — `)
		}
		d := run(t, home, filepath.Join(home.home, ".local", "bin", "jhon"), []string{"doctor"}, "PATH="+bin)
		wantCode(t, d, 0)
		match(t, d.Stdout, `(?m)^  claude-code was not found \(no ~/\.claude, no claude on PATH\) and runtimes\.claude_code is not set; ~/\.claude is not checked$`)
	})

	t.Run("the rendered framework guard of a user install refuses an Edit under current/ (exit 2) and allows the project", func(t *testing.T) {
		agent := support.Read(t, filepath.Join(env.home, ".claude", "agents", "worker.md"))
		m := regexp.MustCompile(`(?m)^ +command: "('[^"]*'pre-tool'[^"]*)"$`).FindStringSubmatch(agent)
		if m == nil {
			t.Fatalf("no pre-tool hook in worker.md:\n%s", agent)
		}
		match(t, m[1], `'framework-guard' '`+regexp.QuoteMeta(share(env.home))+`/releases/[0-9a-f]{40}' `)
		project := temp(t)
		edit := func(path string) support.Result {
			payload := fmt.Sprintf(`{"tool_name":"Edit","cwd":%q,"agent_type":"worker","tool_input":{"file_path":%q,"old_string":"a","new_string":"b"}}`, project, path)
			return support.Run(t, support.Cmd{Args: []string{"sh", "-c", m[1]}, Stdin: payload, Dir: project, Env: support.Env("HOME="+env.home, "PATH="+env.bin+":"+os.Getenv("PATH"))})
		}
		r := edit(filepath.Join(share(env.home), "current", "prose", "orchestrator.md"))
		wantCode(t, r, 2)
		match(t, r.Stderr, `framework-guard: `)
		wantCode(t, edit(filepath.Join(project, "a.go")), 0)
	})

	t.Run("pi chosen in settings with no ~/.pi is created, as Claude Code is", func(t *testing.T) {
		home := scratchHome(t)
		settings := filepath.Join(temp(t), "settings.toml")
		support.Write(t, filepath.Dir(settings), map[string]string{"settings.toml": "schema = 1\n\n[runtimes]\npi = true\n"})
		r := userInstall(t, home, "JHON_SETTINGS="+settings)
		wantCode(t, r, 0)
		equal(t, exists(filepath.Join(home.home, ".pi", "jhon", "settings.json")), true)
	})

	t.Run("pi detected only by its command, with no ~/.pi and no setting, stays absent", func(t *testing.T) {
		home := scratchHome(t)
		support.Write(t, home.home, map[string]string{"stub-bin/pi": "#!/bin/sh\nexit 1\n"})
		if err := os.Chmod(filepath.Join(home.bin, "pi"), 0o755); err != nil {
			t.Fatal(err)
		}
		r := userInstall(t, home)
		wantCode(t, r, 0)
		match(t, r.Stdout, `(?m)^  skip   ~/\.pi is absent; not created$`)
		equal(t, exists(filepath.Join(home.home, ".pi")), false)
	})
}

// TestPackOnAMachineWithoutRuntimes pins B1 (T10j): `jhon release pack`, its
// engine tests run (JHON_SYNC_GO_TEST unset), succeeds in a bare environment
// with an empty HOME and neither claude nor pi on PATH, as on a CI runner.
func TestPackOnAMachineWithoutRuntimes(t *testing.T) {
	parallel(t)
	bin := temp(t)
	for _, tool := range []string{"go", "git", "tar", "gzip"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	path := bin + ":/usr/bin:/bin"
	for _, rt := range []string{"claude", "pi"} {
		for _, dir := range filepath.SplitList(path) {
			if exists(filepath.Join(dir, rt)) {
				t.Skipf("%s is in %s: this machine cannot stand for a runner", rt, dir)
			}
		}
	}
	src := scratchSource(t)
	out := temp(t)
	env := []string{"PATH=" + path, "HOME=" + temp(t), "TMPDIR=" + temp(t)}
	env = append(env, offlineGo(t)...)
	r := support.Run(t, support.Cmd{Args: []string{cliOf(src), "release", "pack", "--version", "0.1.0", "--out", out}, Env: env})
	wantCode(t, r, 0)
	equal(t, exists(filepath.Join(out, artifactName("0.1.0"))), true)
}
