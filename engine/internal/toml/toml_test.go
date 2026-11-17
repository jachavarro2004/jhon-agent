package toml

import (
	"reflect"
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *Table {
	t.Helper()
	doc, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return doc
}

func TestValues(t *testing.T) {
	doc := mustParse(t, "# comment\ntitle = \"a \\\"b\\\" \\u00e9\"\nlit = 'C:\\path'\n"+
		"ml = \"\"\"\nline \\\n   joined\"\"\"\"\nraw = '''\nkept \\n'''\nflag = true\nn = 1_000\n"+
		"when = 1979-05-27 07:32:00Z\nlist = [\n  \"a\", # note\n  'b',\n]\ninline = { x.y = 1, z = false }\n"+
		"[a.\"b c\"]\nk = 1\n[[arr]]\nq = 1\n[[arr]]\nq = 2\n[arr.sub]\nr = 3\n")
	want := map[string]Value{"title": `a "b" é`, "lit": `C:\path`, "ml": `line joined"`, "raw": `kept \n`,
		"flag": true, "n": "1_000", "when": "1979-05-27 07:32:00Z", "list": []Value{"a", "b"}}
	for k, v := range want {
		if got, _ := doc.Get(k); !reflect.DeepEqual(got, v) {
			t.Errorf("%s = %#v, want %#v", k, got, v)
		}
	}
	if y, _ := TableAt(doc, "inline", "x").Get("y"); y != "1" {
		t.Errorf("inline dotted key = %#v", y)
	}
	if k, _ := TableAt(doc, "a", "b c").Get("k"); k != "1" {
		t.Errorf("quoted table key = %#v", k)
	}
	arr, _ := doc.Get("arr")
	if list := arr.([]Value); len(list) != 2 || TableAt(list[1].(*Table), "sub") == nil {
		t.Errorf("an array of tables resolves to its last entry: %#v", arr)
	}
	if TableAt(doc, "title") != nil || TableAt(doc, "nope", "x") != nil || TableAt(nil, "a") != nil {
		t.Error("TableAt is nil for a non-table or absent step")
	}
}

func TestErrors(t *testing.T) {
	for src, want := range map[string]string{
		"a = 1\na = 2\n":            "line 2: duplicate key 'a'",
		"a = \"open\n":              "line 1: unterminated string",
		"a = \"\\q\"\n":             `line 1: invalid escape '\q'`,
		"a = \"\\":                  `line 1: invalid escape '\undefined'`,
		"a = \"\\u12\"\n":           "line 1: invalid unicode escape",
		"a = \"\\U00110000\"\n":     "Invalid code point 1114112",
		"a = [1 2]\n":               "line 1: expected ',' or ']' in an array",
		"a = 1 b\n":                 "line 1: expected the end of the line",
		"a = []\n[a.b]\n":           "line 2: 'a' is not a table",
		"a = 1\n[[a]]\n":            "line 2: 'a' is not an array of tables",
		"= 1\n":                     "line 1: expected a key",
		"a = \n":                    "line 1: expected a value",
		"a = 'x\ny'\n":              "line 1: unterminated literal string",
		"a = \"\"\"x":               "line 1: unterminated multi-line string",
		"[tool\n":                   "line 1: expected ']'",
		"a = { b = 1 c = 2 }\n":     "line 1: expected ',' or '}' in an inline table",
		"a = '''never closed\n\n\n": "line 1: unterminated multi-line literal string",
	} {
		_, err := Parse(src)
		if err == nil || err.Error() != want {
			t.Errorf("Parse(%q) error = %v, want %q", src, err, want)
		}
	}
}

func TestKeysInJavaScriptOrder(t *testing.T) {
	doc := mustParse(t, "b = 1\n10 = 1\na = 1\n2 = 1\n01 = 1\n")
	if got := doc.Keys(); !reflect.DeepEqual(got, []string{"2", "10", "b", "a", "01"}) {
		t.Errorf("Keys = %q", got)
	}
}

func TestPyprojectShapes(t *testing.T) {
	doc := mustParse(t, strings.Join([]string{
		"[project]", `dependencies = ["pytest>=8", "httpx"]`,
		"[dependency-groups]", "test = [\n  # pinned\n  \"pytest ; python_version >= '3.10'\",\n  { include-group = \"lint\" },\n]",
		"[tool.poetry.group.dev.dependencies]", `pytest-cov = { version = "^6" }`, "",
	}, "\n"))
	deps, _ := TableAt(doc, "project").Get("dependencies")
	if !reflect.DeepEqual(deps, []Value{"pytest>=8", "httpx"}) {
		t.Errorf("dependencies = %#v", deps)
	}
	test, _ := TableAt(doc, "dependency-groups").Get("test")
	if list := test.([]Value); len(list) != 2 || TableAt(list[1].(*Table)) == nil {
		t.Errorf("a group mixes strings and inline tables: %#v", test)
	}
	if TableAt(doc, "tool", "poetry", "group", "dev", "dependencies", "pytest-cov") == nil {
		t.Error("an inline table under a dotted header")
	}
}
