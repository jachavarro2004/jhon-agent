package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// One audited session in a few numbers, for the session list's marks and the
// Inicio page (the owner's design 09): its status and findings, the changes
// that need a look and why, its tokens and how many went outside every
// change, and its times. Summaries are kept on disk, one file per session
// under the dashboard's cache, each keyed like the audit cache by the
// transcript's size and mtime, so a restart audits only what changed; a
// background loop audits the recent sessions that have none.

// SummaryOf is the summary of one session's view (View).
func SummaryOf(view *js.Object) *js.Object {
	summary := js.Prop(view, "summary")
	findings := js.Prop(summary, "findings")
	look, relaunches, verifyFail := 0.0, 0.0, 0.0
	recordFailed := false
	for _, c := range list(or(js.Prop(view, "changes"), []any{})) {
		if len(list(js.Prop(c, "attention"))) > 0 {
			look++
		}
		relaunches += num(js.Prop(c, "relaunches"))
		verifyFail += num(js.Prop(js.Prop(c, "verify"), "fail"))
		if rec := js.Prop(c, "record"); js.Truthy(rec) && !js.Truthy(js.Prop(rec, "passed")) {
			recordFailed = true
		}
	}
	// The change the session worked on most, the list's ◆ line; and every
	// change it worked on, the changes page's.
	change, most := any(nil), -1.0
	changes := []any{}
	for _, c := range list(or(js.Prop(view, "changes"), []any{})) {
		if p := num(js.Prop(c, "processed")); p > most {
			change, most = js.Prop(c, "name"), p
		}
		if n, ok := js.Prop(c, "name").(string); ok {
			changes = append(changes, n)
		}
	}
	checks := []any{}
	for _, g := range list(or(js.Prop(view, "findings"), []any{})) {
		checks = append(checks, js.Obj("check", js.Prop(g, "check"), "title", js.Prop(g, "title"), "violation", num(js.Prop(g, "violation")), "suspect", num(js.Prop(g, "suspect"))))
	}
	return js.Obj(
		"status", or(js.Prop(view, "status"), nil),
		"violation", num(js.Prop(findings, "violation")), "suspect", num(js.Prop(findings, "suspect")),
		"look", look, "relaunches", relaunches, "verify_fail", verifyFail, "record_failed", recordFailed,
		"processed", num(js.Prop(summary, "processed")), "outside", num(js.Prop(js.Prop(view, "unattributed"), "processed")),
		"active_ms", num(js.Prop(summary, "active_ms")), "elapsed_ms", num(js.Prop(summary, "elapsed_ms")),
		"checks", checks, "notes", float64(len(list(or(js.Prop(view, "notes"), []any{})))), "errors", float64(len(list(or(js.Prop(view, "errors"), []any{})))), "change", change,
		"changes", changes,
	)
}

// Summaries are the sessions' summaries, in memory and on disk under dir.
type Summaries struct {
	dir string
	mu  sync.Mutex
	m   map[string]summaryEntry
}

type summaryEntry struct {
	Key     string          `json:"key"`
	Summary json.RawMessage `json:"summary"`
}

// summaryKey is a session's audit identity: the transcript's size and mtime.
// The schema version leads the key, so a summary with fewer fields is
// audited again rather than shown without them (v6: the usage).
func summaryKey(s Session) string { return "v6:" + strconv.FormatInt(s.Bytes, 10) + ":" + s.Modified }

// NewSummaries is the summaries kept under dir, read from it; a dir that
// cannot be read is an empty cache that writes nothing.
func NewSummaries(dir string) *Summaries {
	c := &Summaries{dir: dir, m: map[string]summaryEntry{}}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		var e summaryEntry
		if err == nil && json.Unmarshal(b, &e) == nil && e.Key != "" {
			c.m[filepath.Base(f[:len(f)-len(".json")])] = e
		}
	}
	return c
}

// Get is a session's summary as JSON when one matches its transcript as it is now.
func (c *Summaries) Get(s Session) (json.RawMessage, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[s.ID]
	if !ok || e.Key != summaryKey(s) {
		return nil, false
	}
	return e.Summary, true
}

// Put keeps a session's summary from the view of its audit and the usage
// its transcripts record (UsageOf), on disk too.
func (c *Summaries) Put(s Session, view *js.Object) {
	if c == nil || !sessionID.MatchString(s.ID) {
		return
	}
	sum := SummaryOf(view)
	sum.Set("usage", usageValue(UsageOf(s.Path)))
	e := summaryEntry{summaryKey(s), json.RawMessage(js.StringifyValueCompact(sum))}
	c.mu.Lock()
	c.m[s.ID] = e
	c.mu.Unlock()
	if b, err := json.Marshal(e); err == nil && os.MkdirAll(c.dir, 0o755) == nil {
		tmp := filepath.Join(c.dir, s.ID+".json.tmp")
		if os.WriteFile(tmp, b, 0o644) == nil {
			_ = os.Rename(tmp, filepath.Join(c.dir, s.ID+".json"))
		}
	}
}

// Background audits, one at a time, the newest max sessions under projects
// that have no summary, again every interval, until stop closes. It never
// audits a session the server audits at the same moment twice in a row:
// Put after either keeps the newest.
func (c *Summaries) Background(projects string, audit Auditor, max int, interval time.Duration, stop <-chan struct{}) {
	for {
		for _, s := range Sessions(projects, max, false) {
			select {
			case <-stop:
				return
			default:
			}
			if _, ok := c.Get(s); ok {
				continue
			}
			c.Put(s, View(audit(s.Path)))
		}
		select {
		case <-stop:
			return
		case <-time.After(interval):
		}
	}
}
