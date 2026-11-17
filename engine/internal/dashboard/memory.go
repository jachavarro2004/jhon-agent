package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// The Memory page's data (L138, answer 1), read only, in the two sources
// the shell's old Memory tab read: the project's file memory, Claude Code's
// `<projects>/<project>/memory/` (MEMORY.md, its index, and one file per
// memory, shared by pi), and Engram, through its local HTTP API
// (ENGRAM_URL, else http://127.0.0.1:7437): /observations/recent, /stats,
// /search and /observations/<id>, GET only, each cut at engramTimeout. A
// project is a directory under projects that a listed session names; a
// memory file is a bare *.md name in its memory directory, a regular file,
// never a path or a link out of it. Engram files a project under its git
// origin's name, else its folder's (the shell's engramProject).

// engramTimeout bounds each request to Engram; a variable for the tests.
var engramTimeout = 3 * time.Second

var (
	projectDir   = lazyre.New(`^[A-Za-z0-9._-]+$`)
	memoryName   = lazyre.New(`^[^./\\\x00][^/\\\x00]*\.md$`)
	indexLine    = lazyre.New(`^\s*[-*]\s+\[([^\]]+)\]\(([^)]+)\)\s*(?:[—–-]\s*(.*))?$`)
	observation  = lazyre.New(`^[0-9]{1,19}$`)
	errProject   = "a project is a directory the sessions name\n"
	errNoProject = "no session names this project\n"
)

// memoryProject is the newest listed session of the project named by
// dir, an encoded directory name; status 0 when there is one, else the
// status and message to answer.
func memoryProject(projects, dir string) (Session, int, string) {
	if !projectDir.MatchString(dir) || dir == "." || dir == ".." {
		return Session{}, 400, errProject
	}
	for _, s := range Sessions(projects, limit, false) {
		if s.Project == dir {
			return s, 0, ""
		}
	}
	return Session{}, 404, errNoProject
}

// memoryFiles is the project's MEMORY.md index: each `- [Title](file.md) — hook` line.
func memoryFiles(projects, dir string) *js.Object {
	mem, ok := memoryDir(projects, dir)
	shown := filepath.Join(projects, dir, "memory")
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(mem, home+string(filepath.Separator)) {
		shown = "~" + shown[len(home):]
	}
	files := []any{}
	b, err := os.ReadFile(filepath.Join(mem, "MEMORY.md"))
	if !ok {
		b, err = nil, os.ErrNotExist
	}
	for _, line := range strings.Split(string(b), "\n") {
		if m := indexLine.FindStringSubmatch(line); m != nil {
			files = append(files, js.Obj("title", m[1], "file", m[2], "hook", strings.TrimSpace(m[3])))
		}
	}
	return js.Obj("project", dir, "dir", shown, "index", err == nil, "files", files)
}

// memoryFile is one memory file's text; status 0 when it could be read.
func memoryFile(projects, dir, name string) (string, int, string) {
	if !memoryName.MatchString(name) || name != filepath.Base(name) {
		return "", 400, "a memory file is a bare *.md name of the project's memory\n"
	}
	mem, ok := memoryDir(projects, dir)
	if !ok {
		return "", 404, "no such memory file\n"
	}
	file := filepath.Join(mem, name)
	st, err := os.Lstat(file)
	if err != nil || !st.Mode().IsRegular() {
		return "", 404, "no such memory file\n"
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", 404, "no such memory file\n"
	}
	return string(b), 0, ""
}

// memoryDir is the project's memory directory when it is a real directory
// under the real projects directory: neither the project's directory nor
// memory is a link, and nothing between resolves out of projects.
func memoryDir(projects, dir string) (string, bool) {
	root, err := filepath.EvalSymlinks(projects)
	if err != nil {
		return "", false
	}
	mem := filepath.Join(root, dir, "memory")
	for _, p := range []string{filepath.Join(root, dir), mem} {
		if st, err := os.Lstat(p); err != nil || !st.IsDir() {
			return "", false
		}
	}
	real, err := filepath.EvalSymlinks(mem)
	if err != nil || real != mem || !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return "", false
	}
	return real, true
}

// errNotJSON is an Engram answer that does not decode.
var errNotJSON = errors.New("Engram answered something that is not JSON")

// engramFailure is the 502 body for an Engram request that failed.
func engramFailure(err error) string {
	if errors.Is(err, errNotJSON) {
		return err.Error() + "\n"
	}
	return "Engram does not answer: " + err.Error() + "\n"
}

// engramGet is one GET of Engram's local API, decoded into out.
func engramGet(route string, out any) error {
	base := os.Getenv("ENGRAM_URL")
	if base == "" {
		base = "http://127.0.0.1:7437"
	}
	ctx, cancel := context.WithTimeout(context.Background(), engramTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+route, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("no answer within %s", engramTimeout)
		}
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		p, _, _ := strings.Cut(route, "?")
		return fmt.Errorf("%s answered %d", p, res.StatusCode)
	}
	if json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(out) != nil {
		return errNotJSON
	}
	return nil
}

// listed is an Engram list: an array, or an object holding it.
type listed []map[string]any

func (l *listed) UnmarshalJSON(b []byte) error {
	var arr []map[string]any
	if json.Unmarshal(b, &arr) == nil {
		*l = arr
		return nil
	}
	var obj map[string][]map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	for _, k := range []string{"observations", "results"} {
		if v, ok := obj[k]; ok {
			*l = v
			return nil
		}
	}
	*l = nil
	return nil
}

// engramList is the project's observations, the newest or those q finds,
// each without its content, and the project's total.
func engramList(project, q string) (*js.Object, error) {
	p := url.QueryEscape(project)
	var items listed
	route := "/observations/recent?project=" + p + "&limit=40"
	if q != "" {
		route = "/search?q=" + url.QueryEscape(q) + "&project=" + p + "&limit=30"
	}
	if err := engramGet(route, &items); err != nil {
		return nil, err
	}
	var stats struct {
		Total *float64 `json:"total_observations"`
	}
	if err := engramGet("/stats?project="+p, &stats); err != nil {
		return nil, err
	}
	out := []any{}
	for _, o := range items {
		out = append(out, js.Obj("id", toValue(o["id"]), "type", toValue(o["type"]), "title", toValue(o["title"]), "created_at", toValue(o["created_at"])))
	}
	var query, total any
	if q != "" {
		query = q
	}
	if stats.Total != nil {
		total = *stats.Total
	}
	return js.Obj("project", project, "total", total, "query", query, "observations", out), nil
}

// engramOne is one observation whole.
func engramOne(id string) (map[string]any, error) {
	var o map[string]any
	if err := engramGet("/observations/"+id, &o); err != nil {
		return nil, err
	}
	if inner, ok := o["observation"].(map[string]any); ok {
		return inner, nil
	}
	return o, nil
}

// engramProjectFor is the Engram project of s's project: its working
// directory's (engramProjectOf), else the project's readable name.
func engramProjectFor(s Session) string {
	if p := engramProjectOf(sessionCwd(s.Path)); p != "" {
		return p
	}
	return s.Name
}

// engramProjectOf is the name Engram files the repository at cwd under:
// its origin's name (git remote get-url origin, read-only, cut at 2 s),
// else its root folder's; "" for no cwd.
func engramProjectOf(cwd string) string {
	if cwd == "" {
		return ""
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return filepath.Base(cwd)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "remote", "get-url", "origin")
	cmd.Env = project.GitReadEnv()
	if out, err := cmd.Output(); err == nil {
		if name := originName(string(out)); name != "" {
			return name
		}
	}
	return filepath.Base(discover.FindRoot(cwd))
}

// originName is a remote URL's repository name: its last segment without .git.
func originName(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.LastIndexAny(u, "/:"); i >= 0 {
		u = u[i+1:]
	}
	return strings.TrimSuffix(path.Clean("/" + u)[1:], ".git")
}
