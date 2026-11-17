package cli

import (
	"path/filepath"
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
