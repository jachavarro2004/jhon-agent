package sh

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// corpusCommands is the Bash inputs of the guard corpus (test/guards/corpus).
func corpusCommands(f *testing.F) []string {
	f.Helper()
	files, err := filepath.Glob("../../../test/guards/corpus/*.tsv")
	if err != nil || len(files) == 0 {
		f.Fatalf("no guard corpus: %v", err)
	}
	var out []string
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			f.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			col := strings.Split(line, "\t")
			if strings.HasPrefix(line, "#") || len(col) < 3 || col[1] != "Bash" {
				continue
			}
			if s, err := strconv.Unquote(col[2]); err == nil {
				out = append(out, s)
			}
		}
	}
	return out
}

// parseTestStrings is every string literal of parse_test.go: the parser's
// own test inputs.
func parseTestStrings(f *testing.F) []string {
	f.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "parse_test.go", nil, 0)
	if err != nil {
		f.Fatal(err)
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil {
				out = append(out, s)
			}
		}
		return true
	})
	return out
}

// programsOf is the programs of every command Parse reads from src.
func programsOf(src string) ([]*Command, []Program, error) {
	cmds, err := Parse(src)
	if err != nil {
		return nil, nil, err
	}
	var ps []Program
	for _, c := range cmds {
		p, err := CommandPrograms(c, nil, nil, Opts{})
		ps = append(ps, p...)
		if err != nil {
			return cmds, ps, err
		}
	}
	return cmds, ps, nil
}

// renumber is the scopes of cmds and ps numbered from 1 in the order they
// first appear: Parse numbers subshells from a counter it shares across
// calls, so two reads differ only there.
func renumber(cmds []*Command, ps []Program) {
	ids := map[int64]int64{}
	re := func(scope []int64) {
		for i, id := range scope {
			if _, ok := ids[id]; !ok {
				ids[id] = int64(len(ids) + 1)
			}
			scope[i] = ids[id]
		}
	}
	for _, c := range cmds {
		re(c.Scope)
	}
	for i := range ps {
		re(ps[i].Scope)
	}
}

// T53: Parse and CommandPrograms never panic, and read one input the same
// way twice, but for the numbers of their subshells. Seeds are the guard corpus and the parser's test inputs.
func FuzzParse(f *testing.F) {
	for _, s := range append(corpusCommands(f), parseTestStrings(f)...) {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		c1, p1, e1 := programsOf(src)
		c2, p2, e2 := programsOf(src)
		renumber(c1, p1)
		renumber(c2, p2)
		if (e1 == nil) != (e2 == nil) || (e1 != nil && e1.Error() != e2.Error()) {
			t.Fatalf("%q: errors differ: %v, %v", src, e1, e2)
		}
		if !reflect.DeepEqual(c1, c2) || !reflect.DeepEqual(p1, p2) {
			t.Fatalf("%q: two reads differ", src)
		}
	})
}
