package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

const prompt = "Apply task 1.1.\n\n## Allowed edit surfaces\n- `src/a.ts`\n- src/parse/\n- svc/src/**: the service\n\nReturn the fixed shape."

func repo(t *testing.T) string {
	t.Helper()
	d, _ := filepath.EvalSymlinks(t.TempDir())
	os.Mkdir(filepath.Join(d, ".git"), 0o755)
	return d
}

func TestSurfacesOf(t *testing.T) {
	for p, want := range map[string][]string{
		prompt: {"src/a.ts", "src/parse/", "svc/src/**"},
		"## Allowed edit surfaces\nbackend/x.py\nbackend/y/**\n\nWrite nothing else, no code.\n- late.ts": {"backend/x.py", "backend/y/**"},
		"## Allowed edit surfaces\n\n## Next\n- src/a.ts":                                                 {},
	} {
		if got, ok := SurfacesOf(p); !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("SurfacesOf(%q) = %q, %v; want %q", p, got, ok, want)
		}
	}
	if _, ok := SurfacesOf("Fix the bug in src/a.ts."); ok {
		t.Error("no heading read as a scope")
	}
}

func TestInSurfaces(t *testing.T) {
	r := repo(t)
	s := []string{"src/a.ts", "src/parse/", "svc/src/**", "lib/*.py"}
	for _, ok := range []string{"src/a.ts", "src/parse/deep/b.ts", "svc/src/x/y.py", "lib/m.py", "tests/test_a.py", "src/a.test.ts", "openspec/changes/c1/tasks.md"} {
		if !InSurfaces(filepath.Join(r, ok), s) {
			t.Errorf("%s is out of scope", ok)
		}
	}
	for _, no := range []string{"src/b.ts", "src/a.ts.bak", "svc/scripts/seed.py", "lib/sub/m.py", "package.json"} {
		if InSurfaces(filepath.Join(r, no), s) {
			t.Errorf("%s is in scope", no)
		}
	}
	if !InSurfaces(filepath.Join(t.TempDir(), "scratch.md"), s) {
		t.Error("a path outside any repository is out of scope")
	}
}

func TestWriteGroups(t *testing.T) {
	files := WrittenFiles("cd svc && sed -i 's/a/b/' src/a.py; cat >> docs/n.md <<'E'\nx\nE\ncp src/a.py src/b.py; git mv src/b.py src/c.py; rg foo src/q.py; uv run pytest", "/r")
	slices.Sort(files)
	if want := []string{"/r/svc/docs/n.md", "/r/svc/src/a.py", "/r/svc/src/b.py", "/r/svc/src/c.py"}; !reflect.DeepEqual(files, want) {
		t.Errorf("WrittenFiles = %q, want %q", files, want)
	}
	g := WriteGroups("python3 - <<'E'\np = 'src/a.py'\nopen(p, 'w').write(open('docs/ref.md').read().replace('a', 'b'))\nE", "/r")
	want := WriteGroup{Files: []string{"/r/src/a.py", "/r/docs/ref.md"}, Code: true, Targets: []string{"/r/src/a.py", "/r/docs/ref.md"}}
	if len(g) != 1 || !reflect.DeepEqual(g[0], want) {
		t.Errorf("WriteGroups(code) = %+v", g)
	}
	if g := WriteGroups(`python3 -c "print(open('src/a.py').read())"`, "/r"); len(g) != 0 {
		t.Errorf("code that only reads writes %+v", g)
	}
}

func TestWritesOutside(t *testing.T) {
	r := repo(t)
	s := []string{"src/a.py"}
	if got := WritesOutside("sed -i 's/x/y/' src/b.py", r, s); !reflect.DeepEqual(got, []string{filepath.Join(r, "src/b.py")}) {
		t.Errorf("out of scope: %q", got)
	}
	for _, cmd := range []string{
		"sed -i 's/x/y/' src/a.py",
		"python3 - <<'E'\nopen('src/a.py','w').write(open('docs/ref.md').read())\nE",
		"python3 - <<'E'\nS = \"" + t.TempDir() + "\"\nopen(S + \"/out.md\", \"w\").write(open(\"docs/spec.md\").read())\nE",
	} {
		if got := WritesOutside(cmd, r, s); len(got) != 0 {
			t.Errorf("WritesOutside(%q) = %q", cmd, got)
		}
	}
	if got := WritesOutside("python3 - <<'E'\nopen('scripts/seed.py','w').write('x')\nE", r, s); !reflect.DeepEqual(got, []string{filepath.Join(r, "scripts/seed.py")}) {
		t.Errorf("code naming only an outside file: %q", got)
	}
}

// The Bash forms a writer edits files through, each read as the files it
// writes: sed -i (with or without a backup suffix, several files), a
// redirection of stdout (never another descriptor, never /dev/null), tee,
// a heredoc into a file, cp and mv targets, and perl -pi. A script whose
// target is not on the command line is not read.
func TestWrittenFilesForms(t *testing.T) {
	for cmd, want := range map[string][]string{
		"sed -i 's/a/b/' src/a.py":                         {"/r/src/a.py"},
		"sed -i.bak 's/a/b/' src/a.py src/b.py":            {"/r/src/a.py", "/r/src/b.py"},
		"sed -i '' -e 's/a/b/' -e 's/c/d/' src/a.py":       {"/r/src/a.py"},
		"sed --in-place=.orig -E 's/x.y/z.w/' Makefile":    {"/r/Makefile"},
		"sed -n 's/a/b/p' src/a.py":                        nil,
		"echo x > src/a.py":                                {"/r/src/a.py"},
		"echo x >> docs/notes":                             {"/r/docs/notes"},
		"make 2> build.log; ls 2>/dev/null; ls >/dev/null": nil,
		"go test ./... &> out/test.log":                    {"/r/out/test.log"},
		"echo x | tee src/a.py":                            {"/r/src/a.py"},
		"echo x | tee -a src/a.py src/b.py >/dev/null":     {"/r/src/a.py", "/r/src/b.py"},
		"cat > src/Dockerfile <<'EOF'\nFROM x\nEOF":        {"/r/src/Dockerfile"},
		"cat >src/a.py <<EOF\nx\nEOF":                      {"/r/src/a.py"},
		"cp src/a.py src/b.py":                             {"/r/src/b.py"},
		"cp -r src/a.py src/b.py lib/":                     {"/r/lib/a.py", "/r/lib/b.py"},
		"cp -t lib/ src/a.py":                              {"/r/lib/a.py"},
		"mv src/a.py src/c.py":                             {"/r/src/c.py"},
		"perl -pi -e 's/a/b/' src/a.py src/b.py":           {"/r/src/a.py", "/r/src/b.py"},
		"perl -i.bak -pe 's/a/b/' src/a.py":                {"/r/src/a.py"},
		"perl -ne 'print' src/a.py":                        nil,
		"python3 /tmp/fix.py":                              nil,
		"node scripts/gen.js --out src/a.ts":               nil,
	} {
		got := WrittenFiles(cmd, "/r")
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("WrittenFiles(%q) = %q, want %q", cmd, got, want)
		}
	}
}

// Each recognised form is judged against the surfaces: in scope it passes,
// on a file outside them it is named.
func TestWritesOutsideForms(t *testing.T) {
	r := repo(t)
	s := []string{"src/**"}
	for _, form := range []string{
		"sed -i 's/a/b/' %s",
		"sed -i.bak -e 's/a/b/' %s",
		"echo x > %s",
		"echo x >> %s",
		"echo x | tee -a %s",
		"cat > %s <<'EOF'\nx\nEOF",
		"cp README.md %s",
		"mv /tmp/x.py %s",
		"perl -pi -e 's/a/b/' %s",
	} {
		if got := WritesOutside(fmt.Sprintf(form, "src/a.py"), r, s); len(got) != 0 {
			t.Errorf("%q in scope: %q", form, got)
		}
		if got := WritesOutside(fmt.Sprintf(form, "lib/z.py"), r, s); !slices.Equal(got, []string{filepath.Join(r, "lib/z.py")}) {
			t.Errorf("%q outside the surfaces: %q", form, got)
		}
	}
	if got := WritesOutside("python3 /tmp/fix.py", r, s); len(got) != 0 {
		t.Errorf("a script whose target cannot be read is not judged: %q", got)
	}
}

// curl's and wget's downloads: a bundled -o / -O, a file named from the URL,
// an output directory; a device, stdout, and wget's log (-o) are not counted.
func TestDownloadWrites(t *testing.T) {
	for cmd, want := range map[string][]string{
		"curl -sSLo g.sh u":                       {"/p/g.sh"},
		"curl -sSLog.sh u":                        {"/p/g.sh"},
		"wget -qO g.sh u":                         {"/p/g.sh"},
		"wget -qOg.sh u":                          {"/p/g.sh"},
		"curl -O http://x/a/g.sh":                 {"/p/g.sh"},
		"curl -sSLO http://x/a/g.sh":              {"/p/g.sh"},
		"curl --remote-name http://x/g.sh":        {"/p/g.sh"},
		"wget http://x/g.sh?x=1":                  {"/p/g.sh"},
		"curl --output-dir d -o g.sh u":           {"/p/d/g.sh"},
		"curl -o g.sh --output-dir d u":           {"/p/d/g.sh"},
		"curl --output-dir d -O http://x/g.sh":    {"/p/d/g.sh"},
		"wget -P d http://x/g.sh":                 {"/p/d/g.sh"},
		"wget --directory-prefix=d http://x/g.sh": {"/p/d/g.sh"},
		"wget -o log.txt http://x/g.sh":           {"/p/g.sh"},
		"wget -qo log http://x/":                  nil,
		"wget http://x/a/":                        nil,
		"curl u":                                  nil,
		"curl -o /dev/null u":                     nil,
		"curl -o - u":                             nil,
		"wget -O - u":                             nil,
		"dd if=a of=/dev/null":                    nil,
		"curl -o a -o b u":                        {"/p/a", "/p/b"},
	} {
		got := WrittenFiles(cmd, "/p")
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("WrittenFiles(%q) = %q, want %q", cmd, got, want)
		}
	}
}
