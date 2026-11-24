package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
	"github.com/jachavarro2004/jhon-agent/engine/internal/update"
)

// The tools jhon installs from their releases (T242,
// odd/tasks/studio-integration-v1.md S5): each optional cli integration
// that is on and whose program is released on GitHub as jhon reads it
// (update.ToolOf: jhon-studio). `jhon setup` installs it at its Install
// step, `jhon update` updates it after jhon itself, `jhon update
// --rollback` links its previous version again, and `jhon doctor` says
// where it came from (and, with --remote, whether a newer release that
// supports this jhon is out). A development clone is left alone.

// releasedTools are those tools, by Order.
func releasedTools() []update.Tool {
	var out []update.Tool
	for _, m := range integrations.Optional() {
		if !settings.On(m.Setting) {
			continue
		}
		if t, ok := update.ToolOf(m); ok {
			out = append(out, t)
		}
	}
	return out
}

// toolClient reads tool releases; a variable for the tests.
var toolClient = update.NewTools

// toolReport is one tool's entry in `jhon update --json`'s "tools".
type toolReport struct {
	Name    string `json:"name"`
	Current string `json:"current,omitempty"`
	Latest  string `json:"latest,omitempty"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

// updateTools installs each released tool's newest release that supports
// jhon (the jhon that runs after this update), or with check only looks
// for it, saying one line or two each on o; failed when one failed, which
// changed nothing of it.
func updateTools(o *release.Out, jhon string, check bool) (reports []toolReport, failed bool) {
	tools := releasedTools()
	if len(tools) == 0 {
		return nil, false
	}
	c, home := toolClient(), project.Home()
	for _, t := range tools {
		p := t.PlaceIn(home)
		res, err := c.InstallTool(context.Background(), t, p, jhon, check)
		rep := toolReport{Name: t.Binary, Current: res.From, Latest: res.To, Status: string(res.Status)}
		switch {
		case err != nil:
			rep.Status, rep.Error, failed = "failed", err.Error(), true
			o.Die(t.Binary + ": " + err.Error())
		case res.Status == update.ToolDevelopment:
			fmt.Fprintf(o.W, "%s is a development clone at %s: jhon update leaves it alone\n", t.Binary, release.Tilde(p.Dir))
		case res.Status == update.ToolUpToDate:
			fmt.Fprintf(o.W, "%s %s is up to date (the newest release of %s that supports jhon %s)\n", t.Binary, res.To, t.Source(), jhon)
		case res.Status == update.ToolAvailable && res.From == "":
			fmt.Fprintf(o.W, "%s is not installed; its release %s supports jhon %s: run 'jhon update' to install it\n", t.Binary, res.To, jhon)
		case res.Status == update.ToolAvailable:
			fmt.Fprintf(o.W, "update available: %s %s -> %s\nrun 'jhon update' to install it\n", t.Binary, res.From, res.To)
		default:
			o.OK(t.Binary + " " + res.To + ": sha256 and signature verified: signed by " + res.Signer)
			if res.Status == update.ToolInstalled {
				fmt.Fprintf(o.W, "installed %s %s at %s\n", t.Binary, res.To, release.Tilde(filepath.Join(p.Dir, res.To)))
			} else {
				fmt.Fprintf(o.W, "updated %s %s -> %s\n", t.Binary, res.From, res.To)
			}
		}
		reports = append(reports, rep)
	}
	return reports, failed
}

// rollBackTools links each released tool's previous version again, writing
// its line to w (a failure's to o's stderr): how many rolled back, and
// false when one failed.
func rollBackTools(w *bytes.Buffer, o *release.Out) (rolled int, ok bool) {
	ok = true
	home := project.Home()
	for _, t := range releasedTools() {
		p := t.PlaceIn(home)
		from, to, err := update.RollbackTool(t, p)
		switch {
		case errors.Is(err, update.ErrDevelopmentTool):
			fmt.Fprintf(w, "%s is a development clone at %s: left alone\n", t.Binary, release.Tilde(p.Dir))
		case errors.Is(err, update.ErrNoOlderTool) && from == "":
		case errors.Is(err, update.ErrNoOlderTool):
			fmt.Fprintf(w, "no %s release older than %s is kept; it stays at %s\n", t.Binary, from, from)
		case err != nil:
			o.Die(t.Binary + ": " + err.Error())
			ok = false
		default:
			fmt.Fprintf(w, "rolled back %s %s -> %s\n", t.Binary, from, to)
			rolled++
		}
	}
	return rolled, ok
}

// trimUnchanged is a check's error without "; nothing was changed", which
// a check that installs nothing need not say.
func trimUnchanged(err error) string {
	return strings.TrimSuffix(err.Error(), "; nothing was changed")
}
