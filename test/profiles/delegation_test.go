// A profile's delegation mode (T117a): `inline` (the default, never written
// to profiles.json) or `writer`, set with `jhon profile mode`, copied by
// `jhon profile new --from`, and given per profile by `jhon profile json`.
package profiles

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// home is a scratch HOME and the path of its profiles file, holding body.
func home(t *testing.T, body string) (string, string) {
	t.Helper()
	h := t.TempDir()
	file := filepath.Join(h, ".config", "jhon", "profiles.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(body), 0o666); err != nil {
		t.Fatal(err)
	}
	return h, file
}

func run(t *testing.T, h string, args ...string) support.Result {
	t.Helper()
	root := support.Root()
	return support.Jhon(t, "", support.Env("HOME="+h, "XDG_CONFIG_HOME="+filepath.Join(h, ".config"), "JHON_PROFILES=", "JHON_SOURCE="+root, "JHON_ROOT="+root), args...)
}

func want(t *testing.T, r support.Result, code int, pattern string) {
	t.Helper()
	if r.Code != code {
		t.Fatalf("exit %d, want %d\n%s", r.Code, code, r.Out())
	}
	if pattern != "" && !regexp.MustCompile(pattern).MatchString(r.Out()) {
		t.Fatalf("output does not match %q:\n%s", pattern, r.Out())
	}
}

// compact is the file's JSON without whitespace, in its own key order.
func compact(t *testing.T, file string) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(support.Read(t, file))); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func modes(t *testing.T, h string) map[string]string {
	t.Helper()
	r := run(t, h, "profile", "json")
	want(t, r, 0, "")
	var got struct {
		Profiles []struct{ Name, Delegation string }
	}
	if err := json.Unmarshal([]byte(r.Stdout), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, r.Stdout)
	}
	out := map[string]string{}
	for _, p := range got.Profiles {
		out[p.Name] = p.Delegation
	}
	return out
}

func TestDelegationMode(t *testing.T) {
	t.Parallel()

	t.Run("mode writer is written, json carries it, mode inline deletes the key", func(t *testing.T) {
		t.Parallel()
		h, file := home(t, `{"profiles":{"mine":{"tiers":{}}}}`)
		want(t, run(t, h, "profile", "mode", "mine", "writer"), 0, `^profile mine: delegation writer\n$`)
		if got := compact(t, file); got != `{"profiles":{"mine":{"tiers":{},"delegation":"writer"}}}` {
			t.Fatalf("file: %s", got)
		}
		if got := modes(t, h); got["default"] != "inline" || got["mine"] != "writer" {
			t.Fatalf("json delegation: %v", got)
		}
		want(t, run(t, h, "profile", "mode", "mine", "inline"), 0, `^profile mine: delegation inline\n$`)
		if got := compact(t, file); got != `{"profiles":{"mine":{"tiers":{}}}}` {
			t.Fatalf("inline is the default, never written: %s", got)
		}
		if got := modes(t, h); got["mine"] != "inline" {
			t.Fatalf("json delegation: %v", got)
		}
	})

	t.Run("an old file without the key is valid and inline", func(t *testing.T) {
		t.Parallel()
		h, _ := home(t, `{"active":"old","profiles":{"old":{}}}`)
		want(t, run(t, h, "profile", "show", "old"), 0, `profile old`)
		if got := modes(t, h); got["old"] != "inline" {
			t.Fatalf("json delegation: %v", got)
		}
	})

	t.Run("an invalid value is refused and the file is unchanged", func(t *testing.T) {
		t.Parallel()
		body := `{"profiles":{"mine":{}}}`
		h, file := home(t, body)
		r := run(t, h, "profile", "mode", "mine", "full")
		want(t, r, 2, `^jhon profile: delegation 'full' is not one of inline, writer\n`)
		if r.Stdout != "" {
			t.Fatalf("stdout: %q", r.Stdout)
		}
		if got := support.Read(t, file); got != body {
			t.Fatalf("file changed: %s", got)
		}
	})

	t.Run("a hand-written invalid value is a problem", func(t *testing.T) {
		t.Parallel()
		h, _ := home(t, `{"profiles":{"mine":{"delegation":"full"}}}`)
		want(t, run(t, h, "profile", "show", "mine"), 1, `jhon profile: mine\.delegation: 'full' is not one of inline, writer`)
	})

	t.Run("default and unknown profiles are refused; usage errors exit 2", func(t *testing.T) {
		t.Parallel()
		body := `{"profiles":{"mine":{}}}`
		h, file := home(t, body)
		want(t, run(t, h, "profile", "mode", "default", "writer"), 1, `^jhon profile: default is agents.yaml itself and always inline; set the mode on a profile of your own\n$`)
		want(t, run(t, h, "profile", "mode", "nope", "writer"), 1, `^jhon profile: no profile 'nope'; profiles: default, mine\n$`)
		want(t, run(t, h, "profile", "mode", "mine"), 2, `usage: jhon profile .*mode <profile> <inline\|writer>`)
		if got := support.Read(t, file); got != body {
			t.Fatalf("file changed: %s", got)
		}
	})

	t.Run("new --from copies the mode", func(t *testing.T) {
		t.Parallel()
		h, _ := home(t, `{"profiles":{"w":{"delegation":"writer"}}}`)
		want(t, run(t, h, "profile", "new", "copy", "--from", "w"), 0, "")
		want(t, run(t, h, "profile", "new", "fresh"), 0, "")
		if got := modes(t, h); got["copy"] != "writer" || got["fresh"] != "inline" {
			t.Fatalf("json delegation: %v", got)
		}
	})

	t.Run("profile --help documents mode", func(t *testing.T) {
		t.Parallel()
		h, _ := home(t, `{}`)
		want(t, run(t, h, "profile", "--help"), 0, `jhon profile mode <profile> <inline\|writer>`)
	})
}
