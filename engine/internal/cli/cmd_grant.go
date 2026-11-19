package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/telemetry"
)

// `jhon grant`: the owner's authorization of one or more ASK
// classes for one project root, for a while.
//
//	jhon grant <class>… [--for <n>{s|m|h|d}] [--root <path>]
//	jhon grant --list [--json]
//	jhon grant --revoke <class>… [--root <path>]
//
// Several classes in one call share one expiry; an unknown or repeated class
// writes nothing. The root defaults to the git toplevel of the cwd, and a
// linked worktree resolves to its main checkout. Exit 0 on success, 2 on a
// usage error (nothing written).
func init() { register("grant", runGrant) }

var grantUsage = `usage: jhon grant <class>… [--for <n>{s|m|h|d}] [--root <path>]
       jhon grant --list [--json]
       jhon grant --revoke <class>… [--root <path>]

  classes: ` + strings.Join(grant.Classes, ", ") + `
  --for      how long the grant lives (default 30m)
  --root     the project the grant covers (default: the git toplevel of the cwd)
`

var grantUnit = map[string]float64{"s": 1000, "m": 60_000, "h": 3_600_000, "d": 86_400_000}

var grantFor = lazyre.New(`^(\d+)([smhd])$`)

// maxDate is the latest instant a JavaScript Date holds, in milliseconds.
const maxDate = 8.64e15

type grantArgs struct {
	mode    string // grant, list, or revoke
	classes []string
	ms      float64
	root    *string
	json    bool
}

// parseGrant reads the arguments, or the usage error that stops the call.
func parseGrant(argv []string) (grantArgs, string) {
	out := grantArgs{mode: "grant", ms: 30 * grantUnit["m"]}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--list":
			out.mode = "list"
		case a == "--revoke":
			out.mode = "revoke"
		case a == "--json":
			out.json = true
		case a == "--for" || a == "--root":
			i++
			if i >= len(argv) || strings.HasPrefix(argv[i], "-") {
				return out, a + " requires a value"
			}
			v := argv[i]
			if a == "--root" {
				out.root = &v
				continue
			}
			m := grantFor.FindStringSubmatch(v)
			if m == nil {
				return out, fmt.Sprintf("--for must be a positive number with s, m, h, or d, not '%s'", v)
			}
			n, _ := strconv.ParseFloat(m[1], 64)
			if n <= 0 {
				return out, fmt.Sprintf("--for must be a positive number with s, m, h, or d, not '%s'", v)
			}
			out.ms = n * grantUnit[m[2]]
		case strings.HasPrefix(a, "-"):
			return out, "unknown option: " + a
		case slices.Contains(out.classes, a):
			return out, fmt.Sprintf("class '%s' given twice", a)
		default:
			out.classes = append(out.classes, a)
		}
	}
	if out.mode == "list" {
		if len(out.classes) > 0 {
			return out, "--list takes no class"
		}
		return out, ""
	}
	if out.json {
		return out, "--json goes with --list"
	}
	if len(out.classes) == 0 {
		if out.mode == "revoke" {
			return out, "--revoke needs a class"
		}
		return out, "a class is required"
	}
	for _, c := range out.classes {
		if !slices.Contains(grant.Classes, c) && (out.mode != "revoke" || !slices.Contains(grant.Retired, c)) {
			return out, fmt.Sprintf("unknown class '%s'", c)
		}
	}
	return out, ""
}

func grantUsageError(msg string) (int, error) {
	fmt.Fprintf(os.Stderr, "jhon grant: %s\n\n%s", msg, grantUsage)
	return 2, nil
}

func runGrant(argv []string) (int, error) {
	a, bad := parseGrant(argv)
	if bad != "" {
		return grantUsageError(bad)
	}
	if a.mode == "list" && a.json {
		// For the shell's /jhon:grants: every grant, the classes there are, and
		// the project the cwd is in ("" outside one), whose grants it shows first.
		b, err := json.Marshal(map[string]any{
			"root":    grant.Root(project.Root(project.Resolve(cwd(), cwd()))),
			"classes": grant.Classes,
			"grants":  grant.Records(),
		})
		if err != nil {
			return 1, err
		}
		fmt.Println(string(b))
		return 0, nil
	}
	if a.mode == "list" {
		lines := grant.List()
		for _, l := range lines {
			fmt.Println(l)
		}
		if len(lines) == 0 {
			fmt.Println("no grants")
		}
		return 0, nil
	}
	path := cwd()
	if a.root != nil {
		path = *a.root
	}
	// The project a grant covers: its main checkout, so the grant covers
	// every linked worktree too.
	root := grant.Root(project.Root(project.Resolve(cwd(), path)))
	if root == "" {
		return grantUsageError(path + " is not inside a project: " + noProject)
	}
	if a.mode == "revoke" {
		for _, cls := range a.classes {
			if err := grant.Revoke(root, cls); err != nil {
				return 1, err
			}
			fmt.Printf("revoked %s for %s\n", cls, root)
		}
		return 0, nil
	}
	// One expiry for every class of the call; one line per class, which
	// `jhon audit` reads from the transcript as the owner's authorization.
	now := time.Now().UnixMilli()
	if float64(now)+a.ms > maxDate {
		return 1, errors.New("RangeError: Invalid time value")
	}
	grantedAt, expiresAt := isodate.Format(time.UnixMilli(now)), isodate.Format(time.UnixMilli(now+int64(a.ms)))
	telemetry.Note("jhon.grant.classes", a.classes)
	telemetry.Note("jhon.grant.expires_at", expiresAt)
	for _, cls := range a.classes {
		if err := grant.Write(grant.Record{Class: cls, Root: root, GrantedAt: grantedAt, ExpiresAt: expiresAt}); err != nil {
			return 1, err
		}
		fmt.Printf("granted %s for %s until %s\n", cls, root, expiresAt)
	}
	return 0, nil
}

// noProject says which places are no project.
const noProject = "a folder that is not a git repository is none, and HOME and / never are"
