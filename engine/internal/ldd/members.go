package ldd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// A change of a workspace's primary that also changes members delivers one
// PR per repository, the primary's last (T57 slice 7, L1324; owner Q6,
// L1152). Its members are the workspace members a slice of its tasks.md
// names (`Repo: <name>`) or its record holds a run of (`repos.<name>`);
// each one's delivery is judged as `jhon deliver check` judges it in the
// member (CheckMemberDelivery), and the primary's waits for every one. A
// change with no member, and every repository outside a workspace, are
// judged as before.

// MemberDelivery is one member's delivery of a change: ready, or why not.
// Path, its folder, is not printed.
type MemberDelivery struct {
	Name    string   `json:"name"`
	Ready   bool     `json:"ready"`
	Reasons []string `json:"reasons"`
	Path    string   `json:"-"`
}

// ChangeMembers are the members of ws, root's workspace, that change in
// root delivers in, in the config's order; none unless root is a primary.
// A name no member of ws has is none.
func ChangeMembers(root, change string, ws workspace.Info) []workspace.Listed {
	if ws.Role != workspace.Primary {
		return nil
	}
	named := map[string]bool{}
	lay := layout.For(root)
	if b, err := os.ReadFile(filepath.Join(root, execution.ChangesOf(root), change, lay.Tasks())); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if m := lay.SliceRepo().FindStringSubmatch(line); m != nil {
				named[m[1]] = true
			}
		}
	}
	for name := range execution.ReposOf(root, change) {
		named[name] = true
	}
	var out []workspace.Listed
	for _, l := range ws.Members {
		if named[l.Name] {
			out = append(out, l)
		}
	}
	return out
}

// MemberDeliveries are the deliveries of change's members (ChangeMembers),
// each judged in the member with root's record of change, for a merge: a
// member that has not joined is not ready, and an error reading one is its
// reason.
func MemberDeliveries(root, change string, ws workspace.Info) []MemberDelivery {
	var out []MemberDelivery
	for _, l := range ChangeMembers(root, change, ws) {
		d := MemberDelivery{Name: l.Name, Reasons: []string{}, Path: l.Path}
		if !l.Joined {
			d.Reasons = append(d.Reasons, fmt.Sprintf("%s has not joined the workspace: the owner runs `jhon workspace join %s` in it", l.Path, ws.Primary))
		} else if c, err := CheckMemberDelivery(l.Path, root, l.Name, change, Merge(), nil); err != nil {
			d.Reasons = append(d.Reasons, err.Error())
		} else {
			d.Ready = c.Ready
			for _, r := range c.Reasons {
				d.Reasons = append(d.Reasons, layout.Words(l.Path, r))
			}
		}
		out = append(out, d)
	}
	return out
}

// WithMembers is c, root's check of change, with its members' deliveries:
// not ready while one is not, each named with the command that says why.
func WithMembers(c DeliverCheck, root, change string, ws workspace.Info) DeliverCheck {
	c.Members = MemberDeliveries(root, change, ws)
	for _, m := range c.Members {
		if !m.Ready {
			c.Ready = false
			c.Reasons = append(c.Reasons, fmt.Sprintf("workspace member %s is not ready: run `jhon deliver check %s` in %s", m.Name, change, m.Path))
		}
	}
	return c
}

// MembersLine is the members' deliveries in one line, as status prints it.
func MembersLine(change string, ms []MemberDelivery) string {
	parts := make([]string, len(ms))
	waiting := false
	for i, m := range ms {
		parts[i] = m.Name + " ready"
		if !m.Ready {
			parts[i], waiting = m.Name+" not ready", true
		}
	}
	line := "members     " + strings.Join(parts, ", ")
	if waiting {
		line += "  (`jhon deliver check " + change + "` in a member says why)"
	}
	return line
}
