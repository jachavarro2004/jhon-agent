package dashboard

import (
	"bufio"
	"encoding/json"
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
)

// The LDD state of a session's changes as their project holds it now: the
// slices and tasks, the artifacts, the next step and its notes, and the last
// recorded test run, read the way `jhon status <change> --json` reads them.
// It is the project's state today, not the session's at its time: a change
// the session worked on may have moved on, or been archived since.

// sessionCwd is the first working directory a transcript records, or "".
func sessionCwd(transcript string) string {
	f, err := os.Open(transcript)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for n := 0; sc.Scan() && n < 200; n++ {
		var e struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Cwd != "" {
			return e.Cwd
		}
	}
	return ""
}

// LddOf is the LDD state of each named change under the project the
// transcript worked in, by change; a change its project no longer holds
// as active is absent, and so is every change when the project is gone.
func LddOf(transcript string, changes []string) *js.Object {
	out := js.Obj()
	cwd := sessionCwd(transcript)
	if cwd == "" || len(changes) == 0 || ldd.StatusVocabulary() != nil {
		return out
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return out
	}
	root := discover.FindRoot(cwd)
	for _, name := range changes {
		s, err := ldd.StatusOf(root, &name)
		if err != nil || !s.Ok || s.Change == nil || *s.Change != name {
			continue
		}
		out.Set(name, lddView(s))
	}
	return out
}

func lddView(s ldd.Status) *js.Object {
	slices := []any{}
	for _, sl := range s.Tasks.Slices {
		slice := js.Obj("name", sl.Name, "total", float64(sl.Total), "completed", float64(sl.Completed))
		if sl.Delivers != "" {
			slice.Set("delivers", sl.Delivers)
		}
		slices = append(slices, slice)
	}
	artifacts := []any{}
	for _, a := range s.Artifacts {
		artifacts = append(artifacts, js.Obj("artifact", a.Artifact, "state", a.State))
	}
	unchecked := []any{}
	for _, u := range s.Tasks.Unchecked {
		unchecked = append(unchecked, u)
	}
	notes := []any{}
	for _, n := range s.Notes {
		if n != "" {
			notes = append(notes, n)
		}
	}
	var tests any
	if t := s.Tests; t != nil {
		failed := []any{}
		for _, f := range t.Failed {
			failed = append(failed, f)
		}
		tests = js.Obj("at", t.Timestamp, "passed", t.Passed, "failed", failed, "scope", t.Scope, "complete", t.Complete, "dirty", t.Dirty)
	}
	var slice, current any
	if s.Tasks.Slice != nil {
		slice = *s.Tasks.Slice
	}
	// The first slice not done, an outline one included; null when every slice is.
	if s.Tasks.Current != nil {
		current = *s.Tasks.Current
	}
	// The route's level (standard, verified), null when status could not read it.
	var route any
	if s.Route != nil {
		route = s.Route.Level
	}
	return js.Obj(
		"total", float64(s.Tasks.Total), "completed", float64(s.Tasks.Completed), "slices", slices, "slice", slice,
		"unchecked", unchecked, "artifacts", artifacts, "next", s.Next, "notes", notes, "tests", tests, "route", route,
		"current", current, "slicesDone", float64(s.Tasks.SlicesDone),
	)
}
