package audit

import "testing"

// A runtime's automatic notices are not the owner's messages (L48): a
// compaction summary, a background job or sub-agent that ended, a
// system-generated wake. The owner's own words are.
func TestAutomaticNoticesAreNotOwnerMessages(t *testing.T) {
	for _, c := range []struct {
		text  string
		owner bool
	}{
		{"Como va?", true},
		{"Background job job-1 (\"CI for PR 842\") exited with code 0 after 6m 36s.", false},
		{"Background sub-agent agent-1 (verify) completed after 8m 49s.", false},
		{"The conversation history before this point was compacted into the following summary:\n<summary>", false},
		{"Subagent gentle-ai-verify (task x, \"verify\") finished.\n\nverdict: pass", false},
		{"Background job \"t\" exited.\n[System-generated Gentle Agents notification, not written by the user]", false},
	} {
		text := c.text
		e := &Entry{Role: "user", Prompt: &text}
		if got := IsGenuineUserMessage(e); got != c.owner {
			t.Errorf("%q: owner %v, want %v", c.text, got, c.owner)
		}
	}
}

// The versions no real session verified make one note, not one each (L48).
func TestUnverifiedVersionsOneNote(t *testing.T) {

	got := unverifiedNote([]string{"2.1.83", SupportedVersions[0], "2.1.310"})
	want := "Claude Code 2.1.83, 2.1.310 are read by their shape: no real session verified them (verified: " + joinVersions() + ")"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if one := unverifiedNote([]string{"9.9.9"}); one != "Claude Code 9.9.9 is read by its shape: no real session verified it (verified: "+joinVersions()+")" {
		t.Errorf("one version: %q", one)
	}
	if none := unverifiedNote([]string{SupportedVersions[0]}); none != "" {
		t.Errorf("no note when every version is verified: %q", none)
	}
}
