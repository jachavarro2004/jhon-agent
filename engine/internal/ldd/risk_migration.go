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
// -- migrate:down, -- +migrate Down), only the part before it is judged. Any
// other statement, any other language, an empty file, and a file that is
// binary, not UTF-8 or cannot be read keep the migration high. A short
// allow-list of patterns, no parser: when unsure, high.

var (
	downPart = lazyre.New(`(?im)^[ \t]*(def[ \t]+(self\.)?(down|downgrade|backwards)\b|--[ \t]*\+goose[ \t]+down\b|--[ \t]*migrate:down\b|--[ \t]*\+migrate[ \t]+down\b)`)

	sqlBlockComment = lazyre.New(`(?s)/\*.*?\*/`)
	sqlLineComment  = lazyre.New(`--[^\n]*`)
	sqlTx           = lazyre.New(`^(begin( transaction| work)?|start transaction|commit( transaction| work)?|end( transaction)?)$`)
	sqlName         = "(?:[\\w$]+|\"[^\"]+\"|`[^`]+`|\\[[^\\]]+\\])(?:\\.(?:[\\w$]+|\"[^\"]+\"|`[^`]+`|\\[[^\\]]+\\]))?"
	sqlCreateTable  = lazyre.New(`^create table (if not exists )?` + sqlName + ` ?\(`)
	sqlCreateIndex  = lazyre.New(`^create (unique )?index (concurrently )?(if not exists )?(` + sqlName + ` )?on `)
	sqlAddColumn    = lazyre.New(`^alter table (if exists )?(only )?` + sqlName + ` add (column )?(if not exists )?(` + sqlName + `) \w`)
	sqlNotColumn    = lazyre.New(`^(constraint|primary|foreign|unique|check|index|key|partition|column)$`)
	sqlSelect       = lazyre.New(`\bselect\b`)
	sqlNotNull      = lazyre.New(`\bnot null\b`)
	sqlDefault      = lazyre.New(`\bdefault\b`)

	rbBoiler      = lazyre.New(`^(end|class \w+(::\w+)* < ActiveRecord::Migration(\[[\d.]+\])?|def (self\.)?(change|up)|require ['"][^'"]+['"])$`)
	rbCreateTable = lazyre.New(`^create_table\b[^{}]*?(\bdo \|(\w+)\|)?$`)
	rbColumn      = lazyre.New(`^(\w+)\.(\w+)\b`)
	rbChange      = lazyre.New(`^(remove|rename|change)`)
	rbAddColumn   = lazyre.New(`^add_(column|reference)\b`)
	rbAddIndex    = lazyre.New(`^add_index\b`)
	rbNullFalse   = lazyre.New(`\bnull(:| *=>) *false\b`)
	rbDefault     = lazyre.New(`\bdefault(:| *=>)`)

	pyTripleQuoted = lazyre.New(`(?s)"""(.*?)"""|'''(.*?)'''`)
	pyBoiler       = lazyre.New(`^(import [\w., ]+|from [\w.]+ import [\w., ()]+|class \w+(\([\w., ]*\))?:|def \w+\([^()]*\)( *-> *[\w.\[\], ]+)?:|pass|""|''|\.\.\.|operations *= *\[ *\]|\],?)$`)
	pyMeta         = lazyre.New(`^(revision|down_revision|branch_labels|depends_on|initial|atomic|dependencies|replaces|run_before)\b *(:[^=]*)?= *(.*)$`)
	pySwappable    = lazyre.New(`migrations\.swappable_dependency\(settings\.\w+\)`)
	pyCall         = lazyre.New(`\w *\(`)
	pyOpsOpen      = lazyre.New(`^operations *= *\[$`)
	pyCreate       = lazyre.New(`^((migrations\.)?(CreateModel|AddIndex)|op\.(create_table|create_index))\(.*\),?$`)
	pyAddField     = lazyre.New(`^(migrations\.)?AddField\(.*\),?$`)
	pyAddColumn    = lazyre.New(`^op\.add_column\(.*\)$`)
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

// upPart is text before its down part, if any.
func upPart(text string) string {
	if loc := downPart.FindStringIndex(text); loc != nil {
		return text[:loc[0]]
	}
	return text
}

func additiveSQL(text string) bool {
	text = sqlLineComment.ReplaceAllString(sqlBlockComment.ReplaceAllString(text, " "), "")
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
			!sqlNotColumn.MatchString(sqlAddColumn.FindStringSubmatch(s)[5]) &&
			(!sqlNotNull.MatchString(masked) || sqlDefault.MatchString(masked)):
			ops++
		default:
			return false
		}
	}
	return ops > 0
}

func additiveRuby(text string) bool {
	ops, block := 0, ""
	for _, l := range logicalLines(text, true) {
		switch {
		case block != "" && l == "end":
			block = ""
		case block != "":
			m := rbColumn.FindStringSubmatch(l)
			if m == nil || m[1] != block || rbChange.MatchString(m[2]) {
				return false
			}
		case rbBoiler.MatchString(l):
		case rbCreateTable.MatchString(l):
			ops++
			block = rbCreateTable.FindStringSubmatch(l)[2]
		case rbAddIndex.MatchString(l):
			ops++
		case rbAddColumn.MatchString(l) && (!rbNullFalse.MatchString(l) || rbDefault.MatchString(l)):
			ops++
		default:
			return false
		}
	}
	return ops > 0 && block == ""
}

func additivePython(text string) bool {
	text = pyTripleQuoted.ReplaceAllString(text, `""`)
	ops := 0
	for _, l := range logicalLines(text, false) {
		switch {
		case pyBoiler.MatchString(l), pyOpsOpen.MatchString(l):
		case pyMeta.MatchString(l):
			if pyCall.MatchString(maskNested(pySwappable.ReplaceAllString(pyMeta.FindStringSubmatch(l)[3], ""), '\'', '"')) {
				return false
			}
		case pyCreate.MatchString(l),
			pyAddField.MatchString(l) && pyNullable.MatchString(l),
			pyAddColumn.MatchString(l) && pyAlNullable.MatchString(l):
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
