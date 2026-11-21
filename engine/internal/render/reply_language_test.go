package render

import (
	"path/filepath"
	"strings"
	"testing"
)

// replyLanguageRule is T194's rule (L1608, L1613): the owner is answered in
// the language of the owner's latest own message, questions and their
// options included; what is not the owner never changes it, a relayed
// result is translated, and the English artifacts are not the reply
// language.
var replyLanguageRule = []string{
	"Answer the owner in the language of the owner's latest own natural-language message",
	"A slash command, a short confirmation (\"ok\", \"sí\") or pasted text does not change it.",
	"Error text, refusals and quoted output stay literal when relayed; translate only the explanation.",
	"Everything addressed to the owner is in that language: replies, progress lines, todo text, and each question with its header, options, descriptions and recommended tag.",
	"Harness notes, background notices, sub-agent results, tool output, skills and summaries are not the owner and never change it.",
	"Translate a sub-agent's result when you relay it.",
	"that is not the reply language.",
}

// T194: the rule is in the always-on layer of both runtimes, lean and full,
// and in the files each runtime's main session is rendered from: Claude
// Code's main agent definition and pi's jhon-lean.md (the lean text); the
// full text is the full file both read.
func TestReplyLanguageRuleIsAlwaysOn(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	texts := map[string]string{}
	for _, rt := range []string{"claude-code", "pi"} {
		texts[rt+" lean"] = e.leanText(rt)
		texts[rt+" full"] = e.fullText(rt)
	}
	texts["claude-code "+piMain+".md"] = promptOf(t, claude{e}, filepath.Join(e.claudeAgents(), piMain+".md"))
	texts["pi jhon-lean.md"] = promptOf(t, pi{e}, pi{e}.piLeanFile())
	for name, text := range texts {
		flat := flatText(text)
		for _, want := range replyLanguageRule {
			if !strings.Contains(flat, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
	}
}

// T194: the vague line the rule replaces is gone from the lean text.
func TestLeanTextDropsTheVagueLanguageLine(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	for _, rt := range []string{"claude-code", "pi"} {
		if strings.Contains(flatText(e.leanText(rt)), "Reply in the owner's language") {
			t.Errorf("%s's lean text keeps \"Reply in the owner's language\"", rt)
		}
	}
}
