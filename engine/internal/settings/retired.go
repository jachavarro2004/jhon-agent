package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The keys T15.3 removed (L76, the owner-approved T15a table): a file that
// still holds one is read, never refused. Each is a Note that says why it
// goes; a renamed value (animations reduced/potato, the status position
// [shell.extra] held) is read under its key. `jhon settings migrate`
// (Cleanup) drops them from the file, backed up, and says what it did.

// A Note is one retired key a settings file holds: its value, why it goes,
// and the key = value it became, when it became one. Silent notes (the
// audio backend, which only ever had one value) are dropped without a word.
type Note struct {
	Key    string `json:"key"`
	Was    any    `json:"was"`
	Why    string `json:"why"`
	Now    string `json:"now,omitempty"`
	Silent bool   `json:"silent,omitempty"`
	// Ignored is the key it would have become, when the file sets that key
	// itself: the explicit key wins, and the retired one only goes (T15.9).
	Ignored string `json:"ignored,omitempty"`
	// values are what the key reads as now, by key.
	values map[string]any
}

// retiredWhy are the removed keys with no successor, and why each goes.
var retiredWhy = map[string]string{
	"shell.behaviour.quiet_tools":                   "the shell always draws its own tool cards",
	"shell.behaviour.key_hints":                     "the keys are listed in the command palette and the README",
	"shell.notifications.audio.minimum_interval_ms": "the interval between sounds is fixed at 1000 ms",
	"shell.notifications.audio.coalesce_window_ms":  "the coalescing window is fixed at 300 ms",
	"shell.extra.card_content":                      "tool cards always show their result previews",
	"shell.extra.visual_profiles":                   "visual profiles are gone",
	"shell.keys.usage":                              "/jhon:usage is gone: the status line shows the 5h and week windows",
	"integrations.dashboard":                        "/jhon:dashboard opens the dashboard at this session whenever you ask; nothing else read the switch",
	// T24b (L291): Jhon keeps no multiplexer integration.
	"integrations.herdr":            herdrGone,
	"integrations.herdr_bin":        herdrGone,
	"integrations.herdr_plugin_dir": herdrGone,
	// T29 S3 (L243).
	"shell.editor.vim": "the vim editor was removed from the shell",
	// T15.7 (L97); their values no longer pick the palette (T29 E6, L329).
	"integrations.theme.follow_desktop": `the palette is integrations.theme.palette now: set it to "desktop" to follow the desktop palette`,
	"integrations.theme.source":         `the palette names its source: integrations.theme.palette = "desktop"`,
}

const herdrGone = "herdr is no longer part of Jhon; use any multiplexer, or none"

// retiredIn is the release a key went in, when not v0.1.
var retiredIn = map[string]string{
	"integrations.herdr":            "v0.2",
	"integrations.herdr_bin":        "v0.2",
	"integrations.herdr_plugin_dir": "v0.2",
	"shell.editor.vim":              "v0.2",
}

// RetiredIn is the release that removed key: v0.1 unless retiredIn says.
func RetiredIn(key string) string {
	if segs, err := splitKey(key); err == nil {
		key = joinKey(segs)
	}
	if v := retiredIn[key]; v != "" {
		return v
	}
	return "v0.1"
}

// retiredEvents are the notification events that never fire, and the one a
// single context notice replaced.
var retiredEvents = map[string]string{
	"session.shutdown":   "the event never fires",
	"subagent.queued":    "the event never fires",
	"subagent.running":   "the event never fires",
	"subagent.waiting":   "the event never fires",
	"subagent.cancelled": "the event never fires",
	"subagent.timed_out": "the event never fires",
	"context.high":       "one context notice at shell.behaviour.fresh_session_at replaces it",
}

const eventsPrefix = "shell.notifications.audio.events."

// retire is the note for a key the schema no longer has, or false for a
// key that was never known (an unknown key stays an error).
func retire(key string, v any) (Note, bool) {
	n := Note{Key: key, Was: v}
	switch {
	case key == "shell.notifications.audio.backend":
		n.Why, n.Silent = "it only ever had one value", true
	case key == "shell.appearance.animations":
		s, _ := v.(string)
		now := map[string]string{"reduced": "full", "potato": "off"}[s]
		if now == "" {
			return Note{}, false // a current value, or one check refuses
		}
		n.Why = "one animation level now: full or off"
		n.Now, n.values = fmt.Sprintf("shell.appearance.animations = %q", now), map[string]any{key: now}
	case key == "shell.appearance.card_style":
		// T15.12d (L153 answer 1): float and neon are back, under their own key.
		n.Why = "the card style is shell.appearance.cards now (float or neon)"
		if s, _ := v.(string); s == "float" || s == "neon" {
			n.Now, n.values = fmt.Sprintf("shell.appearance.cards = %q", s), map[string]any{"shell.appearance.cards": s}
		}
	case key == "shell.extra.visual_customization":
		n.Why = "one layout now: the status position is shell.appearance.status; header position, density and the section toggles are gone"
		if status := statusFromVisual(v); status != "" {
			n.Now, n.values = fmt.Sprintf("shell.appearance.status = %q", status), map[string]any{"shell.appearance.status": status}
		}
	case strings.HasPrefix(key, eventsPrefix):
		path, err := splitKey(key)
		if err != nil || len(path) != 5 || retiredEvents[path[4]] == "" {
			return Note{}, false
		}
		n.Why = retiredEvents[path[4]]
	case retiredWhy[key] != "":
		n.Why = retiredWhy[key]
	case strings.HasPrefix(key, extraPrefix):
		n.Why = "[shell.extra] is gone; nothing read it"
	default:
		return Note{}, false
	}
	return n, true
}

// statusFromVisual is the status key the old visual_customization JSON
// names: hidden stays hidden; right, bottom and auto are auto (bottom is
// what auto draws under 140 columns).
func statusFromVisual(v any) string {
	s, _ := v.(string)
	var doc struct {
		StatusPlacement string `json:"statusPlacement"`
	}
	if json.Unmarshal([]byte(s), &doc) != nil {
		return ""
	}
	switch doc.StatusPlacement {
	case "hidden":
		return "hidden"
	case "auto", "right", "bottom":
		return "auto"
	}
	return ""
}

// RetiredWhy is why key was removed, or "" for a key never retired.
func RetiredWhy(key string) string {
	if segs, err := splitKey(key); err == nil {
		key = joinKey(segs)
	}
	n, ok := retire(key, nil)
	if !ok || n.Silent {
		return ""
	}
	return n.Why
}

// Retired is every retired key the settings file at path holds, in key
// order; none for an absent file.
func Retired(path string) ([]Note, error) {
	_, _, notes, err := fileValues(path)
	return notes, err
}

// CleanReport is what Cleanup did: Done is false when the file held no
// retired key.
type CleanReport struct {
	Done   bool   `json:"done"`
	Path   string `json:"path"`
	Backup string `json:"backup,omitempty"`
	Notes  []Note `json:"notes"`
}

// Cleanup drops the retired keys from the file at path and writes the
// values they became, line by line, so the owner's comments and every
// other byte stay; a section left with no key goes too. The old file is
// copied to settings-backup-<UTC time>/settings.toml beside it first.
func Cleanup(path string, now time.Time) (CleanReport, error) {
	rep := CleanReport{Path: path, Notes: []Note{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return rep, nil
	}
	if err != nil {
		return rep, err
	}
	text := string(b)
	_, _, notes, err := parse(path, text)
	if err != nil {
		return rep, err
	}
	if len(notes) == 0 {
		return rep, nil
	}
	next := text
	var emptied [][]string
	for _, n := range notes {
		segs, _ := splitKey(n.Key)
		var removed bool
		next, removed = drop(next, segs)
		if removed {
			emptied = append(emptied, segs[:len(segs)-1])
		}
	}
	for _, n := range notes {
		for k, v := range n.values {
			segs, _ := splitKey(k)
			if next, err = place(next, segs, tomlValue(v)); err != nil {
				return rep, err
			}
		}
	}
	for _, section := range emptied {
		next = dropEmptySection(next, section)
	}
	if _, _, left, err := parse(path, next); err != nil || len(left) > 0 {
		// Say what stopped it: the cleaned text not parsing, or retired keys
		// still there (an inline table, say, which no line-level drop reaches).
		why := ""
		if err != nil {
			why = err.Error()
		} else {
			keys := make([]string, len(left))
			for i, n := range left {
				keys[i] = n.Key
			}
			why = "still sets " + strings.Join(keys, ", ")
		}
		return rep, fmt.Errorf("%s: the cleaned settings would not read back (%s); the file was left as is, nothing backed up; edit it by hand", path, why)
	}
	backup := filepath.Join(filepath.Dir(path), "settings-backup-"+now.UTC().Format("20060102T150405Z"), filepath.Base(path))
	if err := copyFile(path, backup); err != nil {
		return rep, fmt.Errorf("backup: %w", err)
	}
	if err := writeAtomic(path, next); err != nil {
		return rep, err
	}
	rep.Done, rep.Backup, rep.Notes = true, backup, notes
	return rep, nil
}

// lines splits text keeping each line's end.
func lines(text string) []string {
	out := strings.SplitAfter(text, "\n")
	if out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// drop removes the line that sets the key segs, and whether it did.
func drop(text string, segs []string) (string, bool) {
	ls := lines(text)
	var current []string
	for i, line := range ls {
		body := strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimSpace(body)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			current = nil
			if path, rest, err := readKey(strings.TrimLeft(trimmed, "[")); err == nil && strings.HasPrefix(strings.TrimSpace(rest), "]") && !strings.HasPrefix(trimmed, "[[") {
				current = path
			}
			continue
		}
		path, rest, err := readKey(body)
		if err != nil || !strings.HasPrefix(strings.TrimLeft(rest, " \t"), "=") {
			continue
		}
		if slices.Equal(append(slices.Clone(current), path...), segs) {
			return strings.Join(slices.Delete(ls, i, i+1), ""), true
		}
	}
	return text, false
}

// dropEmptySection removes the [section] header when no key follows it
// before the next header, with the blank line above it.
func dropEmptySection(text string, section []string) string {
	ls := lines(text)
	for i, line := range ls {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "[[") {
			continue
		}
		path, rest, err := readKey(strings.TrimPrefix(trimmed, "["))
		if err != nil || !strings.HasPrefix(strings.TrimSpace(rest), "]") || !slices.Equal(path, section) {
			continue
		}
		for _, after := range ls[i+1:] {
			t := strings.TrimSpace(after)
			if strings.HasPrefix(t, "[") {
				break
			}
			if t != "" && !strings.HasPrefix(t, "#") {
				return text // a key stays
			}
		}
		start := i
		if start > 0 && strings.TrimSpace(ls[start-1]) == "" {
			start--
		}
		return strings.Join(slices.Delete(ls, start, i+1), "")
	}
	return text
}
