package render

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// repoRoot is the checkout under test: the tree every render reads unless a
// test renders a copy.
var repoRoot = support.Root()

var (
	sharedMu   sync.Mutex
	sharedDirs []string
)

func TestMain(m *testing.M) {
	code := m.Run()
	for _, d := range sharedDirs {
		os.RemoveAll(d)
	}
	os.Exit(code)
}

// sharedDir is a directory that outlives the test creating it, for a
// read-only fixture several tests share; TestMain removes it.
func sharedDir(t testing.TB, prefix string) string {
	t.Helper()
	d, err := os.MkdirTemp("", prefix)
	if err != nil {
		t.Fatal(err)
	}
	if d, err = filepath.EvalSymlinks(d); err != nil {
		t.Fatal(err)
	}
	sharedMu.Lock()
	sharedDirs = append(sharedDirs, d)
	sharedMu.Unlock()
	return d
}

// lazyHome is a HOME built once and then only read.
type lazyHome struct {
	once sync.Once
	home string
	err  string
}

func (l *lazyHome) get(t *testing.T, build func(home string) string) string {
	t.Helper()
	l.once.Do(func() {
		l.home = sharedDir(t, "jhon-render-shared-")
		l.err = build(l.home)
	})
	if l.err != "" {
		t.Fatal(l.err)
	}
	return l.home
}

// copyTree copies the tree at src to dst, without .git, node_modules,
// .codegraph, or a top-level .claude, and places the test binary at
// dst/build/jhon, as the checkout's own build/jhon would be.
func copyTree(t testing.TB, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == ".codegraph" || rel == ".claude") {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == ".git" {
			return nil
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(target, b, info.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dst, "build", "jhon")
	if err := os.MkdirAll(filepath.Dir(bin), 0o777); err != nil {
		t.Fatal(err)
	}
	os.Remove(bin)
	if os.Link(support.Bin(t), bin) == nil {
		return
	}
	in, err := os.Open(support.Bin(t))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(bin, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err == nil {
		_, err = io.Copy(out, in)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		t.Fatal(err)
	}
}

// entries is every entry under dir, relative to it, dotfiles included, sorted.
func entries(t testing.TB, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func write(t testing.TB, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o666); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t testing.TB, p string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(support.Read(t, p)), &v); err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return v
}

// jsonOf is v as JSON decodes it, for comparing with what a file holds.
func jsonOf(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// obj is a JSON object that keeps its keys in order.
type obj []kv

type kv struct {
	k string
	v any
}

func (o obj) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, e := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(e.k)
		v, err := json.Marshal(e.v)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

func sha1Hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func match(t testing.TB, s, re string, msg ...any) {
	t.Helper()
	if !regexp.MustCompile(re).MatchString(s) {
		t.Errorf("%s\nno match for %s in:\n%s", fmt.Sprint(msg...), re, s)
	}
}

func noMatch(t testing.TB, s, re string, msg ...any) {
	t.Helper()
	if regexp.MustCompile(re).MatchString(s) {
		t.Errorf("%s\nunexpected match for %s in:\n%s", fmt.Sprint(msg...), re, s)
	}
}

func wantCode(t testing.TB, r support.Result, want int) {
	t.Helper()
	if r.Code != want {
		t.Fatalf("exit %d, want %d\n%s", r.Code, want, r.Out())
	}
}

func equal(t testing.TB, got, want any, msg ...any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s\ngot  %#v\nwant %#v", fmt.Sprint(msg...), got, want)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func sorted(list []string) []string {
	out := append([]string(nil), list...)
	sort.Strings(out)
	return out
}
