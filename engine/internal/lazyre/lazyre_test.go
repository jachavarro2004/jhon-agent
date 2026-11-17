package lazyre

import (
	"regexp"
	"slices"
	"testing"
)

// A lazy expression answers as the compiled one does, and compiles once, on
// its first use: a command pays only for the expressions it runs.
func TestALazyExpressionAnswersAsTheCompiledOne(t *testing.T) {
	const pattern = `(?i)^(\w+)-(\d+)$`
	lazy, eager := New(pattern), regexp.MustCompile(pattern)
	if lazy.compiled() {
		t.Fatal("compiled before its first use")
	}
	for _, s := range []string{"fix-12", "FIX-7", "nope", ""} {
		if lazy.MatchString(s) != eager.MatchString(s) || !slices.Equal(lazy.FindStringSubmatch(s), eager.FindStringSubmatch(s)) {
			t.Errorf("%q answers differently", s)
		}
	}
	if !lazy.compiled() || lazy.String() != pattern {
		t.Errorf("after use: compiled %v, String %q", lazy.compiled(), lazy.String())
	}
	if got, want := lazy.ReplaceAllString("a-1", "$2"), eager.ReplaceAllString("a-1", "$2"); got != want {
		t.Errorf("ReplaceAllString %q, want %q", got, want)
	}
	if got := lazy.Regexp(); got.String() != pattern {
		t.Errorf("Regexp() is %q", got)
	}
}

// Every expression declared through New compiles: a broken pattern fails the
// tests as it failed the binary's start when it compiled eagerly.
func TestEveryDeclaredExpressionCompiles(t *testing.T) {
	New(`ok`)
	if err := CompileAll(); err != nil {
		t.Fatal(err)
	}
	New(`(`)
	if err := CompileAll(); err == nil {
		t.Fatal("a broken pattern compiled")
	}
	forget(`(`)
}
