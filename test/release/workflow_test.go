// Tests for the public release workflow (T7, T7b): what `jhon update`
// trusts (S9, S10) and what the workflow publishes (S11) must stay one
// contract, so the file is read here against engine/internal/update's
// verify.go (internal to engine, so read as source, not imported).
package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// releaseWorkflows are the two release workflows: the repository's own,
// signed as IdentityPattern, and the old public repository's, kept only to
// build and sign the v0.3.0 bridge as BridgeIdentityPattern (T18).
var releaseWorkflows = []struct {
	name, path, pattern, prefix string
}{
	{"release.yml", ".github/workflows/release.yml", "IdentityPattern",
		"https://github.com/jachavarro2004/jhon-agent/.github/workflows/release.yml@refs/tags/"},
	{"dist/public release.yml (bridge)", "dist/public/.github/workflows/release.yml", "BridgeIdentityPattern",
		"https://github.com/sdsoftwaresas/jhon/.github/workflows/release.yml@refs/tags/"},
}

func TestReleaseWorkflow(t *testing.T) {
	v, err := os.ReadFile(filepath.Join(support.Root(), "engine", "internal", "update", "verify.go"))
	if err != nil {
		t.Fatal(err)
	}
	constant := func(name string) string {
		t.Helper()
		m := regexp.MustCompile(`(?m)^\s*` + name + `\s*=\s*["\x60]([^"\x60]+)["\x60]`).FindStringSubmatch(string(v))
		if m == nil {
			t.Fatalf("verify.go declares no %s", name)
		}
		return m[1]
	}
	files := map[string]string{}
	for _, w := range releaseWorkflows {
		b, err := os.ReadFile(filepath.Join(support.Root(), filepath.FromSlash(w.path)))
		if err != nil {
			t.Fatal(err)
		}
		files[w.path] = string(b)
		t.Run(w.name, func(t *testing.T) {
			releaseWorkflow(t, string(b), constant("Issuer"), constant(w.pattern), w.prefix)
		})
	}

	t.Run("the repository's release.yml builds this repository, runs only in jachavarro2004/jhon-agent, and needs no key (T18)", func(t *testing.T) {
		wf := files[".github/workflows/release.yml"]
		body := wf[strings.Index(wf, "\njobs:\n"):]
		jobs := regexp.MustCompile(`(?m)^  (\w+):\n`).FindAllStringSubmatchIndex(body, -1)
		equal(t, len(jobs), 3)
		for i, j := range jobs {
			end := len(body)
			if i+1 < len(jobs) {
				end = jobs[i+1][0]
			}
			if !regexp.MustCompile(`(?m)^    if: github\.repository == 'jachavarro2004/jhon-agent'$`).MatchString(body[j[0]:end]) {
				t.Errorf("job %s does not run only when github.repository is jachavarro2004/jhon-agent", body[j[2]:j[3]])
			}
		}
		if regexp.MustCompile(`(?m)^\s+repository:`).MatchString(wf) {
			t.Error("the repository's release.yml checks out another repository")
		}
		for _, bad := range []string{"ssh-key", "secrets.", "sdsoftwaresas", "DEPLOY_KEY"} {
			if strings.Contains(wf, bad) {
				t.Errorf("the repository's release.yml names %q", bad)
			}
		}
		equal(t, strings.Count(wf, "uses: actions/checkout@"), 1)
	})

	t.Run("the bridge's release.yml says it serves only v0.3.0 and still builds from the old pair (T18)", func(t *testing.T) {
		wf := files["dist/public/.github/workflows/release.yml"]
		if !strings.HasPrefix(wf, "# BRIDGE ONLY (T18): this file serves only the v0.3.0 bridge release") {
			t.Error("dist/public's release.yml does not open with its bridge-only header")
		}
		for _, s := range []string{"repository: sdsoftwaresas/jhon-agents", "ssh-key: ${{ secrets.JHON_AGENTS_DEPLOY_KEY }}"} {
			if !strings.Contains(wf, s) {
				t.Errorf("the bridge's release.yml lacks %q", s)
			}
		}
	})

	validateChecks(t)

	// Resolved with `gh api repos/<repo>/commits/<tag>` (T7b, L38).
	tags := map[string]string{
		"actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10":          "v6.0.3",
		"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e":          "v7.0.0",
		"sigstore/cosign-installer@756fe2d284c7292c4e9b62c8a28f1e5629c59532": "v4.1.3",
		"actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a":   "v7.0.1",
		"actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c": "v8.0.1",
		"actions/setup-node@949feb2413d6458794dcd2491c4babbbce0c15c1":        "v7.1.0",
	}

	// jhon-agents' own validate workflow pins its actions the same way (T30).
	validate, err := os.ReadFile(filepath.Join(support.Root(), ".github", "workflows", "validate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"release.yml": files[".github/workflows/release.yml"], "dist/public release.yml": files["dist/public/.github/workflows/release.yml"], "validate.yml": string(validate)} {
		pinnedActions(t, name, text, tags)
	}
}

// releaseWorkflow checks one release workflow wf against verify.go's issuer
// and the identity pattern it signs as; prefix is that identity up to the tag.
func releaseWorkflow(t *testing.T, wf, issuer, identityPattern, prefix string) {
	value := func(t *testing.T, key string) string {
		t.Helper()
		m := regexp.MustCompile(`(?m)^\s*` + key + `:\s*(.+?)\s*$`).FindStringSubmatch(wf)
		if m == nil {
			t.Fatalf("the workflow sets no %s", key)
		}
		return m[1]
	}

	t.Run("the signer env is verify.go's Issuer and its identity pattern, verbatim (S9, S10)", func(t *testing.T) {
		equal(t, value(t, "SIGNER_ISSUER"), issuer)
		equal(t, value(t, "SIGNER_IDENTITY"), identityPattern)
	})

	t.Run("the tag job's regex accepts and refuses the tags the identity does (S11)", func(t *testing.T) {
		m := regexp.MustCompile(`grep -Eq '([^']+)'`).FindStringSubmatch(wf)
		if m == nil {
			t.Fatal("the tag job has no grep -Eq '<regex>'")
		}
		tag := regexp.MustCompile(m[1])
		identity := regexp.MustCompile(identityPattern)
		for _, v := range []string{"v0.1.0", "v0.2.0-rc.1", "v10.20.30-rc.123", "v0.0.0", "v01.2.3", "v0.2.0-rc.01", "v1.2.3-rc1", "v0.2.0-beta.1", "v0.2.0x", "vEVIL", "v1.2", "v1.2.3+meta"} {
			equal(t, tag.MatchString(v), identity.MatchString(prefix+v), v)
		}
	})

	t.Run("an rc is a pre-release never latest; the checkout keeps no credentials", func(t *testing.T) {
		for _, s := range []string{"--prerelease --latest=false", "--verify-tag", "persist-credentials: false"} {
			if !strings.Contains(wf, s) {
				t.Errorf("the workflow lacks %q", s)
			}
		}
	})

	t.Run("no herdr, Rust or Zig step remains: the release builds jhon alone (T24b)", func(t *testing.T) {
		for _, m := range regexp.MustCompile(`(?i)\b(herdr|cargo|rust|rustup|zig|nextest|clippy|musl|libghostty)`).FindAllString(wf, -1) {
			t.Errorf("the workflow still names %q", m)
		}
	})

	t.Run("verify-blob, the readelf checks and the prerelease branch are used as meant (T7c, L43)", func(t *testing.T) {
		for _, p := range usage(wf) {
			t.Error(p)
		}
	})

	t.Run("usage catches each mutation of the workflow (T7c, L43)", func(t *testing.T) {
		checks := readelfChecks.FindAllString(wf, -1)
		if len(checks) != 1 {
			t.Fatalf("%d readelf checks, want 1", len(checks))
		}
		for _, m := range []struct{ name, from, to string }{
			{"identity any", `--certificate-identity-regexp "$SIGNER_IDENTITY"`, `--certificate-identity-regexp ".*"`},
			{"issuer any", `--certificate-oidc-issuer "$SIGNER_ISSUER"`, `--certificate-oidc-issuer ".*"`},
			{"identity flag dropped", "--certificate-identity-regexp \"$SIGNER_IDENTITY\" \\\n", ""},
			{"tarball readelf check deleted", checks[0], ""},
			{"readelf error ignored", `if ! h=$(readelf -lW "$b" 2>&1) || ! grep -q 'Program Headers:' <<<"$h"; then`, `if ! h=$(readelf -lW "$b" 2>&1 || true); then`},
			{"prerelease never", `if [ "$PRERELEASE" = true ]; then`, `if [ "$PRERELEASE" = never ]; then`},
			{"prerelease flags dropped", `flags+=(--prerelease --latest=false)`, `flags+=()`},
			{"flags not passed", ` "${flags[@]}"`, ""},
		} {
			if strings.Count(wf, m.from) != 1 {
				t.Errorf("%s: %q is not in the workflow once", m.name, m.from)
				continue
			}
			if len(usage(strings.Replace(wf, m.from, m.to, 1))) == 0 {
				t.Errorf("%s: the mutated workflow passes", m.name)
			}
		}
	})

	t.Run("the readelf snippet passes a static binary and fails a dynamic one or a non-ELF file (T7c)", func(t *testing.T) {
		snippets := readelfChecks.FindAllStringSubmatch(wf, -1)
		if len(snippets) != 1 {
			t.Fatalf("%d readelf checks, want 1", len(snippets))
		}
		dir := t.TempDir()
		src := filepath.Join(dir, "main.go")
		if err := os.WriteFile(src, []byte("package main\n\nimport \"net\"\n\nfunc main() { _, _ = net.LookupHost(\"localhost\") }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		build := func(name, cgo string) string {
			out := filepath.Join(dir, name)
			cmd := exec.Command("go", "build", "-o", out, src)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "CGO_ENABLED="+cgo, "GOFLAGS=", "GO111MODULE=off")
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go build CGO_ENABLED=%s: %v\n%s", cgo, err, b)
			}
			return out
		}
		text := filepath.Join(dir, "notes.txt")
		if err := os.WriteFile(text, []byte("not an ELF\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			name, file string
			ok         bool
			says       string
		}{
			{"static", build("static", "0"), true, ""},
			{"text file", text, false, "::error::"},
			{"missing file", filepath.Join(dir, "absent"), false, "::error::"},
		}
		if _, err := exec.LookPath("cc"); err == nil {
			cases = append(cases, struct {
				name, file string
				ok         bool
				says       string
			}{"dynamic (cgo)", build("dynamic", "1"), false, "dynamically linked"})
		} else {
			t.Log("no C compiler: the dynamic case is skipped")
		}
		for _, s := range snippets {
			for _, c := range cases {
				cmd := exec.Command("bash", "-c", s[0])
				cmd.Env = append(os.Environ(), s[1]+"="+c.file)
				out, err := cmd.CombinedOutput()
				if (err == nil) != c.ok || !strings.Contains(string(out), c.says) {
					t.Errorf("$%s=%s: err %v, output %q; want ok %v and %q", s[1], c.name, err, out, c.ok, c.says)
				}
			}
		}
	})

	t.Run("the build repacks the commit and compares bytes before signing (T52)", func(t *testing.T) {
		pack := strings.Index(wf, `build/jhon release pack --version "$VERSION" --out "$OUT"`)
		repro := strings.Index(wf, `run: tools/release/repro.sh build/jhon "$VERSION" "$OUT"`)
		sign := strings.Index(wf, "cosign sign-blob")
		if pack < 0 || repro < 0 || sign < 0 || !(pack < repro && repro < sign) {
			t.Errorf("want pack, then tools/release/repro.sh, then signing; at %d, %d, %d", pack, repro, sign)
		}
	})

	t.Run("the SBOM is signed with the tarball and published with it (T52)", func(t *testing.T) {
		match(t, wf, `(?m)^\s*for f in "\$OUT/jhon-\$VERSION-linux-\$\{\{ matrix\.arch \}\}\.tar\.gz" "\$OUT/jhon-\$VERSION-linux-\$\{\{ matrix\.arch \}\}\.spdx\.json"; do$`)
		match(t, wf, `(?m)^\s*cosign sign-blob --yes --bundle "\$f\.sigstore\.json" "\$f"$`)
		match(t, wf, `(?m)^\s*for ext in tar\.gz tar\.gz\.sha256 tar\.gz\.sigstore\.json spdx\.json spdx\.json\.sigstore\.json; do$`)
	})

	t.Run("a static build: jhon with CGO_ENABLED=0 (T7b)", func(t *testing.T) {
		equal(t, value(t, "CGO_ENABLED"), `"0"`)
	})

	t.Run("permissions are per job: none at the top, id-token only to build, contents: write only to publish", func(t *testing.T) {
		match(t, wf, `(?m)^permissions: \{\}$`)
		equal(t, strings.Count(wf, "id-token: write"), 1)
		equal(t, strings.Count(wf, "contents: write"), 1)
		build, _, _ := strings.Cut(wf[strings.Index(wf, "\n  build:"):], "\n  publish:")
		match(t, build, `(?m)^    permissions:\n      contents: read\n      id-token: write$`)
		match(t, wf[strings.Index(wf, "\n  publish:"):], `(?m)^    permissions:\n      contents: write$`)
	})

	t.Run("every job has a time limit; a release run is never cancelled half-way (T11d, L62)", func(t *testing.T) {
		// rc1 measured: tag 4 s, build 14m24s (amd64), publish 8 s (L64).
		limits := map[string]int{"tag": 5, "build": 35, "publish": 15}
		jobs := regexp.MustCompile(`(?m)^  (\w+):\n`).FindAllStringSubmatchIndex(wf[strings.Index(wf, "\njobs:\n"):], -1)
		body := wf[strings.Index(wf, "\njobs:\n"):]
		equal(t, len(jobs), len(limits))
		for i, j := range jobs {
			name, end := body[j[2]:j[3]], len(body)
			if i+1 < len(jobs) {
				end = jobs[i+1][0]
			}
			m := regexp.MustCompile(`(?m)^    timeout-minutes: (\d+)$`).FindStringSubmatch(body[j[0]:end])
			if m == nil {
				t.Errorf("job %s has no timeout-minutes (GitHub's default is 360)", name)
				continue
			}
			if n, _ := strconv.Atoi(m[1]); n < 1 || n > limits[name] {
				t.Errorf("job %s: timeout-minutes %d, want 1..%d", name, n, limits[name])
			}
		}
		match(t, wf, `(?m)^concurrency:\n  group: release-\$\{\{ github\.ref \}\}\n  cancel-in-progress: false$`)
		if regexp.MustCompile(`(?m)\b(while|until)\b`).MatchString(wf) {
			t.Error("a run script loops with while/until; every loop must be bounded (for over a list)")
		}
	})

}

// validateChecks checks jhon-agents' validate workflow.
func validateChecks(t *testing.T) {
	t.Run("jhon-agents' validate workflow cannot run for hours if re-enabled (T11d, L62)", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join(support.Root(), ".github", "workflows", "validate.yml"))
		if err != nil {
			t.Fatal(err)
		}
		v := string(b)
		match(t, v, `(?m)^concurrency:\n  group: validate-\$\{\{ github\.ref \}\}\n  cancel-in-progress: true$`)
		match(t, v, `(?m)^    timeout-minutes: 30$`)
		steps := regexp.MustCompile(`(?m)^      - `).FindAllStringIndex(v, -1)
		for i, s := range steps {
			end := len(v)
			if i+1 < len(steps) {
				end = steps[i+1][0]
			}
			if !strings.Contains(v[s[0]:end], "timeout-minutes:") {
				t.Errorf("validate.yml step without timeout-minutes: %s", strings.SplitN(v[s[0]:end], "\n", 2)[0])
			}
		}
	})

	t.Run("validate.yml's Windows job: its portable packages vet for GOOS=windows, and it smokes install.ps1 (T52)", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join(support.Root(), ".github", "workflows", "validate.yml"))
		if err != nil {
			t.Fatal(err)
		}
		v := string(b)
		i := strings.Index(v, "\n  windows:\n")
		if i < 0 {
			t.Fatal("validate.yml has no windows job")
		}
		job := v[i:]
		match(t, job, `(?m)^    runs-on: windows-latest$`)
		match(t, job, `(?m)^    timeout-minutes: 20$`)
		// `go build` refuses a package with only test files (test/hygiene), so
		// the build names only the packages with non-test Go files (wave 1 verify).
		if strings.Contains(job, "go build $PORTABLE") {
			t.Error("the windows job runs `go build $PORTABLE`, which fails on a test-only package")
		}
		for _, s := range []string{"go build $(go list -f '{{if .GoFiles}}{{.ImportPath}}{{end}}' $PORTABLE)", "go vet $PORTABLE", "go test -count=1 -timeout 300s $PORTABLE", "'-File', $script, '-Distro', 'JhonCiNoSuchDistro'", "foreach ($shell in 'pwsh', 'powershell')", "Language.Parser]::ParseFile($script"} {
			if !strings.Contains(job, s) {
				t.Errorf("the windows job lacks %q", s)
			}
		}
		m := regexp.MustCompile(`(?m)^      PORTABLE: >-\n((?:        \S.*\n)+)`).FindStringSubmatch(job)
		if m == nil {
			t.Fatal("the windows job sets no PORTABLE list")
		}
		pkgs := strings.Fields(m[1])
		vet := exec.Command("go", append([]string{"vet"}, pkgs...)...)
		vet.Dir = support.Root()
		vet.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
		if out, err := vet.CombinedOutput(); err != nil {
			t.Errorf("GOOS=windows go vet of the portable list failed: %v\n%s", err, out)
		}
		list := exec.Command("go", append([]string{"list", "-f", "{{if .GoFiles}}{{.ImportPath}}{{end}}"}, pkgs...)...)
		list.Dir, list.Env = support.Root(), vet.Env
		names, err := list.Output()
		if err != nil {
			t.Fatalf("go list of the portable list: %v", err)
		}
		build := exec.Command("go", append([]string{"build"}, strings.Fields(string(names))...)...)
		build.Dir, build.Env = support.Root(), vet.Env
		if out, err := build.CombinedOutput(); err != nil {
			t.Errorf("GOOS=windows go build of the portable packages with Go files failed: %v\n%s", err, out)
		}
	})

}

// pinnedActions checks every uses: line in text is <owner>/<repo>@<40 hex>
// # <tag>, with the tag the commit resolves to.
func pinnedActions(t *testing.T, name, wf string, tags map[string]string) {
	t.Helper()
	t.Run(name+": every action is pinned to a commit with its exact tag", func(t *testing.T) {
		uses := regexp.MustCompile(`(?m)^\s*(?:- )?uses:\s*(.*)$`).FindAllStringSubmatch(wf, -1)
		if len(uses) == 0 {
			t.Fatal("no uses: lines")
		}
		pinned := regexp.MustCompile(`^[\w.-]+/[\w.-]+@[0-9a-f]{40} # v\d+(\.\d+)*$`)
		for _, u := range uses {
			if !pinned.MatchString(u[1]) {
				t.Errorf("not pinned as <owner>/<repo>@<40 hex> # <tag>: %s", u[1])
				continue
			}
			ref, tag, _ := strings.Cut(u[1], " # ")
			if want, ok := tags[ref]; !ok || tag != want {
				t.Errorf("%s: the comment says %s; the resolved tag is %q", ref, tag, want)
			}
		}
	})
}

// readelfChecks is the workflow's check that a binary is static: readelf
// must read its program headers (an error or a non-ELF fails), and none of
// them may name a program interpreter.
var readelfChecks = regexp.MustCompile(`if ! h=\$\(readelf -lW "\$(\w+)" 2>&1\) \|\| ! grep -q 'Program Headers:' <<<"\$h"; then\n\s*echo "::error::readelf cannot read \$\w+: \$h"; exit 1\n\s*fi\n\s*if grep -q 'program interpreter' <<<"\$h"; then\n\s*echo "::error::[^\n]*dynamically linked"; exit 1\n\s*fi`)

// usage is what is wrong with how wf uses the signer, the readelf checks
// and the prerelease flags (T7c, L43); nothing when all is right.
func usage(wf string) (problems []string) {
	verify := regexp.MustCompile(`cosign verify-blob(?:[^\n]*\\\n)*[^\n]*`).FindAllString(wf, -1)
	if len(verify) != 1 {
		problems = append(problems, "want one cosign verify-blob command")
	} else {
		args := strings.Fields(strings.ReplaceAll(verify[0], "\\\n", " "))
		flag := map[string]string{}
		for i, a := range args {
			if strings.HasPrefix(a, "--certificate-") && i+1 < len(args) {
				if _, dup := flag[a]; dup {
					problems = append(problems, "verify-blob repeats "+a)
				}
				flag[a] = args[i+1]
			}
		}
		for f, want := range map[string]string{"--certificate-identity-regexp": `"$SIGNER_IDENTITY"`, "--certificate-oidc-issuer": `"$SIGNER_ISSUER"`} {
			if flag[f] != want {
				problems = append(problems, "verify-blob "+f+" is "+strconv.Quote(flag[f])+", want "+want)
			}
		}
		if len(flag) != 2 {
			problems = append(problems, "verify-blob has certificate flags other than the identity and issuer")
		}
	}
	if n := len(readelfChecks.FindAllString(wf, -1)); n != 1 || strings.Count(wf, "readelf -") != 1 {
		problems = append(problems, "want exactly one readelf check that fails on a readelf error and on a program interpreter")
	}
	if !regexp.MustCompile(`(?m)^\s*flags=\(\)\n\s*if \[ "\$PRERELEASE" = true \]; then\n\s*flags\+=\(--prerelease --latest=false\)\n\s*fi\n\s*gh release create [^\n]*\\\n(?:[^\n]*\\\n)*[^\n]* "\$\{flags\[@\]\}"\n`).MatchString(wf) {
		problems = append(problems, `want flags=(), if [ "$PRERELEASE" = true ]; then flags+=(--prerelease --latest=false); fi, and gh release create ... "${flags[@]}"`)
	}
	return problems
}
