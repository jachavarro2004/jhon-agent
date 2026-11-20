package cli

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
	"github.com/jachavarro2004/jhon-agent/engine/internal/profiles"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/rename"
	"github.com/jachavarro2004/jhon-agent/engine/internal/render"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
	"github.com/jachavarro2004/jhon-agent/engine/internal/telemetry"
)

// `jhon profile` (over internal/profiles):
//
//	jhon profile [list]        the profiles, the active one marked, each with
//	                           the provider pi runs on (its orchestrator's)
//	jhon profile show [<name>] each agent's model in each runtime, and its source
//	jhon profile use <name>    activate a profile (settings' pi.profile, and
//	                           profiles.json's active), then re-render every runtime
//	jhon profile json          every profile, pi's provider, and each agent's
//	                           model, as JSON, for the shell's /jhon:profile
//	jhon profile new <name> [--from <profile>]
//	                           a new profile, empty or a copy of another
//	jhon profile set <profile> <agent> <runtime> <model[@effort] | ->
//	                           one agent's model in one runtime; - removes it,
//	                           back to agents.yaml
//	jhon profile mode <profile> <inline|writer>
//	                           the profile's delegation mode; inline, the
//	                           default, is not written; default is always inline
//	jhon profile rm <name>     delete a profile, never default or the active one
//
// Each edit is checked whole against the manifest before it is written, and
// one to the active profile re-renders every runtime, as use does.
// The re-render after `use`: the active
// release installs every runtime's projections again (release.Install).
func init() { register("profile", runProfile) }

const profileUsage = "usage: jhon profile [list] | show [<name>] | use <name> | json | new <name> [--from <profile>] | set <profile> <agent> <runtime> <model[@effort] | -> | mode <profile> <inline|writer> | rm <name>"

func runProfile(argv []string) (int, error) {
	if len(argv) > 1 {
		telemetry.Note("jhon.profile", argv[1])
	}
	if len(argv) > 0 && slices.Contains([]string{"new", "set", "mode", "rm"}, argv[0]) {
		code, active := profileEdit(argv)
		if code != 0 || !active {
			return code, nil
		}
		return rerender()
	}
	use := len(argv) > 0 && argv[0] == "use"
	code := profileCommand(argv)
	if code != 0 || !use {
		return code, nil
	}
	return rerender()
}

// profileEdit runs new, set, mode, or rm: the edit applied to the file's profiles,
// checked whole, then written; and whether it changed the active profile,
// whose projections must be rendered again.
func profileEdit(argv []string) (int, bool) {
	m := manifest.MustLoadRelease()
	ctx := profiles.Context{Runtimes: m.Matrix.Runtimes, Tiers: m.TierNames(), Agents: m.AgentNames()}
	file := profiles.Path()
	pf, readErrs := render.ReadProfiles()
	fail := func(msg string) (int, bool) {
		fmt.Fprintf(os.Stderr, "jhon profile: %s\n", msg)
		return 1, false
	}
	if len(readErrs) > 0 {
		return fail(strings.Join(readErrs, "; "))
	}
	active, _ := profiles.Active(pf)
	var apply func(root *js.Object) error
	var done string
	touched := ""
	switch {
	case argv[0] == "new" && (len(argv) == 2 || (len(argv) == 4 && argv[2] == "--from")):
		name, from := argv[1], profiles.Default
		if len(argv) == 4 {
			from = argv[3]
		}
		if why := profiles.NameProblem(pf, name); why != "" {
			return fail(why)
		}
		if !slices.Contains(pf.Names(), from) {
			return fail(fmt.Sprintf("no profile '%s' to copy; profiles: %s", from, strings.Join(pf.Names(), ", ")))
		}
		apply = func(root *js.Object) error { profiles.New(root, name, from); return nil }
		done = fmt.Sprintf("profile %s created from %s", name, from)
	case argv[0] == "set" && len(argv) == 5:
		name, agent, runtime, value := argv[1], argv[2], argv[3], argv[4]
		if name == profiles.Default {
			return fail("default is agents.yaml itself; set a model in a profile of your own")
		}
		if !pf.Profiles.Has(name) {
			return fail(fmt.Sprintf("no profile '%s'; profiles: %s", name, strings.Join(pf.Names(), ", ")))
		}
		if value == "-" {
			value = ""
		}
		if now, old := rename.Agent(agent); old {
			fmt.Fprintln(os.Stderr, "jhon profile: "+rename.Warning("set", "agent", agent, now))
			agent = now
		}
		apply = func(root *js.Object) error { return profiles.SetAgent(root, name, agent, runtime, value) }
		done = fmt.Sprintf("profile %s: %s in %s is %s", name, agent, runtime, cmp.Or(value, "agents.yaml's"))
		touched = name
	case argv[0] == "mode" && len(argv) == 3:
		name, mode := argv[1], argv[2]
		if why := profiles.DelegationProblem(mode); why != "" {
			fmt.Fprintf(os.Stderr, "jhon profile: %s\n%s\n", why, profileUsage)
			return 2, false
		}
		if name == profiles.Default {
			return fail("default is agents.yaml itself and always inline; set the mode on a profile of your own")
		}
		if !pf.Profiles.Has(name) {
			return fail(fmt.Sprintf("no profile '%s'; profiles: %s", name, strings.Join(pf.Names(), ", ")))
		}
		// The mode is read by the shell, not rendered: no re-render (touched stays "").
		apply = func(root *js.Object) error { return profiles.SetDelegation(root, name, mode) }
		done = fmt.Sprintf("profile %s: delegation %s", name, mode)
	case argv[0] == "rm" && len(argv) == 2:
		name := argv[1]
		if name == profiles.Default {
			return fail("default is agents.yaml itself; it cannot be removed")
		}
		if name == active {
			return fail(fmt.Sprintf("%s is the active profile; activate another first", name))
		}
		if !pf.Profiles.Has(name) {
			return fail(fmt.Sprintf("no profile '%s'", name))
		}
		apply = func(root *js.Object) error { profiles.Remove(root, name); return nil }
		done = "profile " + name + " removed"
	default:
		fmt.Fprintln(os.Stderr, profileUsage)
		return 2, false
	}
	// The edit is judged on the whole file as it would be written.
	err := profiles.Edit(file, func(root *js.Object) error {
		if err := apply(root); err != nil {
			return err
		}
		next := pf
		next.Profiles, _ = js.Prop(root, "profiles").(*js.Object)
		if next.Profiles == nil {
			next.Profiles = js.NewObject()
		}
		if problems := profiles.Problems(next, ctx); len(problems) > 0 {
			return errors.New(strings.Join(problems, "; "))
		}
		return nil
	})
	if err != nil {
		return fail(err.Error())
	}
	fmt.Println(done)
	for _, w := range pf.Renamed {
		// profiles.Edit wrote each under its new name.
		fmt.Println("rewritten: " + w)
	}
	return 0, touched != "" && touched == active
}

func profileCommand(argv []string) int {
	m := manifest.MustLoadRelease()
	ctx := profiles.Context{Runtimes: m.Matrix.Runtimes, Tiers: m.TierNames(), Agents: m.AgentNames()}
	file := profiles.Path()
	pf, readErrs := render.ReadProfiles()
	cmd, name := "list", ""
	if len(argv) > 0 {
		cmd = argv[0]
	}
	if len(argv) > 1 {
		name = argv[1]
	}
	if len(readErrs) > 0 {
		for _, p := range readErrs {
			fmt.Fprintf(os.Stderr, "jhon profile: %s\n", p)
		}
		return 1
	}
	names := pf.Names()
	active, _ := profiles.Active(pf)
	if cmd != "json" {
		for _, w := range pf.Renamed {
			fmt.Fprintf(os.Stderr, "jhon profile: %s\n", w)
		}
	}
	switch {
	case cmd == "json" && len(argv) == 1:
		var out []map[string]any
		for _, n := range names {
			p := pf
			p.Active, p.HasActive = n, true
			_, profile := profiles.Active(p)
			out = append(out, map[string]any{"name": n, "provider": render.PiProvider(m, profile), "delegation": profiles.Delegation(profile), "agents": profileRows(m, profile)})
		}
		// Each runtime's effort values, for the panel's picker to offer only those.
		efforts := map[string][]string{}
		for rt, e := range profiles.Efforts {
			efforts[rt] = e.Values
		}
		b, _ := json.Marshal(map[string]any{"active": active, "file": strings.Replace(file, profiles.HomeDir(), "~", 1), "profiles": out, "efforts": efforts})
		fmt.Println(string(b))
		return 0
	case cmd == "list" && len(argv) <= 1:
		width := 0
		for _, n := range names {
			width = max(width, js.Len(n))
		}
		for _, n := range names {
			mark := " "
			if n == active {
				mark = "*"
			}
			p := pf
			p.Active, p.HasActive = n, true
			_, profile := profiles.Active(p)
			fmt.Printf("%s %s  pi: %s\n", mark, js.PadEnd(n, width), cmp.Or(render.PiProvider(m, profile), "(none)"))
		}
		absent := ""
		if _, err := os.Stat(file); err != nil {
			absent = " (absent: default)"
		}
		fmt.Printf("\n%s%s\n", strings.Replace(file, profiles.HomeDir(), "~", 1), absent)
		return 0
	case cmd == "show" && len(argv) <= 2, cmd == "use" && len(argv) == 2:
		target := active
		if len(argv) > 1 {
			target = name
		}
		if !slices.Contains(names, target) {
			fmt.Fprintf(os.Stderr, "jhon profile: no profile '%s' in %s; profiles: %s\n", target, file, strings.Join(names, ", "))
			return 1
		}
		pf.Active, pf.HasActive = target, true
		if problems := profiles.Problems(pf, ctx); len(problems) > 0 {
			for _, p := range problems {
				fmt.Fprintf(os.Stderr, "jhon profile: %s\n", p)
			}
			return 1
		}
		if cmd == "use" {
			// settings' pi.profile is the one source (T56b); profiles.json's
			// active follows for older readers, and only once settings took
			// it, so a refused write changes nothing (T56c). The old
			// profiles.json migrates first, so this write never stands in for it.
			path := settings.Path()
			autoMigrate(path)
			if _, err := settings.Set(path, "pi.profile", target); err != nil {
				fmt.Fprintf(os.Stderr, "jhon profile: %s: %v\n", release.Tilde(path), err)
				return 1
			}
			if err := profiles.WriteActive(file, target); err != nil {
				panic(err.Error())
			}
			fmt.Printf("profile %s active\n", target)
			return 0
		}
		_, profile := profiles.Active(pf)
		showProfile(m, target, profile)
		return 0
	}
	fmt.Fprintln(os.Stderr, profileUsage)
	return 2
}

// showProfile prints each agent's model in each runtime under profile, and
// where it comes from, in aligned columns.
func showProfile(m manifest.Manifest, target string, profile any) {
	fmt.Printf("profile %s\n", target)
	var rows [][4]string
	for _, r := range profileRows(m, profile) {
		rows = append(rows, [4]string{r["agent"], r["runtime"], r["model"], r["source"]})
	}
	var w [3]int
	for _, r := range rows {
		for i := range w {
			w[i] = max(w[i], js.Len(r[i]))
		}
	}
	for _, r := range rows {
		fmt.Printf("  %s  %s  %s  %s\n", js.PadEnd(r[0], w[0]), js.PadEnd(r[1], w[1]), js.PadEnd(r[2], w[2]), r[3])
	}
}

// profileRows is each agent's model in each runtime under profile, and where
// it comes from.
func profileRows(m manifest.Manifest, profile any) []map[string]string {
	rows := []map[string]string{}
	for _, a := range m.Agents {
		for _, rt := range m.Matrix.Runtimes {
			if v, ok := profiles.ModelFor(m.TierTable(), profile, rt, profiles.Agent{Name: a.Name, Tier: a.Tier}); ok {
				rows = append(rows, map[string]string{"agent": a.Name, "runtime": rt, "model": js.String(v.Value), "source": v.Source})
			}
		}
	}
	return rows
}

// rerender is the second half of `profile use`: the active release
// installs every runtime's projections again, from the source.
func rerender() (int, error) {
	r := release.Locate()
	current := r.Share + "/current"
	if !release.Exists(current + "/" + release.Binary) {
		return die("no active release at " + release.Tilde(current) + "; run 'jhon sync' first")
	}
	if !release.Install(current, r.Source) {
		return die("the re-render failed; the profile is active, the projections may be stale")
	}
	return 0, nil
}

// die is the CLI's fatal error: `jhon: <message>` on stderr, the prefix red when
// stdout is a terminal, and exit 1.
func die(msg string) (int, error) {
	return out().Die(msg), nil
}
