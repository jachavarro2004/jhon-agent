package ldd

import (
	"path/filepath"
	"strings"
	"testing"
)

// A change's own record (openspec/changes/**, and the specs it is archived
// into, openspec/specs/**) is passive in jhon risk (S13): the record never
// raises the tier or the route.
func TestRiskChangeRecordIsPassive(t *testing.T) {
	jhonRoot(t)
	root := routeRepo(t, 3, "openspec/changes/c1/execution-record.json", "openspec/changes/c1/tasks.md")
	risk, err := RiskOf(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if risk.Tier != Passive {
		t.Errorf("record only: tier %s, want passive: %v", risk.Tier, risk.Reasons)
	}
	for _, r := range risk.Reasons {
		if r.File == "openspec/changes/c1/execution-record.json" && (r.Level != Passive || r.Why != "change record") {
			t.Errorf("execution-record.json: %s %q, want passive \"change record\"", r.Level, r.Why)
		}
	}
	route, err := RouteOf(root)
	if err != nil {
		t.Fatal(err)
	}
	if route.Level != RouteStandard || route.Tier != Passive {
		t.Errorf("record only: route %s tier %s, want standard passive: %v", route.Level, route.Tier, route.Reasons)
	}

	specs := routeRepo(t, 3, "openspec/specs/billing/schema.json")
	if risk, _ := RiskOf(specs, nil); risk.Tier != Passive {
		t.Errorf("openspec/specs/**: tier %s, want passive: %v", risk.Tier, risk.Reasons)
	}
}

// The record never lowers the tier code gives the change.
func TestRiskChangeRecordKeepsCodeTier(t *testing.T) {
	jhonRoot(t)
	root := routeRepo(t, 3, "openspec/changes/c1/execution-record.json", "openspec/changes/c1/tasks.md", "src/app.py", "tests/test_app.py")
	if risk, _ := RiskOf(root, nil); risk.Tier != Medium {
		t.Errorf("record and code with its test: tier %s, want medium: %v", risk.Tier, risk.Reasons)
	}
	untested := routeRepo(t, 3, "openspec/changes/c1/execution-record.json", "src/app.py")
	route, _ := RouteOf(untested)
	if route.Tier != High || route.Level != RouteVerified {
		t.Errorf("record and untested code: route %s tier %s, want verified high: %v", route.Level, route.Tier, route.Reasons)
	}
}

// A project's risk_rules entry still outranks the default for the record.
func TestRiskChangeRecordProjectRule(t *testing.T) {
	jhonRoot(t)
	root := routeRepo(t, 3, "openspec/changes/c1/execution-record.json")
	write(t, filepath.Join(root, "openspec/config.yaml"), "risk_rules:\n  \"openspec/changes/**\": high\n")
	risk, _ := RiskOf(root, nil)
	var got []string
	for _, r := range risk.Reasons {
		got = append(got, string(r.Level)+" "+r.File+": "+r.Why)
	}
	want := "high openspec/changes/c1/execution-record.json: project rule openspec/changes/**\nmedium openspec/config.yaml: other file"
	if strings.Join(got, "\n") != want || risk.Tier != High {
		t.Errorf("tier %s, reasons\n%s\nwant\n%s", risk.Tier, strings.Join(got, "\n"), want)
	}
}

// Only the record's own files (Markdown, JSON, YAML) under a change folder
// are passive (S13, L31 W4); any other file there gets the default rules.
func TestRiskChangeRecordFilesOnly(t *testing.T) {
	jhonRoot(t)
	level := func(files map[string]string, file string) Reason {
		t.Helper()
		root := routeRepo(t, 3)
		for f, text := range files {
			write(t, filepath.Join(root, f), text)
		}
		risk, err := RiskOf(root, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range risk.Reasons {
			if r.File == file {
				return r
			}
		}
		t.Fatalf("%s: no reason in %v", file, risk.Reasons)
		return Reason{}
	}
	for _, f := range []string{"execution-record.json", "tasks.md", "delivery.json", "design.yaml", "meta.yml"} {
		file := "openspec/changes/c1/" + f
		if r := level(map[string]string{file: "x\n"}, file); r.Level != Passive || r.Why != "change record" {
			t.Errorf("%s: %s %q, want passive \"change record\"", file, r.Level, r.Why)
		}
	}
	for _, c := range []struct {
		files map[string]string
		file  string
		level Level
		why   string
	}{
		{map[string]string{"openspec/changes/c1/.env": "TOKEN=x\n"}, "openspec/changes/c1/.env", High, "security path"},
		{map[string]string{"openspec/changes/c1/001_drop.sql": "DROP TABLE users;\n"}, "openspec/changes/c1/001_drop.sql", High, "migration or schema"},
		{map[string]string{"openspec/changes/c1/tool.go": "package c1\n", "tests/test_a.py": "x\n"}, "openspec/changes/c1/tool.go", Medium, "code"},
	} {
		if r := level(c.files, c.file); r.Level != c.level || r.Why != c.why {
			t.Errorf("%s: %s %q, want %s %q", c.file, r.Level, r.Why, c.level, c.why)
		}
	}
}
