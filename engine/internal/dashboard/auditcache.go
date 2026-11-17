package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The audits `jhon audit <session> --view` and `--json` printed, kept on
// disk one file per session and output, so the dashboard, which audits each
// session it shows with `--json`, does not audit an unchanged one again.
// Like the summaries, an audit is keyed by its inputs' sizes and mtimes:
// the session's transcripts (the main one and everything under its
// directory: sub-agents, persisted tool results), whatever else the caller names (the binary, its
// contracts), and what the audit read besides the transcripts (its spec:
// change records, git state), whose identity the caller recomputes on each
// lookup. A hit prints what the audit printed, byte for byte.

// AuditCache is the audits kept under Dir; an empty Dir keeps none.
type AuditCache struct{ Dir string }

// auditSchema leads every key: a change to what an entry holds misses every older one.
const auditSchema = "a2"

// auditCacheEntries is how many audits the cache keeps at most, the newest.
const auditCacheEntries = 500

// StatKey is the identity of paths as they are now: each file's path, size,
// and mtime, a directory's files walked in order; a path that cannot be
// read counts as missing.
func StatKey(paths ...string) string {
	var b strings.Builder
	stat := func(p string, info fs.FileInfo) {
		fmt.Fprintf(&b, "%s\x00%d\x00%d\n", p, info.Size(), info.ModTime().UnixNano())
	}
	for _, p := range paths {
		info, err := os.Stat(p)
		switch {
		case err != nil:
			fmt.Fprintf(&b, "%s\x00missing\n", p)
		case !info.IsDir():
			stat(p, info)
		default:
			_ = filepath.WalkDir(p, func(f string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				if info, err := d.Info(); err == nil {
					stat(f, info)
				}
				return nil
			})
		}
	}
	return b.String()
}

// SessionKey is the identity of a session's transcripts: the main one at
// target and its directory.
func SessionKey(target string) string {
	return StatKey(target, strings.TrimSuffix(target, ".jsonl"))
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// file is where the audit of target printed as mode is kept.
func (c AuditCache) file(target, mode string) string {
	return filepath.Join(c.Dir, digest(target + "\x00" + mode)[:32])
}

// entryKey is what an entry's head holds for key and its inputs' identity.
func entryKey(key, deps string) string {
	return digest(auditSchema + "\x00" + key + "\x00" + deps)
}

// Get is the output and exit code kept for target as mode under key; ok is
// false when none is, or the one kept has another key, or deps, the
// identity of the inputs its spec names as they are now, differs.
func (c AuditCache) Get(target, mode, key string, deps func(spec string) string) (out string, code int, ok bool) {
	if c.Dir == "" {
		return "", 0, false
	}
	b, err := os.ReadFile(c.file(target, mode))
	if err != nil {
		return "", 0, false
	}
	head, rest, found := strings.Cut(string(b), "\n")
	fields := strings.Fields(head)
	if !found || len(fields) != 3 {
		return "", 0, false
	}
	n, err := strconv.Atoi(fields[2])
	if err != nil || n < 0 || n > len(rest) {
		return "", 0, false
	}
	spec, rest := rest[:n], rest[n:]
	if fields[0] != entryKey(key, deps(spec)) {
		return "", 0, false
	}
	code, err = strconv.Atoi(fields[1])
	return rest, code, err == nil
}

// Put keeps the output and exit code of target's audit as mode under key,
// with spec, what the audit read besides the transcripts, and deps, its
// identity now. An audit-error (exit 2) is not kept: what failed may be
// read on the next try. A cache that cannot be written keeps nothing.
func (c AuditCache) Put(target, mode, key, spec, deps, out string, code int) {
	if c.Dir == "" || code == 2 || os.MkdirAll(c.Dir, 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(c.Dir, ".tmp-")
	if err != nil {
		return
	}
	_, err = fmt.Fprintf(tmp, "%s %d %d\n%s%s", entryKey(key, deps), code, len(spec), spec, out)
	if tmp.Close() != nil || err != nil || os.Rename(tmp.Name(), c.file(target, mode)) != nil {
		_ = os.Remove(tmp.Name())
	}
	c.prune()
}

// prune drops what a write that died left behind, a temporary file older
// than a minute, and every entry but the newest auditCacheEntries.
func (c AuditCache) prune() {
	files, err := os.ReadDir(c.Dir)
	if err != nil {
		return
	}
	type entry struct {
		path string
		at   time.Time
	}
	var entries []entry
	for _, f := range files {
		info, err := f.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		p := filepath.Join(c.Dir, f.Name())
		if strings.HasPrefix(f.Name(), ".tmp-") {
			if time.Since(info.ModTime()) > time.Minute {
				_ = os.Remove(p)
			}
			continue
		}
		entries = append(entries, entry{p, info.ModTime()})
	}
	if len(entries) <= auditCacheEntries {
		return
	}
	slices.SortFunc(entries, func(a, b entry) int { return b.at.Compare(a.at) })
	for _, e := range entries[auditCacheEntries:] {
		_ = os.Remove(e.path)
	}
}
