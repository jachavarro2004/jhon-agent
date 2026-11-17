package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/judgment"
)

// `jhon judgment append <record> <event-file|->` and `jhon judgment status
// <record> [--json]`: append one event to a judgment
// record, or report the record's state (internal/judgment). A refusal or a
// failure exits 1 with the record unchanged; a usage error exits 2.
func init() { register("judgment", runJudgment) }

const judgmentUsage = "usage: jhon judgment append <record> <event-file|->\n       jhon judgment status <record> [--json]"

func runJudgment(argv []string) (int, error) {
	judgment.Load()
	cmd, rest := "", []string{}
	if len(argv) > 0 {
		cmd, rest = argv[0], argv[1:]
	}
	var err error
	switch {
	case cmd == "append" && len(rest) == 2:
		err = judgmentAppend(rest[0], rest[1])
	case cmd == "status" && (len(rest) == 1 || len(rest) == 2 && rest[1] == "--json"):
		err = judgmentStatus(rest[0], len(rest) == 2)
	default:
		fmt.Fprintln(os.Stderr, judgmentUsage)
		return 2, nil
	}
	if err == nil {
		return 0, nil
	}
	outcome := "failed"
	if refusal := (*judgment.Refusal)(nil); errors.As(err, &refusal) {
		outcome = "refused"
	}
	fmt.Fprintf(os.Stderr, "jhon judgment %s: %s: %s\n", cmd, outcome, err)
	if cmd == "append" {
		fmt.Fprintln(os.Stderr, "nothing was written; the record is unchanged")
	}
	return 1, nil
}

func judgmentAppend(path, from string) error {
	var b []byte
	var err error
	if from == "-" {
		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(from)
	}
	if err != nil {
		return js.FSError(err)
	}
	if err := judgment.AppendFile(path, string(b)); err != nil {
		return err
	}
	fmt.Println("appended to " + path)
	return nil
}

func judgmentStatus(path string, asJSON bool) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return js.FSError(err)
	}
	s, err := judgment.StatusOf(string(b))
	if err != nil {
		return err
	}
	if !asJSON {
		fmt.Println(judgmentHuman(s))
		return nil
	}
	out, err := js.Stringify(struct {
		Record string `json:"record"`
		judgment.Status
	}{path, s})
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

// judgmentHuman is the state as `jhon judgment status` prints it without --json.
func judgmentHuman(s judgment.Status) string {
	mode, round := "null", "R0 in progress"
	if s.Mode != nil {
		mode = *s.Mode
	}
	if s.Round != nil {
		round = strconv.Itoa(*s.Round)
	}
	out := []string{"mode        " + mode, "round       " + round}
	if len(s.Pending) > 0 {
		out = append(out, "pending     "+strings.Join(s.Pending, ", "))
	}
	out = append(out, "", "points")
	for _, p := range s.Points {
		out = append(out, fmt.Sprintf("  %s %s %s %s", js.PadEnd(p.ID, 4), js.PadEnd(p.Status, 12), js.PadEnd(p.Severity, 10), p.Location))
	}
	if len(s.Points) == 0 {
		out = append(out, "  none")
	}
	if len(s.Info) > 0 {
		out = append(out, "info        "+strings.Join(s.Info, ", "))
	}
	progress := "n/a"
	if s.NewProgress != nil {
		progress = map[bool]string{true: "yes", false: "no"}[*s.NewProgress]
	}
	next := "next        " + s.NextAction
	if s.Reason != nil && *s.Reason != "" {
		next += " (" + *s.Reason + ")"
	}
	out = append(out, "", "progress    "+progress, next)
	if s.Verdict != nil && *s.Verdict != "" {
		out = append(out, "verdict     "+*s.Verdict)
	}
	return strings.Join(out, "\n")
}
