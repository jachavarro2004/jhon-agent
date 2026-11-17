package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// AllVariants are the task variants, in plan order (L395, L483):
//   - plain: the template as it is;
//   - ldd: the template with ldd/config.yaml as openspec/config.yaml and
//     ldd/pyproject.toml at its root, so Jhon's LDD is on with Strict TDD
//     and the project is testable with `python3 -m unittest`;
//   - ldd-native: the same config in Jhon's native layout, ldd/config.yaml,
//     with the same pyproject.toml and no openspec/;
//   - ldd-ask: the plain template, with AskSentence before the first owner
//     message: Jhon creates ldd/ when asked, without asking further (L483).
//
// clean runs only the plain variant: Claude Code without Jhon ignores openspec.
// Jhon always works under LDD (L554-L559): under a Jhon condition a plain
// repo is set up like ldd-ask's (setsUpLDD).
var AllVariants = []string{"plain", "ldd", "ldd-native", "ldd-ask"}

// DefaultVariants are the variants `bench run` takes when none is named.
var DefaultVariants = []string{"plain", "ldd"}

// AskSentence is what the ldd-ask variant puts before the first owner
// message, the same for every task.
const AskSentence = "Use LDD for this work."

// lddFiles are each variant's files: their source under ldd/ and their
// place in the repository.
var lddFiles = map[string][][2]string{
	"ldd":        {{"config.yaml", filepath.Join("openspec", "config.yaml")}, {"pyproject.toml", "pyproject.toml"}},
	"ldd-native": {{"config.yaml", filepath.Join("ldd", "config.yaml")}, {"pyproject.toml", "pyproject.toml"}},
}

// isLDD is whether variant v is one of the LDD variants.
func isLDD(v string) bool { return strings.HasPrefix(v, "ldd") }

// setsUpLDD is whether a run of variant v under cond must set LDD up itself:
// ldd-ask, and plain under a Jhon condition (L554-L559); the clean
// conditions are the no-Jhon baseline and run plain repos (L552).
func setsUpLDD(cond, v string) bool { return v == "ldd-ask" || v == "plain" && !isClean(cond) }

// asked is t as the ldd-ask variant sends it: AskSentence before its first
// message, the rest unchanged.
func asked(t Task) Task {
	t.Prompts = slices.Clone(t.Prompts)
	t.Prompts[0] = AskSentence + "\n\n" + t.Prompts[0]
	t.Prompt = t.Prompts[0]
	return t
}

// repoLayout is the LDD layout repo has: openspec, ldd (Jhon's native one,
// detected by its config or its changes directory) or none.
func repoLayout(repo string) string {
	switch {
	case fileExists(filepath.Join(repo, "openspec")):
		return "openspec"
	case fileExists(filepath.Join(repo, "ldd", "config.yaml")) || fileExists(filepath.Join(repo, "ldd", "changes")):
		return "ldd"
	}
	return "none"
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func checkVariant(v string) error {
	if !slices.Contains(AllVariants, v) {
		return fmt.Errorf("unknown variant %q (known: %s)", v, strings.Join(AllVariants, ", "))
	}
	return nil
}

// runsVariant is whether cond runs in variant v.
func runsVariant(cond, v string) bool { return v == "plain" || !isClean(cond) }

// prepareRepo copies t's template into repo, with the variant's ldd files.
func prepareRepo(t Task, variant, repo string) error {
	if err := checkVariant(variant); err != nil {
		return err
	}
	if err := copyTree(t.Template(), repo); err != nil {
		return err
	}
	for _, f := range lddFiles[variant] {
		b, err := os.ReadFile(filepath.Join(source(), "ldd", f[0]))
		if err != nil {
			return err
		}
		target := filepath.Join(repo, f[1])
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}
