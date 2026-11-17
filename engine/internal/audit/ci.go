package audit

// A pull request's CI as delivery evidence (delivery/tested-revision): a PR
// merged after its CI reported every check green is a tested delivery, as a
// passing `jhon executions run` of its tree is.

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// ciGreen is a CI report of every check of one PR passing.
type ciGreen struct {
	pr string
	at string
}

var (
	// checkRow is one row of `gh pr checks` output: name, state, then more.
	checkRow  = lazyre.New(`^([^\t]+)\t(pass|fail|pending|skipping|cancel)\t`)
	pullURL   = lazyre.New(`/pull/(\d+)(?:[/#?].*)?$`)
	prNumber  = lazyre.New(`^#?(\d+)$`)
	headLimit = lazyre.New(`^-n?(\d+)$`)
)

// ghPR is the command of a simple command `gh pr <sub> <pr>` and the PR it
// names by number or URL; nil when it names none (a branch, the current one).
func ghPR(cmds []*sh.Command, sub string) (*sh.Command, string) {
	for _, c := range cmds {
		w := c.Words
		if len(w) < 3 || w[0] != "gh" || w[1] != "pr" || w[2] != sub {
			continue
		}
		for i := 3; i < len(w); i++ {
			a := w[i]
			if strings.HasPrefix(a, "-") {
				// An option's value is its next word unless written --opt=value.
				if !strings.Contains(a, "=") && (a == "-i" || a == "--interval" || a == "-R" || a == "--repo" || a == "-q" || a == "--jq" || a == "-t" || a == "--template" || a == "--json") {
					i++
				}
				continue
			}
			if m := prNumber.FindStringSubmatch(a); m != nil {
				return c, m[1]
			}
			if m := pullURL.FindStringSubmatch(a); m != nil {
				return c, m[1]
			}
			return nil, "" // a branch: no PR this reader can tie a merge to
		}
		return nil, ""
	}
	return nil, ""
}

// CIGreen is the PR whose checks a command reports all green, read from its
// exit and output; "" when it reports none. A `gh pr checks <pr>` that is
// the command's last and feeds no pipe exits 0 only when every check passed
// (1 for a failure, 8 for one still pending). Otherwise its output counts
// when it is shown whole: piped only through a `tail` or `head` whose limit
// the output does not reach, with at least one check row, and every check's
// last state pass or skipping. Filtered by a `grep` or `rg` (not inverted),
// it counts when it exits 0 and shows the CI's gate row passing (ciGate,
// which sums every check) and no sign of a failure or a pending check
// (ciFailure).
// Formatted output (--json, --jq, --template) is not read.
func CIGreen(command, output string, exitedZero bool) string {
	cmds, err := sh.Parse(WithoutHeredocs(command))
	if err != nil {
		return ""
	}
	c, pr := ghPR(cmds, "checks")
	if c == nil || hasFlag(c.Words, "--json", "--jq", "-q", "--template", "-t") {
		return ""
	}
	var feeds []*sh.Command
	for _, x := range cmds {
		for p := x.PipeFrom; p != nil; p = p.PipeFrom {
			if p == c {
				feeds = append(feeds, x)
			}
		}
	}
	if len(feeds) == 0 && cmds[len(cmds)-1] == c {
		if exitedZero {
			return pr
		}
		return ""
	}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	filtered := false
	for _, x := range feeds {
		if len(x.Words) > 0 && (x.Words[0] == "grep" || x.Words[0] == "rg") {
			if hasFlag(x.Words, "--invert-match") || slices.ContainsFunc(x.Words[1:], func(w string) bool {
				return strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && strings.Contains(w, "v")
			}) {
				return ""
			}
			filtered = true
			continue
		}
		if len(x.Words) == 0 || (x.Words[0] != "tail" && x.Words[0] != "head") {
			return ""
		}
		limit := 10
		for i := 1; i < len(x.Words); i++ {
			w := x.Words[i]
			if w == "-n" && i+1 < len(x.Words) {
				w, i = "-"+x.Words[i+1], i+1
			}
			if m := headLimit.FindStringSubmatch(w); m != nil {
				limit, _ = strconv.Atoi(m[1])
			}
		}
		if len(lines) >= limit {
			return "" // the output may have been cut
		}
	}
	if filtered {
		if exitedZero && ciGate.MatchString(output) && !ciFailure.MatchString(output) {
			return pr
		}
		return ""
	}
	state := map[string]string{}
	for _, l := range lines {
		if m := checkRow.FindStringSubmatch(l); m != nil {
			state[m[1]] = m[2]
		}
	}
	if len(state) == 0 {
		return ""
	}
	for _, s := range state {
		if s != "pass" && s != "skipping" {
			return ""
		}
	}
	return pr
}

// MergedPR is the PR a `gh pr merge <pr>` merges; "" when it names none.
func MergedPR(command string) string {
	cmds, err := sh.Parse(WithoutHeredocs(command))
	if err != nil {
		return ""
	}
	_, pr := ghPR(cmds, "merge")
	return pr
}

// hasFlag is whether words hold any of flags, alone or as flag=value.
func hasFlag(words []string, flags ...string) bool {
	for _, w := range words {
		for _, f := range flags {
			if w == f || strings.HasPrefix(w, f+"=") {
				return true
			}
		}
	}
	return false
}

// A background job's end as delivery evidence: pi's job notice
// (piJobNotice) and Claude Code's task notification, whose output file
// holds what the job printed, show a CI watch's result after the call that
// started it returned.

// jobNotice is how a background job ended, as its notice reports it: its
// command, its exit code (nil when not given), and its last output.
type jobNotice struct {
	command, output string
	exit            *int
}

var (
	// ciGate is the row of the CI's gate job passing: `ci-success	pass`.
	ciGate = lazyre.New(`(?m)^ci-success\tpass\t`)
	// conclusionSuccess is a run's conclusion success: `"conclusion":"success"`, `conclusion: success`.
	conclusionSuccess = lazyre.New(`(?i)\bconclusion"?` + space + `*[:=]?` + space + `*"?success\b`)
	// bareSuccess is the conclusion alone on its line, as `--jq .conclusion` prints it.
	bareSuccess = lazyre.New(`(?m)^success` + space + `*$`)
	// ciFailure is any sign of a check or a run that did not pass, or has not finished.
	ciFailure = lazyre.New(`(?i)\b(?:fail|fails|failed|failing|failure|cancel|cancelled|canceled|timed_out|startup_failure|action_required|pending)\b`)
	prNamed   = lazyre.New(`\bgh` + space + `+pr` + space + `+(?:checks|view|status)` + space + `+#?(\d+)\b`)
	pullNamed = lazyre.New(`/pull/(\d+)\b`)
	// branchFlag is a `gh run list|watch --branch <branch>`.
	branchFlag = lazyre.New(`(?:--branch|-b)(?:=|` + space + `+)["']?([^\s"';&|]+)`)
	// runNamed is a CI run's id: `gh run view|watch|rerun <id>`, an /actions/runs/ URL, `run <id>`.
	runNamed = lazyre.New(`\bgh` + space + `+run` + space + `+(?:view|watch|rerun)` + space + `+["']?(\d+)\b|/actions/runs/(\d+)\b|\brun` + space + `+(\d{6,})\b`)
	cdInto   = lazyre.New(`(?:^|[;&|(])` + space + `*cd` + space + `+["']?([^\s"';&|)]+)`)
	headRef  = lazyre.New(`"headRefName"` + space + `*:` + space + `*"([^"]+)"`)
)

// prRefs is what a session shows of its PRs: the branches and worktrees
// each PR's commands name, and the PR each CI run belongs to.
type prRefs struct {
	branches, worktrees map[string][]string
	runs                map[string]string
}

// addTo adds v once to m[k].
func addTo(m map[string][]string, k, v string) {
	list := m[k]
	addOnce(&list, v)
	m[k] = list
}

// prsNamed is the PRs a command and its output name: by number (`gh pr
// checks|view|status <pr>`, a /pull/ URL in the command), by branch
// (`--branch`), or by CI run, which refs map to their PR.
func (refs prRefs) prsNamed(command, output string) []string {
	var out []string
	for _, re := range []*lazyre.Regexp{prNamed, pullNamed} {
		for _, m := range re.FindAllStringSubmatch(command, -1) {
			addOnce(&out, m[1])
		}
	}
	for _, m := range branchFlag.FindAllStringSubmatch(command, -1) {
		for pr, bs := range refs.branches {
			if slices.Contains(bs, m[1]) {
				addOnce(&out, pr)
			}
		}
	}
	for _, text := range []string{command, output} {
		for _, m := range runNamed.FindAllStringSubmatch(text, -1) {
			if pr := refs.runs[m[1]+m[2]+m[3]]; pr != "" {
				addOnce(&out, pr)
			}
		}
	}
	slices.Sort(out)
	return out
}

// NoticeCIGreen is the PR whose CI a background job's end reports passing;
// "" when it reports none. The job exited 0, its command names exactly one
// PR (prsNamed), and its output shows the CI's gate job passing
// (`ci-success	pass`) or the run's conclusion success, with no sign of a
// failure, a cancel, or a pending check.
func (refs prRefs) NoticeCIGreen(j jobNotice) string {
	if j.exit == nil || *j.exit != 0 || ciFailure.MatchString(j.output) {
		return ""
	}
	if !ciGate.MatchString(j.output) && !conclusionSuccess.MatchString(j.output) &&
		!(bareSuccess.MatchString(j.output) && strings.Contains(j.command, ".conclusion")) {
		return ""
	}
	if prs := refs.prsNamed(j.command, j.output); len(prs) == 1 {
		return prs[0]
	}
	return ""
}

// readPRRefs is the PR references a session's Bash calls show: a `gh pr
// create` whose result is the PR's URL names its branch (`--head`, or a
// `git push` in the same line) and the worktree it ran in (its `cd`); a `gh
// pr view <pr>` names the branch its headRefName output reports. A command
// that names one PR ties the CI runs it and its output name to that PR.
func readPRRefs(s *Session) prRefs {
	refs := prRefs{branches: map[string][]string{}, worktrees: map[string][]string{}, runs: map[string]string{}}
	type ran struct{ command, output string }
	var calls []ran
	for _, t := range s.transcripts() {
		uses := map[string]string{}
		for _, e := range t.Entries {
			for _, u := range e.ToolUses {
				if c, ok := strProp(u.Input, "command"); ok && u.Name == "Bash" {
					uses[u.ID] = c
				}
			}
			for _, r := range e.ToolResults {
				if c, ok := uses[r.ID]; ok && !r.IsError {
					calls = append(calls, ran{c, r.Text})
				}
			}
		}
	}
	for _, c := range calls {
		xs := codeless(plain(c.command))
		ghPRSub := func(x Invocation, sub string) bool {
			return x.Program == "gh" && len(x.Args) > 1 && x.Args[0] == "pr" && x.Args[1] == sub
		}
		if m := pullNamed.FindStringSubmatch(c.output); m != nil && slices.ContainsFunc(xs, func(x Invocation) bool { return ghPRSub(x, "create") }) {
			pr := m[1]
			for _, x := range xs {
				switch {
				case ghPRSub(x, "create"):
					for i, a := range x.Args {
						if v, ok := strings.CutPrefix(a, "--head="); ok {
							addTo(refs.branches, pr, v)
						} else if (a == "--head" || a == "-H") && i+1 < len(x.Args) {
							addTo(refs.branches, pr, x.Args[i+1])
						}
					}
				case x.Program == "git" && x.Sub == "push":
					var operands []string
					for _, a := range x.Args[indexOf(x.Args, x.Sub)+1:] {
						if !strings.HasPrefix(a, "-") {
							operands = append(operands, a)
						}
					}
					for _, spec := range operands[min(1, len(operands)):] {
						if _, after, ok := strings.Cut(spec, ":"); ok {
							spec = after
						}
						addTo(refs.branches, pr, strings.TrimPrefix(strings.TrimPrefix(spec, "+"), "refs/heads/"))
					}
				}
			}
			for _, m := range cdInto.FindAllStringSubmatch(c.command, -1) {
				addTo(refs.worktrees, pr, filepath.Base(m[1]))
			}
		}
		if m := prNamed.FindStringSubmatch(c.command); m != nil && strings.Contains(c.command, "headRefName") && slices.ContainsFunc(xs, func(x Invocation) bool { return ghPRSub(x, "view") }) {
			if h := headRef.FindStringSubmatch(c.output); h != nil {
				addTo(refs.branches, m[1], h[1])
			} else if line := strings.TrimSpace(c.output); line != "" && !strings.ContainsAny(line, " \t\n{}\"") {
				addTo(refs.branches, m[1], line)
			}
		}
	}
	for _, c := range calls {
		prs := refs.prsNamed(c.command, "")
		if len(prs) != 1 {
			continue
		}
		for _, text := range []string{c.command, c.output} {
			for _, m := range runNamed.FindAllStringSubmatch(text, -1) {
				if id := m[1] + m[2] + m[3]; refs.runs[id] == "" {
					refs.runs[id] = prs[0]
				}
			}
		}
	}
	return refs
}

// changeOf is the change a PR's branch names, else the change its
// worktree names, among changes: a name is the change, or is it with `/`
// read as `-`, or its last `/` segment is; else, a branch's last segment
// ends in `-<change>` (`archive-<change>`, `acme-42-<change>`). "" when no
// single change is named.
func (refs prRefs) changeOf(pr string, changes []string) string {
	named := false
	for _, names := range [][]string{refs.branches[pr], refs.worktrees[pr]} {
		var hits []string
		for _, n := range names {
			last := n[strings.LastIndex(n, "/")+1:]
			for _, c := range changes {
				if c == n || c == strings.ReplaceAll(n, "/", "-") || c == last {
					addOnce(&hits, c)
				}
			}
		}
		if len(hits) == 1 {
			return hits[0]
		}
		named = named || len(hits) > 0
	}
	if named {
		return "" // an ambiguous name is not read again by its suffix
	}
	var hits []string
	for _, n := range refs.branches[pr] {
		last := n[strings.LastIndex(n, "/")+1:]
		for _, c := range changes {
			if strings.HasSuffix(last, "-"+c) {
				addOnce(&hits, c)
			}
		}
	}
	if len(hits) == 1 {
		return hits[0]
	}
	return ""
}
