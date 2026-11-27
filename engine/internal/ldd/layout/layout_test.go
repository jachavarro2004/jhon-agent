package layout

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The layouts are named in this package alone (T57b, T57c): no other
// non-test engine file builds an openspec/ or ldd/ path itself, whole or split.
func TestOpenSpecNamedOnlyHere(t *testing.T) {
	// A config is read through the layout (T60): no path is built from the
	// vocabulary's file key or a "config.yaml" literal.
	// "ldd" alone and "ldd/<check>" name commands and audit checks, not the
	// layout: only its own files, and a "ldd" joined into a path, are pinned.
	named := regexp.MustCompile("\"openspec(/[^\"]*)?\"|(^|[(=,:]\\s*)`\\^?(openspec|ldd/(config|changes|specs))|\"open\"\\s*\\+\\s*\"spec|\"ldd/(config\\.yaml|changes|specs)[^\"]*\"|Join\\([^)]*\"ldd\"|(lazyre\\.New|regexp\\.MustCompile)\\([^)]*(openspec|ldd)/|(Join|ReadFile|Exists|Stat)\\(.*(keys\\[\"file\"\\]|\"config\\.yaml\")")
	engine := filepath.Join("..", "..", "..")
	var found []string
	err := filepath.WalkDir(engine, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		if filepath.Base(filepath.Dir(p)) == "layout" && filepath.Base(filepath.Dir(filepath.Dir(p))) == "ldd" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if named.MatchString(line) {
				found = append(found, p+":"+strconv.Itoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Errorf("an openspec path named outside ldd/layout (ask layout.For(root) instead): %s", strings.Join(found, ", "))
	}
}

func TestOpenSpecPaths(t *testing.T) {
	l := For("/any")
	for got, want := range map[string]string{
		l.Name():                        "openspec",
		l.Dir():                         "openspec",
		l.Config():                      "openspec/config.yaml",
		l.ChangesDir():                  "openspec/changes",
		l.ChangeDir("c1"):               "openspec/changes/c1",
		l.Artifact("c1", "tasks.md"):    "openspec/changes/c1/tasks.md",
		l.ArchiveDir():                  "openspec/changes/archive",
		l.Archive("2026-01-02", "c1"):   "openspec/changes/archive/2026-01-02-c1",
		l.SpecsDir():                    "openspec/specs",
		At("/r", "openspec/changes/c1"): filepath.Join("/r", "openspec", "changes", "c1"),
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	for rel, want := range map[string]bool{"openspec/changes/c1/tasks.md": true, "openspec/config.yaml": true, "openspec": false, "src/openspec/x": false, "openspecs/x": false} {
		if l.Ignored(rel) != want {
			t.Errorf("Ignored(%q) = %v", rel, !want)
		}
	}
	dir := t.TempDir()
	if l.Detect(dir) {
		t.Error("detected with no openspec/")
	}
	if err := os.Mkdir(filepath.Join(dir, "openspec"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !l.Detect(dir) {
		t.Error("openspec/ not detected")
	}
}

func TestNativePathsAndDetection(t *testing.T) {
	for got, want := range map[string]string{
		Native.Name():                      "native",
		Native.Config():                    "ldd/config.yaml",
		Native.ChangeDir("c1"):             "ldd/changes/c1",
		Native.Artifact("c1", "tasks.md"):  "ldd/changes/c1/tasks.md",
		Native.Archive("2026-01-02", "c1"): "ldd/changes/archive/2026-01-02-c1",
		Native.SpecsDir():                  "ldd/specs",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	dir := t.TempDir()
	if _, ok := Detected(dir); ok || For(dir) != OpenSpec {
		t.Fatal("no layout: none detected, OpenSpec names the paths")
	}
	os.Mkdir(filepath.Join(dir, "ldd"), 0o755)
	if _, ok := Detected(dir); ok {
		t.Fatal("a folder named ldd alone is no layout")
	}
	os.Mkdir(filepath.Join(dir, "ldd", "changes"), 0o755)
	if l, ok := Detected(dir); !ok || l != Native || For(dir) != Native {
		t.Fatal("ldd/ is the native layout")
	}
	os.Mkdir(filepath.Join(dir, "openspec"), 0o755)
	if For(dir) != OpenSpec {
		t.Fatal("openspec/ comes first")
	}
	if !IgnoredByAny("ldd/changes/c1/tasks.md") || !IgnoredByAny("openspec/x") || IgnoredByAny("src/ldd/x") {
		t.Fatal("IgnoredByAny")
	}
}

// jhon-studio's working output, .jhon/studio/ at any depth (T8g), is no
// code and no test of the project in either layout.
func TestStudioOutputIsIgnored(t *testing.T) {
	for p, want := range map[string]bool{
		".jhon/studio/tour/script.json": true, ".jhon/studio": true, ".jhon/studio/": true, "apps/web/.jhon/studio/tour/out/tour.mp4": true,
		"/work/p/.jhon/studio/tour/review.png": true, ".jhon/studio.yaml": false, ".jhon/studios/x": false, ".jhon/x": false,
		"src/studio/x.go": false, "jhon/studio/x": false, "x.jhon/studio/y": false,
	} {
		if StudioOutput(p) != want {
			t.Errorf("StudioOutput(%q) = %v", p, !want)
		}
		if rel := strings.TrimPrefix(p, "/"); OpenSpec.Ignored(rel) != want || Native.Ignored(rel) != want || IgnoredByAny(rel) != want {
			t.Errorf("Ignored(%q) is not %v in every layout", rel, want)
		}
	}
	if !OpenSpec.Ignored("openspec/changes/c1/tasks.md") || OpenSpec.Ignored("src/app.py") {
		t.Error("the record stays ignored, and code is not")
	}
}
