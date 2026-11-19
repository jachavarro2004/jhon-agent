package ldd

import (
	"bytes"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// This file is the additive exception to the migration rule (T164, L1422,
// L1423): a migration file is high risk unless its text, read as text, holds
// only recognisable additive operations, at least one, and nothing else but
// comments, blank lines, transaction wrappers and framework boilerplate.
//
//   - SQL (.sql): CREATE TABLE (not AS SELECT); CREATE [UNIQUE] INDEX, with
//     CONCURRENTLY or IF NOT EXISTS; ALTER TABLE … ADD [COLUMN] with one
//     action, nullable or with a DEFAULT. BEGIN, START TRANSACTION, COMMIT.
//   - Rails (.rb): create_table (with its t.<column> block), add_column and
//     add_reference (not null: false without a default), add_index.
//   - Django and Alembic (.py): CreateModel, AddField with null=True or
//     default=, AddIndex; op.create_table, op.add_column with nullable=True or
//     server_default, op.create_index. Imports, class and def headers, pass,
//     docstrings, the revision and dependencies assignments, operations = [ ].
//
// When a file has a down part (def down, def downgrade, -- +goose Down,
// -- migrate:down, -- +migrate Down), only the part before it is judged; a
// SQL down marker counts only after its up marker (-- +goose Up,
// -- migrate:up, -- +migrate Up), else it is a comment. Any other
// statement, any other language, an empty file, and a file that is binary,
// not UTF-8 or cannot be read keep the migration high. A statement is judged
// whole (L1427): a second statement on its line (after `;`, or a second
// Python call), a Ruby argument that is no literal, create_table with an
// option off its short list (force: drops the table), and a quoted name or
// string that reads as DEFAULT or hides a comment marker keep it high. A
// short allow-list of patterns, no parser: when unsure, high.

var (
	downPart = lazyre.New(`(?im)^[ \t]*(def[ \t]+(self\.)?(down|downgrade|backwards)\b|--[ \t]*\+goose[ \t]+down\b|--[ \t]*migrate:down\b|--[ \t]*\+migrate[ \t]+down\b)`)
	// upMarker is the up marker that makes a SQL down marker count, by the
	// down marker's tool: goose, dbmate, sql-migrate.
	upMarker = map[string]*lazyre.Regexp{
		"goose":    lazyre.New(`(?im)^[ \t]*--[ \t]*\+goose[ \t]+up\b`),
		"migrate:": lazyre.New(`(?im)^[ \t]*--[ \t]*migrate:up\b`),
		"+migrate": lazyre.New(`(?im)^[ \t]*--[ \t]*\+migrate[ \t]+up\b`),
	}

	sqlTx          = lazyre.New(`^(begin( transaction| work)?|start transaction|commit( transaction| work)?|end( transaction)?)$`)
	sqlName        = "(?:[\\w$]+|\"[^\"]+\"|`[^`]+`|\\[[^\\]]+\\])(?:\\.(?:[\\w$]+|\"[^\"]+\"|`[^`]+`|\\[[^\\]]+\\]))?"
	sqlCreateTable = lazyre.New(`^create table (if not exists )?` + sqlName + ` ?\(`)
	sqlCreateIndex = lazyre.New(`^create (unique )?index (concurrently )?(if not exists )?(` + sqlName + ` )?on `)
	sqlAddColumn   = lazyre.New(`^alter table (if exists )?(only )?` + sqlName + ` add (column )?(if not exists )?(` + sqlName + `) \w`)
	sqlNotColumn   = lazyre.New(`^(constraint|primary|foreign|unique|check|index|key|partition|column)$`)
	sqlSelect      = lazyre.New(`\bselect\b`)
	sqlNotNull     = lazyre.New(`\bnot null\b`)
	sqlDefault     = lazyre.New(`\bdefault\b`)
	sqlDefaultNull = lazyre.New(`\bdefault null\b`)

	rbBoiler    = lazyre.New(`^(end|class \w+(::\w+)* < ActiveRecord::Migration(\[[\d.]+\])?|def (self\.)?(change|up)|require ['"][^'"]+['"])$`)
	rbBlock     = lazyre.New(`^(.*?)\s+do\s+\|(\w+)\|$`)
	rbCall      = lazyre.New(`^(\w+(?:\.\w+)?)(?:\((.*)\)|\s+(.*))?$`)
	rbChange    = lazyre.New(`^(remove|rename|change)`)
	rbArgs      = lazyre.New(`^(?:` + rbArg + `(?:\s*,\s*` + rbArg + `)*)?$`)
	rbKey       = lazyre.New(`(\w+):(?:\s|$)|:(\w+)\s*=>`)
	rbTableKeys = map[string]bool{"id": true, "primary_key": true, "comment": true, "if_not_exists": true, "options": true}
	rbNullFalse = lazyre.New(`\bnull(:|\s*=>)\s*false\b`)
	rbDefault   = lazyre.New(`\bdefault(:|\s*=>)`)
	rbNoDefault = lazyre.New(`\bdefault(:|\s*=>)\s*nil\b`)

	pyTripleQuoted = lazyre.New(`(?s)"""(.*?)"""|'''(.*?)'''`)
	pyBoiler       = lazyre.New(`^(import [\w., ]+|from [\w.]+ import [\w., ()]+|class \w+(\([\w., ]*\))?:|def \w+\([^()]*\)( *-> *[\w.\[\], ]+)?:|pass|""|''|\.\.\.|operations *= *\[ *\]|\],?)$`)
	pyMeta         = lazyre.New(`^(revision|down_revision|branch_labels|depends_on|initial|atomic|dependencies|replaces|run_before)\b *(:[^=]*)?= *(.*)$`)
	pySwappable    = lazyre.New(`migrations\.swappable_dependency\(settings\.\w+\)`)
	pyCall         = lazyre.New(`\w *\(`)
	pyOpsOpen      = lazyre.New(`^operations *= *\[$`)
	pyCreate       = lazyre.New(`^((migrations\.)?(CreateModel|AddIndex)|op\.(create_table|create_index))\(`)
	pyAddField     = lazyre.New(`^(migrations\.)?AddField\(`)
	pyAddColumn    = lazyre.New(`^op\.add_column\(`)
	pyNullable     = lazyre.New(`\bnull *= *True\b|\bdefault *=`)
	pyAlNullable   = lazyre.New(`\bnullable *= *True\b|\bserver_default *=`)
)

// additiveMigration is whether a migration file's text holds only
// recognisable additive operations; ok false is a file that cannot be read.
func additiveMigration(file string, b []byte, ok bool) bool {
	if !ok || len(b) == 0 || bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) {
		return false
	}
	text := upPart(strings.ReplaceAll(string(b), "\r\n", "\n"))
	switch strings.ToLower(path.Ext(file)) {
	case ".sql":
		return additiveSQL(text)
	case ".rb":
		return additiveRuby(text)
	case ".py":
		return additivePython(text)
	}
	return false
}

// upPart is text before its down part, if any. A SQL down marker counts
// only after its own up marker.
func upPart(text string) string {
	for _, loc := range downPart.FindAllStringIndex(text, -1) {
		marker := asciiLower(text[loc[0]:loc[1]])
		if !strings.Contains(marker, "--") {
			return text[:loc[0]]
		}
		for tool, up := range upMarker {
			if strings.Contains(marker, tool) {
				if u := up.FindStringIndex(text); u != nil && u[0] < loc[0] {
					return text[:loc[0]]
				}
			}
		}
	}
	return text
}

// rbArg is a Ruby argument that is a literal: a symbol, a string (emptied),
// a number, true, false, nil, a %i or %w array, a lambda of a string, or
// an array or a hash of those, with or without a key.
const (
	rbLit  = `(?::\w+[?!]?|""|''|-?\d[\d_]*(?:\.\d+)?|true|false|nil|%[iwIW]\[[\w\s]*\]|->\s*\{\s*(?:""|'')\s*\})`
	rbKeyR = `(?:\w+:\s*|(?::\w+|""|'')\s*=>\s*)`
	rbArr  = `\[\s*(?:` + rbLit + `(?:\s*,\s*` + rbLit + `)*\s*,?\s*)?\]`
	rbHash = `\{\s*(?:` + rbKeyR + `(?:` + rbLit + `|` + rbArr + `)(?:\s*,\s*` + rbKeyR + `(?:` + rbLit + `|` + rbArr + `))*\s*,?\s*)?\}`
	rbArg  = rbKeyR + `?(?:` + rbLit + `|` + rbArr + `|` + rbHash + `)`
)

// stripSQLComments is text without its -- and /* */ comments outside
// quotes; ok false is a comment never closed or a quoted string with a
// backslash, which could hide where a string ends.
func stripSQLComments(text string) (string, bool) {
	var b strings.Builder
	var q byte
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case q != 0 && c == '\\':
			return "", false
		case q != 0 && c == q:
			q = 0
		case q != 0:
		case c == '\'' || c == '"' || c == '`':
			q = c
		case strings.HasPrefix(text[i:], "--"):
			j := strings.IndexByte(text[i:], '\n')
			if j < 0 {
				return b.String(), true
			}
			i += j - 1
			continue
		case strings.HasPrefix(text[i:], "/*"):
			j := strings.Index(text[i+2:], "*/")
			if j < 0 {
				return "", false
			}
			b.WriteByte(' ')
			i += j + 3
			continue
		}
		b.WriteByte(c)
	}
	return b.String(), true
}

func additiveSQL(text string) bool {
	text, ok := stripSQLComments(text)
	if !ok {
		return false
	}
	ops := 0
	for _, s := range strings.Split(text, ";") {
		s = strings.Join(strings.Fields(asciiLower(s)), " ")
		masked := maskNested(s, '\'')
		switch {
		case s == "", sqlTx.MatchString(s):
		case sqlCreateTable.MatchString(s) && !sqlSelect.MatchString(masked):
			ops++
		case sqlCreateIndex.MatchString(s):
			ops++
		case sqlAddColumn.MatchString(s) && !strings.Contains(masked, ",") &&
			!sqlNotColumn.MatchString(sqlAddColumn.FindStringSubmatch(s)[5]):
			// NOT NULL and DEFAULT count after the column's name, outside quotes.
			rest := s[sqlAddColumn.FindStringSubmatchIndex(s)[11]:]
			rest = maskNested(maskNested(rest, '\'', '"', '`'), '\'')
			if sqlNotNull.MatchString(rest) && (!sqlDefault.MatchString(rest) || sqlDefaultNull.MatchString(rest)) {
				return false
			}
			ops++
		default:
			return false
		}
	}
	return ops > 0
}

func additiveRuby(text string) bool {
	ops, block := 0, ""
	for _, line := range logicalLines(text, true) {
		for _, l := range splitOutsideQuotes(line, ';') {
			if l = strings.TrimSpace(l); l == "" {
				continue
			}
			if block != "" && l == "end" {
				block = ""
				continue
			}
			if block == "" && rbBoiler.MatchString(l) {
				continue
			}
			opens := ""
			if m := rbBlock.FindStringSubmatch(l); m != nil {
				l, opens = m[1], m[2]
			}
			m := rbCall.FindStringSubmatch(l)
			if m == nil {
				return false
			}
			name, args := m[1], strings.TrimSpace(maskNested(m[2]+m[3], '\'', '"'))
			if !rbArgs.MatchString(args) {
				return false
			}
			if block != "" {
				col, method, _ := strings.Cut(name, ".")
				if col != block || method == "" || rbChange.MatchString(method) || opens != "" {
					return false
				}
				continue
			}
			switch {
			case name == "create_table":
				for _, k := range rbKey.FindAllStringSubmatch(args, -1) {
					if !rbTableKeys[k[1]+k[2]] {
						return false
					}
				}
				block = opens
			case opens != "":
				return false
			case name == "add_index":
			case (name == "add_column" || name == "add_reference") &&
				(!rbNullFalse.MatchString(args) || rbDefault.MatchString(args) && !rbNoDefault.MatchString(args)):
			default:
				return false
			}
			ops++
		}
	}
	return ops > 0 && block == ""
}

// splitOutsideQuotes is s cut at each sep outside ' and " quotes.
func splitOutsideQuotes(s string, sep byte) []string {
	var out []string
	var q byte
	start := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case q != 0 && c == '\\':
			i++
		case q != 0 && c == q:
			q = 0
		case q != 0:
		case c == '\'' || c == '"':
			q = c
		case c == sep:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// pyCallRest is what follows l's first call, the parenthesis after its name
// to the one that closes it, outside quotes; ok false when it never closes.
func pyCallRest(l string) (string, bool) {
	open := strings.IndexByte(l, '(')
	if open < 0 {
		return "", false
	}
	var q byte
	depth := 0
	for i := open; i < len(l); i++ {
		switch c := l[i]; {
		case q != 0 && c == '\\':
			i++
		case q != 0 && c == q:
			q = 0
		case q != 0:
		case c == '\'' || c == '"':
			q = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			if depth--; depth == 0 {
				return strings.TrimSpace(l[i+1:]), true
			}
		}
	}
	return "", false
}

func additivePython(text string) bool {
	text = pyTripleQuoted.ReplaceAllString(text, `""`)
	ops := 0
	for _, l := range logicalLines(text, false) {
		// One call per statement: nothing after it but a Django list comma.
		rest, closed := pyCallRest(l)
		one := closed && (rest == "" || rest == ",")
		switch {
		case pyBoiler.MatchString(l), pyOpsOpen.MatchString(l):
		case pyMeta.MatchString(l):
			if pyCall.MatchString(maskNested(pySwappable.ReplaceAllString(pyMeta.FindStringSubmatch(l)[3], ""), '\'', '"')) {
				return false
			}
		case one && pyCreate.MatchString(l),
			one && pyAddField.MatchString(l) && pyNullable.MatchString(l),
			one && rest == "" && pyAddColumn.MatchString(l) && pyAlNullable.MatchString(l):
			ops++
		default:
			return false
		}
	}
	return ops > 0
}

// logicalLines is text's statements for Ruby (ruby) or Python: comments
// dropped, lines trimmed and joined while a bracket is open (and, in Ruby,
// after a trailing comma or backslash); `operations = [` opens no bracket,
// so each Django operation is a statement of its own.
func logicalLines(text string, ruby bool) []string {
	var out []string
	cur, depth := "", 0
	for _, raw := range strings.Split(text, "\n") {
		l := strings.TrimSpace(dropComment(raw))
		if l == "" {
			continue
		}
		if cur == "" && (pyOpsOpen.MatchString(l) || l == "]" || l == "],") {
			out = append(out, l)
			continue
		}
		if cur != "" {
			cur += " "
		}
		cur += l
		depth += bracketDepth(l)
		if depth > 0 || ruby && (strings.HasSuffix(l, ",") || strings.HasSuffix(l, `\`)) {
			continue
		}
		out = append(out, cur)
		cur, depth = "", 0
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// dropComment is l without a # comment outside quotes.
func dropComment(l string) string {
	var q byte
	for i := 0; i < len(l); i++ {
		switch c := l[i]; {
		case q != 0 && c == '\\':
			i++
		case q != 0 && c == q:
			q = 0
		case q != 0:
		case c == '\'' || c == '"':
			q = c
		case c == '#':
			return l[:i]
		}
	}
	return l
}

// bracketDepth is the brackets l opens less those it closes, outside quotes.
func bracketDepth(l string) int {
	d := 0
	for _, c := range maskNested(l, '\'', '"') {
		switch c {
		case '(', '[', '{':
			d++
		case ')', ']', '}':
			d--
		}
	}
	return d
}

// maskNested is s with the inside of quoted strings (by the quotes given)
// emptied, and, when no double quote is given, the inside of parentheses too.
func maskNested(s string, quotes ...byte) string {
	var b strings.Builder
	parens := !bytes.ContainsRune(quotes, '"')
	var q byte
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == '\\' {
				i++
			} else if c == q {
				q = 0
				b.WriteByte(c)
			}
			continue
		case bytes.IndexByte(quotes, c) >= 0:
			q = c
		case parens && c == '(':
			depth++
			if depth == 1 {
				b.WriteByte(c)
			}
			continue
		case parens && c == ')' && depth > 0:
			depth--
		}
		if !parens || depth == 0 {
			b.WriteByte(c)
		}
	}
	return b.String()
}
