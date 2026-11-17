package discover

import "testing"

func TestIsTestPathFollowsTheStacksConventions(t *testing.T) {
	for path, want := range map[string]bool{
		"engine/internal/ldd/status_test.go": true,
		"web/src/login.test.ts":              true,
		"web/src/login.spec.tsx":             true,
		"api/tests/test_login.py":            true,
		"api/login_test.py":                  true,
		"crate/tests/login.rs":               true,
		"web/__tests__/login.js":             true,
		"engine/internal/ldd/status.go":      false,
		"web/src/login.ts":                   false,
		"docs/testing.md":                    false,
	} {
		if got := IsTestPath(path); got != want {
			t.Errorf("IsTestPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestSkipDirIsWhatDiscoveryNeverEnters(t *testing.T) {
	for _, d := range []string{".git", "node_modules", "vendor", "build"} {
		if !SkipDir(d) {
			t.Errorf("SkipDir(%q) = false", d)
		}
	}
	if SkipDir("src") {
		t.Error("SkipDir(src) = true")
	}
}
