// Package ldd holds the conformance tests of the LDD commands: status,
// context, deliver, executions, risk, mutate, init's discovery, projects,
// compose, and the archive move script. They run the jhon binary and judge
// its output and its effects on disk.
package ldd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// TestMain points the binary at this checkout: support.Bin builds it outside
// the tree, where it cannot find its root the way build/jhon does.
func TestMain(m *testing.M) {
	os.Setenv("JHON_ROOT", support.Root())
	os.Exit(m.Run())
}

// makefileWorkspace is fixture 4 of the Strict TDD resolution: a root
// Makefile whose `test-all` target only runs the api tests, beside three
// discovered projects.
func makefileWorkspace() map[string]string {
	return map[string]string{
		"Makefile":              "test-all:\n\tcd api && pytest\n",
		"api/pyproject.toml":    "[project]\nname = \"api\"\n\n[tool.pytest.ini_options]\naddopts = \"-q\"\n",
		"frontend/package.json": `{"scripts":{"test":"vitest run"}}`,
		"worker/go.mod":         "module example.com/worker\n",
	}
}

// vocab is the bracketed list contracts/vocabulary.yaml declares for key.
func vocab(t *testing.T, key string) []string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*\[(.*)\]`).
		FindStringSubmatch(support.Read(t, support.Path("prose", "contracts", "vocabulary.yaml")))
	if m == nil {
		t.Fatalf("vocabulary.yaml declares no %s", key)
	}
	var out []string
	for _, s := range strings.Split(m[1], ",") {
		out = append(out, strings.TrimSpace(s))
	}
	return out
}

// temp is a fresh temporary directory with its symlinks resolved.
func temp(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// tree is a fresh temporary directory holding files, by relative path.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := temp(t)
	support.Write(t, root, files)
	return root
}

// object decodes a JSON object; it fails the test when s is not one.
func object(t *testing.T, s string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, s)
	}
	return v
}

// keys is a JSON object's keys in the order the document writes them.
func keys(t *testing.T, raw []byte) []string {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := d.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("not a JSON object: %s", raw)
	}
	var out []string
	for d.More() {
		tok, err := d.Token()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, tok.(string))
		var skip json.RawMessage
		if err := d.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// field is the raw JSON of one field of a JSON object.
func field(t *testing.T, raw []byte, name string) []byte {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, raw)
	}
	return m[name]
}

// snapshot is every entry under dir with its bytes (and its mode, when
// withMode), directories as a marker, for a before/after comparison.
func snapshot(t *testing.T, dir string, withMode bool) []string {
	t.Helper()
	var out []string
	var walk func(string)
	walk = func(d string) {
		entries, err := os.ReadDir(d)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			p := filepath.Join(d, e.Name())
			info, err := os.Lstat(p)
			if err != nil {
				t.Fatal(err)
			}
			if info.IsDir() {
				out = append(out, p+"/")
				walk(p)
				continue
			}
			entry := p + "\x00" + base64.StdEncoding.EncodeToString([]byte(support.Read(t, p)))
			if withMode {
				entry += "\x00" + info.Mode().String()
			}
			out = append(out, entry)
		}
	}
	walk(dir)
	return out
}

func equal(t *testing.T, got, want any, msg ...any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v %v", got, want, msg)
	}
}

func match(t *testing.T, s, re string, msg ...any) {
	t.Helper()
	if !regexp.MustCompile(re).MatchString(s) {
		t.Errorf("%q does not match %s %v", s, re, msg)
	}
}

func noMatch(t *testing.T, s, re string, msg ...any) {
	t.Helper()
	if regexp.MustCompile(re).MatchString(s) {
		t.Errorf("%q matches %s %v", s, re, msg)
	}
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// anyMatch reports whether some string in list (a decoded JSON array)
// matches re.
func anyMatch(list any, re string) bool {
	items, _ := list.([]any)
	return slices.ContainsFunc(items, func(x any) bool {
		s, _ := x.(string)
		return regexp.MustCompile(re).MatchString(s)
	})
}
