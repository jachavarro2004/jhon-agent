package dashboard

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// /api/usage: the usage history across the summaries, per day.

func summarized(id, project, entrypoint string, activeMs float64, rows ...UsageRow) Session {
	sum, _ := json.Marshal(map[string]any{"active_ms": activeMs, "usage": Usage{Rows: rows}})
	runtime := "claude-code"
	if entrypoint == "pi" {
		runtime, entrypoint = "pi", ""
	}
	title := "request " + id
	return Session{ID: id, Project: project, Name: ProjectName(project), Runtime: runtime, Entrypoint: entrypoint, Title: &title, Summary: sum}
}

func row(day, model string, total float64, cost *float64) UsageRow {
	return UsageRow{Day: day, Model: model, Input: total / 2, Output: total / 2, Total: total, Cost: cost, Messages: 1}
}

func TestUsageAcrossTheSessionsPerDay(t *testing.T) {
	list := []Session{
		summarized("p1", "-home-u-jhon", "pi", 60_000, row("2026-11-05", "opus", 100, ptr(1.5)), row("2026-11-06", "opus", 50, ptr(0.25))),
		summarized("c1", "-home-u-jhon", "cli", 600_000, row("2026-11-03", "claude-opus-5-5", 300, nil), row("2026-11-06", "claude-haiku-5-5", 10, nil)),
		summarized("c2", "-home-u-Projects-acme", "", 1_000, row("2026-11-04", "claude-opus-5-5", 40, nil)),
		// The pi model bridge's own Claude Code sessions repeat pi's turns, and a scratch run is no project's: neither counts.
		summarized("b1", "-home-u-jhon", "sdk-ts", 9e9, row("2026-11-05", "opus", 1e6, nil)),
		summarized("s1", "-tmp-x", "cli", 9e9, row("2026-11-05", "opus", 1e6, nil)),
		{ID: "n1", Project: "-home-u-jhon", Name: "jhon", Runtime: "claude-code"},
	}
	got := UsageAcross(list, "", "2026-11-06", "")
	want := `{"from":null,"listed":0,"total":0,"capped":false,"to":"2026-11-06","project":null,"sessions":3,"pending":1,` +
		`"totals":{"input":250,"output":250,"cache_read":0,"cache_write":0,"total":500,"cost":1.75,"messages":5},"cost_covers":"pi only",` +
		`"days":[{"day":"2026-11-03","total":300,"cost":null,"messages":1},{"day":"2026-11-04","total":40,"cost":null,"messages":1},{"day":"2026-11-05","total":100,"cost":1.5,"messages":1},{"day":"2026-11-06","total":60,"cost":0.25,"messages":2}],` +
		`"models":[{"model":"claude-opus-5-5","share":0.68,"total":340,"cost":null,"messages":2},{"model":"opus","share":0.3,"total":150,"cost":1.75,"messages":2},{"model":"claude-haiku-5-5","share":0.02,"total":10,"cost":null,"messages":1}],` +
		`"active_days":4,"days_in_range":4,"longest_streak":4,"current_streak":4,"most_active_day":{"day":"2026-11-03","total":300,"cost":null,"messages":1},` +
		`"longest_session":{"id":"c1","title":"request c1","project":"jhon","active_ms":600000}}`
	eqJSON(t, got, json.RawMessage(want), "all history")

	week := UsageAcross(list, "2026-11-05", "2026-11-08", "jhon")
	eqJSON(t, []any{week.Sessions, week.Totals.Total, week.Totals.Cost, week.CostCovers, week.ActiveDays, week.DaysInRange, week.LongestStreak, week.CurrentStreak},
		[]any{2, 160, 1.75, "pi only", 2, 4, 2, 0}, "a range and a project: an idle today and yesterday end the current streak")

	claude := UsageAcross(list, "2026-11-03", "2026-11-04", "")
	if claude.Totals.Cost != nil || claude.CostCovers != "none" || claude.MostActiveDay.Day != "2026-11-03" {
		t.Errorf("Claude Code's days alone record no money: %+v", claude)
	}
	none := UsageAcross(nil, "2026-11-01", "2026-11-07", "")
	if none.MostActiveDay != nil || none.LongestSession != nil || none.ActiveDays != 0 || none.DaysInRange != 7 || len(none.Days) != 0 || len(none.Models) != 0 {
		t.Errorf("nothing recorded: %+v", none)
	}
}

func TestUsageRouteReadsTheSummariesAndRefusesBadDates(t *testing.T) {
	dir := projects(t)
	write(t, filepath.Join(dir, "-home-u-Projects-acme", idA+".jsonl"), claudeLine("2026-11-03T08:00:01.000Z", "msg_1", "claude-opus-5-5", 1, 2, 3, 4)+"\n")
	s := serve(t, dir)
	s.get(t, "/api/session/"+idA, "GET")
	r := s.get(t, "/api/usage?from=2026-11-01&to=2026-11-07&project=acme", "GET")
	var got UsageStats
	if err := json.Unmarshal([]byte(r.body), &got); err != nil || r.status != 200 || got.Totals.Total != 10 || got.Sessions != 1 || got.Pending != 0 {
		t.Errorf("usage of acme: %d %s", r.status, r.body)
	}
	if r := s.get(t, "/api/usage?project=jhon&to=2026-11-07", "GET"); !strings.Contains(r.body, `"sessions":0,"pending":1`) {
		t.Errorf("jhon's session is not audited yet: %s", r.body)
	}
	for _, q := range []string{"from=yesterday", "to=2026-13-01", "from=2026-11-07&to=2026-11-01"} {
		if r := s.get(t, "/api/usage?"+q, "GET"); r.status != 400 || r.body != "from and to are days, YYYY-MM-DD, from not after to\n" {
			t.Errorf("%s: %d %s", q, r.status, r.body)
		}
	}
}
