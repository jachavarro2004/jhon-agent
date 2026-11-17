package cli

import (
	"strings"
	"testing"
)

func TestParseGrant(t *testing.T) {
	a, bad := parseGrant([]string{"push", "gh-write", "--for", "2h", "--root", "/p"})
	if bad != "" || a.mode != "grant" || strings.Join(a.classes, ",") != "push,gh-write" || a.ms != 2*3_600_000 || *a.root != "/p" {
		t.Fatalf("parseGrant = %+v, %q", a, bad)
	}
	if a, bad := parseGrant([]string{"push"}); bad != "" || a.ms != 30*60_000 {
		t.Fatalf("default --for: %+v, %q", a, bad)
	}
	if a, bad := parseGrant([]string{"--list"}); bad != "" || a.mode != "list" {
		t.Fatalf("--list: %+v, %q", a, bad)
	}
	if a, bad := parseGrant([]string{"--list", "--json"}); bad != "" || a.mode != "list" || !a.json {
		t.Fatalf("--list --json: %+v, %q", a, bad)
	}
}

// An unknown or repeated class, a bad duration, or no argument is a usage error.
func TestParseGrantUsageErrors(t *testing.T) {
	for args, want := range map[string]string{
		"force-push":       "unknown class 'force-push'",
		"push force-push":  "unknown class 'force-push'",
		"push push":        "class 'push' given twice",
		"push --for soon":  "--for must be a positive number with s, m, h, or d, not 'soon'",
		"push --for 0m":    "--for must be a positive number with s, m, h, or d, not '0m'",
		"push --for -5m":   "--for requires a value",
		"push --root":      "--root requires a value",
		"":                 "a class is required",
		"--revoke":         "--revoke needs a class",
		"--list push":      "--list takes no class",
		"push --json":      "--json goes with --list",
		"push --force":     "unknown option: --force",
		"force-push push2": "unknown class 'force-push'",
	} {
		if _, bad := parseGrant(strings.Fields(args)); bad != want {
			t.Errorf("parseGrant(%q) = %q, want %q", args, bad, want)
		}
	}
}
