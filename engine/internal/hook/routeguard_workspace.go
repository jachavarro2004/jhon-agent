package hook

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// membersRefusal is why command, a PR opened in top, a workspace's primary,
// is refused, "" when it may open (T57 slice 7, L1324): one PR per
// repository, the primary's last. For each change the branch carries whose
// slices or record name members (ldd.ChangeMembers), archived ones
// included, every member's
// delivery is ready (ldd.MemberDeliveries, as `jhon deliver check` judges
// it there) and the PR's text names, for each member, a pull request of
// the repository its origin remote names (<host>/<owner>/<repo>/pull/<n>),
// read from its git config, with no network. The text is the command line,
// a heredoc body included, and a --body-file it names. A change with no
// member, a merge, and any repository but a primary are not judged here.
//
// The members' check has its own time, memberCheckTime, well under the 10 s
// the shell waits for a guard (T268, L1762): when it runs out, late says so
// and names `jhon deliver check` in each member not judged in time.
func membersRefusal(top, cwd, command string, reads *ldd.Reads) (why, late string) {
	if !deliveryOf(command).pr {
		return "", ""
	}
	ws := workspace.Of(top)
	if ws.Role != workspace.Primary {
		return "", ""
	}
	// The branch's changes, active and archived, as deliveryRefusal reads
	// them; an archived one by its folder under the archive.
	type branchChange struct{ id, name, archive string }
	var changes []branchChange
	active, archived := reads.BranchChanges(top)
	if c := reads.FolderlessChange(top); c != "" && !slices.Contains(active, c) {
		active = append(active, c)
	}
	for _, c := range active {
		changes = append(changes, branchChange{id: c, name: c})
	}
	lay := layout.For(top)
	for _, folder := range archived {
		name := folder
		if m := datedFolder.FindStringSubmatch(folder); m != nil {
			name = m[1]
		}
		changes = append(changes, branchChange{id: ldd.ArchivedChange(top, folder), name: name, archive: path.Join(lay.ArchiveDir(), folder) + "/"})
	}
	text := ""
	deadline := time.Now().Add(memberCheckTime)
	for _, ch := range changes {
		c := ch.name
		lead := "the change " + c + " delivers in members of the workspace too, one PR per repository, the primary's last: "
		if ch.archive != "" {
			lead = "the change " + c + " (archived, " + ch.archive + ") delivers in members of the workspace too, one PR per repository, the primary's last: "
		}
		ms, slow := memberDeliveriesBy(deadline, top, ch.id, ws)
		if len(slow) > 0 {
			return "", lead + lateNote(c, slow)
		}
		for _, m := range ms {
			if m.Ready {
				continue
			}
			// Why, then the steps: the owner's join first for a member
			// that has not joined, named once (L1346).
			why, steps := strings.Join(m.Reasons, "; "), ""
			if !m.Joined() {
				why, steps = m.Path+" has not joined the workspace", "the owner runs `jhon workspace join "+ws.Primary+"` in "+m.Path+", "
			}
			if ch.archive == "" {
				if steps == "" {
					steps = m.Next(c) + ", "
				}
				return lead + "member " + m.Name + " is not ready (" + why + "): " + steps + "open its PR, then the primary's.", ""
			}
			// A member's run is recorded under the active name, c, not
			// the archived folder the check read it from.
			why = strings.ReplaceAll(why, "jhon executions run "+ch.id+" ", "jhon executions run "+c+" ")
			return lead + "member " + m.Name + " is not ready (" + why + "): a member's run is recorded while the change is active: " + steps + "move " + ch.archive + " back to " + lay.ChangeDir(c) + "/, run `jhon executions run " + c + " --repo " + m.Name + "`, open the member's PR, then the primary's.", ""
		}
		for _, m := range ms {
			repo := originRepo(m.Path)
			if repo == "" {
				return lead + "member " + m.Name + " has no origin remote naming its repository on a host (`git -C " + m.Path + " remote get-url origin`), so this PR cannot name its pull request: add the remote, open the member's PR, then this one.", ""
			}
			if text == "" {
				text = prText(command, cwd)
			}
			if !regexp.MustCompile(`(?i)(?:^|[^\w.-])(?:https?://)?` + regexp.QuoteMeta(repo) + `/pull/\d+\b`).MatchString(text) {
				return lead + "this PR's body names no pull request of member " + m.Name + ": open its PR first and name it, https://" + repo + "/pull/<n>, in this PR's body.", ""
			}
		}
	}
	return "", ""
}

// memberCheckTime is how long route-guard judges a PR's members for: well
// under the 10 s the shell waits for a guard, so the guard answers itself
// (T268: the owner's member took 90 s, and the shell refused his PR four
// times with no reason).
var memberCheckTime = 6 * time.Second

// memberGrace is how long a members' check that ran out of time has to stop
// its git and remove its private folder before the guard answers anyway.
const memberGrace = time.Second

// memberDeliveriesBy is ldd.MemberDeliveriesWithin by deadline, in a
// goroutine the guard does not wait for past the deadline (and memberGrace):
// ms are change's members' deliveries, and slow the members not judged in
// time.
func memberDeliveriesBy(deadline time.Time, top, change string, ws workspace.Info) (ms []ldd.MemberDelivery, slow []workspace.Listed) {
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	type answer struct {
		ms   []ldd.MemberDelivery
		done bool
	}
	got := make(chan answer, 1)
	go func() {
		ms, done := ldd.MemberDeliveriesWithin(ctx, top, change, ws)
		got <- answer{ms, done}
	}()
	var a answer
	select {
	case a = <-got:
	case <-ctx.Done():
		select {
		case a = <-got:
		case <-time.After(memberGrace):
			a = answer{}
		}
	}
	if a.done {
		return a.ms, nil
	}
	ms = a.ms
	for _, l := range ldd.ChangeMembers(top, change, ws) {
		if slices.ContainsFunc(ms, func(m ldd.MemberDelivery) bool { return m.Name == l.Name }) {
			continue
		}
		slow = append(slow, l)
	}
	return ms, slow
}

// lateNote is what route-guard says of a PR whose members' check of change
// ran out of its time on slow: which members, the check to run in each,
// and that the PR is not retried before it.
func lateNote(change string, slow []workspace.Listed) string {
	var names, runs []string
	for _, l := range slow {
		names = append(names, l.Name)
		runs = append(runs, "`jhon deliver check "+change+"` in "+l.Path)
	}
	member := "member " + strings.Join(names, ", ")
	if len(slow) > 1 {
		member = "members " + strings.Join(names, ", ")
	}
	return fmt.Sprintf("the delivery check of %s did not finish in %s, so this PR is not judged: run %s, which says what holds it (most often a large untracked folder git does not ignore: build output, or a tool's data, which `jhon doctor` names); open the PR once it is ready. Do not retry the PR before that: it only waits again.",
		member, memberCheckTime, strings.Join(runs, ", then "))
}

// datedFolder is an archived change's folder, <date>-<change>.
var datedFolder = lazyre.New(`^\d{4}-\d{2}-\d{2}-(.+)$`)

// remoteURL is a hosted repository's URL, scp-like (git@host:owner/repo)
// or with a scheme (https://, ssh://, git://), a user and a port optional.
var remoteURL = lazyre.New(`^(?:[a-z][a-z0-9+.-]*://)?(?:[^@/]+@)?([^/:@\s]+)(?::\d+)?[:/]([^/\s]+)/([^/\s]+?)(?:\.git)?/?$`)

// originRepo is <host>/<owner>/<repo> of the repository dir's origin
// remote names, from its git config; "" with none, or one on no host.
func originRepo(dir string) string {
	url, _ := project.ReadGit(dir, "config", "--get", "remote.origin.url")
	m := remoteURL.FindStringSubmatch(url)
	if m == nil {
		return ""
	}
	return m[1] + "/" + m[2] + "/" + m[3]
}

// prText is command and the --body-file (-F) each gh in it names, read
// relative to cwd: what the PR's body can name.
func prText(command, cwd string) string {
	text := command
	ps, _ := sh.JudgeString(command, nil, nil, sh.Opts{})
	for _, p := range ps {
		if p.Name != "gh" {
			continue
		}
		for i, a := range p.Argv {
			file := ""
			switch {
			case (a == "--body-file" || a == "-F") && i+1 < len(p.Argv):
				file = p.Argv[i+1]
			case strings.HasPrefix(a, "--body-file="):
				file = strings.TrimPrefix(a, "--body-file=")
			}
			if file == "" || file == "-" {
				continue
			}
			if !filepath.IsAbs(file) {
				file = filepath.Join(cwd, file)
			}
			if b, err := os.ReadFile(file); err == nil {
				text += "\n" + string(b)
			}
		}
	}
	return text
}
