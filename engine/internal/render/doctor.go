package render

import (
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
)

// RuntimeDoctor is `jhon doctor --runtime <name>`
// (docket AG3): for one runtime, the guarantee level of every behavioural
// rule there and the verification state of every primitive, and the
// always-on bundle measured against the runtime's instruction budget.
// There is no global score: a level qualifies one rule, never the runtime.
// Exit 0 when nothing fails, 1 when the bundle does not fit or the matrix
// has a problem naming this runtime, 2 on a usage error. Read-only.
func RuntimeDoctor(root string, argv []string, stdout, stderr io.Writer) (code int) {
	defer recoverExit(stderr, &code)
	e, err := newEngine(resolveRoot(root), args{}, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if len(argv) != 1 || argv[0] == "" {
		fmt.Fprintln(stderr, "usage: jhon doctor --runtime <name>")
		return 2
	}
	runtime := argv[0]
	if !slices.Contains(e.m.Matrix.Runtimes, runtime) {
		fmt.Fprintf(stderr, "jhon doctor: unknown runtime '%s'; agents.yaml declares %s\n", runtime, strings.Join(e.m.Matrix.Runtimes, ", "))
		return 2
	}
	// A runtime settings.runtimes turns off is not worked in: nothing of it
	// is judged (T56).
	if key, off := e.off[runtime]; off {
		if e.absent[runtime] {
			fmt.Fprintf(stdout, "runtime %s was not found (no ~/%s, no %s on PATH) and %s is not set; nothing to check\n", runtime, runtimeHome[runtime], map[string]string{"claude-code": "claude", "pi": "pi"}[runtime], key)
			return 0
		}
		fmt.Fprintf(stdout, "runtime %s is off (%s = false); nothing to check\n", runtime, key)
		return 0
	}
	// The vocabulary must exist here, and is read whole.
	mustReadText(e.vocabulary().file)
	var problems []manifest.Finding
	for _, p := range e.validate() {
		if p.rule == "runtime-matrix" || p.rule == "runtime-guarantee" {
			problems = append(problems, manifest.Finding{Rule: p.rule, Msg: p.msg})
		}
	}
	vocab := e.vocabulary()
	text, failed := manifest.Report(e.m.Matrix, runtime, manifest.ReportOptions{
		AuditRules:  vocab.auditRules,
		Levels:      vocab.guaranteeLevels,
		BundleBytes: len(e.alwaysOnBundle(runtime)),
		Problems:    problems,
	})
	fmt.Fprint(stdout, text)
	if runtime == "pi" {
		fmt.Fprint(stdout, "\nprovider\n"+e.piProviderLine()+"\n")
	}
	if failed {
		return 1
	}
	return 0
}

// piProviderLine is the provider the active profile runs pi on. Only
// claude-bridge runs Claude Code, so only it looks for the claude binary; a
// missing one is a warning, never a failure, as the bridge is the owner's
// choice to make work.
func (e *engine) piProviderLine() string {
	provider := PiProvider(e.m, e.profile)
	head := fmt.Sprintf("provider %s (profile %s)", provider, e.active)
	if provider != BridgeProvider {
		return "  ok    " + head + ": Claude Code is not needed"
	}
	if p, err := exec.LookPath("claude"); err == nil {
		return "  ok    " + head + ": claude at " + p
	}
	return "  warn  " + head + ": claude is not on PATH; the bridge runs Claude Code"
}
