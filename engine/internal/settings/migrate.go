package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Sources are the files the migration reads. The pre-v0.1 shell's
// jhon-shell/*.json files are no longer among them (T29 E6, L329).
type Sources struct {
	Profiles string // ~/.config/jhon/profiles.json
}

// Evidence is what the machine shows of the opt-in integrations; the
// caller finds it, so this package imports none of them.
type Evidence struct {
	Telemetry bool // telemetry is not off
}

// Mapped is one value the migration wrote, and where it came from.
type Mapped struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
	From  string `json:"from"`
}

// Report is what a migration did. Done is false when it did nothing:
// settings.toml already exists, or there was nothing to migrate.
type Report struct {
	Done    bool     `json:"done"`
	Reason  string   `json:"reason,omitempty"`
	Path    string   `json:"path"`
	Backup  string   `json:"backup,omitempty"`
	Mapped  []Mapped `json:"mapped"`
	Skipped []string `json:"skipped"`
}

// old is every old file that exists.
func (s Sources) old() []string {
	if s.Profiles != "" && isFile(s.Profiles) {
		return []string{s.Profiles}
	}
	return nil
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// Pending is whether a migration has work: no settings file, and old files.
func Pending(path string, src Sources) bool {
	_, err := os.Stat(path)
	return errors.Is(err, os.ErrNotExist) && len(src.old()) > 0
}

// Migrate is the one-time move into settings.toml at path: the active
// profile mapped to its key, and the integrations the machine shows in use
// turned on. The old file is copied to settings-backup-<UTC time>/ beside
// path and left in place. With a settings file already there, or no old
// file, it does nothing.
func Migrate(path string, src Sources, ev Evidence, now time.Time) (Report, error) {
	rep := Report{Path: path, Mapped: []Mapped{}, Skipped: []string{}}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		rep.Reason = "settings.toml exists"
		return rep, nil
	}
	old := src.old()
	if len(old) == 0 {
		rep.Reason = "no old settings files"
		return rep, nil
	}
	add := func(key string, v any, from string) {
		s, known := lookup(key)
		if !known {
			switch n, retired := retire(key, v); {
			case !retired:
				rep.Skipped = append(rep.Skipped, fmt.Sprintf("%s: %s: no such setting", from, key))
			case !n.Silent:
				rep.Skipped = append(rep.Skipped, fmt.Sprintf("%s: %s: %s", from, key, n.Why))
			}
			return
		}
		got, err := s.check(v)
		if err != nil {
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("%s: %v", from, err))
			return
		}
		rep.Mapped = append(rep.Mapped, Mapped{Key: s.key, Value: got, From: from})
	}
	if isFile(src.Profiles) {
		if doc, err := readJSON(src.Profiles); err != nil {
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("%s: unreadable (%v)", src.Profiles, err))
		} else if a, ok := doc["active"].(string); ok {
			add("pi.profile", a, src.Profiles)
		}
	}
	if ev.Telemetry {
		add("integrations.telemetry", true, "telemetry not off")
	}
	text := render(rep.Mapped)
	if _, _, _, err := parse(path, text); err != nil {
		return rep, fmt.Errorf("the migrated settings would not read back: %w", err)
	}
	backup := filepath.Join(filepath.Dir(path), "settings-backup-"+now.UTC().Format("20060102T150405Z"))
	for _, p := range old {
		if err := copyFile(p, filepath.Join(backup, filepath.Base(p))); err != nil {
			return rep, fmt.Errorf("backup: %w", err)
		}
	}
	if err := writeAtomic(path, text); err != nil {
		return rep, err
	}
	rep.Done, rep.Backup = true, backup
	return rep, nil
}

// render is a settings file holding vals, by section in the schema's order.
func render(vals []Mapped) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("# Migrated from profiles.json; a key not written here takes its default.\n")
	var section string
	emit := func(key string, v any) {
		segs, _ := splitKey(key)
		sec := joinKey(segs[:len(segs)-1])
		if sec != section {
			fmt.Fprintf(&b, "\n[%s]\n", sec)
			section = sec
		}
		fmt.Fprintf(&b, "%s = %s\n", quoteSegment(segs[len(segs)-1]), tomlValue(v))
	}
	byKey := map[string]any{}
	for _, m := range vals {
		byKey[m.Key] = m.Value
	}
	for _, s := range schema {
		if v, ok := byKey[s.key]; ok {
			emit(s.key, v)
		}
	}
	return b.String()
}

func readJSON(p string) (map[string]any, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, errors.New("not a JSON object")
	}
	return doc, nil
}

// copyFile copies src to dest, creating dest's directory, keeping the mode.
func copyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
