package render

import (
	"path/filepath"
	"strings"
	"testing"
)

// T238 (odd/tasks/studio-integration-v1.md S5): with integrations.studio
// on, the main session's fixed text, lean and full, in both runtimes, says
// that a request for a video or "jhon:video" means jhon-studio's video
// skill, in the runtime's words, and where its page is installed; off, the
// render is what it was without it.

// studioSkill is what each runtime's text names as the skill.
var studioSkill = map[string]string{
	"claude-code": "the skill `jhon-studio:video`, which the owner runs as `/jhon-studio:video`",
	"pi":          "`/jhon:video`, which the owner types; to follow it yourself, read the `skill` file that `jhon-studio integration --json` names",
}

func TestStudioOnTellsTheMainSessionWhereVideosComeFrom(t *testing.T) {
	t.Setenv("JHON_SETTINGS", writeFile(t, "schema = 1\n\n[integrations]\nstudio = true\n"))
	e := engineAt(t, repo, "--validate")
	page := "`" + filepath.Join(e.current(), "docs", "jhon-studio.md") + "`"
	for _, rt := range []string{"claude-code", "pi"} {
		lean, full := flatText(e.leanText(rt)), flatText(e.fullText(rt))
		if want := "- A request for a video, or \"jhon:video\", with jhon-studio on: its video skill, " + studioSkill[rt] + "; how it works: " + page + "."; !strings.Contains(lean, want) {
			t.Errorf("%s's lean text lacks %q", rt, want)
		}
		if want := "With jhon-studio on, a request for a video, or \"jhon:video\", means its video skill: " + studioSkill[rt] + " (" + page + ")."; !strings.Contains(full, want) {
			t.Errorf("%s's full text lacks %q", rt, want)
		}
		if other := studioSkill[map[string]string{"pi": "claude-code", "claude-code": "pi"}[rt]]; strings.Contains(lean+full, other) {
			t.Errorf("%s's text names the other runtime's skill %q", rt, other)
		}
	}
	// At a representative home, as TestLeanTextFitsBothRuntimes measures it.
	e.home = "/home/owner"
	for _, rt := range []string{"claude-code", "pi"} {
		if n := len(e.leanText(rt)); n > leanLimit {
			t.Errorf("%s's lean text with jhon-studio on is %d bytes, over %d", rt, n, leanLimit)
		}
	}
}

func TestStudioOffLeavesNoWordOfItInTheMainSession(t *testing.T) {
	for _, text := range []string{"schema = 1\n", "schema = 1\n\n[integrations]\nstudio = false\n"} {
		t.Setenv("JHON_SETTINGS", writeFile(t, text))
		e := engineAt(t, repo, "--validate")
		for _, rt := range []string{"claude-code", "pi"} {
			for name, got := range map[string]string{"lean": e.leanText(rt), "full": e.fullText(rt)} {
				if strings.Contains(got, "jhon-studio") || strings.Contains(got, "video") {
					t.Errorf("%q: %s's %s text names jhon-studio or a video", text, rt, name)
				}
				if name == "full" && !strings.Contains(got, "development included.\n\nYou are the top-level thread") {
					t.Errorf("%q: %s's full text changed where the sentence was", text, rt)
				}
				if name == "lean" && !strings.HasSuffix(got, "/prose/contracts/archify.md`.") {
					t.Errorf("%q: %s's lean text changed where the sentence was", text, rt)
				}
			}
		}
	}
}
