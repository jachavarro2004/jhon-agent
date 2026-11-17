package cli

import (
	"bytes"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/dashboard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/version"
)

// `jhon dashboard [--port <n>]` (plan item 14): the read-only
// dashboard (internal/dashboard) on 127.0.0.1, over every project's sessions
// under ~/.claude/projects. It serves until it is stopped; exit 2 on an
// unknown argument. When the port holds a dashboard of this release it says
// so; one proven Jhon's from another release is stopped and this one serves
// in its place; anything else is left running and said, exit 1 (T15.16).
func init() { register("dashboard", runDashboard) }

var digits = lazyre.New(`^\d+$`)

func runDashboard(argv []string) (int, error) {
	port := "7480"
	for i := 0; i < len(argv); i++ {
		if argv[i] == "--port" && i+1 < len(argv) && digits.MatchString(argv[i+1]) {
			i++
			port = js.NumberString(js.Number(argv[i]))
			continue
		}
		fmt.Fprintln(os.Stderr, "usage: jhon dashboard [--port <n>]   (127.0.0.1 only; read-only)")
		return 2, nil
	}
	if n, err := strconv.Atoi(port); err != nil || n > 65535 {
		return 1, fmt.Errorf("dashboard: the port should be >= 0 and < 65536, not %s", port)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return 1, err
	}
	share := release.Locate().Share
	releases := filepath.Join(share, "releases")
	exe, _ := os.Executable()
	own := dashboard.ReleaseOf(exe, releases)
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		// The port is taken: a dashboard of this release is left to answer,
		// one proven Jhon's from another is replaced by this one, anything
		// else is left running and said (T15.16).
		f := dashboard.Inspect(port, dashboard.StateDir(), releases, own)
		switch f.State {
		case dashboard.Current:
			fmt.Printf("jhon dashboard: http://127.0.0.1:%s/  (already running from this release)\n", port)
			return 0, nil
		case dashboard.Unproven:
			fmt.Fprintln(os.Stderr, "jhon: "+f.Report())
			return 1, nil
		case dashboard.Older:
			if err := dashboard.Stop(f, releases, 5*time.Second); err != nil {
				return 1, fmt.Errorf("dashboard on 127.0.0.1:%s (%s) was not restarted: %v", port, dashboard.Short(f.Record.Release), err)
			}
			if ln, err = net.Listen("tcp", "127.0.0.1:"+port); err != nil {
				return 1, err
			}
			fmt.Printf("dashboard restarted on 127.0.0.1:%s (%s → %s)\n", port, dashboard.Short(f.Record.Release), dashboard.Short(own))
		default:
			return 1, err
		}
	}
	dashboard.Build = dashboard.BuildInfo{Version: version.String(), Release: own, Binary: exe, Pid: os.Getpid()}
	assets := filepath.Join(release.Locate().Runtime, "dashboard")
	projects := filepath.Join(home, ".claude", "projects")
	// Jhon's pi sessions are listed beside Claude Code's.
	dashboard.PiSessions = filepath.Join(home, ".pi", "jhon", "sessions")
	// Each session's summary is kept on disk, and the recent sessions without
	// one are audited in the background, one at a time, for the Inicio page.
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" || !filepath.IsAbs(cache) {
		cache = filepath.Join(home, ".cache")
	}
	summaries := dashboard.NewSummaries(filepath.Join(cache, "jhon", "dashboard"))
	go summaries.Background(projects, runAudit, 80, 2*time.Minute, nil)
	// The record is what lets a later sync, update or dashboard prove this
	// server is Jhon's and restart it with the same arguments.
	rec := dashboard.Record{Pid: os.Getpid(), Port: port, Release: own, Binary: exe, Args: append([]string{"dashboard"}, argv...)}
	if err := dashboard.Serve(ln, dashboard.Server(projects, runAudit, assets, summaries), dashboard.StateDir(), rec, func() {
		fmt.Printf("jhon dashboard: http://127.0.0.1:%s/  (Ctrl-C stops it)\n", port)
	}); err != nil {
		return 1, err
	}
	return 0, nil
}

// restartDashboards is what `jhon sync` and `jhon update` run once current
// is switched: a dashboard proven Jhon's from another release is restarted
// from the new current on its port (dashboard.RestartOlder).
func restartDashboards(w io.Writer) {
	share := release.Locate().Share
	cur, err := filepath.EvalSymlinks(filepath.Join(share, "current"))
	if err != nil {
		return
	}
	releases, err := filepath.EvalSymlinks(filepath.Join(share, "releases"))
	if err != nil || filepath.Dir(cur) != releases {
		return
	}
	dashboard.RestartOlder(w, dashboard.StateDir(), releases, filepath.Base(cur))
}

// runAudit is the real auditor: `jhon audit <path> --json --context`, parsed.
// It runs this binary.
func runAudit(path string) any {
	exe, err := os.Executable()
	if err != nil {
		return auditError(err.Error())
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(exe, "audit", path, "--json", "--context")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	_ = cmd.Run() // the audit exits non-zero on findings; its JSON is the answer
	if report, err := js.ParseJSON(js.DecodeUTF8(stdout.Bytes())); err == nil {
		return report
	}
	msg := js.DecodeUTF8(stderr.Bytes())
	if msg == "" {
		msg = js.DecodeUTF8(stdout.Bytes())
	}
	return auditError(js.Trim(msg))
}

func auditError(msg string) any {
	return js.Obj("ok", false, "status", "audit-error", "errors", []any{js.Obj("message", msg)})
}
