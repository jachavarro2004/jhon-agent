package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// A Jhon profile (T77b) picks every agent's model per runtime: the
// orchestrator's and every sub-agent's (jhon-scout, jhon-builder, jhon-checker). It lives
// in the owner's ~/.config/jhon/profiles.json (internal/profiles.Path, read
// under HOME, not XDG_CONFIG_HOME), and `jhon profile use <name>` activates
// it (settings' pi.profile) and re-renders every runtime. `bench prepare
// --pi-profile FILE --profile NAME` does both in the bench HOME, with the
// bench HOME's own jhon, so the owner's own files are never read or written
// by Jhon; the bench reads only the file the owner names.

// benchProfiles is the bench HOME's profiles file.
func benchProfiles(home string) string {
	return filepath.Join(home, ".config", "jhon", "profiles.json")
}

// UseProfile copies file (when given) to the bench HOME's profiles.json and
// runs the bench HOME's `jhon profile use name` there, with an environment
// that names only the bench HOME.
func UseProfile(home, file, name string, base []string, out io.Writer) error {
	if name == "" {
		return fmt.Errorf("--profile: name the profile to activate")
	}
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("--pi-profile: %v", err)
		}
		var o map[string]any
		if json.Unmarshal(b, &o) != nil || o == nil {
			return fmt.Errorf("--pi-profile %s is not a JSON object (a profiles.json: {\"profiles\": {...}})", file)
		}
		if err := os.MkdirAll(filepath.Dir(benchProfiles(home)), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(benchProfiles(home), b, 0o600); err != nil {
			return err
		}
	}
	// JHON_PROFILES would point jhon at another profiles file.
	env := slices.DeleteFunc(benchEnv(base, home, filepath.Join(home, ".cache")), func(kv string) bool { return strings.HasPrefix(kv, "JHON_PROFILES=") })
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	argv := []string{benchJhon(home), "profile", "use", name}
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir, c.Env, c.Stdout, c.Stderr = home, env, out, out
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %v", commandLine(argv), err)
	}
	fmt.Fprintf(out, "profile %s active in the bench HOME %s: every runtime re-rendered with its models, sub-agents included\n", name, home)
	return nil
}

// activeProfile is the bench HOME's active Jhon profile, "" for default:
// settings' pi.profile (the one source, as internal/render reads it), else
// profiles.json's active.
func activeProfile(home string) string {
	name := settingsProfile(filepath.Join(home, ".config", "jhon", "settings.toml"))
	if name == "" {
		var f struct {
			Active string `json:"active"`
		}
		if b, err := os.ReadFile(benchProfiles(home)); err == nil && json.Unmarshal(b, &f) == nil {
			name = f.Active
		}
	}
	if name == "default" {
		return ""
	}
	return name
}

// settingsProfile is a settings.toml's pi.profile: `profile = "x"` under
// [pi], or `pi.profile = "x"` at the top; "" when absent.
func settingsProfile(file string) string {
	b, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	table := ""
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(l, "[") {
			table = strings.TrimSpace(strings.Trim(l, "[]"))
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if table == "pi" && k == "profile" || table == "" && k == "pi.profile" {
			v, _, _ = strings.Cut(strings.TrimSpace(v), "#")
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// profileMainModel is the pi main session's model the active profile
// rendered: the bench HOME's ~/.pi/jhon/settings.json defaultProvider,
// defaultModel and defaultThinkingLevel, as provider/model[@thinking].
func profileMainModel(home string) (string, error) {
	file := filepath.Join(home, ".pi", "jhon", "settings.json")
	var s struct{ DefaultProvider, DefaultModel, DefaultThinkingLevel string }
	b, err := os.ReadFile(file)
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	if err != nil || s.DefaultProvider == "" || s.DefaultModel == "" {
		return "", fmt.Errorf("the bench HOME's profile names no pi main model in %s (%v): give --pi-model", file, err)
	}
	m := s.DefaultProvider + "/" + s.DefaultModel
	if s.DefaultThinkingLevel != "" {
		m += "@" + s.DefaultThinkingLevel
	}
	return m, nil
}

// isJhon is whether cond runs Jhon, so a profile sets its sub-agents' models.
func isJhon(cond string) bool { _, ok := noJhon[cond]; return ok }

// profileSep joins a model and its profile in a cell's model identity.
const profileSep = " profile "

// ident is a run's model identity: its model, and its profile when one was
// active, so cells of two profiles never merge (T77b).
func ident(model, profile string) string {
	if model == "" || profile == "" {
		return model
	}
	return model + profileSep + profile
}

// withoutProfile is an identity's model alone.
func withoutProfile(id string) string {
	m, _, _ := strings.Cut(withoutSettings(id), profileSep)
	return m
}

// printProfile writes the profile the Jhon conditions run, and which model
// pi's main session takes.
func printProfile(o *Options) {
	if o.Profile == "" {
		return
	}
	jhons := slices.DeleteFunc(slices.Clone(o.Conditions), func(c string) bool { return !isJhon(c) })
	if len(jhons) == 0 {
		return
	}
	fmt.Fprintf(o.Out, "profile: %s (the bench HOME's active Jhon profile: the sub-agents of %s)\n", o.Profile, strings.Join(jhons, ", "))
	if !slices.Contains(o.Conditions, "pi-jhon") {
		return
	}
	if o.PiModel != "" {
		fmt.Fprintf(o.Out, "--pi-model %s sets pi-jhon's main session only; its sub-agents run profile %s's models\n", o.PiModel, o.Profile)
	} else {
		fmt.Fprintf(o.Out, "pi main session: %s, profile %s's orchestrator model\n", o.profileMain, o.Profile)
	}
}
