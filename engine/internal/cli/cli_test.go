package cli

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestHookWithoutName(t *testing.T) {
	if code, err := Run([]string{"hook"}); code != 2 || err == nil {
		t.Fatalf("Run(hook) = %d, %v; want 2 and a usage error", code, err)
	}
}

func TestUnknownHook(t *testing.T) {
	t.Setenv("JHON_ROOT", t.TempDir())
	if code, err := Run([]string{"hook", "no-such-guard"}); code != 2 || err == nil {
		t.Fatalf("Run(hook no-such-guard) = %d, %v; want 2 and an error", code, err)
	}
}

// Plan mode is gone (T50d, L436): its guard is no hook any more.
func TestPlanGuardIsUnknown(t *testing.T) {
	t.Setenv("JHON_ROOT", t.TempDir())
	if code, err := Run([]string{"hook", "plan-guard"}); code != 2 || err == nil || err.Error() != "unknown hook: plan-guard" {
		t.Fatalf("Run(hook plan-guard) = %d, %v; want 2 and unknown hook: plan-guard", code, err)
	}
}

// T157: `jhon sessions` folded into `jhon doctor`; the CLI answers it as any
// command it does not dispatch, and neither the usage nor the command table
// (what the render's prose check reads) names it.
func TestSessionsIsUnknown(t *testing.T) {
	for _, args := range [][]string{{"sessions"}, {"sessions", "--json"}} {
		code, out, errOut := captureAll(t, args)
		if code != 1 || out != "" || !strings.HasPrefix(errOut, "jhon: unknown command: sessions\n\n") {
			t.Errorf("jhon %v = %d\nstdout: %q\nstderr: %q\nwant 1 and the unknown-command answer", args, code, out, errOut)
		}
	}
	if slices.Contains(names(), "sessions") {
		t.Error("sessions is still a registered command")
	}
	if strings.Contains(usage, "jhon sessions") {
		t.Error("the usage still lists jhon sessions")
	}
	if _, out := capture(t, []string{"doctor", "--help"}); !strings.Contains(strings.Join(strings.Fields(out), " "), "warns of each live orchestrator session that started before the last install (resume it with jhon start --resume <id>)") {
		t.Errorf("jhon doctor --help does not name its sessions report:\n%s", out)
	}
}

func TestRootFromEnvironment(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "release")
	t.Setenv("JHON_ROOT", dir)
	if r, err := Root(); err != nil || r != dir {
		t.Fatalf("Root() = %q, %v; want %q", r, err, dir)
	}
}

func TestPanickingCommandFailsCleanly(t *testing.T) {
	code, err := guarded(func([]string) (int, error) { panic("TypeError: boom") }, nil)
	if code != 1 || err == nil || err.Error() != "TypeError: boom" {
		t.Fatalf("guarded = %d, %v; want 1 and the panic's message", code, err)
	}
}
