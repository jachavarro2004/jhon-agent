package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
)

// ActOnAnnounceSetting turns announce-stop off (T135, L1055): on by default.
const ActOnAnnounceSetting = "agents.act_on_announce"

// quoteLimit is how many characters of the closing words a send-back quotes.
const quoteLimit = 160

var (
	fenceRe     = lazyre.New("(?s)```.*?(?:```|$)")
	paragraphRe = lazyre.New(`\n\s*\n`)
	listItemRe  = lazyre.New(`^\s*(?:[-*+•]|\d+[.)])\s+`)
	headingRe   = lazyre.New(`^\s*(?:#+|>)\s*`)
	sentenceRe  = lazyre.New(`[.!…]+["'”)\]]*\s+`)
	clauseRe    = lazyre.New(`(?i)\s*[;:,—–]\s+|\s+-\s+|\s+(?:y|e|and|then|luego|y luego|and then)\s+`)
	fillerRe    = lazyre.New(`(?i)^(?:(?:ok|okay|bien|bueno|listo|perfecto|vale|dale|entendido|hecho|done|good|great|alright|right|so|entonces|ahora|now|then|luego|ya)[,:;]?\s+)+`)

	// ownerRe is a question or a choice put to the owner: the turn waits on them.
	ownerRe = lazyre.New(`(?i)[?¿]|\bsi\s+(?:quieres|prefieres|te\s+parece|lo\s+apruebas|confirmas|me\s+dices|est[aá]s\s+de\s+acuerdo|lo\s+ves\s+bien|das\s+el\s+ok)\b|` +
		`\b(?:dime|av[ií]same|conf[ií]rmame|ind[ií]came)\b|\bcuando\s+(?:me\s+)?(?:confirmes|digas|apruebes|respondas|decidas)\b|` +
		`\bcon\s+tu\s+(?:ok|visto\s+bueno|aprobaci[oó]n|confirmaci[oó]n|permiso)\b|\btu\s+(?:decisi[oó]n|elecci[oó]n|llamada)\b|` +
		`\bopci[oó]n\s+(?:[a-d1-4]|uno|dos)\b|\bo\s+(?:prefieres|bien)\b|\bqu[eé]\s+prefieres\b|\bespero\s+tu\b|` +
		`\bif\s+you\s+(?:want|'d\s+like|would\s+like|prefer|agree|approve|confirm|say\s+so|like)\b|\blet\s+me\s+know\b|` +
		`\byour\s+(?:call|choice|decision|go-ahead|approval|confirmation|ok)\b|\b(?:once|after|when)\s+you\b|` +
		`\boption\s+(?:[a-d1-4]|one|two)\b|\b(?:should|shall)\s+i\b|\bdo\s+you\s+want\b|\bwould\s+you\s+like\b|\bwant\s+me\s+to\b|\bup\s+to\s+you\b`)

	// waitRe is a wait on a running job or sub-agent: the turn waits on it.
	waitRe = lazyre.New(`(?i)\bcuando\s+(?:termine|terminen|acabe|acaben|vuelva|vuelvan|responda|respondan|finalice|finalicen|complete|est[eé]n?\s+listos?|haya\s+terminado)\b|` +
		`\bal\s+(?:terminar|acabar|finalizar|completar|volver)\b|\ben\s+cuanto\s+(?:termine|acabe|vuelva|est[eé])\b|` +
		`\bte\s+(?:aviso|avisar[eé]|informo|informar[eé]|cuento|contar[eé])\b|\besperando\s+(?:a|al|que|el|la|los|las)\b|` +
		`\bespero\s+(?:a\s+que|que\s+termine|el\s+resultado|su\s+resultado)\b|\bmientras\s+(?:corre|termina|trabaja)\b|` +
		`\b(?:en|de)\s+(?:segundo\s+plano|background)\b|\b(?:est[aá]n?|sigue|siguen)\s+corriendo\b|` +
		`\b(?:when|once|after|as\s+soon\s+as)\s+.{0,60}?\b(?:finish(?:es|ed)?|complete[sd]?|is\s+done|are\s+done|returns?|returned|lands?|landed|reports?|comes?\s+back|ends|is\s+ready|are\s+ready)\b|` +
		`\blet\s+you\s+know\b|\breport\s+back\b|\bwait(?:ing)?\s+(?:for|on|until)\b|\bin\s+the\s+background\b|\b(?:is|are)\s+(?:still\s+)?running\b|\bstill\s+running\b`)

	// conditionRe is a sentence whose action waits on a condition.
	conditionRe = lazyre.New(`(?i)\b(?:si|if|cuando|when|once|unless|salvo\s+que|a\s+menos\s+que|en\s+cuanto|as\s+soon\s+as|despu[eé]s\s+de\s+que|after)\b`)

	// announceRe is a clause that opens with an action of the agent's own,
	// now; a `verb` group's verb is checked against stopVerbs.
	announceRe = lazyre.New(`(?i)^(?:` +
		`(?:no\s+freno\s+y\s+)?sigo(?:\s+(?:con|adelante|ahora|entonces|de\s+inmediato|en\s+seguida|enseguida|ya|directo|sin\s+parar|sin\s+frenar)\b|\W*$)|` +
		`contin[uú]o(?:\s+(?:con|ahora|entonces|adelante|ya)\b|\W*$)|` +
		`procedo(?:\s+(?:a|con|ahora|entonces)\b|\W*$)|` +
		`voy\s+a\s+(?P<verb1>\pL+)|` +
		`(?:ahora|a\s+continuaci[oó]n|enseguida|en\s+seguida|ya|luego|entonces)\s+(?:corro|lanzo|relanzo|aplico|hago|ejecuto|reviso|edito|escribo|implemento|arreglo|corrijo|actualizo|agrego|añado|mergeo|commiteo|pruebo|verifico|valido|compruebo|ajusto|preparo|genero|instalo|configuro|integro|despliego|publico|abro|leo|busco|delego|reintento|repito|cambio|elimino|borro|creo\s+(?:el|la|los|las|un|una)|paso\s+a|empiezo|arranco|retomo)\b|` +
		`(?:corro|lanzo|relanzo|aplico|ejecuto|implemento|arreglo|mergeo|commiteo|delego|reintento|retomo|arranco\s+con|empiezo\s+(?:con|a|por)|me\s+pongo\s+con)\b|` +
		`paso\s+a\s+\pL+(?:ar|er|ir)\b|` +
		`(?:i'll|ill|i\s+will|i'm\s+going\s+to|i\s+am\s+going\s+to|i'm\s+gonna)\s+now\b|` +
		`(?:next|now|then)\s*,?\s+(?:i'll|i\s+will|i'm\s+going\s+to)\s+(?P<verb2>\pL+)|` +
		`(?:i'll|i\s+will|i'm\s+going\s+to|i\s+am\s+going\s+to)\s+(?:go\s+ahead\s+and\s+)?(?:run|rerun|re-run|apply|fix|implement|commit|merge|push|continue|proceed|start|launch|write|update|add|create|edit|refactor|test|check|verify|build|open|read|look|investigate|dig|tackle|wire|move\s+on|finish|deploy|install|delegate|kick\s+off|spin\s+up|retry|patch|remove|delete|clean\s+up|rebase|regenerate|generate|draft|handle|address)\b|` +
		`let\s+me\s+now\s+(?P<verb3>\pL+)|` +
		`let\s+me\s+(?:go\s+ahead\s+and\s+)?(?:run|rerun|re-run|apply|fix|implement|commit|merge|push|continue|proceed|start|launch|write|update|add|create|edit|refactor|test|check|verify|build|open|read|look|investigate|dig|tackle|wire|finish|deploy|install|delegate|kick\s+off|retry|patch|remove|delete|clean\s+up|rebase|regenerate|generate|draft|handle|address)\b|` +
		`(?:continuing|proceeding|moving\s+on)(?:\s+(?:with|to|on)\b|\W*$)|on\s+to\s+the\b|next\s+up\b|` +
		`(?:now|next)\s*,?\s+(?:running|applying|fixing|implementing|committing|merging|writing|updating|adding|creating|editing|testing|checking|building|launching|starting)\b|` +
		`(?:i'm|i\s+am)\s+(?:now\s+)?(?:continuing|proceeding|moving\s+on)\b` +
		`)`)
)

// stopVerbs are the verbs after "voy a", "next I'll" or "let me now" that
// stop, wait, or ask instead of acting.
var stopVerbs = []string{"esperar", "dejar", "parar", "detener", "frenar", "necesitar", "quedar", "pedir", "preguntar", "consultar",
	"wait", "let", "need", "stop", "pause", "leave", "hold", "know", "ask"}

// AnnouncedAction is, for a final reply (one that made no tool call), the
// closing words that announce an action of the agent's own it did not take,
// or "" when it announces none (T135, L1055). The closing part is the last
// paragraph, outside code blocks: its last sentence, the one before when
// the last is a word or three, or the line a closing list hangs from. It
// does not fire on a closing part that asks the owner a question or offers
// a choice, waits on a running job or sub-agent, or puts the action under
// a condition, nor on a finished outcome. Missing a case is preferred to a
// false send-back.
func AnnouncedAction(reply string) string {
	text := strings.ReplaceAll(strings.ReplaceAll(reply, "’", "'"), "\r", "")
	text = fenceRe.ReplaceAllString(text, "\n\n")
	var paragraphs []string
	for _, p := range paragraphRe.Split(text, -1) {
		if strings.TrimSpace(p) != "" {
			paragraphs = append(paragraphs, p)
		}
	}
	if len(paragraphs) == 0 {
		return ""
	}
	last := lines(paragraphs[len(paragraphs)-1])
	closing := strings.Join(last, " ")
	var candidates []string
	switch {
	case allItems(last) && len(paragraphs) > 1:
		// A closing list hangs from the paragraph before when it ends with ':'.
		lead := strings.Join(lines(paragraphs[len(paragraphs)-2]), " ")
		closing = lead + " " + closing
		if strings.HasSuffix(strings.TrimSpace(plain(lead)), ":") {
			candidates = lastSentences(lead)
		}
	case len(last) > 1 && strings.HasSuffix(strings.TrimSpace(plain(last[0])), ":") && allItems(last[1:]):
		candidates = lastSentences(last[0])
	default:
		candidates = lastSentences(closing)
	}
	if ownerRe.MatchString(closing) || waitRe.MatchString(closing) {
		return ""
	}
	for i := len(candidates) - 1; i >= 0; i-- {
		s := candidates[i]
		if conditionRe.MatchString(s) {
			continue
		}
		for _, clause := range clauseRe.Split(s, -1) {
			if announces(clause) {
				return quote(s)
			}
		}
	}
	return ""
}

// lines are a paragraph's non-empty lines, headings' and quotes' marks off.
func lines(paragraph string) []string {
	var out []string
	for _, l := range strings.Split(paragraph, "\n") {
		if l = headingRe.ReplaceAllString(l, ""); strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// allItems is whether every line is a list item.
func allItems(ls []string) bool {
	for _, l := range ls {
		if !listItemRe.MatchString(l) {
			return false
		}
	}
	return len(ls) > 0
}

// plain is text with its emphasis marks and list markers off.
func plain(s string) string {
	s = listItemRe.ReplaceAllString(s, "")
	return strings.TrimSpace(strings.NewReplacer("**", "", "__", "").Replace(s))
}

// lastSentences are the last sentence of text, and the one before it when
// the last has three words or fewer.
func lastSentences(text string) []string {
	text = plain(text)
	var sentences []string
	start := 0
	for _, m := range sentenceRe.FindAllStringIndex(text, -1) {
		sentences = append(sentences, strings.TrimSpace(text[start:m[1]]))
		start = m[1]
	}
	if rest := strings.TrimSpace(text[start:]); rest != "" {
		sentences = append(sentences, rest)
	}
	switch n := len(sentences); {
	case n == 0:
		return nil
	case n > 1 && len(strings.Fields(sentences[n-1])) <= 3:
		return sentences[n-2:]
	default:
		return sentences[n-1:]
	}
}

// announces is whether a clause opens with an action of the agent's own.
func announces(clause string) bool {
	clause = strings.TrimLeftFunc(clause, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	m := announceRe.FindStringSubmatch(clause)
	if m == nil {
		// "ahora corro" is matched whole; "ok, ahora, sigo" past its fillers.
		m = announceRe.FindStringSubmatch(fillerRe.ReplaceAllString(clause, ""))
	}
	if m == nil {
		return false
	}
	for i, name := range announceRe.SubexpNames() {
		if !strings.HasPrefix(name, "verb") || m[i] == "" {
			continue
		}
		verb := strings.ToLower(m[i])
		for _, stop := range stopVerbs {
			if strings.HasPrefix(verb, stop) {
				return false
			}
		}
	}
	return true
}

// quote is a sentence cut to quoteLimit characters.
func quote(s string) string {
	if r := []rune(s); len(r) > quoteLimit {
		return strings.TrimSpace(string(r[:quoteLimit])) + "…"
	}
	return s
}

// SendBackReason is what an announce-and-stop turn is sent back with, in
// both runtimes: the quoted closing words, act now or say why not.
func SendBackReason(closing string) string {
	return fmt.Sprintf("Jhon: you announced %q and ended the turn without doing it. Do it now, or say in one line why not.", closing)
}

// EmptyReplyReason is what a turn that ended with an empty reply (no text,
// no tool call) is sent back with, in both runtimes (KA2, L1063).
const EmptyReplyReason = "Jhon: your turn ended with an empty reply. Continue the work, or say in one line why you stopped."

// AnnounceStop is the main session's Stop hook (announce-stop, T135): a
// final reply that announces an action of the agent's own and ends the
// turn is sent back once, {"decision":"block","reason":SendBackReason},
// and an empty one (no text, no tool call) with EmptyReplyReason. The reply
// is the payload's last_assistant_message, or, when the payload has none,
// the last reply in its transcript_path (L1063). Never after a send-back in
// the same owner turn (stop_hook_active), never in a sub-agent (agent_id,
// or SubagentStop), never with agents.act_on_announce off or settings that
// cannot be read. pi's shell calls it with the same payload
// (shell/lib/announce-stop.ts). Advisory: it always exits 0, and a failure
// (a reply that cannot be read) sends nothing back.
func AnnounceStop(_ []string, io IO) int {
	p, ok := read(io.In)
	if !ok || p.Str("hook_event_name") != "Stop" || p["stop_hook_active"] == true || strings.TrimSpace(p.Str("agent_id")) != "" {
		return 0
	}
	reply, ok := p["last_assistant_message"].(string)
	if !ok {
		if reply, ok = LastReply(p.Str("transcript_path")); !ok {
			return 0
		}
	}
	reason := EmptyReplyReason
	if strings.TrimSpace(reply) != "" {
		closing := AnnouncedAction(reply)
		if closing == "" {
			return 0
		}
		reason = SendBackReason(closing)
	}
	if !settings.On(ActOnAnnounceSetting) {
		return 0
	}
	fmt.Fprint(io.Out, marshal(map[string]string{"decision": "block", "reason": reason}))
	return 0
}

// transcriptTail is how much of a transcript's end LastReply reads.
const transcriptTail = 1 << 20

// block is a transcript message's content block.
type block struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// LastReply is the text of a Claude Code transcript's last assistant reply:
// the text blocks of the assistant entries after the last user entry (a
// prompt or a tool result), joined by newlines, "" when they hold none (a
// reply of only thinking). ok is false when the transcript cannot be read,
// a line in that reply is not JSON, there is no such reply, or it made a
// tool call or is an API error. Only the file's last transcriptTail bytes
// are read; sub-agent (sidechain) entries and other entry types are passed.
func LastReply(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	off := max(info.Size()-transcriptTail, 0)
	data := make([]byte, info.Size()-off)
	if n, err := f.ReadAt(data, off); err != nil && !(errors.Is(err, io.EOF) && n == len(data)) {
		return "", false
	}
	if off > 0 {
		// The tail starts inside a line: drop it.
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return "", false
		}
		data = data[i+1:]
	}
	var texts []string
	found := false
	lines := bytes.Split(data, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var e struct {
			Type        string `json:"type"`
			IsSidechain bool   `json:"isSidechain"`
			APIError    bool   `json:"isApiErrorMessage"`
			Message     struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &e) != nil {
			return "", false
		}
		if e.IsSidechain {
			continue
		}
		if e.Type == "user" {
			break
		}
		if e.Type != "assistant" {
			continue
		}
		if e.APIError {
			return "", false
		}
		found = true
		var blocks []block
		var s string
		switch {
		case len(e.Message.Content) == 0 || string(e.Message.Content) == "null":
		case json.Unmarshal(e.Message.Content, &s) == nil:
			blocks = []block{{"text", s}}
		case json.Unmarshal(e.Message.Content, &blocks) != nil:
			return "", false
		}
		var entry []string
		for _, b := range blocks {
			switch b.Type {
			case "tool_use":
				return "", false
			case "text":
				entry = append(entry, b.Text)
			}
		}
		texts = append(entry, texts...)
	}
	if !found {
		return "", false
	}
	return strings.Join(texts, "\n"), true
}
