package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// A pi models file (T146 F5-b) is the owner's pi models.json, which defines
// a provider pi has no built-in for (NaN): `bench prepare --pi-models FILE`
// copies it to the bench HOME's ~/.pi/jhon/models.json, the agent dir
// `jhon start pi` gives pi-jhon and every sub-agent it spawns
// (PI_CODING_AGENT_DIR), and `bench run --pi-models FILE` copies it into
// each pi-clean run's own agent dir before pi starts, so both conditions
// resolve the same provider. The file is copied byte for byte, mode 0600,
// and only checked to be a JSON object: its apiKey may be a command
// (`!secret-tool …`), which the bench never runs, prints or logs.

// benchPiModels is the bench HOME's pi models file, pi-jhon's.
func benchPiModels(home string) string { return filepath.Join(home, ".pi", "jhon", "models.json") }

// readPiModels is file's bytes when it holds a JSON object.
func readPiModels(file string) ([]byte, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("--pi-models %s: cannot read it (%v)", file, errorsKind(err))
	}
	var o map[string]json.RawMessage
	if json.Unmarshal(b, &o) != nil || o == nil {
		// No parse detail: it could quote the file.
		return nil, fmt.Errorf("--pi-models %s is not a JSON object (a pi models.json: {\"providers\": {...}})", file)
	}
	return b, nil
}

// errorsKind is a file error without its path, which the message names.
func errorsKind(err error) error {
	if pe, ok := err.(*os.PathError); ok {
		return pe.Err
	}
	return err
}

// writePrivate writes b to file with mode 0600, replacing what is there.
func writePrivate(file string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".models-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}

// PutPiModels copies the pi models file to the bench HOME's
// ~/.pi/jhon/models.json, where pi-jhon and its sub-agents read it.
func PutPiModels(home, file string, out io.Writer) error {
	b, err := readPiModels(file)
	if err != nil {
		return err
	}
	if err := writePrivate(benchPiModels(home), b); err != nil {
		return err
	}
	fmt.Fprintf(out, "pi models file %s copied to %s (mode 0600): pi-jhon and its sub-agents read it\n", file, benchPiModels(home))
	return nil
}

// benchPiModelsState is how the bench HOME's models file stands against
// the run's: the same file, another one, or none.
func benchPiModelsState(home string, models []byte) string {
	b, err := os.ReadFile(benchPiModels(home))
	switch {
	case err != nil:
		return "absent"
	case bytes.Equal(b, models):
		return "the same file"
	}
	return "not this file"
}

// checkBenchPiModels refuses a pi-jhon run whose --pi-models is not the
// bench HOME's models file.
func checkBenchPiModels(o *Options) error {
	if o.PiModels == "" || !slices.Contains(o.Conditions, "pi-jhon") {
		return nil
	}
	if s := benchPiModelsState(o.Home, o.piModels); s != "the same file" {
		return fmt.Errorf("pi-jhon reads %s, which is %s, not --pi-models %s: run `bench prepare --pi-models %s` first", benchPiModels(o.Home), s, o.PiModels, o.PiModels)
	}
	return nil
}

// subAgentModels are the bench HOME's rendered pi sub-agents
// (~/.pi/jhon/agents/<name>.md) by model, `model[@thinking]`, sorted.
func subAgentModels(home string) (models []string, agents map[string][]string) {
	files, _ := filepath.Glob(filepath.Join(home, ".pi", "jhon", "agents", "*.md"))
	agents = map[string][]string{}
	for _, f := range files {
		model, thinking := frontmatterModel(f)
		if model == "" {
			continue
		}
		if thinking != "" {
			model += "@" + thinking
		}
		if _, ok := agents[model]; !ok {
			models = append(models, model)
		}
		agents[model] = append(agents[model], strings.TrimSuffix(filepath.Base(f), ".md"))
	}
	slices.Sort(models)
	for _, m := range models {
		slices.Sort(agents[m])
	}
	return models, agents
}

// frontmatterModel is an agent definition's frontmatter model and thinking.
func frontmatterModel(file string) (model, thinking string) {
	b, err := os.ReadFile(file)
	if err != nil {
		return "", ""
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for n := 0; sc.Scan(); n++ {
		l := sc.Text()
		if l == "---" {
			if n > 0 {
				break
			}
			continue
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "model":
			model = strings.TrimSpace(v)
		case "thinking":
			thinking = strings.TrimSpace(v)
		}
	}
	return model, thinking
}

// printPiModels writes, on a dry run with --pi-models, the models file,
// where each pi condition reads it, and the model each condition runs.
func printPiModels(o *Options, runs []*Run) {
	if o.PiModels == "" {
		return
	}
	fmt.Fprintf(o.Out, "pi models file: %s (a JSON object; its contents are never printed)\n", o.PiModels)
	if slices.Contains(o.Conditions, "pi-jhon") {
		state := benchPiModelsState(o.Home, o.piModels)
		if state != "the same file" {
			state += "; run `bench prepare --pi-models " + o.PiModels + "` first"
		}
		fmt.Fprintf(o.Out, "  pi-jhon and its sub-agents read %s: %s\n", benchPiModels(o.Home), state)
	}
	if slices.Contains(o.Conditions, "pi-clean") {
		fmt.Fprintln(o.Out, "  pi-clean: copied to each run's pi-agent/models.json (mode 0600) before pi starts")
	}
	fmt.Fprintln(o.Out, "models per condition:")
	for _, c := range o.Conditions {
		var mains []string
		for _, r := range runs {
			if r.Cond.Name() == c && !slices.Contains(mains, r.Model) {
				mains = append(mains, r.Model)
			}
		}
		if len(mains) == 0 {
			continue
		}
		main := strings.Join(mains, ", ")
		switch c {
		case "pi-jhon":
			models, agents := subAgentModels(o.Home)
			var subs []string
			for _, m := range models {
				subs = append(subs, fmt.Sprintf("%s (%s)", m, strings.Join(agents[m], ", ")))
			}
			sub := "none rendered in " + filepath.Join(o.Home, ".pi", "jhon", "agents")
			if len(subs) > 0 {
				sub = strings.Join(subs, ", ")
			}
			fmt.Fprintf(o.Out, "  pi-jhon: main session %s; sub-agents %s\n", main, sub)
		case "pi-clean":
			fmt.Fprintf(o.Out, "  pi-clean: %s, no sub-agents\n", main)
		default:
			fmt.Fprintf(o.Out, "  %s: %s (Claude Code)\n", c, main)
		}
	}
}
