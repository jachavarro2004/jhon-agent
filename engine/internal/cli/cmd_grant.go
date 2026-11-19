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
//	jhon grant outside-write --session <id> [--root <folder>]
//	jhon grant --revoke outside-write --session <id>
//
// With --session, the grant is of a folder that is no project (no git
// repository) for one session: writes under it pass until that session
// ends (grant.GrantSession, T57a).
// Several classes in one call share one expiry; an unknown or repeated class
// writes nothing. The root defaults to the git toplevel of the cwd, and a
// linked worktree resolves to its main checkout. Exit 0 on success, 2 on a
// usage error (nothing written).
func init() { register("grant", runGrant) }

var grantUsage = `usage: jhon grant <class>… [--for <n>{s|m|h|d}] [--root <path>]
       jhon grant --list [--json]
       jhon grant --revoke <class>… [--root <path>]
       jhon grant [--revoke] outside-write --session <id> [--root <folder>]

  classes: ` + strings.Join(grant.Classes, ", ") + `
  --for      how long the grant lives (default 30m)
  --root     the project the grant covers (default: the git toplevel of the cwd)
  --session  writes in a folder that is no git repository (the cwd, or
             --root), and below it, pass for that session only; never HOME
             or /, and only outside-write. It ends with the session (pi
             revokes it), at the latest after 12h. --revoke with --session
             revokes every folder of that session.
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
	session *string
	forSet  bool
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
		case a == "--for" || a == "--root" || a == "--session":
			i++
			if i >= len(argv) || strings.HasPrefix(argv[i], "-") {
				return out, a + " requires a value"
			}
			v := argv[i]
			if a == "--root" {
				out.root = &v
				continue
			}
			if a == "--session" {
				out.session = &v
				continue
			}
			out.forSet = true
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
	if out.session != nil {
		switch {
		case len(out.classes) != 1 || out.classes[0] != grant.SessionClass:
			return out, "--session grants outside-write alone"
		case out.forSet:
			return out, "--session lasts the session; it takes no --for"
		case grant.SessionIDError(*out.session) != nil:
			return out, grant.SessionIDError(*out.session).Error()
		case out.mode == "revoke" && out.root != nil:
			return out, "--revoke --session revokes every folder of the session; it takes no --root"
		}
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
	if a.session != nil {
		return runSessionGrant(a, project.Resolve(cwd(), path))
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

// runSessionGrant grants, or revokes, a.session's outside-write in the folder dir.
func runSessionGrant(a grantArgs, dir string) (int, error) {
	if a.mode == "revoke" {
		folders, err := grant.RevokeSession(*a.session)
		if err != nil {
			return 1, err
		}
		for _, f := range folders {
			fmt.Printf("revoked %s for %s (session %s)\n", grant.SessionClass, f, *a.session)
		}
		if len(folders) == 0 {
			fmt.Printf("no session grant for session %s\n", *a.session)
		}
		return 0, nil
	}
	rec, err := grant.GrantSession(dir, *a.session)
	if err != nil {
		return grantUsageError(err.Error())
	}
	telemetry.Note("jhon.grant.classes", []string{rec.Class})
	fmt.Printf("granted %s for %s for session %s (at the latest until %s)\n", rec.Class, rec.Root, rec.Session, rec.ExpiresAt)
	return 0, nil
}
