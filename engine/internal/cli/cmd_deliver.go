package cli

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/telemetry"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// `jhon deliver check <change> [--action merge|deploy|release]
// [--artifact <digest|commit>] [--json] [--root <project>]`: whether
// HEAD is the change's tested revision (internal/ldd, deliver.go). A ready
// check records what it saw in the change's delivery.json under the git
// common dir, jhon/changes/<change>/ (ldd.DeliveryRecord), replacing the
// previous one, so the tree stays clean (T225); a check that is not ready
// writes nothing. In a workspace member, a change of the primary's is
// checked against the member's run in the primary's record
// (ldd.CheckMemberDelivery), and a ready check's delivery.json goes under
// the member's git common dir, so neither tree changes. In a primary, a
// change whose slices or record name members (ldd.ChangeMembers) lists each
// member's delivery, judged there, and is not ready while one is not (T57
// slice 7): one PR per repository, the primary's last. Consent is not this
// command's business: the safety guard asks before a push or a deploy runs.
// Exit 0 ready, 1 not ready, 2 usage.
func init() { register("deliver", runDeliver) }

const deliverUsage = "usage: jhon deliver check <change> [--action <merge|deploy|release>] [--artifact <name@sha256:digest|commit>] [--json] [--root <project>]"

func runDeliver(argv []string) (int, error) {
	// The vocabulary is read before anything else: a broken one fails every
	// call, a usage error included.
	actions := ldd.Actions()
	if err := execution.Vocabulary(); err != nil {
		return 1, err
	}
	usage := func(prefix string) (int, error) {
		fmt.Fprintln(os.Stderr, prefix+deliverUsage)
		return 2, nil
	}
	if len(argv) == 0 || argv[0] != "check" {
		return usage("")
	}
	rest := argv[1:]
	json := false
	opt := map[string]string{}
	var pos []string
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch a {
		case "--json":
			json = true
		case "--action", "--artifact", "--root":
			i++
			_, dup := opt[a[2:]]
			if i >= len(rest) || strings.HasPrefix(rest[i], "--") || dup {
				return usage("")
			}
			opt[a[2:]] = rest[i]
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) != 1 {
		return usage("")
	}
	var root string
	if r, ok := opt["root"]; ok {
		root = project.Resolve(cwd(), r)
	} else {
		root = discover.FindRoot(cwd())
	}
	change := pos[0]
	// In a workspace member, a change of the primary's is checked against
	// the member's entry in the primary's record (T57 slice 6).
	var ws workspace.Info
	if !activeChange(root, change) {
		ws = workspace.Of(root)
	}
	member := ws.Role == workspace.Member && activeChange(ws.Primary, change)
	if !activeChange(root, change) && !member {
		fmt.Fprintf(os.Stderr, "jhon deliver: %s/ does not exist or is not an active change under %s\n", filepath.Join(execution.ChangesOf(root), change), root)
		return 2, nil
	}
	action, ok := opt["action"]
	if !ok {
		action = actions[0]
	}
	if !slices.Contains(actions, action) {
		return usage(fmt.Sprintf("jhon deliver: --action must be one of %s\n", strings.Join(actions, ", ")))
	}
	var artifact *string
	if a, ok := opt["artifact"]; ok {
		artifact = &a
	}
	if action != actions[0] && artifact == nil {
		fmt.Fprintf(os.Stderr, "jhon deliver: %s ships an artifact; name it by its immutable digest with --artifact <name@sha256:...>, or for an infrastructure deploy by its full commit\n", action)
		return 2, nil
	}
	if artifact != nil && !ldd.Digest.MatchString(*artifact) {
		if action == "release" && ldd.IsCommitArtifact(*artifact) {
			fmt.Fprintln(os.Stderr, "jhon deliver: a release names its artifact by its content digest (<name@>sha256:<hex>); a commit names only an infrastructure deploy")
			return 2, nil
		}
		if action == "merge" && ldd.IsCommitArtifact(*artifact) {
			fmt.Fprintln(os.Stderr, "jhon deliver: a merge names no commit artifact; a commit names only an infrastructure deploy")
			return 2, nil
		}
		if !ldd.IsCommitArtifact(*artifact) {
			fmt.Fprintf(os.Stderr, "jhon deliver: '%s' is not an immutable artifact identifier: give its content digest (<name@>sha256:<hex>), or for an infrastructure deploy the full commit it deploys from; never a tag or a short sha\n", *artifact)
			return 2, nil
		}
	}

	check := ldd.CheckDelivery
	if member {
		check = func(root, change, action string, artifact *string) (ldd.DeliverCheck, error) {
			return ldd.CheckMemberDelivery(root, ws.Primary, ws.Name, change, action, artifact)
		}
	}
	c, err := check(root, change, action, artifact)
	if err != nil {
		return 1, err
	}
	// In a workspace's primary, the change's members deliver first (T57 slice 7).
	if !member {
		c = ldd.WithMembers(c, root, change, workspace.Of(root))
	}
	for i := range c.Reasons {
		c.Reasons[i] = layout.Words(root, c.Reasons[i])
	}
	telemetry.Note("jhon.change", change)
	telemetry.Note("jhon.delivery.action", action)
	telemetry.Note("jhon.delivery.ready", c.Ready)
	telemetry.Note("jhon.delivery.risk", c.Risk.Tier)
	if len(c.Reasons) > 0 {
		telemetry.Note("jhon.reason", strings.Join(c.Reasons, "; "))
	}
	if c.Ready {
		if err := ldd.WriteDelivery(root, change, ldd.DeliveryOf(c, isodate.Format(time.Now()))); err != nil {
			return 1, err
		}
	}
	if json {
		s, err := js.Stringify(c)
		if err != nil {
			return 1, err
		}
		fmt.Println(s)
	} else {
		status := "not ready"
		if c.Ready {
			status = "ready"
		}
		art, verified := "", ""
		if artifact != nil && *artifact != "" {
			art = "  artifact " + *artifact
		}
		if c.VerifiedBy != nil {
			verified = "  verified by " + *c.VerifiedBy
		}
		none := func(s *string) string {
			if s == nil {
				return "none"
			}
			return *s
		}
		lines := []string{
			fmt.Sprintf("%s  %s%s", action, status, art),
			fmt.Sprintf("head    %s  tree %s", c.HeadCommit, c.HeadTree),
			fmt.Sprintf("tested  %s  tree %s", none(c.TestedCommit), none(c.TestedTree)),
			fmt.Sprintf("risk    %s%s", c.Risk.Tier, verified),
		}
		for _, r := range c.Reasons {
			lines = append(lines, "  - "+r)
		}
		for _, m := range c.Members {
			ready := "ready"
			if !m.Ready {
				ready = "not ready"
			}
			lines = append(lines, "member  "+m.Name+"  "+ready)
			for _, r := range m.Reasons {
				lines = append(lines, "    - "+r)
			}
		}
		for _, n := range c.Notes {
			lines = append(lines, "note  "+n)
		}
		fmt.Println(strings.Join(lines, "\n"))
	}
	if c.Ready {
		return 0, nil
	}
	return 1, nil
}
