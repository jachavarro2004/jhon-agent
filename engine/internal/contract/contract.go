// Package contract reads the machine-readable values the contracts declare:
// contracts/vocabulary.yaml and the small YAML blocks of
// the contract documents, through one deliberately small parser.
package contract

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Root is the release or checkout the binary belongs to: JHON_ROOT, or the
// parent of the directory holding the binary (<root>/build/jhon).
func Root() (string, error) {
	if r := os.Getenv("JHON_ROOT"); r != "" {
		return r, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(exe)), nil
}

// Contract is the text of contracts/<name>.
func Contract(name string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(root, "prose", "contracts", name))
	return string(b), err
}

// MustContract is Contract, or a panic: a tool cannot run without its contract.
func MustContract(name string) string {
	s, err := Contract(name)
	if err != nil {
		panic(fmt.Sprintf("contracts/%s: %v", name, err))
	}
	return s
}

var quoted = regexp.MustCompile(`^"(.*)"$`)

// unquote is a trimmed value without its surrounding double quotes.
func unquote(s string) string { return quoted.ReplaceAllString(strings.TrimSpace(s), "$1") }

func items(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = unquote(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// List is `key: [a, b]` at the start of a line, as its trimmed, non-empty items.
func List(src, key string) []string {
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*\[(.*)\]`).FindStringSubmatch(src)
	if m == nil {
		return nil
	}
	return items(m[1])
}

var kvLine = regexp.MustCompile(`^\s+([\w-]+):\s*(.+)$`)

// block is the two-space-indented lines directly under `key:`.
func block(src, key string) []string {
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\n((?:  .*\n)+)`).FindStringSubmatch(src)
	if m == nil {
		return nil
	}
	return strings.Split(m[1], "\n")
}

// Map is the two-space-indented `k: v` lines directly under `key:`, in order.
func Map(src, key string) (keys []string, values map[string]string) {
	values = map[string]string{}
	for _, line := range block(src, key) {
		if kv := kvLine.FindStringSubmatch(line); kv != nil {
			if _, seen := values[kv[1]]; !seen {
				keys = append(keys, kv[1])
			}
			values[kv[1]] = unquote(kv[2])
		}
	}
	return keys, values
}

// Scalar is a `key: value` or `key: "value"` declared at the start of a line; "" when absent.
func Scalar(src, key string) string {
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:[ \t]*(?:"([^"\n]*)"|([^\n]*))$`).FindStringSubmatch(src)
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(m[2])
}

var recordLine = regexp.MustCompile(`^  (- |  )([\w-]+):\s*(.*)$`)

// Records is a block sequence of flat mappings under `key:`.
func Records(src, key string) []map[string]string {
	var out []map[string]string
	for _, line := range block(src, key) {
		kv := recordLine.FindStringSubmatch(line)
		if kv == nil {
			continue
		}
		if kv[1] == "- " {
			out = append(out, map[string]string{})
		}
		if len(out) > 0 {
			out[len(out)-1][kv[2]] = unquote(kv[3])
		}
	}
	return out
}

// Vocabulary is a contracts/vocabulary.yaml list of exactly n values: a
// list that grows or shrinks stops the tool rather than shift every meaning.
func Vocabulary(key string, n int) ([]string, error) {
	src, err := Contract("vocabulary.yaml")
	if err != nil {
		return nil, err
	}
	v := List(src, key)
	if len(v) != n {
		return nil, fmt.Errorf("contracts/vocabulary.yaml: %s must declare %d values, found %d", key, n, len(v))
	}
	return v, nil
}

// MustVocabulary is Vocabulary, or a panic, for package-level values.
func MustVocabulary(key string, n int) []string {
	v, err := Vocabulary(key, n)
	if err != nil {
		panic(err)
	}
	return v
}

var flow = regexp.MustCompile(`^\[(.*)\]$`)

// InlineList is a flow-list value (`[a, b]`) as its items; anything else is empty.
func InlineList(value string) []string {
	m := flow.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return nil
	}
	return items(m[1])
}

// ErrUsage marks a command-line usage error: exit 2.
var ErrUsage = errors.New("usage")
