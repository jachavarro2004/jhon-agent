// `jhon dashboard` (plan item 14): a read-only dashboard on a local server.
// It lists the recent Claude Code sessions of every project and serves each
// one's audit as the `jhon audit --html` page, built on request and cached
// while the transcript is unchanged. It binds 127.0.0.1 only, answers GET
// only, and takes a session id only as hex and dashes.
//
// The projects directory is a fixture under a temporary HOME: no test reads
// ~/.claude. The routes, the cache, and the names are unit-tested in
// internal/dashboard.
package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const (
	sessionA = "aaaaaaaa-0000-4000-8000-000000000001"
	sessionB = "bbbbbbbb-0000-4000-8000-000000000002"
)

func projects(t *testing.T, dir string) {
	t.Helper()
	support.Write(t, dir, map[string]string{
		"-home-u-Projects-acme/" + sessionA + ".jsonl": js(t, map[string]any{"type": "user", "isMeta": true, "message": map[string]any{"role": "user", "content": "<local-command-caveat>ignore</local-command-caveat>"}}) + "\n" +
			js(t, map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "<command-name>/clear</command-name>"}}) + "\n" +
			js(t, map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "Fix the soft delete in   the ABC service, then run the tests"}}}}) + "\n",
		"-home-u-jhon/" + sessionB + ".jsonl": "{}\n{}\n",
	})
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "-home-u-jhon", sessionB+".jsonl"), old, old); err != nil {
		t.Fatal(err)
	}
}

type reply struct {
	status     int
	typ, cache string
	body       string
}

func TestServe(t *testing.T) {
	t.Run("`jhon dashboard` listens on 127.0.0.1 with the same routes, and refuses what it does not answer", func(t *testing.T) {
		t.Parallel()
		home, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		projects(t, filepath.Join(home, ".claude", "projects"))
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		l.Close()
		child := exec.Command(support.Bin(t), "dashboard", "--port", strconv.Itoa(port))
		child.Env = jhonEnv("HOME=" + home)
		child.Stderr = os.Stderr
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		stdout, err := child.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() {
			syscall.Kill(-child.Process.Pid, syscall.SIGTERM)
			child.Wait()
		}()
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		equal(t, line, fmt.Sprintf("jhon dashboard: http://127.0.0.1:%d/  (Ctrl-C stops it)\n", port))
		get := func(path, method string) reply {
			t.Helper()
			req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
			if err != nil {
				t.Fatal(err)
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			return reply{res.StatusCode, res.Header.Get("Content-Type"), res.Header.Get("Cache-Control"), string(body)}
		}
		index := get("/", "GET")
		equal(t, []any{index.status, index.typ, index.cache}, []any{200, "text/html; charset=utf-8", "no-store"})
		match(t, index.body, `<script type="module" src="app.js"></script>`)
		equal(t, get("/app.js", "GET").typ, "text/javascript; charset=utf-8")
		var list []struct {
			ID    string  `json:"id"`
			Title *string `json:"title"`
		}
		if err := json.Unmarshal([]byte(get("/api/sessions", "GET").body), &list); err != nil {
			t.Fatal(err)
		}
		var pairs [][2]any
		for _, s := range list {
			var title any
			if s.Title != nil {
				title = *s.Title
			}
			pairs = append(pairs, [2]any{s.ID, title})
		}
		equal(t, pairs, [][2]any{{sessionA, "Fix the soft delete in the ABC service, then run the tests"}, {sessionB, nil}})
		equal(t, get("/", "POST"), reply{405, "text/plain; charset=utf-8", "no-store", "GET only\n"})
		equal(t, get("/api/session/..%2F..%2Fetc%2Fpasswd", "GET").body, "a session id is hex and dashes\n")
		equal(t, get("/api/session/cccccccc-0000-4000-8000-000000000003", "GET").body, "no such session\n")
		// The old page's address is gone (T17b, D8).
		equal(t, get("/session/"+sessionA, "GET"), reply{404, "text/plain; charset=utf-8", "no-store", "not found\n"})
		match(t, get("/api/session/"+sessionA, "GET").body, `^\{"session":\{"id":"`+sessionA+`"`)
		equal(t, get("/nowhere", "GET").body, "not found\n")
	})

	t.Run("`jhon dashboard` takes only --port <digits>", func(t *testing.T) {
		t.Parallel()
		for _, args := range [][]string{{"--port"}, {"--port", "abc"}, {"--bogus"}} {
			r := support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "dashboard"}, args...), Env: jhonEnv()})
			equal(t, r.Code, 2, strings.Join(args, " "))
			equal(t, r.Stderr, "usage: jhon dashboard [--port <n>]   (127.0.0.1 only; read-only)\n")
		}
	})
}
