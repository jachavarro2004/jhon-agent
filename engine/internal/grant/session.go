package grant

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// A session grant is the owner's yes to writes in a folder that is no
// project (no git repository), for one session (T57a, L460: "Preguntar y
// recordar"). It names outside-write alone, one folder and everything
// below it, and one session id; it ends when the owner's session ends (pi
// revokes it on shutdown) and, at the latest, SessionTTL after it was given.
// It is recorded under ${XDG_STATE_HOME:-~/.local/state}/jhon/session-grants/,
// beside the grants and as much the owner's (InGrants), and in the grants'
// history. It lifts the outside-write ask only: a refusal, a deletion,
// credentials, .git, the framework and the settings stay as they are.

// SessionClass is the one class a session grant names.
const SessionClass = "outside-write"

// SessionTTL is the longest a session grant lives, for a session that
// never ended cleanly.
const SessionTTL = 12 * time.Hour

// SessionDir is where session grants live.
func SessionDir() string { return filepath.Join(filepath.Dir(Dir()), "session-grants") }

// SessionFile is the record file of session's grant of folder.
func SessionFile(folder, session string) string {
	sum := sha256.Sum256([]byte(folder + "\n" + session))
	return filepath.Join(SessionDir(), hex.EncodeToString(sum[:])+".json")
}

// SessionRecord is a session grant's record file.
type SessionRecord struct {
	Record
	Session string `json:"session"`
}

// SessionFolder is the folder a session grant of dir would name, or why
// none may: a folder that exists, in no git repository, that is not HOME
// or /, holds neither, and is not jhon's state.
func SessionFolder(dir string) (string, error) {
	folder := project.Realish(project.Resolve("", dir))
	home := project.Realish(project.Home())
	switch info, err := os.Stat(folder); {
	case err != nil || !info.IsDir():
		return "", fmt.Errorf("%s is not a folder", dir)
	case folder == string(filepath.Separator) || project.Within(home, folder):
		return "", fmt.Errorf("%s is or holds the home directory or /, which no grant covers", dir)
	case project.Root(folder) != "":
		return "", fmt.Errorf("%s is inside a project; grant outside-write there for a while instead (jhon grant outside-write --for 30m)", dir)
	case InGrants(folder) || project.Within(folder, SessionDir()) || project.Within(project.Realish(filepath.Dir(Dir())), folder):
		return "", fmt.Errorf("%s is jhon's state, which no grant covers", dir)
	}
	return folder, nil
}

// GrantSession records session's grant of the folder dir is, until the
// session ends or SessionTTL passes.
func GrantSession(dir, session string) (SessionRecord, error) {
	if err := SessionIDError(session); err != nil {
		return SessionRecord{}, err
	}
	folder, err := SessionFolder(dir)
	if err != nil {
		return SessionRecord{}, err
	}
	now := time.Now()
	rec := SessionRecord{Record{Class: SessionClass, Root: folder, GrantedAt: isodate.Format(now), ExpiresAt: isodate.Format(now.Add(SessionTTL))}, session}
	file := SessionFile(folder, session)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return rec, err
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rec); err != nil {
		return rec, err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		return rec, err
	}
	if err := os.Rename(tmp, file); err != nil {
		return rec, err
	}
	return rec, appendHistory(rec.Record)
}

// RevokeSession removes every grant of session, and records each in the
// history; none is no error. It is the folders it revoked.
func RevokeSession(session string) ([]string, error) {
	entries, err := os.ReadDir(SessionDir())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var folders []string
	for _, e := range entries {
		file := filepath.Join(SessionDir(), e.Name())
		rec, ok := readSession(file)
		if !ok || rec.Session != session {
			continue
		}
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return folders, err
		}
		now := isodate.Format(time.Now())
		if err := appendHistory(Record{Class: SessionClass, Root: rec.Root, GrantedAt: now, ExpiresAt: now}); err != nil {
			return folders, err
		}
		folders = append(folders, rec.Root)
	}
	return folders, nil
}

func readSession(file string) (SessionRecord, bool) {
	var rec SessionRecord
	b, err := os.ReadFile(file)
	if err != nil || json.Unmarshal(b, &rec) != nil || rec.Class != SessionClass || rec.Session == "" || rec.Root == "" {
		return rec, false
	}
	return rec, true
}

// SessionIDError is why session is no session id: empty, or holding a
// control character (a newline included).
func SessionIDError(session string) error {
	if strings.TrimSpace(session) == "" {
		return errors.New("a session grant needs the session's id")
	}
	if strings.IndexFunc(session, unicode.IsControl) >= 0 {
		return fmt.Errorf("session id %q holds a control character", session)
	}
	return nil
}

// CoversWrite is whether a session grant of folder covers a write to abs:
// its resolved path is inside folder (a link out lends nothing) and in no
// git repository (one nested in the folder is a project of its own).
func CoversWrite(folder, abs string) bool {
	real := project.Realish(abs)
	// project.Root runs git in a directory: an existing file is asked of
	// its own directory, or git fails there and the file reads as in no
	// repository (L475).
	dir := real
	if info, err := os.Stat(real); err == nil && !info.IsDir() {
		dir = filepath.Dir(real)
	}
	return folder != "" && project.Within(real, folder) && project.Root(dir) == ""
}

// SessionGranted is the folder of session's live grant that covers cwd,
// cwd's own folder or one above it, below HOME and /; "" for none. A cwd
// inside a project has none: a project's writes go by its own grants.
func SessionGranted(cwd, session string) string {
	if strings.TrimSpace(session) == "" || cwd == "" {
		return ""
	}
	d := project.Realish(cwd)
	if project.Root(d) != "" {
		return ""
	}
	home := project.Realish(project.Home())
	for ; d != filepath.Dir(d) && !project.Within(home, d); d = filepath.Dir(d) {
		rec, ok := readSession(SessionFile(d, session))
		if !ok || rec.Root != d || rec.Session != session {
			continue
		}
		if until, ok := isodate.Parse(rec.ExpiresAt); ok && until.After(time.Now()) {
			if _, err := SessionFolder(d); err == nil {
				return d
			}
		}
	}
	return ""
}
