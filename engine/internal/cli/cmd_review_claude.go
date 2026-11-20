package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
)

// `jhon review run [--base <ref>] [--change <name>] [--timeout <seconds>]`:
// the native Prism review in one Bash call, Claude Code's variant of pi's jhon_review
// tool (T118d, L704, L706; shell/lib/review-tool.ts is the shell's). It runs
// `start`, then each facet prompt as one tool-less `claude -p` call, all
// facets at once (at most reviewClaudeParallel), then the challenger and the
// arbiter the same way when there are findings, then `record`: the same
// steps, prompts and record as the shell's, each step in process.
//
// Each call is exactly `claude -p --tools "" --disallowedTools "mcp__*"
// --output-format json --max-turns 1 --no-session-persistence
// --setting-sources "" [--effort <e>] --model <m>`, m and e the active
// profile's Claude Code model and effort for its routing key (no --effort
// when it names none), the prompt on stdin, in a scratch directory; no user,
// project or local settings load, so no hook or MCP server; it is bounded by a timeout (reviewClaudeTimeout, or
// --timeout) and its output by reviewClaudeOutputMax. Any call that fails
// (claude not on PATH, a non-zero exit, output that is not claude's JSON
// result or not the role's answer, a timeout) stops the review: one line on
// stderr says why and names the facet agents, the contract's fallback, nothing
// is recorded, exit 1. A step the engine refuses (an oversized prompt, a
// stale subject) prints its own refusal with its exit code. Exit 0 prints a
// short result: the verdict, the record and its sets.
//
// The calls share one cancellable context (L1159, F1, F2): the first that
// fails prints its line at once and cancels the rest, and SIGINT or SIGTERM
// does the same with "stopped by <signal>". Each call runs in a process
// group of its own, killed whole on cancel or timeout, so nothing it started
// outlives it, and its scratch directory is removed before the run exits.
// A signal that arrives while the record is written lets it finish.

const (
	reviewClaudeParallel  = 4
	reviewClaudeOutputMax = 256 << 10
	reviewClaudeErrMax    = 64 << 10
)

// reviewClaudeArgs is the argv of one review call on model at effort ("":
// no --effort).
func reviewClaudeArgs(model, effort string) []string {
	args := []string{"-p", "--tools", "", "--disallowedTools", "mcp__*", "--output-format", "json", "--max-turns", "1", "--no-session-persistence", "--setting-sources", ""}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	return append(args, "--model", model)
}

// reviewClaudeTimeout is a call's time bound, as the shell's
// reviewTimeoutMs: 3 minutes plus 1.5 s per KiB of prompt, at most 30.
func reviewClaudeTimeout(promptBytes int) time.Duration {
	kib := (max(promptBytes, 0) + 1023) / 1024
	return min(3*time.Minute+time.Duration(kib)*1500*time.Millisecond, 30*time.Minute)
}

// reviewNativeOff is why the native review must not run, "" when it may:
// JHON_REVIEW_NATIVE=off (or 0, false), the shell's switch.
func reviewNativeOff() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("JHON_REVIEW_NATIVE"))) {
	case "off", "0", "false":
		return "the native review is off (JHON_REVIEW_NATIVE=off)"
	}
	return ""
}

type reviewFacetCall struct {
	Facet      string `json:"facet"`
	RoutingKey string `json:"routing_key"`
	Prompt     string `json:"prompt"`
}

func runReviewClaude(argv []string) (int, error) {
	var opts []string
	var timeout time.Duration
	for i := 0; i < len(argv); i++ {
		switch {
		case (argv[i] == "--base" || argv[i] == "--change") && i+1 < len(argv):
			opts = append(opts, argv[i], argv[i+1])
			i++
		case argv[i] == "--timeout" && i+1 < len(argv):
			i++
			n, err := strconv.Atoi(argv[i])
			if err != nil || n <= 0 {
				fmt.Fprintln(os.Stderr, reviewUsage)
				return 2, nil
			}
			timeout = time.Duration(n) * time.Second
		default:
			fmt.Fprintln(os.Stderr, reviewUsage)
			return 2, nil
		}
	}
	// step runs one native step in process; a refusal is passed on as is.
	step := func(sub string, args []string, input string) ([]byte, int) {
		var out, errb bytes.Buffer
		code := reviewNative(sub, append(append([]string{"--json"}, args...), opts...), strings.NewReader(input), &out, &errb)
		if code != 0 {
			os.Stderr.Write(errb.Bytes())
		}
		return out.Bytes(), code
	}

	raw, code := step("start", nil, "")
	if code != 0 {
		return code, nil
	}
	var start struct {
		Subject string            `json:"subject"`
		Facets  []reviewFacetCall `json:"facets"`
	}
	if err := json.Unmarshal(raw, &start); err != nil {
		return 2, fmt.Errorf("jhon review start's output: %v", err)
	}
	keys := []string{}
	for _, l := range start.Facets {
		keys = append(keys, l.RoutingKey)
	}
	// stop cancels every call with why, the first reason only, and prints
	// its one line at once; failed is stop as the run's result.
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	var printed sync.Once
	stop := func(why error) {
		cancel(why)
		printed.Do(func() {
			fmt.Fprintf(os.Stderr, "jhon review run: %s; nothing recorded. Fallback: launch the %s agents, then the prism-challenger and prism-arbiter agents, as the Prism contract's fallback says\n",
				oneLine(context.Cause(ctx).Error(), 400), andList(keys))
		})
	}
	failed := func(reason string) (int, error) {
		stop(errors.New(reason))
		return 1, nil
	}
	// A signal stops the run as a failed call does, until the record step.
	var recording sync.Mutex
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)
	go func() {
		select {
		case sig := <-sigs:
			recording.Lock()
			defer recording.Unlock()
			if ctx.Err() != nil {
				return // the record step has run
			}
			name := map[os.Signal]string{syscall.SIGINT: "SIGINT", syscall.SIGTERM: "SIGTERM"}[sig]
			stop(fmt.Errorf("stopped by %s", name))
		case <-ctx.Done():
		}
	}()
	if why := reviewNativeOff(); why != "" {
		return failed(why)
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		return failed("claude is not on PATH")
	}
	root, _ := Root()
	m, err := manifest.Load(root)
	if err != nil {
		return failed("agents.yaml: " + err.Error())
	}
	profile := activeProfile()
	ask := func(key, prompt string) (string, error) {
		model, effort, _ := reviewModelIn(m, profile, "claude-code", key)
		if model == "" {
			return "", fmt.Errorf("no Claude Code model for %s in the active profile", key)
		}
		limit := timeout
		if limit == 0 {
			limit = reviewClaudeTimeout(len(prompt))
		}
		return claudeComplete(ctx, bin, model, effort, prompt, limit)
	}

	// The facets, at once.
	facets := make([]json.RawMessage, len(start.Facets))
	sem := make(chan struct{}, reviewClaudeParallel)
	var wg sync.WaitGroup
	for i, l := range start.Facets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			text, err := ask(l.RoutingKey, l.Prompt)
			if err == nil {
				facets[i], err = facetAnswer(text, l.Facet, start.Subject)
			}
			if err != nil && ctx.Err() == nil {
				stop(fmt.Errorf("the %s facet call failed: %v", l.Facet, err))
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return 1, nil
	}
	findings := 0
	for _, l := range facets {
		var v struct {
			Findings []json.RawMessage `json:"findings"`
		}
		_ = json.Unmarshal(l, &v)
		findings += len(v.Findings)
	}

	// The challenger and the arbiter, when there is something to judge.
	role := func(name, sub string, input any) (json.RawMessage, int, error) {
		in, _ := json.Marshal(input)
		raw, code := step(sub, []string{"--subject", start.Subject}, string(in))
		if code != 0 {
			return nil, code, nil
		}
		var p struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(raw, &p); err != nil || p.Prompt == "" {
			return nil, 2, fmt.Errorf("jhon review %s's output has no prompt", sub)
		}
		text, err := ask("prism-"+name, p.Prompt)
		if err == nil {
			var answer json.RawMessage
			if answer, err = roleAnswer(text, name); err == nil {
				return answer, 0, nil
			}
		}
		if ctx.Err() == nil {
			stop(fmt.Errorf("the %s call failed: %v", name, err))
		}
		return nil, 1, nil
	}
	empty := func(name string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"subject_hash": start.Subject, "role": name, "results": []any{}})
		return b
	}
	challenger, arbiter := empty("challenger"), empty("arbiter")
	if findings > 0 {
		var code int
		if challenger, code, err = role("challenger", "refute", facets); challenger == nil {
			return code, err
		}
		if standing(facets, challenger, start.Subject) > 0 {
			if arbiter, code, err = role("arbiter", "validate", map[string]any{"facets": facets, "challenger": challenger}); arbiter == nil {
				return code, err
			}
		}
	}

	recording.Lock()
	if ctx.Err() != nil {
		recording.Unlock()
		return 1, nil
	}
	in, _ := json.Marshal(map[string]any{"facets": facets, "challenger": challenger, "arbiter": arbiter})
	raw, code = step("record", []string{"--subject", start.Subject}, string(in))
	cancel(errRunDone) // the record is written or refused: a signal no longer stops it
	recording.Unlock()
	if code != 0 {
		return code, nil
	}
	var rec struct {
		Record, Verdict                                                        string
		Facets                                                                 []string
		CorrectionSet, ForVerify, FollowUps, Information, Refuted, Unavailable []string
	}
	var o map[string]json.RawMessage
	if err := json.Unmarshal(raw, &o); err != nil {
		return 2, fmt.Errorf("jhon review record's output: %v", err)
	}
	get := func(k string, v any) { _ = json.Unmarshal(o[k], v) }
	get("record", &rec.Record)
	get("verdict", &rec.Verdict)
	get("facets", &rec.Facets)
	get("correction_set", &rec.CorrectionSet)
	get("for_verify", &rec.ForVerify)
	get("follow_ups", &rec.FollowUps)
	get("information", &rec.Information)
	get("refuted", &rec.Refuted)
	get("unavailable", &rec.Unavailable)
	fmt.Printf("Prism native review (claude -p): verdict %s · facets %s · record %s\n", rec.Verdict, strings.Join(rec.Facets, ", "), rec.Record)
	fmt.Printf("correction set %d · for verify %d · follow-ups %d · information %d · refuted %d · unavailable %d\n",
		len(rec.CorrectionSet), len(rec.ForVerify), len(rec.FollowUps), len(rec.Information), len(rec.Refuted), len(rec.Unavailable))
	if len(rec.CorrectionSet) > 0 {
		fmt.Printf("Correction set: %s (in %s)\n", strings.Join(rec.CorrectionSet, ", "), rec.Record)
	}
	if len(rec.ForVerify) > 0 {
		fmt.Printf("For verify: %s\n", strings.Join(rec.ForVerify, ", "))
	}
	if len(rec.Unavailable) > 0 {
		fmt.Printf("Unavailable: %s\n", oneLine(strings.Join(rec.Unavailable, "; "), 300))
	}
	return 0, nil
}

// errRunDone ends the run's context once the record step has run.
var errRunDone = errors.New("the run is over")

// standing is how many of the facets' findings the challenger's answer leaves
// standing, by distinct known finding id as record reduces them
// (ldd.Standing): a refuted id given twice, or one that names no finding,
// leaves the others standing. Results the engine cannot read count every
// finding as standing, so the validate step judges them.
func standing(facets []json.RawMessage, challenger json.RawMessage, subject string) int {
	raw, _ := json.Marshal(facets)
	results, err := ldd.ParseFacetResults(raw)
	if err == nil {
		results, err = ldd.CheckFacets(results, subject)
	}
	var r ldd.RoleResult
	if err == nil {
		err = json.Unmarshal(challenger, &r)
	}
	all := ldd.AllFindings(results)
	if err != nil {
		return max(len(all), 1)
	}
	return len(ldd.Standing(all, &r))
}

// boundedBuffer keeps the first max bytes written to it and notes the rest.
// It has no ReadFrom (a bytes.Buffer's would take every byte past max: exec
// copies a child's output with io.Copy, which prefers ReadFrom).
type boundedBuffer struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room < len(p) {
		b.over = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *boundedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *boundedBuffer) String() string { return b.buf.String() }

// claudeComplete runs one review call under parent and gives the text of its
// result. The call is a process group of its own, killed whole when parent
// is cancelled or limit passes.
func claudeComplete(parent context.Context, bin, model, effort, prompt string, limit time.Duration) (string, error) {
	dir, err := os.MkdirTemp("", "jhon-review-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, reviewClaudeArgs(model, effort)...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(prompt)
	out, errb := &boundedBuffer{max: reviewClaudeOutputMax}, &boundedBuffer{max: reviewClaudeErrMax}
	cmd.Stdout, cmd.Stderr = out, errb
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if parent.Err() != nil {
		return "", context.Cause(parent)
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("claude did not answer within %s", limit)
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			why := oneLine(errb.String(), 300)
			if why == "" {
				why = oneLine(out.String(), 300)
			}
			if why != "" {
				why = ": " + why
			}
			return "", fmt.Errorf("claude exited %d%s", exit.ExitCode(), why)
		}
		return "", fmt.Errorf("claude could not run: %v", err)
	}
	if out.over {
		return "", fmt.Errorf("claude's output passed %d KiB", reviewClaudeOutputMax>>10)
	}
	var res struct {
		Type    string  `json:"type"`
		Subtype string  `json:"subtype"`
		IsError bool    `json:"is_error"`
		Result  *string `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil || res.Type != "result" {
		return "", errors.New("claude's output is not its JSON result")
	}
	if res.IsError || res.Subtype != "success" {
		why := ""
		if res.Result != nil {
			why = ": " + oneLine(*res.Result, 200)
		}
		return "", fmt.Errorf("claude reported an error (%s)%s", res.Subtype, why)
	}
	if res.Result == nil || strings.TrimSpace(*res.Result) == "" {
		return "", errors.New("claude's result has no text")
	}
	return *res.Result, nil
}

// jsonLine is the last line of text that parses as a JSON object, code
// fences aside, as the shell reads an answer; nil when none does.
func jsonLine(text string) json.RawMessage {
	ls := strings.Split(text, "\n")
	for i := len(ls) - 1; i >= 0; i-- {
		l := strings.TrimSpace(ls[i])
		if !strings.HasPrefix(l, "{") {
			continue
		}
		var v map[string]json.RawMessage
		if json.Unmarshal([]byte(l), &v) == nil {
			return json.RawMessage(l)
		}
	}
	return nil
}

// facetAnswer is the facet's answer in text, when it is one line of JSON for
// facet and subject, as the shell checks it.
func facetAnswer(text, facet, subject string) (json.RawMessage, error) {
	v := jsonLine(text)
	var a struct {
		SubjectHash string `json:"subject_hash"`
		Facet       string `json:"facet"`
		Inspection  struct {
			Status string `json:"status"`
		} `json:"inspection"`
		Findings []json.RawMessage `json:"findings"`
	}
	if v == nil || json.Unmarshal(v, &a) != nil || a.Facet != facet || strings.TrimPrefix(a.SubjectHash, "sha256:") != subject || a.Findings == nil ||
		(a.Inspection.Status != "completed" && a.Inspection.Status != "unavailable") {
		return nil, fmt.Errorf("its answer is not one line of JSON for the %s facet and this subject", facet)
	}
	return v, nil
}

// roleAnswer is the challenger's or arbiter's answer in text, with its role
// set as the shell sets it.
func roleAnswer(text, name string) (json.RawMessage, error) {
	v := jsonLine(text)
	var a struct {
		SubjectHash string            `json:"subject_hash"`
		Results     []json.RawMessage `json:"results"`
	}
	if v == nil || json.Unmarshal(v, &a) != nil || a.Results == nil {
		return nil, errors.New("its answer is not one line of JSON with results")
	}
	b, err := json.Marshal(map[string]any{"subject_hash": a.SubjectHash, "role": name, "results": a.Results})
	return b, err
}

// oneLine is s with its whitespace collapsed, at most n bytes.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		s = strings.ToValidUTF8(s[:n-3], "") + "..."
	}
	return s
}

// andList is xs joined as prose: "a", "a and b", "a, b and c".
func andList(xs []string) string {
	if len(xs) < 2 {
		return strings.Join(xs, "")
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}
