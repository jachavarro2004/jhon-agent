package hook

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// membersRefusal is why command, a PR opened in top, a workspace's primary,
// is refused, "" when it may open (T57 slice 7, L1324): one PR per
// repository, the primary's last. For each change the branch carries whose
// slices or record name members (ldd.ChangeMembers), every member's
// delivery is ready (ldd.MemberDeliveries, as `jhon deliver check` judges
// it there) and the PR's text names, for each member, a pull request of
// the repository its origin remote names (<host>/<owner>/<repo>/pull/<n>),
// read from its git config, with no network. The text is the command line,
// a heredoc body included, and a --body-file it names. A change with no
// member, a merge, and any repository but a primary are not judged here.
func membersRefusal(top, cwd, command string) string {
	if !deliveryOf(command).pr {
		return ""
	}
	ws := workspace.Of(top)
	if ws.Role != workspace.Primary {
		return ""
	}
	changes, _ := ldd.BranchChanges(top)
	if c := ldd.FolderlessChange(top); c != "" && !slices.Contains(changes, c) {
		changes = append(changes, c)
	}
	text := ""
	for _, c := range changes {
		lead := "the change " + c + " delivers in members of the workspace too, one PR per repository, the primary's last: "
		ms := ldd.MemberDeliveries(top, c, ws)
		for _, m := range ms {
			if !m.Ready {
				return lead + "member " + m.Name + " is not ready (" + strings.Join(m.Reasons, "; ") + "): run `jhon deliver check " + c + "` in " + m.Path + ", open its PR, then the primary's."
			}
		}
		for _, m := range ms {
			repo := originRepo(m.Path)
			if repo == "" {
				return lead + "member " + m.Name + " has no origin remote naming its repository on a host (`git -C " + m.Path + " remote get-url origin`), so this PR cannot name its pull request: add the remote, open the member's PR, then this one."
			}
			if text == "" {
				text = prText(command, cwd)
			}
			if !regexp.MustCompile(`(?i)(?:^|[^\w.-])(?:https?://)?` + regexp.QuoteMeta(repo) + `/pull/\d+\b`).MatchString(text) {
				return lead + "this PR's body names no pull request of member " + m.Name + ": open its PR first and name it, https://" + repo + "/pull/<n>, in this PR's body."
			}
		}
	}
	return ""
}

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
