package integrations

import "testing"

// T237 (odd/tasks/studio-integration-v1.md S5): a cli tool declares the
// jhon it needs in `<binary> integration --json`: requires_jhon, a SemVer
// range, and contract, jhon.integration/v1. Judge says whether the running
// jhon fits it, and when it does not, the one note that keeps the tool off.

var studioFixture = Manifest{Name: "studio", Kind: "cli", Doctor: Doctor{Binary: "jhon-studio"}}

func TestJudgeAcceptsAJhonInsideTheToolsRange(t *testing.T) {
	for _, c := range []struct{ requires, jhon string }{
		{">=0.5.0-rc.1", "0.5.0-rc.1"},
		{">=0.5.0-rc.1", "0.5.0-rc.2"},
		{">=0.5.0-rc.1", "0.5.0"},
		{">=0.5.0-rc.1", "0.6.1"},
		{">=0.5.0 <0.7.0", "0.6.9"},
		{">= 0.5.0", "0.5.0"},
		{"0.5.0", "0.5.0"},
		{"=0.5.0", "0.5.0"},
		{">0.4.9", "0.5.0"},
		{"<=0.5.0", "0.5.0"},
		{"<1.0.0 || >=2.0.0", "2.1.0"},
		{">=v0.5.0", "0.5.0"},
		{">=0.5.0+build.7", "0.5.0"},
	} {
		got := Judge(studioFixture, Answer{Version: "0.1.0", RequiresJhon: c.requires, Contract: Contract}, c.jhon)
		if got != (Fit{}) {
			t.Errorf("requires %q, jhon %s: %+v, want it to fit", c.requires, c.jhon, got)
		}
	}
}

// SemVer precedence: a pre-release is older than its release.
func TestJudgeKeepsOffATooOldJhonWithOneNote(t *testing.T) {
	for _, c := range []struct{ requires, jhon string }{
		{">=0.5.0", "0.5.0-rc.1"},
		{">=0.5.0-rc.2", "0.5.0-rc.1"},
		{">=0.6.0", "0.5.0-rc.1"},
		{">=0.5.0 <0.6.0", "0.6.0"},
		{"<1.0.0 || >=2.0.0", "1.5.0"},
	} {
		got := Judge(studioFixture, Answer{Version: "0.2.0", RequiresJhon: c.requires, Contract: Contract}, c.jhon)
		want := "jhon-studio 0.2.0 requires jhon " + c.requires + ", and this is jhon " + c.jhon +
			": jhon-studio is off for this run; update jhon (jhon update), or install a jhon-studio version that supports jhon " + c.jhon
		if got.Off != want {
			t.Errorf("requires %q, jhon %s:\n got %q\nwant %q", c.requires, c.jhon, got.Off, want)
		}
	}
}

func TestJudgeKeepsOffAToolOfAnotherContract(t *testing.T) {
	got := Judge(studioFixture, Answer{Version: "1.0.0", RequiresJhon: ">=0.9.0", Contract: "jhon.integration/v2"}, "0.5.0")
	want := "jhon-studio 1.0.0 speaks jhon.integration/v2 (it requires jhon >=0.9.0), and this jhon 0.5.0 speaks jhon.integration/v1: jhon-studio is off for this run; update jhon (jhon update), or install a jhon-studio version that speaks jhon.integration/v1"
	if got.Off != want {
		t.Errorf("another contract:\n got %q\nwant %q", got.Off, want)
	}
	// A development build satisfies any minimum, never another contract.
	if got := Judge(studioFixture, Answer{Version: "1.0.0", Contract: "jhon.integration/v2"}, "0.0.0-dev+abc1234"); got.Off != "jhon-studio 1.0.0 speaks jhon.integration/v2, and this jhon 0.0.0-dev+abc1234 speaks jhon.integration/v1: jhon-studio is off for this run; update jhon (jhon update), or install a jhon-studio version that speaks jhon.integration/v1" {
		t.Errorf("another contract on a development build: %q", got.Off)
	}
}

func TestJudgeKeepsOffARangeItCannotRead(t *testing.T) {
	for _, requires := range []string{"1.x", "^0.5.0", ">=0.5", ">=", "0.5.0 ||", "latest"} {
		got := Judge(studioFixture, Answer{Version: "0.2.0", RequiresJhon: requires}, "0.5.0")
		want := "jhon-studio 0.2.0 requires jhon \"" + requires + "\", a range this jhon 0.5.0 cannot read: jhon-studio is off for this run; update jhon (jhon update), or install a jhon-studio version that supports jhon 0.5.0"
		if got.Off != want {
			t.Errorf("requires %q:\n got %q\nwant %q", requires, got.Off, want)
		}
	}
}

// An older tool, which names neither, is accepted: its contract is v1, and
// it declares no minimum.
func TestJudgeAcceptsAnOlderToolThatDeclaresNothing(t *testing.T) {
	if got := Judge(studioFixture, Answer{Version: "0.1.0"}, "0.5.0"); got != (Fit{NoMinimum: true}) {
		t.Errorf("no requires_jhon, no contract: %+v", got)
	}
	if got := Judge(studioFixture, Answer{Version: "0.1.0", RequiresJhon: ">=0.5.0"}, "0.5.0"); got != (Fit{}) {
		t.Errorf("no contract: %+v", got)
	}
	if got := Judge(studioFixture, Answer{Version: "0.1.0", RequiresJhon: ">=0.6.0"}, "0.5.0"); got.Off == "" {
		t.Errorf("no contract, a minimum above: %+v", got)
	}
}

// A development build of jhon (jhon sync stamps 0.0.0-dev+<hash>; a plain
// build is 0.0.0-dev) satisfies any minimum, said as not checked.
func TestJudgeLetsADevelopmentBuildPassAnyMinimum(t *testing.T) {
	for _, jhon := range []string{"0.0.0-dev", "0.0.0-dev+abc1234"} {
		for _, requires := range []string{">=0.5.0-rc.1", ">=99.0.0", "<0.0.0"} {
			if got := Judge(studioFixture, Answer{Version: "0.2.0", RequiresJhon: requires, Contract: Contract}, jhon); got != (Fit{Unchecked: true}) {
				t.Errorf("jhon %s, requires %q: %+v", jhon, requires, got)
			}
		}
		if got := Judge(studioFixture, Answer{Version: "0.2.0"}, jhon); got != (Fit{NoMinimum: true}) {
			t.Errorf("jhon %s, no minimum: %+v", jhon, got)
		}
	}
	// A pre-release that is not a development build is compared.
	if got := Judge(studioFixture, Answer{Version: "0.2.0", RequiresJhon: ">=0.5.0"}, "0.0.0-rc.1"); got.Off == "" {
		t.Errorf("0.0.0-rc.1 passed >=0.5.0: %+v", got)
	}
}

func TestJudgeNamesAToolWithNoVersion(t *testing.T) {
	got := Judge(studioFixture, Answer{RequiresJhon: ">=0.6.0"}, "0.5.0")
	if want := "jhon-studio (no version) requires jhon >=0.6.0, and this is jhon 0.5.0: jhon-studio is off for this run; update jhon (jhon update), or install a jhon-studio version that supports jhon 0.5.0"; got.Off != want {
		t.Errorf("no version:\n got %q\nwant %q", got.Off, want)
	}
}

// A tool of another contract may name no plugin_dir: Judge, not the
// question, says why it is off.
func TestAskIntegrationReadsTheNewFieldsAndLeavesAnotherContractToJudge(t *testing.T) {
	fakeCLI(t, "clifixture-bin", `{"version":"0.2.0","plugin_dir":"/p","skill":"/p/SKILL.md","requires_jhon":">=0.5.0-rc.1","contract":"jhon.integration/v1"}`, 0, "", 0)
	a, err := AskIntegration(cliFixture)
	if err != nil || a != (Answer{Version: "0.2.0", PluginDir: "/p", Skill: "/p/SKILL.md", RequiresJhon: ">=0.5.0-rc.1", Contract: "jhon.integration/v1"}) {
		t.Errorf("AskIntegration = %+v, %v", a, err)
	}
	fakeCLI(t, "clifixture-bin", `{"version":"2.0.0","contract":"jhon.integration/v2"}`, 0, "", 0)
	if a, err := AskIntegration(cliFixture); err != nil || a.Contract != "jhon.integration/v2" {
		t.Errorf("another contract, no plugin_dir: %+v, %v", a, err)
	}
	fakeCLI(t, "clifixture-bin", `{"version":"0.1.0"}`, 0, "", 0)
	if _, err := AskIntegration(cliFixture); err == nil || err.Error() != "clifixture-bin integration --json: names no plugin_dir" {
		t.Errorf("v1, no plugin_dir: %v", err)
	}
}
