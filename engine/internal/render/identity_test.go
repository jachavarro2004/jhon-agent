package render

import (
	"path/filepath"
	"strings"
	"testing"
)

// T226: the owner asked "Hay algo que no me gusta de jhon... no sabe el
// mismo lo que es, siempre busca en memorias de claude o engram pero no
// sabe en el contexto que es o quien es?". The always-on text of both
// runtimes, lean and full, says what Jhon is, how it works, where its
// release and docs are installed, and that questions about Jhon are
// answered from that text, `jhon help` and the installed docs, never from
// memory searches.

// jhonRuntimes is where runtime's text says Jhon runs: pi's names no other
// runtime (L264).
var jhonRuntimes = map[string]string{"claude-code": "Claude Code or pi", "pi": "pi or another agent runtime"}

// identityLines are the lines that say what Jhon is (S1) and where its
// answers come from (S2) in runtime's text, with the install's real path
// for e.
func identityLines(e *engine, runtime string) []string {
	return []string{
		"Jhon is an open-source agent framework (github.com/jachavarro2004/jhon-agent) that runs in " + jhonRuntimes[runtime] + ".",
		"LDD records (tasks.md, RED before GREEN through `jhon executions`, delivery checks)",
		"guards and the OS sandbox",
		"the agents jhon-scout, jhon-builder and jhon-checker",
		"the `jhon` CLI (status, executions, deliver, audit, doctor, help)",
		"Its README and prose are installed at `" + e.current() + "`.",
		"Answer what Jhon is and how to use it from this text, `jhon <command> --help` and those files, never from memory searches; memory keeps the owner's projects and decisions, Jhon's own development included.",
	}
}

// S1, S2, S4: both runtimes' lean and full texts hold the identity lines
// and the "not from memory" rule, the full text's persona included.
func TestAlwaysOnTextSaysWhatJhonIs(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	persona := "You are the main agent of Jhon, an open-source agent framework; what Jhon is and how to use it come from `jhon <command> --help` and the README at `" + e.current() + "`, never from memory searches."
	for _, rt := range []string{"claude-code", "pi"} {
		for mode, text := range map[string]string{"lean": e.leanText(rt), "full": e.fullText(rt)} {
			flat := flatText(text)
			want := identityLines(e, rt)
			if mode == "full" {
				want = append(want, persona)
			}
			for _, w := range want {
				if n := strings.Count(flat, w); n != 1 {
					t.Errorf("%s's %s text holds %q %d times, want once", rt, mode, w, n)
				}
			}
		}
	}
}

// T226 verify: the identity and the persona's "main agent" sentence reach only
// the main agent; every sub-agent definition of both runtimes carries neither.
func TestSubagentsCarryNoMainAgentIdentity(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	main := e.mainAgent(piMain).Name
	for _, f := range append(claude{e}.Files(), pi{e}.Files()...) {
		if !strings.Contains(f.Path, string(filepath.Separator)+"agents"+string(filepath.Separator)) || strings.HasSuffix(f.Path, main+".md") {
			continue
		}
		flat := flatText(f.Content)
		for _, w := range []string{"You are the main agent of Jhon", "Answer what Jhon is and how to use it"} {
			if strings.Contains(flat, w) {
				t.Errorf("%s carries %q", f.Path, w)
			}
		}
	}
}
