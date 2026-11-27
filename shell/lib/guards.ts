/**
 * pi/guards.ts — how Jhon's guards judge a pi tool call.
 *
 * Each call is reshaped as the Claude Code hook payload the guards read and
 * run past `jhon hook <name>`: a shell command (`bash`, and any tool whose
 * input carries a `command`) goes to the safety and framework guards; a
 * `write` or `edit` to the VCS and framework guards, and every one, a
 * `subagent` launch included, to the route guard. A guard's exit 2 blocks
 * the call with its refusal; its ask is returned for the caller to put to the
 * owner. A child carries an `agent_id`, so a guard never asks on its behalf. A
 * child has no agent name, so the guards run unscoped and the framework
 * guard's owner is `*`.
 *
 * Calls no guard can judge are refused: `powershell`, and `AskClaude` in any
 * mode but `read` or `none`, since a full-mode Claude Code session writes
 * outside every guard here.
 */

import { spawn, spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";

/** What the renderer writes to `<agent dir>/jhon.json`. */
export type JhonPiConfig = {
  /** The release's binary, which runs each guard as `jhon hook <name>`. */
  jhon: string;
  /** The file holding the main session's appended prompt. */
  orchestrator: string;
  /** T146h: the lean prompt a small task starts with, and the install's full context file. */
  lean?: string;
  full?: string;
  vcsDirs: string[];
  /** The framework paths framework-guard protects (internal/render, frameworkProtected). */
  protected: string[];
  /** agents.yaml `inspect_allow`. */
  inspectAllow: string[];
};

export const CONFIG_FILE = "jhon.json";

/** The surface of the binary this shell consumes (CONTRACT.md), as its package.json names it. */
export const SHELL_CONTRACT: string = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8")).jhon.contract;

/**
 * How long a guard may take before its call is refused, and `jhon contract`
 * before the binary counts as speaking none: each runs synchronously, so a
 * jhon that never answers would otherwise freeze pi, Esc included.
 */
export const HOOK_TIMEOUT_MS = 10_000;
export const CONTRACT_TIMEOUT_MS = 5_000;

/**
 * Why the binary cannot serve this shell: it does not run `jhon contract`, or
 * does not answer within `timeoutMs`, or it speaks another contract than
 * `expected`. Null when they match.
 */
export function contractMismatch(config: JhonPiConfig, expected = SHELL_CONTRACT, timeoutMs = CONTRACT_TIMEOUT_MS): string | null {
  const r = spawnSync(config.jhon, ["contract", "--json"], { encoding: "utf8", timeout: timeoutMs, killSignal: "SIGKILL" });
  let got: unknown;
  try { got = JSON.parse(r.stdout).shell; } catch { got = undefined; }
  if (r.status === 0 && got === expected) return null;
  const spoken = typeof got === "string" ? got : "(none)";
  return `jhon: ${config.jhon} speaks shell contract ${spoken} and this shell needs ${expected}, so no guard can judge this call; run \`jhon sync\``;
}

/** The call as Claude Code's hooks read it, a refusal no guard is asked about, or null when no guard reads it. */
export type ClaudeCall = { tool_name: "Bash" | "Read" | "Grep" | "Glob" | "Write" | "Edit" | "Agent"; tool_input: Record<string, unknown> } | { refuse: string } | null;

export type Decision = { block: true; reason: string } | { ask: string } | null;

/**
 * One call's context. A child carries its limits: `tools`, the tools it may
 * call (JHON_PI_TOOLS), and `launchPrompt`, a bounded agent's task, which the
 * scope guard reads for its `## Allowed edit surfaces` (JHON_PI_TASK_FILE).
 * `hookTimeoutMs` replaces HOOK_TIMEOUT_MS (tests).
 */
export type CallContext = { config: JhonPiConfig; cwd: string; child: boolean; interactive: boolean; tools?: string[]; launchPrompt?: string; hookTimeoutMs?: number; sessionId?: string };

export function loadConfig(agentDir: string): JhonPiConfig | null {
  const file = join(agentDir, CONFIG_FILE);
  try {
    const c = JSON.parse(readFileSync(file, "utf8"));
    const lists = ["vcsDirs", "protected", "inspectAllow"].every((k) => Array.isArray(c[k]) && c[k].every((x: unknown) => typeof x === "string"));
    return typeof c.jhon === "string" && typeof c.orchestrator === "string" && lists ? c : null;
  } catch {
    return null;
  }
}

export function claudeCall(toolName: string, input: Record<string, unknown>): ClaudeCall {
  // A read is judged only for the credentials it may name (secrets-guard).
  if (toolName === "read") return { tool_name: "Read", tool_input: { file_path: input.path } };
  if (toolName === "grep") return { tool_name: "Grep", tool_input: { path: input.path, glob: input.glob } };
  // pi's find is Claude Code's Glob: a pattern of names under a path.
  if (toolName === "find") return { tool_name: "Glob", tool_input: { pattern: input.pattern, path: input.path } };
  if (toolName === "write") return { tool_name: "Write", tool_input: { file_path: input.path, content: input.content } };
  if (toolName === "edit") {
    const edits = Array.isArray(input.edits) ? input.edits.map((e: { oldText?: unknown; newText?: unknown }) => ({ old_string: e?.oldText, new_string: e?.newText })) : undefined;
    return { tool_name: "Edit", tool_input: { file_path: input.path, ...(edits && { edits }) } };
  }
  // A sub-agent launch is judged as Claude Code's Agent call: the route guard
  // runs jhon-checker and Prism reviewers only on its route.
  // Its task is the prompt: the change it names picks the worktree whose route counts.
  if (toolName === "subagent") return { tool_name: "Agent", tool_input: { subagent_type: input.agent, prompt: input.task } };
  if (toolName === "powershell") return { refuse: "jhon: powershell is outside the safety guard's reading; use bash" };
  if (toolName === "AskClaude" && input.mode !== "read" && input.mode !== "none") {
    return { refuse: "jhon: AskClaude runs Claude Code outside Jhon's guards; call it with mode \"read\" or \"none\", or do the work here" };
  }
  if (typeof input.command === "string") return { tool_name: "Bash", tool_input: { command: input.command } };
  return null;
}

const ASK = /"permissionDecision"\s*:\s*"ask"/;

/** What a program run to its end gave: its exit status (null on a signal or a failure to start), stdout, and stderr. */
export type Ran = { status: number | null; stdout: string; stderr: string; error?: Error; timedOut?: boolean };

/**
 * Run `program` to its end without blocking pi's event loop, so the TUI
 * keeps drawing while it runs. Output past `maxBuffer` characters per stream
 * is dropped. With `timeoutMs`, a program still running then is killed
 * (SIGKILL) and the result, marked `timedOut`, settles at once.
 */
export function runAsync(program: string, args: string[], options: { cwd?: string; input?: string; maxBuffer?: number; timeoutMs?: number } = {}): Promise<Ran> {
  const max = options.maxBuffer ?? 16 * 1024 * 1024;
  return new Promise((resolve) => {
    let stdout = "", stderr = "";
    let timer: ReturnType<typeof setTimeout> | undefined;
    const settle = (r: Ran) => { if (timer) clearTimeout(timer); resolve(r); };
    const child = spawn(program, args, { cwd: options.cwd, stdio: ["pipe", "pipe", "pipe"] });
    if (options.timeoutMs !== undefined) {
      timer = setTimeout(() => {
        child.kill("SIGKILL");
        // Settle now: a grandchild still holding the pipes must not hold the caller.
        settle({ status: null, stdout, stderr, timedOut: true });
      }, options.timeoutMs);
    }
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (d: string) => { if (stdout.length < max) stdout += d; });
    child.stderr.on("data", (d: string) => { if (stderr.length < max) stderr += d; });
    child.on("error", (error) => settle({ status: null, stdout, stderr, error }));
    child.on("close", (status) => settle({ status, stdout, stderr }));
    child.stdin.on("error", () => { /* a program that reads no input closes it early */ });
    child.stdin.end(options.input ?? "");
  });
}

/**
 * A guard's notes on calls it allowed (a PreToolUse additionalContext), by
 * call, until the call's result carries them to the agent (formatEdited).
 */
const guardNotes = new Map<string, string>();
const callKey = (toolName: string, input: Record<string, unknown>, cwd: string) => JSON.stringify([cwd, toolName, input]);

/** The additionalContext a hook printed, or undefined. */
function contextOf(stdout: string): string | undefined {
  try {
    const note = JSON.parse(stdout).hookSpecificOutput?.additionalContext;
    return typeof note === "string" ? note : undefined;
  } catch { return undefined; }
}

/**
 * The note for the agent on a call's result: the guards' note on the call,
 * once, and, for a file pi just edited or wrote, its project's formatter's
 * (`jhon hook format-edit`, docket AY B2) when it rewrote the file;
 * otherwise undefined. It never fails the edit, and a slow formatter never
 * freezes the TUI.
 */
export async function formatEdited(config: JhonPiConfig, toolName: string, input: Record<string, unknown>, cwd: string): Promise<string | undefined> {
  const key = callKey(toolName, input, cwd);
  const guardNote = guardNotes.get(key);
  guardNotes.delete(key);
  const withGuardNote = (note: string | undefined) => [guardNote, note].filter(Boolean).join("\n\n") || undefined;
  if (toolName !== "edit" && toolName !== "write") return withGuardNote(undefined);
  const call = claudeCall(toolName, input);
  if (!call || "refuse" in call) return withGuardNote(undefined);
  const r = await runAsync(config.jhon, ["hook", "format-edit"], { input: JSON.stringify({ ...call, cwd }) });
  if (r.status !== 0 || !r.stdout?.trim()) return withGuardNote(undefined);
  return withGuardNote(contextOf(r.stdout));
}

/**
 * Run one guard on `payload`: its refusal, its ask, or null when it allows
 * the call, handing `onNote` the note it gives the agent on an allowed call.
 * A guard that fails otherwise, or does not answer within `timeoutMs`,
 * refuses.
 */
export async function runGuard(config: JhonPiConfig, guard: string, args: string[], payload: object, onNote?: (note: string) => void, timeoutMs = HOOK_TIMEOUT_MS): Promise<Decision> {
  // Spawned, not spawnSync: the TUI keeps drawing while the guard judges (T102).
  const r = await runAsync(config.jhon, ["hook", guard, ...args], { input: JSON.stringify(payload), timeoutMs });
  // Killed on the timeout: whatever it printed is not a verdict.
  if (r.timedOut) {
    // Never "try again" (T268, L1762): a check too slow for the timeout is
    // as slow on the next call, and the owner's agent retried five times.
    return { block: true, reason: `jhon hook ${guard} did not answer in ${timeoutMs / 1000}s; the call is refused. Do not run the same call again: run \`jhon doctor\` (and \`jhon deliver check <change>\` for a PR) to find what is slow.` };
  }
  if (r.status === 0) {
    if (!ASK.test(r.stdout ?? "")) {
      const note = r.stdout?.trim() ? contextOf(r.stdout) : undefined;
      if (note && onNote) onNote(note);
      return null;
    }
    try { return { ask: JSON.parse(r.stdout).hookSpecificOutput.permissionDecisionReason }; } catch { return { ask: r.stdout.trim() }; }
  }
  const why = (r.stderr ?? "").trim() || (r.error ? String(r.error) : `exit ${r.status}`);
  return { block: true, reason: r.status === 2 ? why : `${guard} failed (${why}); the call is refused` };
}

/** The guards' verdict on one pi tool call: a refusal, an ask, or null. */
export async function judge(toolName: string, input: Record<string, unknown>, ctx: CallContext): Promise<Decision> {
  if (ctx.tools && !ctx.tools.includes(toolName)) {
    return { block: true, reason: `jhon: ${toolName} is not one of this agent's tools (${ctx.tools.join(", ")}); the call is refused` };
  }
  const call = claudeCall(toolName, input);
  if (call === null) return null;
  if ("refuse" in call) return { block: true, reason: call.refuse };
  const payload = {
    hook_event_name: "PreToolUse", tool_name: call.tool_name, tool_input: call.tool_input, cwd: ctx.cwd,
    ...(ctx.child ? { agent_id: "pi-child" } : { permission_mode: ctx.interactive ? "default" : "dontAsk" }),
    ...(ctx.launchPrompt ? { launch_prompt: ctx.launchPrompt } : {}),
    // The session a folder's session grant names (grant.SessionGranted).
    ...(ctx.sessionId ? { session_id: ctx.sessionId } : {}),
  };
  const { config } = ctx;
  // One process judges the call (docket AY B4): `jhon hook pre-tool` applies
  // each guard to the tools its matcher takes. pi's payloads carry no
  // agent_type, so `--agent *` scopes every guard to this session.
  const guards = ["safety-guard", "vcs-guard", ...config.vcsDirs, "framework-guard", ...config.protected, "secrets-guard", "route-guard"];
  if (ctx.launchPrompt) guards.push("scope-guard");
  // A note on an allowed call waits for its result (formatEdited).
  return runGuard(config, "pre-tool", ["--agent", "*", ...guards], payload, (note) => guardNotes.set(callKey(toolName, input, ctx.cwd), note), ctx.hookTimeoutMs);
}

/** How a program the model named starts: as given, through another program's argv, or not at all (the refusal). */
export type Started = { program: string; args: string[] } | { refuse: string };

/**
 * The argv that runs an inspect line (T271): bash reads the very string the
 * inspect guard judged, as one argument, with no profile, rc file, BASH_ENV
 * or ENV of its own that could run anything the guard did not read.
 */
export function inspectArgv(command: string): string[] {
  return ["env", "-u", "BASH_ENV", "-u", "ENV", "bash", "--noprofile", "--norc", "-c", command];
}

/**
 * Run a line the inspect guard admits (read-only by construction, T271)
 * through bash, as inspectArgv gives it; a long one never freezes the TUI.
 * `start` says how that argv starts in this session (lib/sandbox.ts: in the
 * sandbox with `sandbox.mode` on, or refused where it cannot run); without
 * it, the program itself.
 */
export async function inspect(command: string, config: JhonPiConfig, cwd: string, start?: (program: string, args: string[]) => Promise<Started>): Promise<{ ok: boolean; text: string }> {
  const verdict = await runGuard(config, "inspect-guard", config.inspectAllow, { tool_name: "Bash", tool_input: { command }, cwd });
  if (verdict && "block" in verdict) return { ok: false, text: verdict.reason };
  const [program, ...args] = inspectArgv(command);
  const started = start ? await start(program, args) : { program, args };
  if ("refuse" in started) return { ok: false, text: started.refuse };
  const r = await runAsync(started.program, started.args, { cwd });
  if (r.error) return { ok: false, text: `inspect: could not run ${program}: ${r.error.message}` };
  const text = `${r.stdout ?? ""}${r.stderr ? `\n${r.stderr}` : ""}`.trim();
  const LIMIT = 50_000;
  return { ok: r.status === 0, text: text.length > LIMIT ? `${text.slice(0, LIMIT)}\n… (truncated at ${LIMIT} characters)` : text || `(exit ${r.status}, no output)` };
}
