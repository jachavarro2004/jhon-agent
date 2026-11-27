package cli

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
)

// `jhon sync` (docket AV1, internal/release): build a release from the
// source's committed HEAD, preflight it, switch ~/.local/share/jhon/current
// to it, and install the runtime projections from it. It pushes nothing and
// takes no argument (docket BB1: an unread one once published). A dashboard
// proven Jhon's from an older release is then restarted from the new one
// (T15.16). Before all that, from the owner's terminal, it seeds the
// launch check's records of the roots `jhon start` never saw (seedHooks,
// T266 L1768), so the question comes while he is there and the build runs
// unattended after it.
//
// `jhon render` (docket AV1): rendering from a checkout no longer installs
// anything; only a sync, from a committed release, activates a change.
func init() {
	register("sync", runSync)
	register("render", runRender)
}

func runSync(argv []string) (int, error) {
	if len(argv) > 0 {
		return usageError("sync takes no argument: " + strings.Join(argv, " "))
	}
	// Sync reads the integrations from settings.toml: migrate the old
	// settings first, or a first sync would read telemetry as off.
	autoMigrate(settings.Path())
	o := out()
	seedHooks(o)
	code := release.Sync(release.Locate(), o)
	if code == 0 {
		restartDashboards(o.W)
	}
	return code, nil
}

func runRender([]string) (int, error) {
	return die("render no longer installs: 'jhon sync' builds a release from the committed HEAD and activates it; 'jhon check' reports drift")
}
