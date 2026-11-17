package dashboard

import (
	"encoding/json"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/telemetry"
)

// `jhon dashboard`: a read-only dashboard on a local server. The page is the
// dashboard component (the release's dashboard/, its CONTRACT.md), served
// as files; this serves its data. `/api/sessions` lists the recent Claude
// Code sessions of every project under ~/.claude/projects, newest first,
// each by its project's readable name and its first request;
// `/api/session/<id>` is that session's metadata and its view (View) of
// `jhon audit --json --context`; `/api/changes`, every change those
// sessions name (changes.go); `/api/usage`, their usage per day
// (usagestats.go). An audit runs on request and is cached
// while the transcript's size and mtime are unchanged, or until a request
// asks for a fresh one (`?fresh=1`).
//
// `/api/memory/*` is a project's memory, read only (memory.go).
// `/api/version` is the build this server runs (process.go, T15.16).
//
// It answers GET only, writes nothing, and runs nothing but the audit and,
// for the memory, `git remote get-url origin` and GETs to Engram's local API. A
// session id is hex and dashes, else 400; one with no transcript is 404.
// Each session in the list carries its summary (summary.go) once one matches
// its transcript, from this server's audits or the background ones.

// limit is how many sessions the index lists.
const limit = 80

var sessionID = lazyre.New(`(?i)^[0-9a-f-]{6,64}$`)

// A Session is one main transcript. Path is left out of the JSON.
type Session struct {
	ID       string  `json:"id"`
	Project  string  `json:"project"`
	Name     string  `json:"name"`
	Title    *string `json:"title"`
	Path     string  `json:"-"`
	Modified string  `json:"modified"`
	Bytes    int64   `json:"bytes"`
	// Runtime is the agent the session ran in: claude-code, or pi.
	Runtime string `json:"runtime"`
	// Entrypoint is how Claude Code was started, as its transcript records
	// it: cli (the owner, interactive), sdk-ts (an SDK, such as pi's model
	// bridge), sdk-cli (claude -p); "" for pi or when the head names none.
	Entrypoint string `json:"entrypoint,omitempty"`
	// Summary is its audit in a few numbers (SummaryOf), once one matches it.
	Summary json.RawMessage `json:"summary,omitempty"`
}

var (
	scratch    = lazyre.New(`^-tmp-` + js.Dot + `*?[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}-scratchpad-?(` + js.Dot + `*)$`)
	homePrefix = lazyre.New(`^-home-[^-]+-`)
	dashes     = lazyre.New(`^-+`)
	folders    = lazyre.New(`^(Projects-)?(work-)?`)
)

// ProjectName is a project directory's readable name: Claude Code encodes the
// project path with dashes, so the home prefix and the usual
// `Projects-`/`work-` folders are dropped (`-home-u-Projects-work-acme-app`
// is `acme-app`).
func ProjectName(dir string) string {
	// A scratch directory under /tmp is named by its session id; what follows it is the useful part.
	if m := scratch.FindStringSubmatch(dir); m != nil {
		if m[1] != "" {
			return "scratch " + m[1]
		}
		return "scratch"
	}
	name := homePrefix.ReplaceAllLiteralString(dir, "")
	name = dashes.ReplaceAllLiteralString(name, "")
	name = folders.ReplaceAllLiteralString(name, "")
	if name == "" {
		return dir
	}
	return name
}

// titleBytes is how much of a transcript the title is looked for in.
const titleBytes = 256 * 1024

var (
	pasted      = lazyre.New(`</?pasted_content[^>]{0,80}>`)
	boxLines    = lazyre.New(`[\x{2500}-\x{257f}]+`)
	spaces      = lazyre.New(js.Space + `+`)
	notARequest = lazyre.New(`^<(command-|local-command|system-reminder|bash-|task-notification)`)
	titleLen    = 120 // in UTF-16 units, as String.prototype.length
)

// SessionEntrypoint is the first entrypoint a Claude Code transcript's head
// records, "" for none.
func SessionEntrypoint(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	buf := make([]byte, titleBytes)
	n, _ := io.ReadFull(f, buf)
	f.Close()
	for _, line := range strings.Split(js.DecodeUTF8(buf[:n]), "\n") {
		if !strings.Contains(line, `"entrypoint"`) {
			continue
		}
		var o struct {
			Entrypoint string `json:"entrypoint"`
		}
		if json.Unmarshal([]byte(line), &o) == nil && o.Entrypoint != "" {
			return o.Entrypoint
		}
	}
	return ""
}

// SessionTitle is the session's first request, as the owner typed it: the
// first user message that is not meta, a command, or a command's output,
// whitespace collapsed, cut at 120 characters; nil when the transcript's head
// has none.
func SessionTitle(path string) *string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	buf := make([]byte, titleBytes)
	n, err := io.ReadFull(f, buf)
	f.Close()
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil
	}
	for _, line := range strings.Split(js.DecodeUTF8(buf[:n]), "\n") {
		if !strings.Contains(line, `"user"`) {
			continue
		}
		var o any
		if json.Unmarshal([]byte(line), &o) != nil {
			continue
		}
		m, _ := o.(map[string]any)
		msg, _ := m["message"].(map[string]any)
		// Claude Code's user entry, or pi's message entry whose role is user.
		claude := m != nil && m["type"] == "user" && !js.Truthy(jsonValue(m["isMeta"])) && !js.Truthy(jsonValue(m["isSidechain"]))
		pi := m != nil && m["type"] == "message" && msg != nil && msg["role"] == "user"
		if !claude && !pi {
			continue
		}
		t := pasted.ReplaceAllLiteralString(requestText(msg["content"]), " ")
		t = boxLines.ReplaceAllLiteralString(t, " ")
		t = js.Trim(spaces.ReplaceAllLiteralString(t, " "))
		if t == "" || notARequest.MatchString(t) {
			continue
		}
		if u := utf16.Encode([]rune(t)); len(u) > titleLen {
			t = string(utf16.Decode(u[:titleLen-1])) + "…"
		}
		return &t
	}
	return nil
}

// requestText is a message's text: the string, or its text blocks joined by a space.
func requestText(c any) string {
	switch x := c.(type) {
	case string:
		return x
	case []any:
		var parts []string
		for _, b := range x {
			if m, ok := b.(map[string]any); ok && m["type"] == "text" {
				switch t := m["text"].(type) {
				case nil:
					parts = append(parts, "")
				case map[string]any:
					parts = append(parts, "[object Object]")
				default:
					parts = append(parts, js.String(t))
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// jsonValue is a value encoding/json decoded as the dashboard's own values
// hold it, for truthiness.
func jsonValue(v any) any {
	if _, ok := v.(map[string]any); ok {
		return js.Obj()
	}
	return v
}

// PiSessions is where Jhon's pi sessions are kept (~/.pi/jhon/sessions,
// one directory per working directory); "" lists Claude Code's only.
var PiSessions string

// piFile is a pi session's file name: its start and its id.
var piFile = lazyre.New(`^[0-9T:.-]+Z_([0-9a-f-]+)\.jsonl$`)

// notWord is what Claude Code turns into a dash when it names a project's directory.
var notWord = lazyre.New(`[^A-Za-z0-9]`)

// Sessions is every main transcript under projects (Claude Code's) and
// PiSessions (pi's), newest first, at most max; each with its title when
// withTitles. A pi session's project is named as Claude Code would name
// its directory, so one project groups both runtimes' sessions.
func Sessions(projects string, max int, withTitles bool) []Session {
	out := sessionsIn(projects, "claude-code", func(dir, name string) (string, string, bool) {
		if !strings.HasSuffix(name, ".jsonl") {
			return "", "", false
		}
		return strings.TrimSuffix(name, ".jsonl"), dir, true
	})
	if PiSessions != "" {
		out = append(out, sessionsIn(PiSessions, "pi", func(dir, name string) (string, string, bool) {
			m := piFile.FindStringSubmatch(name)
			if m == nil {
				return "", "", false
			}
			// --home-u-Projects-work-acme_app-- is /home/u/Projects/work/acme_app.
			return m[1], notWord.ReplaceAllLiteralString("/"+strings.Trim(dir, "-"), "-"), true
		})...)
	}
	slices.SortStableFunc(out, func(a, b Session) int { return strings.Compare(b.Modified, a.Modified) })
	out = out[:min(len(out), max)]
	if withTitles {
		for i := range out {
			out[i].Title = SessionTitle(out[i].Path)
			if out[i].Runtime == "claude-code" {
				out[i].Entrypoint = SessionEntrypoint(out[i].Path)
			}
		}
	}
	return out
}

// sessionsIn is the transcripts one level below root's directories; name
// maps a directory and a file name to the session id and its project's
// directory name, ok false for a file that is not a session.
func sessionsIn(root, runtime string, name func(dir, file string) (id, project string, ok bool)) []Session {
	entries, err := os.ReadDir(root)
	if err != nil {
		return []Session{}
	}
	out := []Session{}
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		names, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range names {
			id, project, ok := name(e.Name(), f.Name())
			if !ok {
				continue
			}
			path := filepath.Join(dir, f.Name())
			st, err := os.Stat(path)
			if err != nil {
				continue
			}
			out = append(out, Session{
				ID: id, Project: project, Name: ProjectName(project), Runtime: runtime,
				Path: path, Modified: isodate.Format(mtime(st.ModTime())), Bytes: st.Size(),
			})
		}
	}
	return out
}

// mtime is a file's mtime as node's stat gives it: milliseconds, rounded
// from sec*1e3 + nsec/1e6 in floating point.
func mtime(t time.Time) time.Time {
	ms := round(float64(t.Unix())*1e3 + float64(t.Nanosecond())/1e6)
	return time.UnixMilli(int64(ms))
}

// An Auditor audits one transcript: the parsed `jhon audit --json --context`.
type Auditor func(path string) any

var sessionRoute = lazyre.New(`^/api/session/(` + js.Dot + `+)$`)

// Server is the dashboard's handler over the sessions under projects,
// auditing each with audit, serving the component's files from assets; the
// caller listens.
func Server(projects string, audit Auditor, assets string, summaries *Summaries) http.Handler {
	type cached struct {
		key    string
		report any
	}
	var mu sync.Mutex
	cache := map[string]cached{}
	reportOf := func(s Session) any {
		key := strconv.FormatInt(s.Bytes, 10) + ":" + s.Modified
		mu.Lock()
		hit, ok := cache[s.ID]
		mu.Unlock()
		if ok && hit.key == key {
			return hit.report
		}
		report := audit(s.Path)
		mu.Lock()
		cache[s.ID] = cached{key, report}
		mu.Unlock()
		summaries.Put(s, View(report))
		return report
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		send := func(status int, typ, body string) {
			h := w.Header()
			h.Set("Content-Type", typ+"; charset=utf-8")
			h.Set("Cache-Control", "no-store")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Jhon-Dashboard", "1")
			w.WriteHeader(status)
			io.WriteString(w, body)
		}
		if r.Method != http.MethodGet {
			send(405, "text/plain", "GET only\n")
			return
		}
		path, ok := pathname(r.URL)
		if !ok {
			send(400, "text/plain", "bad request\n")
			return
		}
		if strings.HasPrefix(path, "/api/") {
			if why := foreign(r); why != "" {
				send(403, "text/plain", why)
				return
			}
		}
		if f, ok := assetFiles[path]; ok {
			b, err := os.ReadFile(filepath.Join(assets, f.name))
			if err != nil {
				send(500, "text/plain", "the dashboard component is missing: "+f.name+"; run `jhon sync`\n")
				return
			}
			send(200, f.typ, string(b))
			return
		}
		switch path {
		case "/palette.css":
			send(200, "text/css", paletteCSS())
			return
		case "/api/telemetry":
			days, err := strconv.Atoi(r.URL.Query().Get("days"))
			if err != nil {
				days = 7
			}
			send(200, "application/json", telemetry.Last(days).JSON())
			return
		case "/api/sessions":
			list := Sessions(projects, limit, true)
			for i := range list {
				list[i].Summary, _ = summaries.Get(list[i])
			}
			body, _ := js.StringifyCompact(list)
			send(200, "application/json", body)
			return
		case "/api/usage":
			q := r.URL.Query()
			from, to, ok := usageRange(q.Get("from"), q.Get("to"))
			if !ok {
				send(400, "text/plain", "from and to are days, YYYY-MM-DD, from not after to\n")
				return
			}
			list := Sessions(projects, limit, true)
			for i := range list {
				list[i].Summary, _ = summaries.Get(list[i])
			}
			u := UsageAcross(list, from, to, q.Get("project"))
			u.Listed, u.Total = len(list), len(Sessions(projects, math.MaxInt, false))
			u.Capped = u.Total > u.Listed
			body, _ := json.Marshal(u)
			send(200, "application/json", string(body))
			return
		case "/api/integrations":
			send(200, "application/json", js.StringifyValueCompact(integrationRows(r.URL.Query().Get("probe") == "1")))
			return
		case "/api/version":
			b, _ := json.Marshal(Build)
			send(200, "application/json", string(b))
			return
		case "/api/coverage":
			listed, total := len(Sessions(projects, limit, false)), len(Sessions(projects, math.MaxInt, false))
			send(200, "application/json", js.StringifyValueCompact(js.Obj("listed", float64(listed), "total", float64(total), "capped", total > listed)))
			return
		case "/api/changes":
			list := Sessions(projects, limit, true)
			for i := range list {
				list[i].Summary, _ = summaries.Get(list[i])
			}
			send(200, "application/json", js.StringifyValueCompact(changesOf(list, changeLdd)))
			return
		}
		switch path {
		case "/api/memory/files", "/api/memory/file", engramRoute:
			q := r.URL.Query()
			s, status, msg := memoryProject(projects, q.Get("project"))
			if status != 0 {
				send(status, "text/plain", msg)
				return
			}
			switch path {
			case "/api/memory/files":
				send(200, "application/json", js.StringifyValueCompact(memoryFiles(projects, s.Project)))
			case "/api/memory/file":
				text, status, msg := memoryFile(projects, s.Project, q.Get("name"))
				if status != 0 {
					send(status, "text/plain", msg)
					return
				}
				send(200, "application/json", js.StringifyValueCompact(js.Obj("name", q.Get("name"), "text", text)))
			default:
				list, err := engramList(engramProjectFor(s), js.Trim(q.Get("q")))
				if err != nil {
					send(502, "text/plain", engramFailure(err))
					return
				}
				send(200, "application/json", js.StringifyValueCompact(list))
			}
			return
		}
		if id, ok := strings.CutPrefix(path, engramRoute+"/"); ok {
			if !observation.MatchString(id) {
				send(400, "text/plain", "an observation id is digits\n")
				return
			}
			o, err := engramOne(id)
			if err != nil {
				send(502, "text/plain", engramFailure(err))
				return
			}
			send(200, "application/json", js.StringifyValueCompact(toValue(o)))
			return
		}
		m := sessionRoute.FindStringSubmatch(path)
		if m == nil {
			send(404, "text/plain", "not found\n")
			return
		}
		if !sessionID.MatchString(m[1]) {
			send(400, "text/plain", "a session id is hex and dashes\n")
			return
		}
		all := Sessions(projects, math.MaxInt, false)
		i := slices.IndexFunc(all, func(s Session) bool {
			return s.ID == m[1] || (strings.HasPrefix(s.ID, m[1]) && len(m[1]) >= 8)
		})
		if i < 0 {
			send(404, "text/plain", "no such session\n")
			return
		}
		s := all[i]
		// ?fresh=1 audits again whatever the cache holds: the page's Re-auditar.
		if r.URL.Query().Get("fresh") == "1" {
			mu.Lock()
			delete(cache, s.ID)
			mu.Unlock()
		}
		send(200, "application/json", js.StringifyValueCompact(SessionData(s, reportOf(s))))
	})
}

// foreign is why a request to the API is refused, "" when it is not: it
// must name this server as 127.0.0.1, localhost or [::1] on the port it
// reached (a page another site loads through DNS rebinding names its own
// host), and an Origin, when it has one, must be that same address.
func foreign(r *http.Request) string {
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	port := ""
	if local != nil {
		_, port, _ = net.SplitHostPort(local.String())
	}
	host, hport, err := net.SplitHostPort(r.Host)
	if err != nil || port == "" || hport != port || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		return "the dashboard answers only at 127.0.0.1, localhost or [::1] on its own port\n"
	}
	if o, ok := r.Header["Origin"]; ok && (len(o) != 1 || o[0] != "http://"+r.Host) {
		return "a request from another origin is refused\n"
	}
	return ""
}

// assetFiles are the component's files the server answers, by path.
var assetFiles = map[string]struct{ name, typ string }{
	"/":        {"index.html", "text/html"},
	"/app.css": {"app.css", "text/css"},
	"/app.js":  {"app.js", "text/javascript"},
}

// SessionData is what the component reads of one session: its metadata,
// the view of its audit report, and the usage its transcripts record. It shows what the session did; the changes'
// state today is the changes page's (changes.go, L138).
func SessionData(s Session, report any) *js.Object {
	runtime := s.Runtime
	if runtime == "" {
		runtime = "claude-code"
	}
	meta := js.Obj("id", s.ID, "project", s.Name, "modified", s.Modified, "runtime", runtime)
	if t := SessionTitle(s.Path); t != nil {
		meta.Set("title", *t)
	}
	return js.Obj("session", meta, "view", View(report), "usage", usageValue(UsageOf(s.Path)))
}

// pathname is decodeURIComponent(new URL(url, base).pathname): the path with
// its dot segments resolved (`%2e` is a dot), then decoded; false where
// decodeURIComponent would throw (an invalid escape or UTF-8), which this
// answers with 400.
func pathname(u *url.URL) (string, bool) {
	raw := strings.ReplaceAll(u.EscapedPath(), `\`, "/")
	if strings.HasPrefix(raw, "//") {
		// `//host/path` is a URL without a scheme: its path is what follows the host.
		_, rest, _ := strings.Cut(strings.TrimLeft(raw, "/"), "/")
		raw = "/" + rest
	}
	var out []string
	segs := strings.Split(strings.TrimPrefix(raw, "/"), "/")
	for i, seg := range segs {
		last := i == len(segs)-1
		switch strings.ToLower(seg) {
		case ".", "%2e":
			if last {
				out = append(out, "")
			}
		case "..", ".%2e", "%2e.", "%2e%2e":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			if last {
				out = append(out, "")
			}
		default:
			out = append(out, seg)
		}
	}
	p, err := url.PathUnescape("/" + strings.Join(out, "/"))
	if err != nil || !utf8.ValidString(p) {
		return "", false
	}
	return p, true
}
