package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Project settings (T86, L615): `bench run --project-setting KEY=VALUE`
// writes a setting the product reads into each run's LDD config after the
// repo is laid out and before the agent starts, committed with the template
// as the variant's config is. Only the variants whose repo has a config at
// the start take one. Each run records its settings, and a cell's identity
// carries them (identity), so report, value and certify never mix runs
// with and without a setting.

// projectSettingValues are the keys the product reads, with their values.
var projectSettingValues = map[string][]string{
	"worktree_per_change": {"true", "false"}, // engine/internal/ldd/config.go (T85a)
}

// settingsSep joins a cell's model identity and its project settings.
const settingsSep = " with "

// checkProjectSettings checks each KEY=VALUE of settings.
func checkProjectSettings(settings []string) error {
	seen := map[string]bool{}
	for _, s := range settings {
		k, v, ok := strings.Cut(s, "=")
		if !ok || k == "" || v == "" {
			return fmt.Errorf("--project-setting %s: give KEY=VALUE", s)
		}
		values, known := projectSettingValues[k]
		if !known {
			return fmt.Errorf("--project-setting %s: unknown key %s (known: %s)", s, k, strings.Join(slices.Sorted(func(yield func(string) bool) {
				for k := range projectSettingValues {
					if !yield(k) {
						return
					}
				}
			}), ", "))
		}
		if !slices.Contains(values, v) {
			return fmt.Errorf("--project-setting %s: %s takes %s", s, k, strings.Join(values, " or "))
		}
		if seen[k] {
			return fmt.Errorf("--project-setting %s is given twice", k)
		}
		seen[k] = true
	}
	return nil
}

// configVariants are the variants whose repo has an LDD config at the start.
func configVariants() []string {
	var out []string
	for _, v := range AllVariants {
		if configFile(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

// configFile is variant v's LDD config in the repo, "" when it has none.
func configFile(v string) string {
	for _, f := range lddFiles[v] {
		if f[0] == "config.yaml" {
			return f[1]
		}
	}
	return ""
}

// refuseWithoutConfig is an error naming the variants with no config.
func refuseWithoutConfig(variants []string) error {
	var none []string
	for _, v := range variants {
		if configFile(v) == "" {
			none = append(none, v)
		}
	}
	if len(none) == 0 {
		return nil
	}
	noun, verb := "variant", "has"
	if len(none) > 1 {
		noun, verb = "variants", "have"
	}
	return fmt.Errorf("--project-setting sets the task repo's LDD config; %s %s %s none at the start (it applies to %s)", noun, strings.Join(none, ", "), verb, strings.Join(configVariants(), ", "))
}

// writeProjectSettings sets each KEY=VALUE in repo's LDD config of variant:
// a top-level `KEY: ...` line is replaced, else `KEY: VALUE` is appended.
func writeProjectSettings(repo, variant string, settings []string) error {
	if len(settings) == 0 {
		return nil
	}
	rel := configFile(variant)
	if rel == "" {
		return refuseWithoutConfig([]string{variant})
	}
	file := filepath.Join(repo, rel)
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	text := string(b)
	for _, s := range settings {
		k, v, _ := strings.Cut(s, "=")
		line := k + ": " + v
		lines := strings.Split(text, "\n")
		replaced := false
		for i, l := range lines {
			if strings.HasPrefix(l, k+":") {
				lines[i], replaced = line, true
			}
		}
		if replaced {
			text = strings.Join(lines, "\n")
			continue
		}
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += line + "\n"
	}
	return os.WriteFile(file, []byte(text), 0o644)
}

// identity is a cell's model identity: the model, its profile and the
// runs' project settings.
func identity(model, profile string, settings []string) string {
	id := ident(model, profile)
	if len(settings) > 0 {
		id += settingsSep + strings.Join(settings, ",")
	}
	return id
}

// withoutSettings is an identity without its project settings.
func withoutSettings(id string) string { m, _, _ := strings.Cut(id, settingsSep); return m }

// projectSettingsFlag is --project-setting: repeatable, comma-separated.
type projectSettingsFlag []string

func (p *projectSettingsFlag) String() string { return strings.Join(*p, ",") }
func (p *projectSettingsFlag) Set(s string) error {
	*p = append(*p, split(s)...)
	return nil
}
