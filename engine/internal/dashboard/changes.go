package dashboard

import (
	"encoding/json"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The changes page's data (`/api/changes`): every change the listed
// sessions' summaries name, one row per project and change, each with its
// sessions newest first and its LDD state as the project holds it now
// (ldd.go), read from the newest session's working directory. A session not
// audited yet names none; a summary written before `changes` names its main
// change only.

// changesOf is the rows of the sessions, newest first as Sessions lists
// them; ldd is a change's LDD state now, read from one of its sessions, or nil.
func changesOf(list []Session, ldd func(Session, string) any) []any {
	type row struct {
		project, name string
		sessions      []Session
	}
	var rows []*row
	seen := map[string]*row{}
	for _, s := range list {
		for _, name := range summaryChanges(s.Summary) {
			key := s.Name + "\x00" + name
			r := seen[key]
			if r == nil {
				r = &row{project: s.Name, name: name}
				seen[key] = r
				rows = append(rows, r)
			}
			r.sessions = append(r.sessions, s)
		}
	}
	out := []any{}
	for _, r := range rows {
		sessions := []any{}
		for _, s := range r.sessions {
			var title any
			if s.Title != nil {
				title = *s.Title
			}
			runtime := s.Runtime
			if runtime == "" {
				runtime = "claude-code"
			}
			sessions = append(sessions, js.Obj("id", s.ID, "title", title, "modified", s.Modified, "runtime", runtime))
		}
		out = append(out, js.Obj("name", r.name, "project", r.project, "sessions", sessions, "ldd", ldd(r.sessions[0], r.name)))
	}
	return out
}

// summaryChanges is the changes a session's summary names: `changes`, else
// its main `change`.
func summaryChanges(raw json.RawMessage) []string {
	var sum struct {
		Change  *string  `json:"change"`
		Changes []string `json:"changes"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &sum) != nil {
		return nil
	}
	if sum.Changes != nil {
		return sum.Changes
	}
	if sum.Change != nil && *sum.Change != "" {
		return []string{*sum.Change}
	}
	return nil
}

// changeLdd is a change's LDD state now from s's working directory, or nil
// when its project no longer holds it as active.
func changeLdd(s Session, name string) any {
	if v := js.Prop(LddOf(s.Path, []string{name}), name); !js.Nullish(v) {
		return v
	}
	return nil
}
