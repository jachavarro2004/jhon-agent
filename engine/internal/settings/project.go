package settings

import (
	"regexp"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

var topKey = regexp.MustCompile(`^("[^"]*"|'[^']*'|[A-Za-z0-9_.-]+)\s*:(\s.*|)$`)

// Project is a project's LDD rules, read-only: each top-level key of its
// openspec/config.yaml, in file order, with source project. A scalar is
// its text (unquoted, without a trailing comment); a block (a map or a
// list) is its lines, trimmed, comments dropped. nil when the file is absent.
func Project(root string) []Value {
	b, err := project.ReadLddConfig(root)
	if err != nil {
		return nil
	}
	out := []Value{}
	var block *Value
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' || line[0] == '-' {
			if block != nil {
				lines, _ := block.Value.([]string)
				block.Value = append(lines, trimmed)
			}
			continue
		}
		m := topKey.FindStringSubmatch(line)
		if m == nil {
			block = nil
			continue
		}
		key, value := unquote(m[1]), yamlScalar(m[2])
		if value == "" {
			out = append(out, Value{Key: key, Value: []string{}, Source: SourceProject})
			block = &out[len(out)-1]
			continue
		}
		out = append(out, Value{Key: key, Value: value, Source: SourceProject})
		block = nil
	}
	return out
}

// yamlScalar is a scalar's text: a quoted one unquoted, else up to a comment.
func yamlScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 0 && (s[0] == '"' || s[0] == '\'') {
		if j := strings.IndexByte(s[1:], s[0]); j >= 0 {
			return s[1 : 1+j]
		}
	}
	if i := strings.Index(s, " #"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}
