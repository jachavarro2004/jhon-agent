// Package mutate is the mutation witness for behavior that already exists,
// without git (contracts/tdd.md): one occurrence of a text is
// replaced in a file, the test runs, the file is restored byte for byte, and
// the test runs again.
//
// The original is recorded under ~/.cache/jhon/mut/<project key>/ before the
// file changes, so a process killed outright leaves a record that
// `jhon mutate --restore` writes back.
package mutate

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// Sha is the hex sha256 of b.
func Sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// RecordDir is where a project's pending mutations are recorded.
func RecordDir(root string) string {
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" || !filepath.IsAbs(cache) {
		cache = filepath.Join(project.Home(), ".cache")
	}
	return filepath.Join(cache, "jhon", "mut", Sha([]byte(root))[:16])
}

// Record is a pending mutation: the file, and its original's sha256 and base64 bytes.
type Record struct {
	File     string `json:"file"`
	Sha256   string `json:"sha256"`
	Original string `json:"original"`
}

// Pending is a record and the path it is kept at.
type Pending struct {
	Path string
	Rec  Record
}

// Records are the pending mutations in dir, by name; a record that cannot be read is an error.
func Records(dir string) ([]Pending, error) {
	if !project.Exists(dir) {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Pending
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var rec Record
		if err := json.Unmarshal(data, &rec); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, Pending{path, rec})
	}
	return out, nil
}

// modeBits keeps the permission, setuid, setgid, and sticky bits of a mode.
const modeBits = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

// WriteAtomic replaces path with data in one rename, keeping its mode: a new
// file would come back 644, an executable unrunnable.
func WriteAtomic(path string, data []byte) error {
	tmp := fmt.Sprintf("%s.jhon-%d.tmp", path, os.Getpid())
	fi, statErr := os.Stat(path)
	if err := os.WriteFile(tmp, data, 0o666); err != nil {
		return err
	}
	if statErr == nil {
		if err := os.Chmod(tmp, fi.Mode()&modeBits); err != nil {
			return err
		}
	}
	return os.Rename(tmp, path)
}

// RestoreOne writes one recorded original back, checks it, and drops its record.
func RestoreOne(p Pending) error {
	original, err := base64.StdEncoding.DecodeString(p.Rec.Original)
	if err != nil {
		return err
	}
	if err := WriteAtomic(p.Rec.File, original); err != nil {
		return err
	}
	back, err := os.ReadFile(p.Rec.File)
	if err != nil {
		return err
	}
	if Sha(back) != p.Rec.Sha256 {
		return fmt.Errorf("%s does not match its original after the restore", p.Rec.File)
	}
	if err := os.Remove(p.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// A Mutation is one recorded file: mutated at most while it is not
// restored, and restored once, from the main flow or a signal.
type Mutation struct {
	mu       sync.Mutex
	pending  Pending
	restored bool
}

// Begin records the original of file under dir before anything changes it,
// so a crash can always be undone. name is the record's file name.
func Begin(dir, name, file string, original []byte) (*Mutation, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	rec := Record{File: file, Sha256: Sha(original), Original: base64.StdEncoding.EncodeToString(original)}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	if err := WriteAtomic(path, bytes.TrimSuffix(b.Bytes(), []byte("\n"))); err != nil {
		return nil, err
	}
	return &Mutation{pending: Pending{path, rec}}, nil
}

// Write writes the mutant, unless the file was already restored.
func (m *Mutation) Write(mutant []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.restored {
		return nil
	}
	return WriteAtomic(m.pending.Rec.File, mutant)
}

// Restore writes the original back once; later calls do nothing.
func (m *Mutation) Restore() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.restored {
		return nil
	}
	if err := RestoreOne(m.pending); err != nil {
		return err
	}
	m.restored = true
	return nil
}

// failure matches a line of test output that reports a failure.
var failure = lazyre.New(`(?i)\bnot ok\b|\bfail(?:ed|ure|ing)?\b|\berror\b|assert|✖|expected`)

// Result is one run of the test command: its exit code, the last five
// lines of its output, and the line that reports its failure.
type Result struct {
	Code          int
	Tail, Failure string
}

// Run runs argv without a shell, stdin closed, and reads stdout then stderr.
func Run(argv []string) Result {
	cmd := exec.Command(argv[0], argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code, msg := 1, ""
	if cmd.ProcessState != nil {
		if c := cmd.ProcessState.ExitCode(); c >= 0 {
			code = c
		}
	} else if err != nil {
		msg = js.SpawnError("spawnSync", argv[0], err)
	}
	text := strings.ToValidUTF8(stdout.String(), "�") + strings.ToValidUTF8(stderr.String(), "�") + msg
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if js.Trim(l) != "" {
			lines = append(lines, l)
		}
	}
	r := Result{Code: code, Tail: strings.Join(lines[max(0, len(lines)-5):], "\n")}
	for _, l := range slices.Backward(lines) {
		if failure.MatchString(l) {
			r.Failure = js.Trim(l)
			return r
		}
	}
	if len(lines) > 0 {
		r.Failure = js.Trim(lines[len(lines)-1])
	}
	return r
}
