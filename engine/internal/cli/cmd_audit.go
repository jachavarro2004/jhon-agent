package cli

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/audit"
	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/dashboard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// `jhon audit <session-id|path> [--json | --html | --view] [--context]`:
// one Claude Code or pi session's transcripts, audited after the
// fact (internal/audit). Read-only. Exit 0 when clean, nothing-to-audit, or
// suspect-only, 1 on any violation, 2 on an audit-error or a usage error.
func init() { register("audit", runAuditCommand) }

func runAuditCommand(argv []string) (int, error) {
	if slices.Contains(argv, "--all") {
		return runAuditAll(argv)
	}
	json, html, view, context := slices.Contains(argv, "--json"), slices.Contains(argv, "--html"), slices.Contains(argv, "--view"), slices.Contains(argv, "--context")
	var rest []string
	for _, a := range argv {
		if a != "--json" && a != "--html" && a != "--view" && a != "--context" {
			rest = append(rest, a)
		}
	}
	if len(rest) != 1 || strings.HasPrefix(rest[0], "-") {
		fmt.Fprintln(os.Stderr, "usage: jhon audit <session-id|path/to/session.jsonl> [--json | --html | --view] [--context]\n       "+auditAllUsage[len("usage: "):])
		return 2, nil
	}
	home, _ := os.UserHomeDir()
	target := audit.ResolveTarget(rest[0], cwd(), filepath.Join(home, ".claude", "projects"))
	if target == "" {
		// A pi session: Jhon's own pi agent directory, then the owner's.
		target = audit.ResolvePiTarget(rest[0], []string{filepath.Join(home, ".pi", "jhon", "sessions"), filepath.Join(home, ".pi", "agent", "sessions")})
	}
	if target == "" {
		fmt.Fprintf(os.Stderr, "jhon audit: no session %s (a .jsonl path, or an id under ~/.claude/projects, ~/.pi/jhon/sessions, or ~/.pi/agent/sessions)\n", rest[0])
		return 2, nil
	}
	// --json and --view are kept per session (dashboard.AuditCache): a hit
	// prints what the audit printed, and its exit code, without auditing,
	// while the transcripts, the binary, and every other input the audit read
	// (audit.Inputs: change records, git state) are as they were.
	var cache dashboard.AuditCache
	var cached, mode, key string
	if json || view {
		cached = target
		if abs, err := filepath.Abs(target); err == nil {
			cached = abs
		}
		mode = map[bool]string{true: "--json", false: "--view"}[json]
		key = dashboard.SessionKey(cached) + auditBinaryKey()
		if dir, err := os.UserCacheDir(); err == nil {
			cache.Dir = filepath.Join(dir, "jhon", "audit")
		}
		if out, code, ok := cache.Get(cached, mode, key, func(spec string) string { return audit.DecodeInputs(spec).Key() }); ok {
			fmt.Print(out)
			return code, nil
		}
	}
	began := time.Now()
	r, inputs := audit.AuditTracked(target)
	code := 1
	switch {
	case len(r.Errors) > 0:
		code = 2
	case r.OK:
		code = 0
	}
	session := func() dashboard.Session {
		s := dashboard.Session{ID: strings.TrimSuffix(filepath.Base(target), ".jsonl"), Name: dashboard.ProjectName(filepath.Base(filepath.Dir(target))), Path: target}
		if st, err := os.Stat(target); err == nil {
			s.Modified = st.ModTime().UTC().Format("2006-01-02T15:04:05.000Z")
		}
		if strings.Contains(target, string(filepath.Separator)+".pi"+string(filepath.Separator)) {
			s.Runtime = "pi"
		}
		return s
	}
	switch {
	case json || view:
		var out string
		if json {
			out = js.StringifyValue(r.Report()) + "\n"
		} else {
			// What the dashboard component reads of the session (/api/session/<id>'s
			// shape), printed for a reader outside the browser; nothing in the
			// tree runs --view since the shell's Audit tab went (T17b).
			out = js.StringifyValueCompact(dashboard.SessionData(session(), r.Report())) + "\n"
		}
		fmt.Print(out)
		// Inputs it cannot name, or that moved while it ran, are not kept.
		spec, named := inputs.Encode()
		if deps, still := inputs.KeyAsOf(began); named && still {
			cache.Put(cached, mode, key, spec, deps, out, code)
		}
	case html:
		report := r.Report()
		page, err := dashboard.Page(filepath.Join(release.Locate().Runtime, "dashboard"), dashboard.SessionData(session(), report), report)
		if err != nil {
			return 1, err
		}
		fmt.Println(page)
	default:
		fmt.Println(audit.Human(r, target, context))
	}
	return code, nil
}

// auditBinaryKey is what an audit's output depends on besides the session
// and its inputs: this binary, the contracts it reads, and the grants'
// history where this environment names it, each by size and mtime.
func auditBinaryKey() string {
	paths := []string{grant.HistoryFile()}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		paths = append(paths, exe)
	}
	if root, err := contract.Root(); err == nil {
		paths = append(paths, filepath.Join(root, "prose", "contracts"))
	}
	return dashboard.StatKey(paths...)
}
