package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T135 K1 (L1056): a final reply whose closing part announces an action of
// the agent's own fires; one that ends with a question or an offered
// choice, waits on a named running job or sub-agent, or reports a finished
// outcome does not. Every text is synthetic.
func TestAnnouncedActionFires(t *testing.T) {
	for _, c := range []struct{ reply, quoted string }{
		{"Listo el slice 2. Sigo con el slice 3.", "Sigo con el slice 3."},
		{"El slice 2 quedó en verde.\n\nNo freno y sigo.", "No freno y sigo."},
		{"Sigo.", "Sigo."},
		{"Entendido, sigo.", "Entendido, sigo."},
		{"Continúo con la migración del módulo de pagos.", "Continúo con la migración del módulo de pagos."},
		{"Procedo a correr la suite completa.", "Procedo a correr la suite completa."},
		{"Voy a aplicar el cambio en `parser.go`.", "Voy a aplicar el cambio en `parser.go`."},
		{"Ahora corro los tests del paquete.", "Ahora corro los tests del paquete."},
		{"Ahora lanzo el worker para el slice 4.", "Ahora lanzo el worker para el slice 4."},
		{"Ahora aplico el parche.", "Ahora aplico el parche."},
		{"Ahora hago el commit del slice.", "Ahora hago el commit del slice."},
		{"Tests en verde; ahora corro la suite completa.", "Tests en verde; ahora corro la suite completa."},
		{"Corrí el linter y voy a corregir los dos avisos.", "Corrí el linter y voy a corregir los dos avisos."},
		{"✅ Paso 2 hecho.\n\n**Sigo con el paso 3**", "Sigo con el paso 3"},
		{"Ahora corro:\n\n```\ngo test ./...\n```", "Ahora corro:"},
		{"Sigo con:\n- el slice 3\n- el slice 4", "Sigo con:"},
		{"I'll now run the tests.", "I'll now run the tests."},
		{"The parser is fixed. Next I'll update the docs.", "Next I'll update the docs."},
		{"Next, I'll wire the handler into the router.", "Next, I'll wire the handler into the router."},
		{"Continuing with step 4.", "Continuing with step 4."},
		{"Let me now run the full suite.", "Let me now run the full suite."},
		{"Let me run the linter.", "Let me run the linter."},
		{"Done with the schema. I'll proceed with the migration.", "I'll proceed with the migration."},
		{"Moving on to the second endpoint.", "Moving on to the second endpoint."},
		{"Sigo con el slice 3. Vamos.", "Sigo con el slice 3."},
	} {
		if got := AnnouncedAction(c.reply); got != c.quoted {
			t.Errorf("AnnouncedAction(%q) = %q; want %q", c.reply, got, c.quoted)
		}
	}
}

func TestAnnouncedActionDoesNotFire(t *testing.T) {
	for _, reply := range []string{
		// A question to the owner, or a choice offered.
		"Sigo con el slice 3?",
		"Puedo seguir con el slice 3. ¿Lo hago?",
		"Next I'll update the docs. Should I also bump the version?",
		"Sigo con el slice 3 si te parece.",
		"Tengo dos caminos: opción A, reescribir el parser; opción B, parchearlo. Dime cuál.",
		"I can run the migration now, or leave it for tomorrow. Let me know.",
		"I'll proceed once you confirm the schema.",
		// A wait on a named running job or sub-agent.
		"El job-4 está corriendo; cuando termine te aviso.",
		"Al terminar el job-4 mergeo.",
		"When job-4 finishes I'll merge the branch.",
		"The explore agent is still running. I'll let you know when it finishes.",
		"Lancé el sub-agente de revisión; sigo cuando vuelva.",
		"Esperando al worker del slice 3.",
		// A finished outcome, no own pending action.
		"Listo: los tres slices están en verde y commiteados.",
		"Done. All 42 tests pass.",
		"I ran the tests; all green.",
		"Seguí con el slice 3 y quedó en verde.",
		"Procedí a correr la suite: 120 pasan.",
		"Sigo pensando que la opción simple es la correcta.",
		"Sigo sin acceso a la base de datos, así que paro aquí.",
		"Termino aquí.",
		"No sigo: falta tu decisión sobre el esquema.",
		"Voy a dejarlo así por hoy.",
		"I'll wait for the review.",
		// A condition, not an action now.
		"Si falla, sigo con el plan B.",
		"If the build breaks, I'll revert it.",
		// An announce earlier, then a report closing the reply.
		"Voy a correr la suite.\n\nResultado: 120 pasan, 0 fallan.",
		"",
		"   \n\n",
	} {
		if got := AnnouncedAction(reply); got != "" {
			t.Errorf("AnnouncedAction(%q) = %q; want no firing", reply, got)
		}
	}
}

// The quoted closing words are cut to a short line.
func TestAnnouncedActionQuotesAShortLine(t *testing.T) {
	long := "Sigo con " + strings.Repeat("la parte siguiente del plan ", 20) + "."
	got := AnnouncedAction(long)
	if got == "" || len([]rune(got)) > 161 || !strings.HasPrefix(got, "Sigo con la parte") || !strings.HasSuffix(got, "…") {
		t.Errorf("long announce quoted as %q", got)
	}
}

// announceStop runs the announce-stop hook with a settings file holding text.
func announceStop(t *testing.T, settingsText string, payload map[string]any) (int, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(path, []byte(settingsText), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JHON_SETTINGS", path)
	data, _ := json.Marshal(payload)
	var out, errOut bytes.Buffer
	code := Handlers["announce-stop"](nil, IO{bytes.NewReader(data), &out, &errOut})
	return code, out.String(), errOut.String()
}

func stopEvent(kv ...any) map[string]any {
	p := map[string]any{"hook_event_name": "Stop", "stop_hook_active": false, "session_id": "s1",
		"last_assistant_message": "Slice 2 en verde. Sigo con el slice 3."}
	for i := 0; i+1 < len(kv); i += 2 {
		p[kv[i].(string)] = kv[i+1]
	}
	return p
}

// K3: the Stop hook answers decision block with a reason that quotes the
// closing words and asks the agent to act or say why not in one line.
func TestAnnounceStopSendsTheTurnBack(t *testing.T) {
	code, out, errOut := announceStop(t, "schema = 1\n", stopEvent())
	if code != 0 || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	var j struct{ Decision, Reason string }
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		t.Fatalf("stdout %q: %v", out, err)
	}
	want := `Jhon: you announced "Sigo con el slice 3." and ended the turn without doing it. Do it now, or say in one line why not.`
	if j.Decision != "block" || j.Reason != want {
		t.Errorf("got %+v; want block with %q", j, want)
	}
	if SendBackReason("Sigo con el slice 3.") != want {
		t.Errorf("SendBackReason = %q", SendBackReason("Sigo con el slice 3."))
	}
}

// K2, K4, K5: the turn ends normally after one send-back (stop_hook_active),
// in a sub-agent, on another event, with the setting off, with no
// announcement, and when the payload or the settings cannot be read.
func TestAnnounceStopLetsTheTurnEnd(t *testing.T) {
	for _, c := range []struct {
		why, settings string
		payload       map[string]any
	}{
		{"already sent back once this owner turn", "schema = 1\n", stopEvent("stop_hook_active", true)},
		{"a sub-agent", "schema = 1\n", stopEvent("agent_id", "a1")},
		{"a sub-agent's stop", "schema = 1\n", stopEvent("hook_event_name", "SubagentStop")},
		{"the setting off", "schema = 1\n\n[agents]\nact_on_announce = false\n", stopEvent()},
		{"no announcement", "schema = 1\n", stopEvent("last_assistant_message", "Done. All tests pass.")},
		{"no reply", "schema = 1\n", stopEvent("last_assistant_message", nil)},
		{"unreadable settings", "schema = 1\n[agents]\nact_on_announce = \"maybe\"\n", stopEvent()},
	} {
		code, out, errOut := announceStop(t, c.settings, c.payload)
		if code != 0 || out != "" || errOut != "" {
			t.Errorf("%s: code %d, stdout %q, stderr %q; want 0 and nothing", c.why, code, out, errOut)
		}
	}
	var out bytes.Buffer
	if code := AnnounceStop(nil, IO{strings.NewReader("{ nope"), &out, &out}); code != 0 || out.Len() != 0 {
		t.Errorf("not JSON: code %d, output %q", code, out.String())
	}
}

// transcriptLine is one synthetic transcript entry: an assistant message
// with the given content blocks, or a user entry (a prompt or a tool result).
func transcriptLine(t *testing.T, kind, id string, blocks ...map[string]any) string {
	t.Helper()
	entry := map[string]any{"type": kind, "message": map[string]any{"role": kind, "content": blocks}}
	if id != "" {
		entry["message"].(map[string]any)["id"] = id
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func textBlock(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

var (
	thinkingBlock = map[string]any{"type": "thinking", "thinking": "plan", "signature": "x"}
	toolUseBlock  = map[string]any{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"file_path": "a.txt"}}
	toolResult    = map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "ok"}
)

// writeTranscript writes lines as a JSONL transcript and returns its path.
func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// transcriptEvent is a Stop payload without last_assistant_message, the
// reply only in the transcript at path.
func transcriptEvent(path string) map[string]any {
	p := stopEvent("transcript_path", path)
	delete(p, "last_assistant_message")
	return p
}

// sentBack is the reason a hook output blocks with, or "" with no block.
func sentBack(t *testing.T, out string) string {
	t.Helper()
	if out == "" {
		return ""
	}
	var j struct{ Decision, Reason string }
	if err := json.Unmarshal([]byte(out), &j); err != nil || j.Decision != "block" {
		t.Fatalf("stdout %q: want a block", out)
	}
	return j.Reason
}

const emptyReason = "Jhon: your turn ended with an empty reply. Continue the work, or say in one line why you stopped."

// KA1 (L1063): a Stop payload without last_assistant_message is judged on
// the last reply read from transcript_path: the text blocks of the
// assistant entries after the last user or tool-result entry.
func TestAnnounceStopReadsTheTranscript(t *testing.T) {
	announced := `Jhon: you announced "Sigo con la lectura de a.txt." and ended the turn without doing it. Do it now, or say in one line why not.`
	for _, c := range []struct {
		why   string
		lines []string
		want  string
	}{
		{"a reply in one entry", []string{
			transcriptLine(t, "user", "", textBlock("lee a.txt")),
			transcriptLine(t, "assistant", "m1", textBlock("Sigo con la lectura de a.txt.")),
		}, announced},
		{"a reply split over entries of one message, after a tool result", []string{
			transcriptLine(t, "user", "", textBlock("lee a.txt")),
			transcriptLine(t, "assistant", "m0", toolUseBlock),
			transcriptLine(t, "user", "", toolResult),
			transcriptLine(t, "assistant", "m1", thinkingBlock),
			transcriptLine(t, "assistant", "m1", textBlock("Leí la mitad.")),
			transcriptLine(t, "assistant", "m1", textBlock("Sigo con la lectura de a.txt.")),
			`{"type":"system","subtype":"turn_duration","durationMs":12}`,
		}, announced},
		{"an earlier turn's announcement is not this turn's", []string{
			transcriptLine(t, "assistant", "m0", textBlock("Sigo con la lectura de a.txt.")),
			transcriptLine(t, "user", "", textBlock("gracias")),
			transcriptLine(t, "assistant", "m1", textBlock("Done. All tests pass.")),
		}, ""},
		{"a trailing tool call is not a final reply", []string{
			transcriptLine(t, "user", "", textBlock("lee a.txt")),
			transcriptLine(t, "assistant", "m1", textBlock("Sigo con la lectura de a.txt."), toolUseBlock),
		}, ""},
		{"a finished outcome", []string{
			transcriptLine(t, "user", "", textBlock("corre")),
			transcriptLine(t, "assistant", "m1", textBlock("Done. All tests pass.")),
		}, ""},
	} {
		code, out, errOut := announceStop(t, "schema = 1\n", transcriptEvent(writeTranscript(t, c.lines...)))
		if code != 0 || errOut != "" {
			t.Fatalf("%s: code %d, stderr %q", c.why, code, errOut)
		}
		if got := sentBack(t, out); got != c.want {
			t.Errorf("%s: sent back %q; want %q", c.why, got, c.want)
		}
	}

	// The payload's own field wins over the transcript.
	p := transcriptEvent(writeTranscript(t, transcriptLine(t, "assistant", "m1", textBlock("Sigo con la lectura de a.txt."))))
	p["last_assistant_message"] = "Done. All tests pass."
	if _, out, _ := announceStop(t, "schema = 1\n", p); out != "" {
		t.Errorf("last_assistant_message present: stdout %q; want nothing", out)
	}

	// Only the tail is read: a long session's reply is still found.
	long := []string{transcriptLine(t, "user", "", textBlock(strings.Repeat("x", 3<<20)))}
	long = append(long, transcriptLine(t, "user", "", textBlock("sigue")), transcriptLine(t, "assistant", "m1", textBlock("Sigo con la lectura de a.txt.")))
	if _, out, _ := announceStop(t, "schema = 1\n", transcriptEvent(writeTranscript(t, long...))); sentBack(t, out) != announced {
		t.Errorf("long transcript: stdout %q; want the send-back", out)
	}
}

// KA1: an absent, unreadable or malformed transcript fails open.
func TestAnnounceStopTranscriptFailsOpen(t *testing.T) {
	dir := t.TempDir()
	reply := transcriptLine(t, "assistant", "m1", textBlock("Sigo con la lectura de a.txt."))
	for _, c := range []struct {
		why     string
		payload map[string]any
	}{
		{"no transcript_path", transcriptEvent("")},
		{"a missing file", transcriptEvent(filepath.Join(dir, "missing.jsonl"))},
		{"a directory", transcriptEvent(dir)},
		{"a malformed last line", transcriptEvent(writeTranscript(t, reply, `{"type":"assistant","mess`))},
		{"not JSONL", transcriptEvent(writeTranscript(t, "hello", "world"))},
		{"an empty file", transcriptEvent(writeTranscript(t))},
		{"no reply after the owner's message", transcriptEvent(writeTranscript(t, reply, transcriptLine(t, "user", "", textBlock("sigue"))))},
		{"an API error entry", transcriptEvent(writeTranscript(t, transcriptLine(t, "user", "", textBlock("sigue")),
			`{"type":"assistant","isApiErrorMessage":true,"message":{"role":"assistant","content":[{"type":"text","text":""}]}}`))},
		{"a single line over the tail", transcriptEvent(writeTranscript(t, transcriptLine(t, "assistant", "m1", textBlock("Sigo con la lectura de a.txt. "+strings.Repeat("y", 2<<20)))))},
	} {
		code, out, errOut := announceStop(t, "schema = 1\n", c.payload)
		if code != 0 || out != "" || errOut != "" {
			t.Errorf("%s: code %d, stdout %q, stderr %q; want 0 and nothing", c.why, code, out, errOut)
		}
	}
}

// KA2 (L1063): an empty final reply (no text, or only thinking, and no tool
// call) is sent back once with its own reason, from the payload or the
// transcript; KA3: never with stop_hook_active, an agent_id, SubagentStop
// or the setting off.
func TestAnnounceStopSendsAnEmptyReplyBack(t *testing.T) {
	emptyTranscript := writeTranscript(t, transcriptLine(t, "user", "", textBlock("sigue")),
		transcriptLine(t, "assistant", "m1", thinkingBlock), transcriptLine(t, "assistant", "m1", textBlock("  \n")))
	for _, c := range []struct {
		why     string
		payload map[string]any
	}{
		{"an empty last_assistant_message", stopEvent("last_assistant_message", "")},
		{"a blank last_assistant_message", stopEvent("last_assistant_message", " \n\t")},
		{"a transcript reply with only thinking", transcriptEvent(writeTranscript(t, transcriptLine(t, "user", "", toolResult), transcriptLine(t, "assistant", "m1", thinkingBlock)))},
		{"a transcript reply with no blocks", transcriptEvent(writeTranscript(t, transcriptLine(t, "user", "", textBlock("sigue")), transcriptLine(t, "assistant", "m1")))},
		{"a transcript reply with blank text", transcriptEvent(emptyTranscript)},
	} {
		code, out, errOut := announceStop(t, "schema = 1\n", c.payload)
		if code != 0 || errOut != "" {
			t.Fatalf("%s: code %d, stderr %q", c.why, code, errOut)
		}
		if got := sentBack(t, out); got != emptyReason {
			t.Errorf("%s: sent back %q; want %q", c.why, got, emptyReason)
		}
	}
	if EmptyReplyReason != emptyReason {
		t.Errorf("EmptyReplyReason = %q", EmptyReplyReason)
	}
	for _, c := range []struct {
		why, settings string
		payload       map[string]any
	}{
		{"already sent back once", "schema = 1\n", stopEvent("last_assistant_message", "", "stop_hook_active", true)},
		{"a sub-agent", "schema = 1\n", stopEvent("last_assistant_message", "", "agent_id", "a1")},
		{"a sub-agent's stop", "schema = 1\n", stopEvent("last_assistant_message", "", "hook_event_name", "SubagentStop")},
		{"the setting off", "schema = 1\n\n[agents]\nact_on_announce = false\n", stopEvent("last_assistant_message", "")},
		{"the setting off, from the transcript", "schema = 1\n\n[agents]\nact_on_announce = false\n", transcriptEvent(emptyTranscript)},
		{"a trailing tool call with no text", "schema = 1\n", transcriptEvent(writeTranscript(t, transcriptLine(t, "user", "", textBlock("sigue")), transcriptLine(t, "assistant", "m1", toolUseBlock)))},
	} {
		code, out, errOut := announceStop(t, c.settings, c.payload)
		if code != 0 || out != "" || errOut != "" {
			t.Errorf("%s: code %d, stdout %q, stderr %q; want 0 and nothing", c.why, code, out, errOut)
		}
	}
}
