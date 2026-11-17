package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fake runtimes stand in for claude, jhon and pi: each logs its argv and
// environment to $FAKE_DIR/calls.log, writes the transcript or session file
// where the real one lands, solves the task by copying $FAKE_SOLUTION into
// its working directory when that is set, and prints a scripted stream.
// $FAKE_STREAM is a file of extra stream records each prints before it ends.
// pi logs, to $FAKE_DIR/pi-models.log, whether its agent dir holds a
// models.json and whether it is $FAKE_PI_MODELS byte for byte.
// $FAKE_DIR/utils holds the seven_day utilization the n-th claude run reports.
// With $FAKE_HOLD (seconds) each claude run holds that long and counts the
// claude runs alive at once, the most in $FAKE_DIR/max (--parallel, T83).

const fakeClaude = `#!/bin/sh
echo "claude $* |HOME=$HOME|XDG_CACHE_HOME=$XDG_CACHE_HOME|PWD=$(pwd -P)|JHON_ROOT=$JHON_ROOT|ENGRAM_DATA_DIR=$ENGRAM_DATA_DIR|ENGRAM_URL=$ENGRAM_URL|ENGRAM_PORT=$ENGRAM_PORT" >> "$FAKE_DIR/calls.log"
echo "claude env |HOME=$HOME|CLAUDE_CODE_DISABLE_CLAUDE_MDS=$CLAUDE_CODE_DISABLE_CLAUDE_MDS|CLAUDE_CODE_DISABLE_AUTO_MEMORY=$CLAUDE_CODE_DISABLE_AUTO_MEMORY|CLAUDE_CONFIG_DIR=$CLAUDE_CONFIG_DIR|CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=$CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD|PWD=$(pwd -P)" >> "$FAKE_DIR/calls.log"
if [ "$1" = auth ]; then cat "$FAKE_DIR/auth.json"; exit 0; fi
sid=""; prev=""; resume=""
for a in "$@"; do [ "$prev" = --session-id ] && sid=$a; [ "$prev" = --resume ] && sid=$a && resume=1; prev=$a; done
m=$(cat "$FAKE_DIR/msg-$sid" 2>/dev/null || echo 0); m=$((m+1)); echo $m > "$FAKE_DIR/msg-$sid"
[ -n "$FAKE_SOLUTIONS" ] && [ -d "$FAKE_SOLUTIONS/$m" ] && cp -r "$FAKE_SOLUTIONS/$m"/. .
lock() { while ! mkdir "$FAKE_DIR/lock" 2>/dev/null; do sleep 0.01; done; }
unlock() { rmdir "$FAKE_DIR/lock"; }
lock; n=$(cat "$FAKE_DIR/n" 2>/dev/null || echo 0); n=$((n+1)); echo $n > "$FAKE_DIR/n"; unlock
util=$(sed -n "${n}p" "$FAKE_DIR/utils")
if [ -n "$FAKE_HOLD" ]; then
  lock; a=$(cat "$FAKE_DIR/active" 2>/dev/null || echo 0); a=$((a+1)); echo $a > "$FAKE_DIR/active"
  mx=$(cat "$FAKE_DIR/max" 2>/dev/null || echo 0); [ $a -gt $mx ] && echo $a > "$FAKE_DIR/max"; unlock
  sleep "$FAKE_HOLD"
  lock; a=$(cat "$FAKE_DIR/active"); echo $((a-1)) > "$FAKE_DIR/active"; unlock
fi
[ -n "$FAKE_SOLUTION" ] && cp -r "$FAKE_SOLUTION"/. .
enc=$(pwd -P | tr '/.' '--')
mkdir -p "$HOME/.claude/projects/$enc"
if [ -n "$resume" ]; then echo '{"type":"user"}' >> "$HOME/.claude/projects/$enc/$sid.jsonl"
elif [ -n "$FAKE_TRANSCRIPT" ]; then cp "$FAKE_TRANSCRIPT" "$HOME/.claude/projects/$enc/$sid.jsonl"
else echo '{"type":"user"}' > "$HOME/.claude/projects/$enc/$sid.jsonl"; fi
printf '{"type":"system","subtype":"init","session_id":"%s"}\n' "$sid"
[ -n "$util" ] && printf '{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","rateLimitType":"seven_day","unifiedWindows":{"seven_day":{"utilization":%s}}}}\n' "$util"
[ -n "$FAKE_STREAM" ] && cat "$FAKE_STREAM"
printf '{"type":"result","subtype":"success","num_turns":7,"usage":{"output_tokens":900},"session_id":"%s"}\n' "$sid"
exit ${FAKE_CLAUDE_EXIT:-0}
`

const fakePi = `#!/bin/sh
echo "pi $* |HOME=$HOME|XDG_CACHE_HOME=$XDG_CACHE_HOME|PWD=$(pwd -P)|PI_CODING_AGENT_DIR=$PI_CODING_AGENT_DIR|ENGRAM_DATA_DIR=$ENGRAM_DATA_DIR|ENGRAM_URL=$ENGRAM_URL|ENGRAM_PORT=$ENGRAM_PORT|CLAUDE_BRIDGE_DEBUG=$CLAUDE_BRIDGE_DEBUG|CLAUDE_BRIDGE_DEBUG_PATH=$CLAUDE_BRIDGE_DEBUG_PATH" >> "$FAKE_DIR/calls.log"
models=absent; [ -f "$PI_CODING_AGENT_DIR/models.json" ] && { models=differs; [ -n "$FAKE_PI_MODELS" ] && cmp -s "$PI_CODING_AGENT_DIR/models.json" "$FAKE_PI_MODELS" && models=same; }
echo "$PI_CODING_AGENT_DIR models=$models" >> "$FAKE_DIR/pi-models.log"
[ "$CLAUDE_BRIDGE_DEBUG" = 1 ] && mkdir -p "$(dirname "$CLAUDE_BRIDGE_DEBUG_PATH")" && echo "systemPrompt: the dumped prompt" > "$CLAUDE_BRIDGE_DEBUG_PATH"
sid=""; prev=""
for a in "$@"; do [ "$prev" = --session-id ] && sid=$a; prev=$a; done
[ -n "$FAKE_SOLUTION" ] && cp -r "$FAKE_SOLUTION"/. .
dir="${PI_CODING_AGENT_DIR:-$HOME/.pi/jhon}/sessions/--$(pwd -P | sed 's#^/##; s#/#-#g')--"
mkdir -p "$dir"
if [ -n "$FAKE_TRANSCRIPT" ]; then cp "$FAKE_TRANSCRIPT" "$dir/2026-11-09T00-00-00-000Z_$sid.jsonl"
else echo '{"type":"session"}' > "$dir/2026-11-09T00-00-00-000Z_$sid.jsonl"; fi
printf '{"type":"session","version":3,"id":"%s"}\n' "$sid"
turns=${FAKE_PI_TURNS:-3}; i=0
case " $* " in *" --mode rpc "*)
  n=0; total=0; busied=""
  while IFS= read -r line; do
    printf '%s\n' "$line" >> "$FAKE_DIR/pi-prompts.log"
    n=$((n+1)); i=0
    if [ $n = 2 ] && [ -n "$FAKE_PI_BUSY" ] && [ -z "$busied" ]; then
      busied=1; n=1
      echo '{"type":"agent_start"}'
      echo '{"type":"message_end","message":{"role":"custom","customType":"jhon-tasks.notice","details":{"jhonTasks":{"id":"job-2","kind":"monitor","status":"running","event":"events"}}}}'
      echo '{"id":"bench-2","type":"response","command":"prompt","success":false,"error":"Agent is already processing. Specify streamingBehavior ('"'"'steer'"'"' or '"'"'followUp'"'"') to queue the message."}'
      echo '{"type":"turn_start"}'
      echo '{"type":"agent_settled","aborted":false}'
      continue
    fi
    [ -n "$FAKE_SOLUTIONS" ] && [ -d "$FAKE_SOLUTIONS/$n" ] && cp -r "$FAKE_SOLUTIONS/$n"/. .
    printf '{"id":"bench-%s","type":"response","command":"prompt","success":true,"data":{"disposition":"started"}}\n' "$n"
    if [ $n = 1 ] && [ -n "$FAKE_PI_UI" ]; then
      echo '{"type":"extension_ui_request","id":"ui-1","method":"setStatus","statusKey":"fff"}'
      echo '{"type":"extension_ui_request","id":"ui-2","method":"select","title":"Jhon safety guard","options":["Yes","No"]}'
      IFS= read -r ans; printf '%s\n' "$ans" >> "$FAKE_DIR/ui-answers.log"
    fi
    if [ $n = 1 ] && [ -n "$FAKE_PI_BG" ]; then
      echo '{"type":"tool_execution_end","toolCallId":"t1","toolName":"bash_background","result":{"content":[],"details":{"jhonTasks":{"id":"job-1","kind":"command","status":"running"}}},"isError":false}'
      echo '{"type":"agent_settled","aborted":false}'
      sleep 0.3
      [ "$FAKE_PI_BG" = notice ] && echo '{"type":"message_end","message":{"role":"custom","customType":"jhon-tasks.notice","details":{"jhonTasks":{"id":"job-1","kind":"command","status":"exited"}}}}'
    fi
    while [ $i -lt $turns ]; do
      echo '{"type":"turn_start"}'
      echo '{"type":"message_end","message":{"role":"assistant","usage":{"input":10,"output":100,"cacheRead":1000,"cacheWrite":50}}}'
      i=$((i+1)); total=$((total+1))
      [ $total -gt 60 ] && sleep 30
    done
    echo '{"type":"agent_settled","aborted":false}'
  done
  exit 0;;
esac
while [ $i -lt $turns ]; do
  echo '{"type":"turn_start"}'
  echo '{"type":"message_end","message":{"role":"assistant","usage":{"input":10,"output":100}}}'
  i=$((i+1))
done
[ "$turns" -gt 60 ] && sleep 30
[ -n "$FAKE_STREAM" ] && cat "$FAKE_STREAM"
echo '{"type":"agent_settled","aborted":false}'
`

// fakePiMCP is the engram entry `bench prepare` leaves in the bench HOME's
// ~/.pi/jhon/mcp.json, as the release renders it.
const fakePiMCP = `{"mcpServers":{"engram":{"command":"engram","args":["mcp","--tools=agent"],"exposure":"direct"},
"jhon":{"command":"/release/jhon","args":["mcp"],"exposure":"direct"}}}`

// fakeJhon is the bench HOME's jhon: start passes through to the fake
// claude, start pi to the fake pi (as jhon start does), audit prints
// $FAKE_DIR/audit.json for a transcript that exists, and status --json
// prints $FAKE_DIR/status.json, or a status with no test record.
const fakeJhon = `#!/bin/sh
echo "jhon $* |HOME=$HOME|XDG_CACHE_HOME=$XDG_CACHE_HOME" >> "$FAKE_DIR/calls.log"
case "$1" in
start) shift
  if [ "$1" = pi ]; then shift; PI_CODING_AGENT_DIR="$HOME/.pi/jhon" exec pi "$@"; fi
  for a in "$@"; do case "$a" in --agent|--plugin-dir) printf 'jhon: jhon start sets %s itself\n' "$a" >&2; exit 2;; esac; done
  exec claude --agent jhon-orchestrator --plugin-dir /release/plugin "$@";;
audit) [ -f "$2" ] || { echo "no transcript $2" >&2; exit 1; }; cat "$FAKE_DIR/audit.json";;
version) if [ -f "$FAKE_DIR/version.json" ]; then cat "$FAKE_DIR/version.json"; else echo '{"version":"0.0.0-dev","commit":"fake"}'; fi;;
profile) [ "$2" = use ] || exit 2
  echo "profile env |XDG_CONFIG_HOME=$XDG_CONFIG_HOME|JHON_PROFILES=$JHON_PROFILES|PWD=$(pwd -P)" >> "$FAKE_DIR/calls.log"
  [ -n "$FAKE_PROFILE_EXIT" ] && { echo "jhon profile: no profile '$3'" >&2; exit $FAKE_PROFILE_EXIT; }
  printf '[pi]\nprofile = "%s"\n' "$3" > "$XDG_CONFIG_HOME/jhon/settings.toml"
  mkdir -p "$HOME/.pi/jhon"
  echo '{"defaultProvider":"zai","defaultModel":"glm-5.3"}' > "$HOME/.pi/jhon/settings.json"
  echo "profile $3 active";;
status) if [ -f "$FAKE_DIR/status.json" ]; then cat "$FAKE_DIR/status.json"; else echo '{"ok":true,"tests":null}'; fi;;
*) exit 2;;
esac
`

const fakeAudit = `{"ok":true,"findings":[{"check":"tdd-order"},{"check":"tdd-order"},{"check":"scope"}],
"context_use":{"agents":[{"agent":"main","turns":7,"input_tokens":40,"cache_creation_input_tokens":6000,"cache_read_input_tokens":30000},
{"agent":"a1","turns":3,"input_tokens":2,"cache_creation_input_tokens":1000,"cache_read_input_tokens":5000}]},
"outcomes":{"session":{"elapsed_ms":120000,"active_ms":90000},"changes":[{"agents":1,"verify_runs":2}]},
"timing":{"main":{"cache_hit":0.83}}}`

type fake struct {
	dir  string // FAKE_DIR, with bin/ on PATH
	home string // the bench HOME
}

func newFake(t *testing.T, utils ...string) fake {
	t.Helper()
	f := fake{dir: t.TempDir(), home: t.TempDir()}
	write := func(path, body string, mode os.FileMode) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(f.dir, "bin", "claude"), fakeClaude, 0o755)
	write(filepath.Join(f.dir, "bin", "pi"), fakePi, 0o755)
	write(filepath.Join(f.home, ".local", "bin", "jhon"), fakeJhon, 0o755)
	write(filepath.Join(f.home, ".pi", "jhon", "mcp.json"), fakePiMCP, 0o644)
	write(filepath.Join(f.dir, "audit.json"), fakeAudit, 0o644)
	write(filepath.Join(f.dir, "utils"), strings.Join(utils, "\n")+"\n", 0o644)
	t.Setenv("FAKE_DIR", f.dir)
	t.Setenv("FAKE_SOLUTION", "")
	t.Setenv("FAKE_SOLUTIONS", "")
	t.Setenv("FAKE_CLAUDE_EXIT", "")
	t.Setenv("FAKE_TRANSCRIPT", "")
	t.Setenv("FAKE_PI_UI", "")
	t.Setenv("FAKE_PI_BG", "")
	t.Setenv("FAKE_PI_BUSY", "")
	t.Setenv("FAKE_PROFILE_EXIT", "")
	t.Setenv("FAKE_HOLD", "")
	t.Setenv("FAKE_STREAM", "")
	t.Setenv("FAKE_PI_MODELS", "")
	t.Setenv("PATH", filepath.Join(f.dir, "bin")+":"+os.Getenv("PATH"))
	return f
}

// calls are the fake runtimes' logged calls; none when nothing ran.
func (f fake) calls(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "calls.log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// ids is a deterministic session id source.
func ids() func() string {
	n := 0
	return func() string {
		n++
		return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
	}
}
