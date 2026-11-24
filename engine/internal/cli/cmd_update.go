package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/palette"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
	"github.com/jachavarro2004/jhon-agent/engine/internal/setupui"
	"github.com/jachavarro2004/jhon-agent/engine/internal/update"
)

// `jhon update` (T6, S1, S8): on a terminal, the setup TUI's update screens;
// otherwise plain lines: check the latest release, and install it when it
// is newer, verified (sha256 and the cosign keyless signature) before
// anything changes. Exit 0 up to date, updated, or no stable release
// published yet (only pre-releases, S11: --json status "no-release"), 1
// failed, 2 on a usage error. --check only reports; --json prints one
// object. --rollback
// re-activates the previous release kept on disk. Once current is switched,
// a dashboard proven Jhon's from another release is restarted (T15.16; the
// TUI's screens print nothing of it). A development build (installed by
// `jhon sync` from a source checkout) is never replaced by a release, a
// downgrade: update, --check, --json and the screens install nothing and
// say so in one line, exit 2 for an install, 0 for --check and --json.
//
// Then, unless jhon's own step failed, each optional cli integration that
// is on and released on GitHub (jhon-studio) is updated to its newest
// release that supports the jhon that now runs, verified the same way, or
// with --check only looked for (cmd_update_tools.go, T242); --json lists
// them under "tools"; one that fails exits 1, changing nothing of it.
// --rollback links each one's previous version again too.
func init() { register("update", runUpdate) }

const updateUsage = "usage: jhon update [--check] [--json] | jhon update --rollback\n"

func runUpdate(argv []string) (int, error) {
	check, asJSON, rollback := false, false, false
	for _, a := range argv {
		switch a {
		case "--check":
			check = true
		case "--json":
			asJSON = true
		case "--rollback":
			rollback = true
		default:
			fmt.Fprintf(os.Stderr, "jhon update: unknown argument: %s\n%s", a, updateUsage)
			return 2, nil
		}
	}
	if rollback && (check || asJSON) {
		fmt.Fprintf(os.Stderr, "jhon update: --rollback takes no other option\n%s", updateUsage)
		return 2, nil
	}
	r, c := release.Locate(), update.New()
	if rollback {
		return rollBack(r, c.Current), nil
	}
	if update.IsDev(c.Current) {
		return devBuild(c.Current, check, asJSON), nil
	}
	if !check && !asJSON && isTerminal(os.Stdin) && isTerminal(os.Stdout) {
		return updateTUI(r, c)
	}
	return updatePlain(r, c, check, asJSON), nil
}

func updatePlain(r release.Roots, c *update.Client, check, asJSON bool) int {
	o := out()
	if asJSON {
		// stdout is the one object; the install's lines go to stderr.
		o.W = os.Stderr
	}
	var tools []toolReport
	report := func(status, latest string, err error) {
		if !asJSON {
			return
		}
		v := struct {
			Current string       `json:"current"`
			Latest  string       `json:"latest,omitempty"`
			Status  string       `json:"status"`
			Error   string       `json:"error,omitempty"`
			Tools   []toolReport `json:"tools,omitempty"`
		}{c.Current, latest, status, "", tools}
		if err != nil {
			v.Error = err.Error()
		}
		b, _ := json.Marshal(v)
		fmt.Println(string(b))
	}
	failed := func(latest string, err error) int {
		report("failed", latest, err)
		if errors.Is(err, update.ErrInstall) {
			return o.Die("update failed: the install of " + latest + " stopped (above)")
		}
		return o.Die("update failed: " + err.Error() + "; nothing was changed")
	}
	// done is the tools' step, against running (the jhon that runs once
	// this update is over), then jhon's report: exit 1 when a tool failed.
	done := func(status, latest, running string) int {
		var toolFailed bool
		tools, toolFailed = updateTools(o, running, check)
		report(status, latest, nil)
		if toolFailed {
			return 1
		}
		return 0
	}
	ctx := context.Background()
	l, err := c.Latest(ctx)
	if errors.Is(err, update.ErrNoRelease) {
		if !asJSON {
			fmt.Fprintf(o.W, "no stable release is published yet; jhon %s is installed\n", c.Current)
		}
		return done("no-release", "", c.Current)
	}
	if err != nil {
		return failed(l.Version, err)
	}
	if !update.Newer(c.Current, l.Version) {
		if !asJSON {
			fmt.Fprintf(o.W, "jhon %s is up to date (latest release %s)\n", c.Current, l.Version)
		}
		return done("up-to-date", l.Version, c.Current)
	}
	if !asJSON {
		fmt.Fprintf(o.W, "update available: jhon %s -> %s (released %s)\n", c.Current, l.Version, l.Date)
	}
	if check {
		if !asJSON {
			fmt.Fprintln(o.W, "run 'jhon update' to install it")
		}
		return done("available", l.Version, c.Current)
	}
	verified := func(signer string) { o.OK("sha256 and signature verified: signed by " + signer) }
	if _, err := c.Install(ctx, r, o, l, verified); err != nil {
		return failed(l.Version, err)
	}
	fmt.Fprintf(o.W, "updated jhon %s -> %s\n", c.Current, l.Version)
	restartDashboards(o.W)
	return done("updated", l.Version, l.Version)
}

// devBuild is `jhon update` on a development build: no release is read,
// nothing is installed.
func devBuild(current string, check, asJSON bool) int {
	if asJSON {
		b, _ := json.Marshal(struct {
			Current string `json:"current"`
			Status  string `json:"status"`
			Dev     bool   `json:"dev"`
			Update  bool   `json:"update"`
		}{current, "dev", true, false})
		fmt.Println(string(b))
		return 0
	}
	line := "jhon: this is a development build (" + current + ") installed by `jhon sync`; `jhon update` installs only published releases. Run `jhon sync` in the source checkout to update it.\n"
	if check {
		fmt.Print(line)
		return 0
	}
	fmt.Fprint(os.Stderr, line)
	return 2
}

// rollBack is `jhon update --rollback`: re-activate the previous release,
// then link each released tool's previous version again (T242). With no
// older jhon kept, the tools still roll back; with nothing to roll back at
// all, it says so and exits 1, as before.
func rollBack(r release.Roots, current string) int {
	o := out()
	k, ok := update.Previous(r, current)
	if !ok {
		active := current
		for _, k := range release.KeptReleases(r) {
			if k.Active {
				active = k.Version
			}
		}
		var lines bytes.Buffer
		rolled, toolsOK := rollBackTools(&lines, o)
		if rolled == 0 && toolsOK {
			return o.Die("no release older than jhon " + active + " is kept; nothing was changed")
		}
		fmt.Fprintf(o.W, "no release older than jhon %s is kept; jhon stays at %s\n", active, active)
		o.W.Write(lines.Bytes())
		if !toolsOK {
			return 1
		}
		return 0
	}
	from := current
	for _, a := range release.KeptReleases(r) {
		if a.Active {
			from = a.Version
		}
	}
	fmt.Fprintf(o.W, "rolling back jhon %s -> %s\n", from, k.Version)
	if code := release.Reactivate(r, o, k); code != 0 {
		return code
	}
	fmt.Fprintf(o.W, "rolled back to jhon %s\n", k.Version)
	restartDashboards(o.W)
	var lines bytes.Buffer
	_, toolsOK := rollBackTools(&lines, o)
	o.W.Write(lines.Bytes())
	if !toolsOK {
		return 1
	}
	return 0
}

// updateTUI opens the setup TUI's update screens on the real updater.
func updateTUI(r release.Roots, c *update.Client) (int, error) {
	p, desktop := palette.Current()
	sys := setupui.NewSystem(settings.Path(), filepath.Join(project.Home(), ".local", "bin", "jhon"), setupPins(), reinstall)
	u := &tuiUpdater{c: c, r: r}
	defer u.close()
	sys.Updates = u
	if err := setupui.Run(os.Stdin, os.Stdout, sys, setupui.ModeUpdate, setupui.Colors(palette.Semantic(p, desktop))); err != nil {
		return die(err.Error())
	}
	// The tools follow jhon once the screens are closed, in plain lines:
	// updated after jhon is current or updated, rolled back after a
	// roll-back; nothing when the check failed or the owner left.
	o := out()
	switch u.then {
	case "update":
		if _, failed := updateTools(o, u.running, false); failed {
			return 1, nil
		}
	case "rollback":
		var lines bytes.Buffer
		_, ok := rollBackTools(&lines, o)
		o.W.Write(lines.Bytes())
		if !ok {
			return 1, nil
		}
	}
	return 0, nil
}

// tuiUpdater is setupui's Updater on internal/update: Latest also downloads
// and verifies a newer release, so the screen can say it is verified, and
// Update installs that verified download. The install's output is kept for
// the failure screen, never drawn over the TUI.
type tuiUpdater struct {
	c        *update.Client
	r        release.Roots
	dir      string
	artifact string
	fetchErr error
	// then is what the tools do once the screens close (updateTUI):
	// "update" against running, "rollback", or nothing.
	then, running string
}

func (u *tuiUpdater) close() {
	if u.dir != "" {
		os.RemoveAll(u.dir)
		u.dir = ""
	}
}

func (u *tuiUpdater) Latest() (setupui.Release, error) {
	u.close()
	rel := setupui.Release{Current: u.c.Current, Latest: u.c.Current, Source: update.Source}
	if k, ok := update.Previous(u.r, u.c.Current); ok {
		rel.Previous = k.Version
	}
	l, err := u.c.Latest(context.Background())
	u.then, u.running = "", u.c.Current
	if errors.Is(err, update.ErrNoRelease) {
		rel.NoStable = true
		u.then = "update"
		return rel, nil
	}
	if err != nil {
		return rel, err
	}
	rel.Date, rel.Notes = l.Date, l.Notes
	if !update.Newer(u.c.Current, l.Version) {
		u.then = "update"
		return rel, nil
	}
	rel.Latest = l.Version
	if u.dir, err = os.MkdirTemp("", "jhon-update-"); err != nil {
		return rel, err
	}
	artifact, signer, err := u.c.Fetch(context.Background(), l, u.dir)
	u.artifact, u.fetchErr = artifact, err
	if err == nil {
		rel.Verified, rel.Signature = true, "cosign keyless · "+signer
	}
	return rel, nil
}

func (u *tuiUpdater) Update(rel setupui.Release) error {
	if u.fetchErr != nil {
		return fmt.Errorf("%v; nothing was changed", u.fetchErr)
	}
	if u.artifact == "" {
		return errors.New("no verified release to install")
	}
	var b bytes.Buffer
	if release.InstallArtifact(u.r, &release.Out{W: &b, E: &b}, u.artifact) != 0 {
		return errors.New(strings.TrimSpace(b.String()))
	}
	u.then, u.running = "update", rel.Latest
	restartDashboards(io.Discard)
	return nil
}

func (u *tuiUpdater) Rollback(version string) error {
	k, ok := update.Find(u.r, version)
	if !ok {
		return fmt.Errorf("jhon %s is not kept on disk; nothing was changed", version)
	}
	var b bytes.Buffer
	if release.Reactivate(u.r, &release.Out{W: &b, E: &b}, k) != 0 {
		return errors.New(strings.TrimSpace(b.String()))
	}
	u.then = "rollback"
	restartDashboards(io.Discard)
	return nil
}
