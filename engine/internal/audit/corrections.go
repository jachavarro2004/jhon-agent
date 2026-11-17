package audit

import (
	"strings"
	"unicode"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// How often the owner had to correct the agent, in counts (T76b, L579,
// L1074): the owner's messages whose words correct it, by kind, in Spanish
// (accents optional) and English. A statistic like the rest of the
// dialogue: never a finding, and no message text ever leaves it. The words
// are a heuristic: a correction in other words is not counted, and a phrase
// may count where it corrects nothing.

// Corrections are the corrections of one main thread, or of many summed.
//
// OwnerMessages are the messages the owner wrote (IsGenuineUserMessage):
// notifications, messages the system generated, and answers through the ask
// tool are none. Correcting are those of them with at least one kind; a
// message counts once in each of its kinds:
//
//   - KeepGoing: "sigue", "continúa", "no pares", "no frenes", "no te
//     detengas", "por qué paraste" (or frenaste, te detuviste), "keep
//     going", "carry on", "don't stop", "why did you stop"; never in a
//     message that answers a question: one written when the main thread's
//     last response ended its text in a question mark or called the ask
//     tool (L1062: most "sigue" answer "¿sigo?").
//   - Repeat: "te dije" (and "ya te dije", "como te dije"), "te lo dije",
//     "otra vez", "te repito", "I told you", "I already told you", "as I
//     said", "like I said", "I repeat".
//   - Unrequested: "no te pedí", "nadie te pidió", "eso no" at the start of
//     a sentence or clause ("por eso no" is not one), "sobreingeniería",
//     "no hagas eso", "I didn't ask", "nobody asked", "no one asked",
//     "overengineering", "don't do that".
//   - Decision: "no decidas", "pregúntame", "esa decisión es mía", "la
//     decisión es mía", "don't decide", "ask me", "that's my decision",
//     "that's my call", "that decision is mine".
type Corrections struct {
	OwnerMessages, Correcting                int
	KeepGoing, Repeat, Unrequested, Decision int
}

// The kinds' phrases, matched against a message lower-cased with its
// accents and curly quotes undone (folded).
var (
	keepGoing   = lazyre.New(`\b(sigue|continua|no pares|no frenes|no te detengas|por ?que (paraste|frenaste|te detuviste)|keep going|carry on|don'?t stop|do not stop|why did you stop)\b`)
	repeated    = lazyre.New(`\b(te dije|te lo dije|otra vez|te repito|i told you|i already told you|as i said|like i said|i repeat)\b`)
	unrequested = lazyre.New(`\b(no te pedi|nadie te pidio|sobre[- ]?ingenieria|no hagas eso|i didn'?t ask|i did not ask|nobody asked|no one asked|over-?engineer(ing|ed)?|don'?t do that|do not do that)\b|(^|[.,;:!?¡¿()\n-])\s*eso no\b`)
	decided     = lazyre.New(`\b(no decidas|preguntame|(esa|la) decision es mia|don'?t decide|do not decide|ask me|(that'?s|that is|it'?s|it is) my (decision|call)|that decision is mine)\b`)
)

// folded is text lower-cased, its accents and curly quotes undone.
func folded(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case strings.ContainsRune("áàäâ", r):
			b.WriteRune('a')
		case strings.ContainsRune("éèëê", r):
			b.WriteRune('e')
		case strings.ContainsRune("íìïî", r):
			b.WriteRune('i')
		case strings.ContainsRune("óòöô", r):
			b.WriteRune('o')
		case strings.ContainsRune("úùüû", r):
			b.WriteRune('u')
		case r == 'ñ':
			b.WriteRune('n')
		case r == '’' || r == '‘':
			b.WriteRune('\'')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// count adds one message the owner wrote; answering is whether it answers a question.
func (c *Corrections) count(text string, answering bool) {
	c.OwnerMessages++
	t := folded(text)
	hit := false
	for _, k := range []struct {
		n  *int
		re *lazyre.Regexp
		ok bool
	}{{&c.KeepGoing, keepGoing, !answering}, {&c.Repeat, repeated, true}, {&c.Unrequested, unrequested, true}, {&c.Decision, decided, true}} {
		if k.ok && k.re.MatchString(t) {
			*k.n++
			hit = true
		}
	}
	if hit {
		c.Correcting++
	}
}

// add sums o into c.
func (c *Corrections) add(o Corrections) {
	c.OwnerMessages += o.OwnerMessages
	c.Correcting += o.Correcting
	c.KeepGoing += o.KeepGoing
	c.Repeat += o.Repeat
	c.Unrequested += o.Unrequested
	c.Decision += o.Decision
}

func (c Corrections) value() *js.Object {
	return js.Obj("owner_messages", float64(c.OwnerMessages), "correcting", float64(c.Correcting), "keep_going", float64(c.KeepGoing),
		"repeat", float64(c.Repeat), "unrequested", float64(c.Unrequested), "decision", float64(c.Decision))
}

// asksOwner is whether the response whose last line is es[last] calls the
// ask tool or ends its text in a question mark.
func asksOwner(es []*Entry, last int) bool {
	id := es[last].MessageID
	for i := last; i >= 0; i-- {
		e := es[i]
		if e.Role != "assistant" || (i < last && (id == nil || e.MessageID == nil || *e.MessageID != *id)) {
			break
		}
		for _, u := range e.ToolUses {
			if u.Name == askTool {
				return true
			}
		}
	}
	return endsInQuestion(es, last)
}
