package contract

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	root, _ := filepath.Abs("../../..")
	os.Setenv("JHON_ROOT", root)
	os.Exit(m.Run())
}

const src = "vcs_dirs: [.git, \".hg\", .svn]\nmarkers:\n  go-mod: go\n  pkg: \"*x\"\nrules:\n  - id: a\n    check: b\n  - id: c\nname: \"quoted\"\n"

func TestReaders(t *testing.T) {
	if got := List(src, "vcs_dirs"); !reflect.DeepEqual(got, []string{".git", ".hg", ".svn"}) {
		t.Errorf("List = %q", got)
	}
	keys, m := Map(src, "markers")
	if !reflect.DeepEqual(keys, []string{"go-mod", "pkg"}) || m["pkg"] != "*x" {
		t.Errorf("Map = %q %q", keys, m)
	}
	if got := Records(src, "rules"); len(got) != 2 || got[0]["check"] != "b" || got[1]["id"] != "c" {
		t.Errorf("Records = %v", got)
	}
	if Scalar(src, "name") != "quoted" || Scalar(src, "absent") != "" {
		t.Error("Scalar")
	}
	if got := InlineList(" [a, \"b\"] "); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("InlineList = %q", got)
	}
}

var fenced = strings.Join([]string{
	"prose mentioning phases: [not, a, list] inline is ignored",
	"```yaml",
	"phases: [explore, research , propose]",
	"empty: []",
	"types:",
	"  explore: discovery",
	"  REMOVED: [Reason, Migration]",
	"after: x",
	"```",
	"",
}, "\n")

func TestListReadsAFlowListAtTheStartOfALine(t *testing.T) {
	if got := List(fenced, "phases"); !reflect.DeepEqual(got, []string{"explore", "research", "propose"}) {
		t.Errorf("phases = %q", got)
	}
	for _, key := range []string{"empty", "absent"} {
		if got := List(fenced, key); len(got) != 0 {
			t.Errorf("%s = %q", key, got)
		}
	}
}

func TestMapStopsAtTheFirstUnindentedLine(t *testing.T) {
	keys, m := Map(fenced, "types")
	if !reflect.DeepEqual(keys, []string{"explore", "REMOVED"}) ||
		!reflect.DeepEqual(m, map[string]string{"explore": "discovery", "REMOVED": "[Reason, Migration]"}) {
		t.Errorf("types = %q %q", keys, m)
	}
	if keys, m := Map(fenced, "absent"); len(keys) != 0 || len(m) != 0 {
		t.Errorf("absent = %q %q", keys, m)
	}
}

func TestRecordsStopAtTheFirstUnindentedLine(t *testing.T) {
	src := strings.Join([]string{
		"rules:",
		"  - anchor: a/one",
		"    check: launch-vcs",
		"    evidence_class: deterministic",
		"  - anchor: a/two",
		"    check: none",
		"    reason: not observable, by design: see the contract",
		"after: x",
		"  - anchor: not/read",
		"",
	}, "\n")
	want := []map[string]string{
		{"anchor": "a/one", "check": "launch-vcs", "evidence_class": "deterministic"},
		{"anchor": "a/two", "check": "none", "reason": "not observable, by design: see the contract"},
	}
	if got := Records(src, "rules"); !reflect.DeepEqual(got, want) {
		t.Errorf("rules = %v", got)
	}
	if got := Records(src, "absent"); len(got) != 0 {
		t.Errorf("absent = %v", got)
	}
}

func TestInlineList(t *testing.T) {
	if got := InlineList("[Reason, Migration]"); !reflect.DeepEqual(got, []string{"Reason", "Migration"}) {
		t.Errorf("flow = %q", got)
	}
	if got := InlineList("plain"); len(got) != 0 {
		t.Errorf("plain = %q", got)
	}
}

func TestContractReadsThisRepositorysContracts(t *testing.T) {
	src := MustContract("spec-delta.md")
	if got := List(src, "delta_order"); !reflect.DeepEqual(got, []string{"RENAMED", "MODIFIED", "REMOVED", "ADDED"}) {
		t.Errorf("delta_order = %q", got)
	}
	if got := List(src, "delta_metadata"); !reflect.DeepEqual(got, []string{"Previously", "Reason", "Migration"}) {
		t.Errorf("delta_metadata = %q", got)
	}
}

func TestScalarQuotedOrBare(t *testing.T) {
	src := "prose absent_key: no\nquoted: \"N/A\"\nbare: plain value \nlist: [a, b]\n"
	for key, want := range map[string]string{"quoted": "N/A", "bare": "plain value", "absent_key": "", "missing": ""} {
		if got := Scalar(src, key); got != want {
			t.Errorf("Scalar(%s) = %q, want %q", key, got, want)
		}
	}
}

func TestDoubleQuotedItemsAndValuesLoseTheirQuotes(t *testing.T) {
	src := strings.Join([]string{
		`globs: ["*.test.*", plain, "*_test.go"]`,
		"notes:",
		`  legacy: "{file} sets {keys}"`,
		"  bare: kept as is",
		"rules:",
		`  - anchor: "a/one"`,
		"",
	}, "\n")
	if got := List(src, "globs"); !reflect.DeepEqual(got, []string{"*.test.*", "plain", "*_test.go"}) {
		t.Errorf("globs = %q", got)
	}
	if _, m := Map(src, "notes"); !reflect.DeepEqual(m, map[string]string{"legacy": "{file} sets {keys}", "bare": "kept as is"}) {
		t.Errorf("notes = %q", m)
	}
	if got := Records(src, "rules"); !reflect.DeepEqual(got, []map[string]string{{"anchor": "a/one"}}) {
		t.Errorf("rules = %v", got)
	}
	if got := InlineList(`["*.py", tests/]`); !reflect.DeepEqual(got, []string{"*.py", "tests/"}) {
		t.Errorf("InlineList = %q", got)
	}
}

func TestTheVocabularyIsAYAMLFileInContracts(t *testing.T) {
	src := MustContract("vocabulary.yaml")
	if !slices.Contains(List(src, "phases"), "apply") {
		t.Errorf("phases = %q", List(src, "phases"))
	}
	if len(Records(src, "audit_rules")) == 0 {
		t.Error("audit_rules is empty")
	}
}
