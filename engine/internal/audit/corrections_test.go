package audit

import (
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// How often the owner had to correct the agent (T76b, L1074 K1-K4): owner
// messages counted by kind, in Spanish (accents optional) and English, the
// same on a Claude Code transcript and a pi session; a statistic, never a
// finding.

// noticed is a message the runtime or an extension wrote in the owner's place.
func noticed(text string) said { return said{kind: "notice", text: text} }

// corrected is a session with each kind of correction, none an answer to a question.
var corrected = []said{
	owner("arranca con el cambio"), // the first message: nothing
	agent("Listo el paso 1."),
	owner("Sigue, ¿por qué paraste?"), // keep-going
	agent("Hecho."),
	owner("Ya te dije que no."), // repeat
	agent("Agregué una tabla extra."),
	owner("Eso no. No te pedí eso: sobreingeniería."), // unrequested, once
	agent("Elegí la base grande."),
	owner("No decidas tú: pregúntame."), // decision
	agent("Ok."),
	owner("I told you: keep going, and ask me before deciding"), // repeat, keep-going, decision
	agent("Done."),
	owner("I didn't ask for that. Don't stop now."), // unrequested, keep-going
	agent("Fine."),
	owner("CONTINUA, no frenes"), // keep-going, no accent
	agent("Hecho todo."),
	owner("gracias, por eso no funcionaba; lo consigue y persigue el error"), // nothing
	agent("De nada."),
}

func TestCorrectionsAreCountedByKind(t *testing.T) {
	onBoth(t, corrected, func(t *testing.T, r *Result) {
		eq(t, "corrections", r.Dialogue.Corrections, Corrections{OwnerMessages: 9, Correcting: 7, KeepGoing: 4, Repeat: 2, Unrequested: 2, Decision: 2})
	})
}

// K2: a message that answers a question is never keep-going, whether the
// question was asked in plain text or through the ask tool; its other kinds
// still count.
func TestCorrectionsNeverCountAnAnswerAsKeepGoing(t *testing.T) {
	for _, c := range []struct {
		what string
		d    []said
		want Corrections
	}{
		{"a plain question in Spanish", []said{owner("go"), agent("Terminé el 2. ¿Sigo con el cambio 3?"), owner("Sí, sigue con el cambio 3")},
			Corrections{OwnerMessages: 2}},
		{"closing markdown after the mark", []said{owner("go"), agent("**¿Continúo?**"), owner("continúa, no pares")}, Corrections{OwnerMessages: 2}},
		{"a plain question in English", []said{owner("go"), agent("Shall I go on with step 2?"), owner("yes, keep going, don't stop")}, Corrections{OwnerMessages: 2}},
		{"an ask-tool call", []said{owner("go"), {kind: "ask", questions: []string{"¿Cuál opción?"}, failed: true}, owner("Opción 1 y sigue")}, Corrections{OwnerMessages: 2}},
		{"an answer that also corrects", []said{owner("go"), agent("¿Agrego la tabla?"), owner("No, te dije que no; sigue sin ella")},
			Corrections{OwnerMessages: 2, Correcting: 1, Repeat: 1}},
		{"a question followed by more text did not end the reply", []said{owner("go"), agent("¿Sigo? Te espero."), owner("sí, sigue")},
			Corrections{OwnerMessages: 2, Correcting: 1, KeepGoing: 1}},
		{"a reply with no question", []said{owner("go"), agent("Paso 2 listo."), owner("sigue")}, Corrections{OwnerMessages: 2, Correcting: 1, KeepGoing: 1}},
		{"the reply after the question", []said{owner("go"), agent("¿Sigo?"), owner("sí"), agent("Hecho el paso 1."), owner("sigue")},
			Corrections{OwnerMessages: 3, Correcting: 1, KeepGoing: 1}},
	} {
		onBoth(t, c.d, func(t *testing.T, r *Result) {
			eq(t, c.what, r.Dialogue.Corrections, c.want)
		})
	}
}

// K4: a notification or a message the system generated is not the owner's,
// whatever its words.
func TestCorrectionsLeaveNotificationsOut(t *testing.T) {
	onBoth(t, []said{owner("go"), agent("Lanzo el CI."), noticed("sigue, no pares, te dije, eso no, pregúntame"), agent("CI listo.")}, func(t *testing.T, r *Result) {
		eq(t, "corrections", r.Dialogue.Corrections, Corrections{OwnerMessages: 1})
	})
}

// K1: the report states the corrections as statistics, in the dialogue's
// JSON and a third dialogue line; no finding, no status change, no text.
func TestCorrectionsAreReportedAsStatisticsNeverAFinding(t *testing.T) {
	onBoth(t, corrected, func(t *testing.T, r *Result) {
		got := js.StringifyValueCompact(js.Prop(js.Prop(r.Report(), "dialogue").(*js.Object), "corrections"))
		eq(t, "corrections", got, `{"owner_messages":9,"correcting":7,"keep_going":4,"repeat":2,"unrequested":2,"decision":2}`)
		out := Human(r, "s.jsonl", false)
		line := "\n  dialogue  corrections 7 of 9 owner messages  keep-going 4  repeat 2  unrequested 2  decision 2\n"
		if !strings.Contains(out, line) {
			t.Errorf("missing %q in\n%s", line, out)
		}
		eq(t, "status", r.Status, "nothing-to-audit")
		eq(t, "findings", len(r.Findings), 0)
		for _, text := range []string{"sobreingenier", "pregúntame", "I told you", "CONTINUA"} {
			if strings.Contains(out, text) || strings.Contains(js.StringifyValue(r.Report()), text) {
				t.Errorf("the report holds the session's text %q", text)
			}
		}
	})
}

// K3: jhon audit --all sums the corrections, Jhon's sessions apart.
func TestAggregateSumsTheCorrections(t *testing.T) {
	first := auditOf(t, claudeSteps(corrected)...)
	second := piAudit(t, piSteps([]said{owner("go"), agent("Hecho."), owner("te repito: no decidas")})...)
	f := Aggregate([]Audited{{Path: "a.jsonl", Result: first}, {Path: "b.jsonl", Jhon: true, Result: second}}, nil, time.Time{})
	eq(t, "all", f.Dialogue.Corrections, Corrections{OwnerMessages: 11, Correcting: 8, KeepGoing: 4, Repeat: 3, Unrequested: 2, Decision: 3})
	eq(t, "Jhon's", f.JhonDialogue.Corrections, Corrections{OwnerMessages: 2, Correcting: 1, Repeat: 1, Decision: 1})
	got := js.StringifyValueCompact(js.Prop(f.JhonDialogue.Value(), "corrections"))
	eq(t, "Jhon's JSON", got, `{"owner_messages":2,"correcting":1,"keep_going":0,"repeat":1,"unrequested":0,"decision":1}`)
}
