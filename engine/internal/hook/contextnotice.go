package hook

import (
	"encoding/json"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// FreshSession is the one-session-per-change notice (S35): why, then what
// to run in each runtime. handoff is the `jhon context` command that saves
// the handoff. `jhon archive` prints it once per change, and shell/extensions/
// jhon.ts says the same at half of pi's context window or its token budget,
// whichever is first.
func FreshSession(why, handoff string) string {
	return "One session per change: " + why + " Save a handoff (`" + handoff + "`, then mem_session_summary), " +
		"then start a fresh session (/new in pi; /clear or a restart in Claude Code) and give it the handoff."
}

// ContextNotice is the Stop hook that tells the owner, once per band, that
// the session's context is long: save a handoff and start a fresh session
// (context-notice). Advisory: it always exits 0.
func ContextNotice(_ []string, io IO) int {
	p, ok := read(io.In)
	if !ok {
		return 0
	}
	path, isStr := p["transcript_path"].(string)
	sess := regexp.MustCompile(`[^\w.-]`).ReplaceAllString(jsString(p["session_id"]), "")
	if !isStr || sess == "" || !project.Exists(path) {
		return 0
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	tokens, ok := LastContext(string(text))
	if !ok {
		return 0
	}
	first, step := bands(os.Getenv("JHON_CONTEXT_NOTICE"))
	band := BandOf(tokens, first, step)
	if band == 0 {
		return 0
	}
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" || !filepath.IsAbs(cache) {
		cache = filepath.Join(project.Home(), ".cache")
	}
	dir := filepath.Join(cache, "jhon", "context-notice")
	state := filepath.Join(dir, sess)
	last := 0.0
	if b, err := os.ReadFile(state); err == nil {
		if n, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64); err == nil {
			last = n
		}
	}
	if float64(band) <= last {
		return 0
	}
	if os.MkdirAll(dir, 0o777) == nil {
		_ = os.WriteFile(state, []byte(strconv.Itoa(band)), 0o666)
	}
	k := int(math.Floor(tokens/1000 + 0.5))
	fmt.Fprintln(io.Out, marshal(map[string]string{
		"systemMessage": FreshSession(fmt.Sprintf("the context is at %dK tokens, and every turn re-reads it.", k), "jhon context <change> --phase none --handoff"),
	}))
	return 0
}

// LastContext is the context of a transcript's last assistant turn, in tokens.
func LastContext(text string) (float64, bool) {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if !strings.Contains(lines[i], `"usage"`) {
			continue
		}
		var rec struct {
			Type    any `json:"type"`
			Message struct {
				Usage map[string]any `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(lines[i]), &rec) != nil || rec.Type != "assistant" || rec.Message.Usage == nil {
			continue
		}
		n := func(k string) float64 {
			v, _ := rec.Message.Usage[k].(float64)
			return v
		}
		return n("input_tokens") + n("cache_creation_input_tokens") + n("cache_read_input_tokens"), true
	}
	return 0, false
}

// BandOf is the band a context falls in: 0 below the first, then 1, 2, … every step.
func BandOf(tokens float64, first, step int) int {
	if tokens < float64(first) {
		return 0
	}
	return 1 + int(math.Floor((tokens-float64(first))/float64(step)))
}

var bandsRe = lazyre.New(`^(\d+),(\d+)$`)

func bands(env string) (int, int) {
	if m := bandsRe.FindStringSubmatch(env); m != nil {
		a, errA := strconv.Atoi(m[1])
		b, errB := strconv.Atoi(m[2])
		if errA == nil && errB == nil && a > 0 && b > 0 {
			return a, b
		}
	}
	return 250_000, 150_000
}
