package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/profiles"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
	"github.com/jachavarro2004/jhon-agent/engine/internal/telemetry"
)

// `jhon settings`: the owner's settings (S38), settings.toml schema 1.
//
//	jhon settings [list] [--json]
//	jhon settings get <key> [--json]
//	jhon settings set <key> <value>
//	jhon settings migrate [--json]
//
// list prints every key with its value and source (default, file, env;
// --json adds the schema's kind, accepted values and default), then the
// project's LDD rules from openspec/config.yaml (source project, read-only).
// set edits the file in place, keeping comments; a value the schema refuses
// exits 2 with what it accepts, and nothing is written.
// list and get first migrate the active profile from profiles.json when
// there is no settings.toml yet. A key T15.3 removed is no error: list says on stderr
// what goes and why (--json lists it under "retired"), and migrate drops it
// from the file, backed up. Exit 1 when the file cannot be used.
func init() { register("settings", runSettings) }

var settingsUsage = `usage: jhon settings [list] [--json]
       jhon settings get <key> [--json]
       jhon settings set <key> <value>
       jhon settings migrate [--json]
`

func settingsUsageError(msg string) (int, error) {
	fmt.Fprintf(os.Stderr, "jhon settings: %s\n\n%s", msg, settingsUsage)
	return 2, nil
}

func runSettings(argv []string) (int, error) {
	asJSON := false
	var args []string
	for _, a := range argv {
		switch {
		case a == "--json":
			asJSON = true
		case strings.HasPrefix(a, "-") && a != "-":
			return settingsUsageError("unknown option: " + a)
		default:
			args = append(args, a)
		}
	}
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	path := settings.Path()
	switch sub {
	case "list":
		if len(args) != 0 {
			return settingsUsageError("list takes no argument")
		}
		autoMigrate(path)
		return settingsList(path, asJSON)
	case "get":
		if len(args) != 1 {
			return settingsUsageError("get takes one key")
		}
		if !settings.Known(args[0]) {
			if why := settings.RetiredWhy(args[0]); why != "" {
				return settingsInvalid(&settings.InvalidError{Key: args[0], Msg: "removed in " + settings.RetiredIn(args[0]) + ": " + why})
			}
			return settingsInvalid(&settings.InvalidError{Key: args[0], Msg: "unknown key"})
		}
		autoMigrate(path)
		vals, err := settings.Load(path, os.LookupEnv)
		if err != nil {
			return settingsFileError(err)
		}
		v, ok := settings.Find(vals, args[0])
		if !ok { // never for a schema key: Load gives every one
			return settingsInvalid(&settings.InvalidError{Key: args[0], Msg: "not set"})
		}
		if asJSON {
			return 0, encodeJSON(v)
		}
		fmt.Println(display(v.Value))
		return 0, nil
	case "set":
		if len(args) != 2 {
			return settingsUsageError("set takes a key and a value")
		}
		if asJSON {
			return settingsUsageError("set takes no --json")
		}
		v, err := settings.Set(path, args[0], args[1])
		if err != nil {
			return settingsInvalid(err)
		}
		fmt.Printf("%s = %s in %s\n", v.Key, display(v.Value), release.Tilde(path))
		if vals, err := settings.Load(path, os.LookupEnv); err == nil {
			if now, _ := settings.Find(vals, v.Key); now.Source == settings.SourceEnv {
				fmt.Printf("note: %s is set and overrides it (%s)\n", now.Env, display(now.Value))
			}
		}
		return 0, nil
	case "migrate":
		if len(args) != 0 {
			return settingsUsageError("migrate takes no argument")
		}
		rep, err := migrate(path)
		if err != nil {
			return 1, err
		}
		clean, err := settings.Cleanup(path, time.Now())
		if err != nil {
			return settingsFileError(err)
		}
		if asJSON {
			return 0, encodeJSON(struct {
				settings.Report
				Retired settings.CleanReport `json:"retired"`
			}{rep, clean})
		}
		if !rep.Done && !clean.Done {
			fmt.Printf("nothing to migrate (%s)\n", rep.Reason)
			return 0, nil
		}
		if rep.Done {
			fmt.Printf("migrated %d value(s) into %s\nbackup: %s\n", len(rep.Mapped), release.Tilde(path), release.Tilde(rep.Backup))
			for _, m := range rep.Mapped {
				fmt.Printf("  %s = %s  <- %s\n", m.Key, display(m.Value), release.Tilde(m.From))
			}
			for _, s := range rep.Skipped {
				fmt.Printf("  skipped: %s\n", s)
			}
		}
		if clean.Done {
			fmt.Printf("dropped %d retired setting(s) from %s\nbackup: %s\n", len(clean.Notes), release.Tilde(path), release.Tilde(clean.Backup))
			for _, n := range clean.Notes {
				fmt.Printf("  %s\n", retiredLine(n))
			}
		}
		return 0, nil
	}
	return settingsUsageError("unknown subcommand: " + sub)
}

// settingsInvalid is a refused key or value: exit 2, or 1 for any other error.
func settingsInvalid(err error) (int, error) {
	var inv *settings.InvalidError
	if !errors.As(err, &inv) {
		return settingsFileError(err)
	}
	msg := inv.Error()
	if a := settings.Accepted(inv.Key); a != "" && !strings.Contains(msg, "accepted:") && !strings.Contains(msg, "required for now") && inv.Msg != "not set" {
		msg += " (accepted: " + a + ")"
	}
	if inv.Msg == "unknown key" {
		msg += " (jhon settings list shows every key)"
	}
	fmt.Fprintf(os.Stderr, "jhon settings: %s\n", msg)
	return 2, nil
}

func settingsFileError(err error) (int, error) {
	fmt.Fprintf(os.Stderr, "jhon settings: %v\n", err)
	return 1, nil
}

// retiredLine says what one retired key was and what became of it.
func retiredLine(n settings.Note) string {
	was := fmt.Sprintf("%s = %s", n.Key, tomlShow(n.Was))
	if n.Ignored != "" {
		return fmt.Sprintf("%s is ignored: %s is set", was, n.Ignored)
	}
	if n.Now != "" {
		return fmt.Sprintf("%s reads as %s: %s", was, n.Now, n.Why)
	}
	return fmt.Sprintf("%s is no longer read: %s", was, n.Why)
}

// tomlShow is a value as the file wrote it: a string quoted.
func tomlShow(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	return fmt.Sprint(v)
}

// warnRetired says, on stderr, which retired keys the file holds and how to drop them.
func warnRetired(path string, notes []settings.Note) {
	var said bool
	for _, n := range notes {
		if n.Silent {
			continue
		}
		fmt.Fprintf(os.Stderr, "jhon settings: %s\n", retiredLine(n))
		said = true
	}
	if said {
		fmt.Fprintf(os.Stderr, "jhon settings: `jhon settings migrate` drops them from %s (backed up first)\n", release.Tilde(path))
	}
}

// settingsList is `jhon settings list`.
func settingsList(path string, asJSON bool) (int, error) {
	vals, err := settings.Load(path, os.LookupEnv)
	if err != nil {
		return settingsFileError(err)
	}
	notes, _ := settings.Retired(path)
	if notes == nil {
		notes = []settings.Note{}
	}
	root := project.Root(cwd())
	proj := settings.Project(root)
	if asJSON {
		type projectRules struct {
			Root     string           `json:"root"`
			File     string           `json:"file"`
			Settings []settings.Value `json:"settings"`
		}
		out := struct {
			Schema   int              `json:"schema"`
			Path     string           `json:"path"`
			Settings []settings.Value `json:"settings"`
			Retired  []settings.Note  `json:"retired"`
			Project  *projectRules    `json:"project"`
		}{settings.Schema, path, vals, notes, nil}
		if proj != nil {
			out.Project = &projectRules{root, project.LddConfigFile(root), proj}
		}
		return 0, encodeJSON(out)
	}
	warnRetired(path, notes)
	fmt.Printf("settings  %s\n", release.Tilde(path))
	printRows(vals)
	if proj != nil {
		fmt.Printf("\nproject  %s (read-only)\n", release.Tilde(project.LddConfigFile(root)))
		printRows(proj)
	}
	return 0, nil
}

func printRows(vals []settings.Value) {
	kw, vw := 0, 0
	for _, v := range vals {
		kw, vw = max(kw, len(v.Key)), max(vw, len(display(v.Value)))
	}
	for _, v := range vals {
		source := v.Source
		if v.Env != "" {
			source += " (" + v.Env + ")"
		}
		fmt.Printf("  %-*s  %-*s  %s\n", kw, v.Key, vw, display(v.Value), source)
	}
}

// display is a value as the listing shows it: a string bare ("" when empty),
// a block's lines joined.
func display(v any) string {
	switch x := v.(type) {
	case string:
		if x == "" {
			return `""`
		}
		return x
	case []string:
		return strings.Join(x, "; ")
	}
	return fmt.Sprint(v)
}

func encodeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func migrationSources() settings.Sources {
	return settings.Sources{Profiles: profiles.Path()}
}

// integrationEvidence is what this machine shows of each opt-in integration.
func integrationEvidence() settings.Evidence {
	return settings.Evidence{
		Telemetry: !telemetry.Off(),
	}
}

func migrate(path string) (settings.Report, error) {
	return settings.Migrate(path, migrationSources(), integrationEvidence(), time.Now())
}

// autoMigrate runs the one-time migration before a read, when it has work;
// a failure is said and the read goes on from the defaults.
func autoMigrate(path string) {
	if !settings.Pending(path, migrationSources()) {
		return
	}
	rep, err := migrate(path)
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "jhon settings: the migration of the old settings failed: %v\n", err)
	case rep.Done:
		fmt.Fprintf(os.Stderr, "jhon settings: migrated %d value(s) from the old settings into %s (backup: %s); jhon settings migrate --json lists them\n",
			len(rep.Mapped), release.Tilde(path), release.Tilde(rep.Backup))
	}
}
