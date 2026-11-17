package judgment

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The append side of a judgment record: one replay decides whether an event
// may join the record, and the record file only ever grows by the event's
// canonical bytes, written atomically.

// Load reads the vocabulary, panicking on a broken one, so every command
// fails up front, usage included.
func Load() { vocab() }

// AppendEvent is the record with one event appended: the old bytes followed
// by the new event's canonical bytes. The error is a *Refusal, and nothing
// is computed to write, when the record is invalid or the contract forbids
// the event.
func AppendEvent(record, input string) (out string, err error) {
	defer recovered(&err)
	events := parseRecord(record)
	v, perr := js.ParseJSON(input)
	if perr != nil {
		refuse("the event is not valid JSON")
	}
	e, ok := v.(*js.Object)
	_, named := js.Prop(v, "event").(string)
	need(ok && named, "the input must be exactly one event object with an event field")
	tip := sha256Hex(record)
	prev := js.Prop(e, "prev")
	_, absent := prev.(js.Undefined)
	need(absent || prev == any(tip), "%s: prev does not match the record's current bytes; it was built against another version", js.Prop(e, "event"))
	e.Set("prev", tip)
	replay(append(events, e))
	return record + renderEvent(e), nil
}

// RecordLocationOK is whether a record path is one the contract allows: one
// of judgment_record's locations, matched against the end of the absolute path.
func RecordLocationOK(path string) bool {
	abs := filepath.ToSlash(resolve(path))
	for _, shape := range vocab().recordShapes {
		re := regexp.QuoteMeta(shape)
		re = strings.Replace(re, "<change>", "[^/]+", 1)
		re = strings.Replace(re, "<date>", `[0-9]{4}-[0-9]{2}-[0-9]{2}`, 1)
		re = strings.Replace(re, "<slug>", "[a-z0-9][a-z0-9-]*", 1)
		if regexp.MustCompile(`(^|/)` + re + `$`).MatchString(abs) {
			return true
		}
	}
	return false
}

// resolve is path.resolve: p made absolute against process.cwd().
func resolve(p string) string {
	if !filepath.IsAbs(p) {
		wd, _ := syscall.Getwd()
		p = filepath.Join(wd, p)
	}
	return filepath.Clean(p)
}

// AppendFile appends to a record file atomically: a temporary file in its
// directory, renamed over it. A refusal is a *Refusal; a failed read or
// write carries node's message for it.
func AppendFile(path, input string) error {
	v := vocab()
	if !RecordLocationOK(path) {
		return &Refusal{path + " is not a record location; contracts/judgment.md allows " + strings.Join(v.recordShapes, " or ")}
	}
	old := ""
	info, statErr := os.Stat(path)
	if statErr == nil {
		b, err := os.ReadFile(path)
		if err != nil {
			return js.FSError(err)
		}
		old = string(b)
	}
	next, err := AppendEvent(old, input)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return js.FSError(err)
	}
	tmp := filepath.Join(dir, "."+filepath.Base(path)+".append-"+strconv.Itoa(os.Getpid())+"-"+strconv.FormatInt(time.Now().UnixMilli(), 10))
	if err := replace(tmp, path, next, info); err != nil {
		os.Remove(tmp) // never created, or already renamed
		return js.FSError(err)
	}
	return nil
}

// replace puts the bytes in tmp, gives it the mode of the file it replaces
// (info, nil when there is none), and renames it over path.
func replace(tmp, path, bytes string, info fs.FileInfo) error {
	if err := os.WriteFile(tmp, []byte(bytes), 0o666); err != nil {
		return err
	}
	if info != nil {
		if err := os.Chmod(tmp, info.Mode()&(fs.ModePerm|fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky)); err != nil {
			return err
		}
	}
	return os.Rename(tmp, path)
}
