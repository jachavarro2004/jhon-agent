// Package guards is the conformance suite for the hooks that guard a
// session's tool calls (safety-guard, vcs-guard, framework-guard,
// scope-guard, inspect-guard, context-notice, return-shape) and for
// `jhon grant`, which writes the grants the guards read. Each guard is
// spawned as Claude Code runs it: `jhon hook <name> [args]` with the
// payload on stdin; exit 0 allows, exit 2 blocks.
package guards

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// js is v as JSON, with no HTML escaping.
func js(t testing.TB, v any) string {
	t.Helper()
	if s, ok := v.(string); ok {
		return s
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// hook runs `jhon hook <name> <args>` with stdin (a string as is, anything
// else as JSON) and env (support.Env() when nil).
func hook(t testing.TB, env []string, name string, args []string, stdin any) support.Result {
	t.Helper()
	if env == nil {
		env = support.Env()
	}
	return support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "hook", name}, args...), Env: env, Stdin: js(t, stdin)})
}

// realTemp is a new temporary directory by its real path, removed when t
// ends. Not t.TempDir: its name carries the test's, whose quotes and `$`
// would become part of the commands the guards read.
func realTemp(t testing.TB) string {
	t.Helper()
	d, err := os.MkdirTemp("", "jhon-guards-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	if d, err = filepath.EvalSymlinks(d); err != nil {
		t.Fatal(err)
	}
	return d
}

func mkdir(t testing.TB, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	return dir
}

func symlink(t testing.TB, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func rootKey(root string) string {
	h := sha256.Sum256([]byte(root))
	return hex.EncodeToString(h[:])
}

// writeGrant writes a grant record of cls for root under grants, expiring
// in minutes (negative: already expired), with extra fields over it.
func writeGrant(t testing.TB, grants, cls, root string, minutes int, extra map[string]any) {
	t.Helper()
	now := time.Now().UTC()
	rec := map[string]any{
		"class": cls, "root": root,
		"granted_at": now.Format("2006-01-02T15:04:05.000Z"),
		"expires_at": now.Add(time.Duration(minutes) * time.Minute).Format("2006-01-02T15:04:05.000Z"),
	}
	for k, v := range extra {
		rec[k] = v
	}
	support.Write(t, filepath.Join(grants, rootKey(root)), map[string]string{cls + ".json": js(t, rec)})
}

// decision is a top-level call's printed permission decision and reason;
// ok is false when it printed none.
func decision(t testing.TB, out string) (d, why string, ok bool) {
	t.Helper()
	if strings.TrimSpace(out) == "" {
		return "", "", false
	}
	var j struct {
		HookSpecificOutput struct {
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		t.Fatalf("decision: %v: %q", err, out)
	}
	return j.HookSpecificOutput.PermissionDecision, j.HookSpecificOutput.PermissionDecisionReason, true
}

func with(m map[string]any, kv ...any) map[string]any {
	out := make(map[string]any, len(m)+len(kv)/2)
	for k, v := range m {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}
