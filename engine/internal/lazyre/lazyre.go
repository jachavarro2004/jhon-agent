// Package lazyre is a regular expression compiled on its first use. The
// binary declares a few hundred package-level expressions, and every hook a
// tool call runs is a fresh process: compiled eagerly, they cost each
// process their compile time whether it uses them or not. Declared through
// New, a process compiles only the ones it runs. CompileAll keeps a broken
// pattern a test failure, as it was a start failure when compiled eagerly.
package lazyre

import (
	"fmt"
	"regexp"
	"sync"
)

// Regexp is a *regexp.Regexp compiled on its first use; it answers as the
// compiled expression does.
type Regexp struct {
	pattern string
	once    sync.Once
	re      *regexp.Regexp
}

var (
	mu       sync.Mutex
	declared []*Regexp
)

// New declares pattern, compiled on the expression's first use. A pattern
// that does not compile panics there, as regexp.MustCompile does.
func New(pattern string) *Regexp {
	r := &Regexp{pattern: pattern}
	mu.Lock()
	declared = append(declared, r)
	mu.Unlock()
	return r
}

// CompileAll compiles every expression New declared; the error names the
// first that does not compile.
func CompileAll() error {
	mu.Lock()
	all := append([]*Regexp{}, declared...)
	mu.Unlock()
	for _, r := range all {
		if _, err := regexp.Compile(r.pattern); err != nil {
			return fmt.Errorf("lazyre: %q does not compile: %w", r.pattern, err)
		}
	}
	return nil
}

// forget drops the declarations of pattern (tests only).
func forget(pattern string) {
	mu.Lock()
	defer mu.Unlock()
	kept := declared[:0]
	for _, r := range declared {
		if r.pattern != pattern {
			kept = append(kept, r)
		}
	}
	declared = kept
}

// Regexp is the compiled expression.
func (r *Regexp) Regexp() *regexp.Regexp {
	r.once.Do(func() { r.re = regexp.MustCompile(r.pattern) })
	return r.re
}

func (r *Regexp) compiled() bool { return r.re != nil }

// String is the pattern; it compiles nothing.
func (r *Regexp) String() string { return r.pattern }

func (r *Regexp) Match(b []byte) bool                  { return r.Regexp().Match(b) }
func (r *Regexp) MatchString(s string) bool            { return r.Regexp().MatchString(s) }
func (r *Regexp) FindString(s string) string           { return r.Regexp().FindString(s) }
func (r *Regexp) FindStringIndex(s string) []int       { return r.Regexp().FindStringIndex(s) }
func (r *Regexp) FindStringSubmatch(s string) []string { return r.Regexp().FindStringSubmatch(s) }
func (r *Regexp) FindStringSubmatchIndex(s string) []int {
	return r.Regexp().FindStringSubmatchIndex(s)
}
func (r *Regexp) FindAllString(s string, n int) []string { return r.Regexp().FindAllString(s, n) }
func (r *Regexp) FindAllStringIndex(s string, n int) [][]int {
	return r.Regexp().FindAllStringIndex(s, n)
}
func (r *Regexp) FindAllStringSubmatch(s string, n int) [][]string {
	return r.Regexp().FindAllStringSubmatch(s, n)
}
func (r *Regexp) FindAllStringSubmatchIndex(s string, n int) [][]int {
	return r.Regexp().FindAllStringSubmatchIndex(s, n)
}
func (r *Regexp) ReplaceAllString(s, repl string) string { return r.Regexp().ReplaceAllString(s, repl) }
func (r *Regexp) ReplaceAllLiteralString(s, repl string) string {
	return r.Regexp().ReplaceAllLiteralString(s, repl)
}
func (r *Regexp) ReplaceAllStringFunc(s string, repl func(string) string) string {
	return r.Regexp().ReplaceAllStringFunc(s, repl)
}
func (r *Regexp) Split(s string, n int) []string { return r.Regexp().Split(s, n) }
func (r *Regexp) SubexpNames() []string          { return r.Regexp().SubexpNames() }
func (r *Regexp) SubexpIndex(name string) int    { return r.Regexp().SubexpIndex(name) }
func (r *Regexp) NumSubexp() int                 { return r.Regexp().NumSubexp() }
