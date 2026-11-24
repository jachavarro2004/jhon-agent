package integrations

import "testing"

// The independent verify's boundary probes (studio-integration-v1 T2, S5/T237):
// pre-releases order below their release, ranges jhon cannot read turn the
// tool off on every build, and null fields load as missing ones.
func TestJudgeProbeBoundaries(t *testing.T) {
	cases := []struct {
		requires, jhon string
		off            bool
	}{
		{">=0.6.0", "0.6.0-rc.1", true}, {"<0.6.0", "0.6.0-rc.1", false},
		{">=0.5.0-rc.1", "0.4.0", true}, {">=0.5.0-rc.1", "0.6.0-rc.1", false},
		{"  ", "0.5.0", true}, {"^0.5", "0.0.0-dev+abc1234", true},
		{">=0.5", "0.0.0-dev", true}, {"1.x", "0.0.0-dev", true},
		{">=0.5.0, <0.7.0", "0.6.0", true}, {"0.5.0 - 0.7.0", "0.6.0", true},
		{" >=0.5.0-rc.1 ", "0.5.0-rc.1", false},
	}
	for _, c := range cases {
		got := Judge(studioFixture, Answer{Version: "0.1.0", RequiresJhon: c.requires, Contract: Contract}, c.jhon)
		if (got.Off != "") != c.off {
			t.Errorf("requires %q, jhon %s: %+v, want off=%v", c.requires, c.jhon, got, c.off)
		}
	}
}

func TestAskIntegrationProbeNullFieldsLoad(t *testing.T) {
	fakeCLI(t, "clifixture-bin", `{"version":"0.1.0","plugin_dir":"/p","skill":"/p/SKILL.md","requires_jhon":null,"contract":null}`, 0, "", 0)
	a, err := AskIntegration(cliFixture)
	if err != nil {
		t.Fatal(err)
	}
	if got := Judge(studioFixture, a, "0.5.0"); got != (Fit{NoMinimum: true}) {
		t.Errorf("null fields: %+v", got)
	}
}
