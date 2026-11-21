package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// header opens a new settings file.
const header = "# Jhon settings: `jhon settings list` shows every key, its value and its source.\nschema = 1\n"

// Set writes key = raw (a value as the command line gives it) into the file
// at path, creating the file, the section or the key when missing, and
// returns the value as Load would read it. The edit is line-level: the
// owner's comments and every other byte stay. A value the schema refuses is
// an *InvalidError, and nothing is written.
func Set(path, key, raw string) (Value, error) {
	segs, err := splitKey(key)
	if err != nil {
		return Value{}, &InvalidError{key, "unknown key"}
	}
	key = joinKey(segs)
	s, ok := lookup(key)
	if !ok {
		if n, retired := retire(key, nil); retired {
			return Value{}, &InvalidError{key, "removed in " + RetiredIn(key) + ": " + n.Why}
		}
		return Value{}, &InvalidError{key, "unknown key"}
	}
	v, err := s.check(fromCommandLine(s, raw))
	if err != nil {
		return Value{}, err
	}
	text := header
	if b, err := os.ReadFile(path); err == nil {
		text = string(b)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Value{}, err
	}
	// The file as it stands must be usable: Set never builds on a broken one.
	if _, _, _, err := parse(path, text); err != nil {
		return Value{}, err
	}
	next, err := place(text, segs, tomlValue(v))
	if err != nil {
		return Value{}, err
	}
	vals, _, _, err := parse(path, next)
	if err != nil {
		// A cross-key rule (the runtimes) is the value's own refusal.
		var fe *FileError
		if errors.As(err, &fe) && len(fe.Problems) == 1 && strings.HasPrefix(fe.Problems[0], "runtimes: ") {
			return Value{}, &InvalidError{"runtimes", strings.TrimPrefix(fe.Problems[0], "runtimes: ")}
		}
		return Value{}, fmt.Errorf("%s: the edit would not read back (%v); edit the file by hand", path, err)
	}
	if got, ok := vals[key]; !ok || !same(got, v) {
		return Value{}, fmt.Errorf("%s: %s is set elsewhere in a form this edit cannot place; edit the file by hand", path, key)
	}
	if err := writeAtomic(path, next); err != nil {
		return Value{}, err
	}
	return Value{Key: key, Value: v, Source: SourceFile}, nil
}

// fromCommandLine is raw typed for the key: a bool or a whole number when
// it reads as one, else the string (which check then refuses).
func fromCommandLine(s spec, raw string) any {
	switch s.kind {
	case kBool:
		if b, err := strconv.ParseBool(raw); err == nil && (raw == "true" || raw == "false") {
			return b
		}
	case kInt:
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return n
		}
	case kPaths, kNames:
		// Comma-separated; an empty entry is one, and check refuses it. A
		// list of names takes "" (or blanks) as none: the reset.
		if s.kind == kNames && strings.TrimSpace(raw) == "" {
			return []string{}
		}
		items := strings.Split(raw, ",")
		for i := range items {
			items[i] = strings.TrimSpace(items[i])
		}
		return items
	}
	return raw
}

// place puts `key = value` into text: on the line that holds the key, in
// place of its old value; else after the last key of its section; else in
// a new section at the end.
func place(text string, segs []string, value string) (string, error) {
	nl := "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	section, last := segs[:len(segs)-1], segs[len(segs)-1]
	var current []string
	headerAt, lastKeyAt := -1, -1
	for i, line := range lines {
		body := strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimSpace(body)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			continue
		case strings.HasPrefix(trimmed, "[["):
			current = nil
			continue
		case strings.HasPrefix(trimmed, "["):
			path, rest, err := readKey(trimmed[1:])
			if err != nil || !strings.HasPrefix(strings.TrimSpace(rest), "]") {
				current = nil
				continue
			}
			current = path
			if slices.Equal(path, section) {
				headerAt, lastKeyAt = i, i
			}
			continue
		}
		path, rest, err := readKey(body)
		if err != nil || !strings.HasPrefix(strings.TrimLeft(rest, " \t"), "=") {
			continue
		}
		if slices.Equal(current, section) {
			lastKeyAt = i
		}
		if !slices.Equal(append(slices.Clone(current), path...), segs) {
			continue
		}
		eq := len(body) - len(rest) + strings.Index(rest, "=")
		start := eq + 1
		for start < len(body) && (body[start] == ' ' || body[start] == '\t') {
			start++
		}
		end, err := valueEnd(body, start)
		if err != nil {
			return "", err
		}
		lines[i] = body[:start] + value + body[end:] + line[len(body):]
		return strings.Join(lines, ""), nil
	}
	entry := quoteSegment(last) + " = " + value + nl
	if headerAt >= 0 {
		at := lastKeyAt + 1
		if !strings.HasSuffix(lines[lastKeyAt], "\n") {
			lines[lastKeyAt] += nl
		}
		lines = slices.Insert(lines, at, entry)
		return strings.Join(lines, ""), nil
	}
	out := strings.Join(lines, "")
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += nl
	}
	if out != "" {
		out += nl
	}
	return out + "[" + joinKey(section) + "]" + nl + entry, nil
}

// valueEnd is where the scalar value that starts at body[i] ends.
func valueEnd(body string, i int) (int, error) {
	switch {
	case strings.HasPrefix(body[i:], `"""`), strings.HasPrefix(body[i:], `'''`), strings.HasPrefix(body[i:], "{"):
		return 0, errors.New("the key holds a multi-line or compound value; edit the file by hand")
	case strings.HasPrefix(body[i:], "["):
		// A list that closes on its line (a kPaths key); one that runs over
		// lines is the owner's to edit.
		depth := 0
		for j := i; j < len(body); j++ {
			switch body[j] {
			case '"':
				for j++; j < len(body) && body[j] != '"'; j++ {
					if body[j] == '\\' {
						j++
					}
				}
			case '\'':
				for j++; j < len(body) && body[j] != '\''; j++ {
				}
			case '[':
				depth++
			case ']':
				if depth--; depth == 0 {
					return j + 1, nil
				}
			}
		}
		return 0, errors.New("the key holds a multi-line or compound value; edit the file by hand")
	case strings.HasPrefix(body[i:], `"`):
		for j := i + 1; j < len(body); j++ {
			switch body[j] {
			case '\\':
				j++
			case '"':
				return j + 1, nil
			}
		}
	case strings.HasPrefix(body[i:], "'"):
		if j := strings.IndexByte(body[i+1:], '\''); j >= 0 {
			return i + 2 + j, nil
		}
	default:
		j := i
		for j < len(body) && body[j] != ' ' && body[j] != '\t' && body[j] != '#' {
			j++
		}
		return j, nil
	}
	return 0, errors.New("an unterminated string; edit the file by hand")
}

// writeAtomic replaces path with text through a temporary file beside it,
// keeping the old file's mode (0600 for a new one).
func writeAtomic(path, text string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".settings-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
