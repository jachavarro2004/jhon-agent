package execution

import (
	"testing"
)

func TestLockGroupsWaitsForAnotherHolderAndStops(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	release, ok := LockGroups(root, []string{"db"}, nil, func(string) { t.Error("a free group is taken at once") })
	if !ok {
		t.Fatal("a free group is taken")
	}
	stop := make(chan struct{})
	var waited []string
	_, ok = LockGroups(root, []string{"db"}, stop, func(g string) { waited = append(waited, g); close(stop) })
	if ok || len(waited) != 1 || waited[0] != "db" {
		t.Errorf("a held group is waited for until stop: ok %v, waited %q", ok, waited)
	}
	release()
	again, ok := LockGroups(root, []string{"db"}, nil, func(string) { t.Error("a released group is free") })
	if !ok {
		t.Fatal("a released group is taken")
	}
	again()
	if _, ok := LockGroups(t.TempDir(), []string{"db"}, nil, func(string) { t.Error("another repository's group is its own") }); !ok {
		t.Fatal("another repository's group is taken")
	}
}
