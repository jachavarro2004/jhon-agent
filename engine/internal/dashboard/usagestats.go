package dashboard

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// The Usage page's data (`/api/usage?from=&to=&project=`): the usage the
// listed sessions' summaries carry (UsageOf), per day, within a range of
// days and one project or all. Only the owner's own sessions count, as on
// Home: Claude Code at its prompt and pi, not the pi model bridge's Claude
// Code sessions (they repeat pi's turns) nor claude -p runs, and no scratch
// run (app.js's originOf and isScratch). Money is what pi recorded;
// cost_covers says what a sum covers: "all", "pi only" or "none".

// UsageTotals is a sum of usage; Cost is null when nothing recorded one.
type UsageTotals struct {
	Input      float64  `json:"input"`
	Output     float64  `json:"output"`
	CacheRead  float64  `json:"cache_read"`
	CacheWrite float64  `json:"cache_write"`
	Total      float64  `json:"total"`
	Cost       *float64 `json:"cost"`
	Messages   int      `json:"messages"`
}

// UsageDay is one active day.
type UsageDay struct {
	Day      string   `json:"day"`
	Total    float64  `json:"total"`
	Cost     *float64 `json:"cost"`
	Messages int      `json:"messages"`
}

// UsageModel is one model's part, its share of the tokens.
type UsageModel struct {
	Model    string   `json:"model"`
	Share    float64  `json:"share"`
	Total    float64  `json:"total"`
	Cost     *float64 `json:"cost"`
	Messages int      `json:"messages"`
}

// UsageStats is the usage across the sessions in a range.
type UsageStats struct {
	From           *string      `json:"from"`
	Listed         int          `json:"listed"`
	Total          int          `json:"total"`
	Capped         bool         `json:"capped"`
	To             string       `json:"to"`
	Project        *string      `json:"project"`
	Sessions       int          `json:"sessions"`
	Pending        int          `json:"pending"`
	Totals         UsageTotals  `json:"totals"`
	CostCovers     string       `json:"cost_covers"`
	Days           []UsageDay   `json:"days"`
	Models         []UsageModel `json:"models"`
	ActiveDays     int          `json:"active_days"`
	DaysInRange    int          `json:"days_in_range"`
	LongestStreak  int          `json:"longest_streak"`
	CurrentStreak  int          `json:"current_streak"`
	MostActiveDay  *UsageDay    `json:"most_active_day"`
	LongestSession *struct {
		ID       string  `json:"id"`
		Title    *string `json:"title"`
		Project  string  `json:"project"`
		ActiveMs float64 `json:"active_ms"`
	} `json:"longest_session"`
}

var scratchProject = lazyre.New(`(^|-)tmp(-|$)|-claude-jobs-|^-tmp-`)

// yours is whether s is the owner's own session, not scratch (app.js's originOf "yours" and isScratch).
func yours(s Session) bool {
	return (s.Runtime == "pi" || s.Entrypoint == "" || s.Entrypoint == "cli") && !scratchProject.MatchString(s.Project)
}

func addCost(sum **float64, c *float64) {
	if c != nil {
		v := roundCost(deref(*sum) + *c)
		*sum = &v
	}
}

// UsageAcross is the usage of the owner's sessions in list from from
// ("" for all history) to to, both days, in project (a readable name, ""
// for all); a session with no summary yet is pending.
func UsageAcross(list []Session, from, to, project string) UsageStats {
	out := UsageStats{To: to, Days: []UsageDay{}, Models: []UsageModel{}}
	if from != "" {
		out.From = &from
	}
	if project != "" {
		out.Project = &project
	}
	days := map[string]*UsageDay{}
	models := map[string]*UsageModel{}
	withCost, without := false, false
	for _, s := range list {
		if !yours(s) || (project != "" && s.Name != project) {
			continue
		}
		var sum struct {
			ActiveMs float64 `json:"active_ms"`
			Usage    *Usage  `json:"usage"`
		}
		if len(s.Summary) == 0 || json.Unmarshal(s.Summary, &sum) != nil || sum.Usage == nil {
			out.Pending++
			continue
		}
		counted := false
		for _, r := range sum.Usage.Rows {
			if r.Day > to || (from != "" && r.Day < from) {
				continue
			}
			counted = true
			t := &out.Totals
			t.Input += r.Input
			t.Output += r.Output
			t.CacheRead += r.CacheRead
			t.CacheWrite += r.CacheWrite
			t.Total += r.Total
			t.Messages += r.Messages
			addCost(&t.Cost, r.Cost)
			if r.Cost != nil {
				withCost = true
			} else {
				without = true
			}
			d := days[r.Day]
			if d == nil {
				d = &UsageDay{Day: r.Day}
				days[r.Day] = d
			}
			d.Total += r.Total
			d.Messages += r.Messages
			addCost(&d.Cost, r.Cost)
			m := models[r.Model]
			if m == nil {
				m = &UsageModel{Model: r.Model}
				models[r.Model] = m
			}
			m.Total += r.Total
			m.Messages += r.Messages
			addCost(&m.Cost, r.Cost)
		}
		if !counted {
			continue
		}
		out.Sessions++
		if l := out.LongestSession; l == nil || sum.ActiveMs > l.ActiveMs {
			out.LongestSession = &struct {
				ID       string  `json:"id"`
				Title    *string `json:"title"`
				Project  string  `json:"project"`
				ActiveMs float64 `json:"active_ms"`
			}{s.ID, s.Title, s.Name, sum.ActiveMs}
		}
	}
	switch {
	case withCost && without:
		out.CostCovers = "pi only"
	case withCost:
		out.CostCovers = "all"
	default:
		out.CostCovers = "none"
	}
	for _, d := range days {
		out.Days = append(out.Days, *d)
		if out.MostActiveDay == nil || d.Total > out.MostActiveDay.Total || (d.Total == out.MostActiveDay.Total && d.Day < out.MostActiveDay.Day) {
			out.MostActiveDay = d
		}
	}
	slices.SortFunc(out.Days, func(a, b UsageDay) int { return strings.Compare(a.Day, b.Day) })
	for _, m := range models {
		if out.Totals.Total > 0 {
			m.Share = math.Round(m.Total/out.Totals.Total*1e4) / 1e4
		}
		out.Models = append(out.Models, *m)
	}
	slices.SortFunc(out.Models, func(a, b UsageModel) int {
		if a.Total != b.Total {
			return cmpDesc(a.Total, b.Total)
		}
		if a.Messages != b.Messages {
			return b.Messages - a.Messages
		}
		return strings.Compare(a.Model, b.Model)
	})
	out.ActiveDays = len(out.Days)
	first := from
	if first == "" && len(out.Days) > 0 {
		first = out.Days[0].Day
	}
	if first != "" {
		out.DaysInRange = daysBetween(first, to) + 1
	}
	run := 0
	for i, d := range out.Days {
		if i > 0 && daysBetween(out.Days[i-1].Day, d.Day) == 1 {
			run++
		} else {
			run = 1
		}
		out.LongestStreak = max(out.LongestStreak, run)
	}
	// A streak stays alive through an idle last day: it counts back from it when active, else from the day before.
	day := to
	if days[day] == nil {
		day = shiftDay(to, -1)
	}
	for days[day] != nil {
		out.CurrentStreak++
		day = shiftDay(day, -1)
	}
	return out
}

func cmpDesc(a, b float64) int {
	if a > b {
		return -1
	}
	return 1
}

func shiftDay(day string, by int) string {
	t, _ := time.Parse("2006-01-02", day)
	return t.AddDate(0, 0, by).Format("2006-01-02")
}

func daysBetween(from, to string) int {
	a, _ := time.Parse("2006-01-02", from)
	b, _ := time.Parse("2006-01-02", to)
	return int(math.Round(b.Sub(a).Hours() / 24))
}

// usageRange is a request's from and to: days, from never after to, to
// today when absent; ok false otherwise.
func usageRange(from, to string) (string, string, bool) {
	if to == "" {
		to = time.Now().In(zone).Format("2006-01-02")
	}
	for _, d := range []string{from, to} {
		if d == "" {
			continue
		}
		if t, err := time.Parse("2006-01-02", d); err != nil || t.Format("2006-01-02") != d {
			return "", "", false
		}
	}
	return from, to, from <= to
}
