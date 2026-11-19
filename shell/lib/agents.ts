/**
 * pi/agents.ts — Jhon's named sub-agents for pi.
 *
 * Each agent is a file `<agent dir>/agents/<name>.md` the renderer writes: a
 * frontmatter (name, description, model, optional thinking, a block list of
 * pi tool names, optional `bounded: true`) and a body that is the agent's
 * prompt. Any other key is ignored: no agent has a turn cap (T131), so a
 * `maxTurns` line an older install left there means nothing. The `subagent` tool runs one as a child pi process, one-shot, in the
 * parent's working directory. The child's session is saved beside the
 * parent's, so `jhon audit` reads what it did (childSession): under
 * `<parent session file without .jsonl>/subagents/`, with the id the subagent
 * call gives it and a `<id>.meta.json` naming the agent, the call, and the
 * parent session. A parent without a saved session runs its children without
 * one too. The child loads
 * this same extension (it shares PI_CODING_AGENT_DIR), so the guards judge its
 * calls; JHON_PI_CHILD=1 marks it a child and JHON_PI_AGENT names it.
 *
 * A bounded agent writes only the surfaces its handoff names, so a task for
 * one without a `## Allowed edit surfaces` heading is refused before it runs,
 * and its task goes to the child in a file (JHON_PI_TASK_FILE) that the
 * child's scope guard reads as its launch prompt; a continue that carries
 * the heading again replaces that list (continuedScope). JHON_PI_TOOLS names the
 * tools the child may call; its extension refuses any other.
 */

import { spawn } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { recordEvent, type Attributes } from "./telemetry.ts";
import { childContext, leanBody } from "./child-context.ts";
import { SESSION_PROFILE_ENV } from "./session-profile.ts";
import { withoutLaunchContext } from "./launch-context.ts";

export type AgentDef = {
  name: string;
  description: string;
  model: string;
  thinking?: string;
  tools: string[];
  bounded: boolean;
  /** The agent's prompt: everything after the frontmatter. */
  body: string;
};

export const AGENTS_DIR = "agents";
const NAME = /^[A-Za-z0-9][A-Za-z0-9_.-]*$/;
const SURFACES = /^##\s+Allowed edit surfaces\s*$/m;

/** A frontmatter scalar: a JSON-quoted string, a single-quoted string, true/false, or the bare text. */
function scalar(raw: string): string | boolean {
  const v = raw.trim();
  if (v.startsWith("\"")) return JSON.parse(v);
  if (v.startsWith("'") && v.endsWith("'") && v.length >= 2) return v.slice(1, -1).replace(/''/g, "'");
  if (v === "true") return true;
  if (v === "false") return false;
  return v;
}

/** Parse the YAML subset the renderer writes: `key: scalar`, `key: [a, b]`, and `key:` followed by `- item` lines. */
export function parseFrontmatter(text: string): { fields: Record<string, string | boolean | string[]>; body: string } {
  const m = /^---\r?\n([\s\S]*?)\r?\n---[ \t]*(?:\r?\n|$)/.exec(text);
  if (!m) throw new Error("no frontmatter (the file must start with a --- block)");
  const fields: Record<string, string | boolean | string[]> = {};
  let list: string[] | null = null;
  for (const line of m[1].split(/\r?\n/)) {
    if (!line.trim() || line.trim().startsWith("#")) continue;
    const item = /^\s+-\s*(.*)$/.exec(line) ?? /^-\s+(.*)$/.exec(line);
    if (item && list) {
      list.push(String(scalar(item[1])));
      continue;
    }
    const kv = /^([A-Za-z_][\w-]*)\s*:\s*(.*)$/.exec(line);
    if (!kv) throw new Error(`unreadable frontmatter line: ${line}`);
    const [, key, value] = kv;
    if (value.trim() === "") {
      list = [];
      fields[key] = list;
    } else if (value.trim().startsWith("[")) {
      const inner = value.trim().replace(/^\[|\]$/g, "").trim();
      fields[key] = inner ? inner.split(",").map((s) => String(scalar(s))) : [];
      list = null;
    } else {
      fields[key] = scalar(value);
      list = null;
    }
  }
  return { fields, body: text.slice(m[0].length).replace(/^\s*\n/, "") };
}

/** Read one agent definition; throws naming what is wrong. */
export function parseAgent(text: string, fallbackName: string): AgentDef {
  const { fields, body } = parseFrontmatter(text);
  const str = (k: string) => (typeof fields[k] === "string" ? (fields[k] as string) : undefined);
  const model = str("model");
  if (!model) throw new Error("its frontmatter names no model");
  const tools = fields.tools === undefined ? [] : fields.tools;
  if (!Array.isArray(tools)) throw new Error("its tools are not a list");
  return {
    name: str("name") || fallbackName, description: str("description") ?? "", model,
    ...(str("thinking") ? { thinking: str("thinking") } : {}),
    tools, bounded: fields.bounded === true, body: body.trim(),
  };
}

/** The names of the agents in `<agentDir>/agents`, sorted. */
export function knownAgents(agentDir: string): string[] {
  try {
    return readdirSync(join(agentDir, AGENTS_DIR)).filter((f) => f.endsWith(".md")).map((f) => f.slice(0, -3)).filter((n) => NAME.test(n)).sort();
  } catch {
    return [];
  }
}

/** The named agent, or an error that lists the agents there are. */
export function resolveAgent(agentDir: string, name: string): AgentDef {
  const known = knownAgents(agentDir);
  const file = join(agentDir, AGENTS_DIR, `${name}.md`);
  if (!NAME.test(name) || !existsSync(file)) {
    throw new Error(`jhon: no sub-agent named "${name}"; known agents: ${known.length ? known.join(", ") : "(none)"}`);
  }
  try {
    return parseAgent(readFileSync(file, "utf8"), name);
  } catch (e) {
    throw new Error(`jhon: the sub-agent "${name}" (${file}) is malformed: ${(e as Error).message}`);
  }
}

/**
 * The scope a continue leaves a sub-agent with (T133): `message` itself when
 * it carries a `## Allowed edit surfaces` heading, a corrected list replacing
 * the one before it; else the `scope` it had.
 */
export function continuedScope(scope: string, message: string): string {
  return SURFACES.test(message) ? message : scope;
}

/** Null when `task` may go to `agent`, else why not. */
export function refuseTask(agent: AgentDef, task: string): string | null {
  if (!task.trim()) return `jhon: the task for "${agent.name}" is empty`;
  if (agent.bounded && !SURFACES.test(task)) {
    return `jhon: "${agent.name}" is a bounded agent: its task must carry a "## Allowed edit surfaces" heading listing the files it may write; none was found, so the task is refused`;
  }
  return null;
}

/** Where a child's session is saved, and the link the audit reads back to the parent and the call. */
export type ChildSession = { dir: string; id: string; meta: { agentType: string; toolUseId: string; parentSession: string } };

/** The parent session as pi's session manager reports it; either may be absent (an in-memory session). */
export type ParentSession = { file?: string; id?: string };

/**
 * The child session for the subagent call `toolCallId` of a parent saved at
 * `parent.file` (`…/<timestamp>_<id>.jsonl`): the directory
 * `…/<timestamp>_<id>/subagents`, and a session id pi accepts (letters,
 * digits, `.`, `_`, `-`, starting and ending alphanumeric) made from the call
 * id, which the meta file keeps verbatim. Null when the parent saves no
 * session or the call has no id.
 */
export function childSession(agent: AgentDef, parent: ParentSession | undefined, toolCallId: string): ChildSession | null {
  const file = parent?.file;
  if (!file || !file.endsWith(".jsonl") || !parent?.id) return null;
  const id = toolCallId.replace(/[^A-Za-z0-9._-]+/g, "-").replace(/^[^A-Za-z0-9]+|[^A-Za-z0-9]+$/g, "");
  if (!id) return null;
  return { dir: join(file.slice(0, -".jsonl".length), "subagents"), id, meta: { agentType: agent.name, toolUseId: toolCallId, parentSession: parent.id } };
}

/** What a child receives besides its definition: the skill paths the parent resolved, and its filtered context (child-context.ts). */
export type ChildBase = { skills?: string[]; context?: string };

/**
 * The pi argv that runs `agent` on `task`, one-shot: the final assistant text
 * goes to stdout; saved as `session` when given. The child starts lean
 * (T115f): no skills catalog (`--no-skills`, only the parent's exact skill
 * paths as `--skill`), no raw context files (`--no-context-files`, the
 * filtered `base.context` appended instead), and its body without the
 * orchestrator-only sections or, with no memory tool, the memory protocol.
 */
export function childArgs(agent: AgentDef, task: string, session?: ChildSession | null, mode: "print" | "json" = "print", base: ChildBase = {}): string[] {
  // json streams pi's JSONL events (docs/json.md), which a background run reads as its activity.
  const output = mode === "json" ? ["--mode", "json"] : ["--print"];
  const args = [...output, ...(session ? ["--session-dir", session.dir, "--session-id", session.id] : ["--no-session"]), "--model", agent.model];
  if (agent.thinking) args.push("--thinking", agent.thinking);
  if (agent.tools.length) args.push("--tools", agent.tools.join(","));
  args.push("--no-skills", "--no-context-files");
  for (const skill of base.skills ?? []) if (skill) args.push("--skill", skill);
  const body = leanBody(agent.body, agent.tools);
  if (body) args.push("--append-system-prompt", body);
  if (base.context) args.push("--append-system-prompt", base.context);
  // `--` ends option parsing, so a task beginning with `-` is still the prompt;
  // a leading space keeps a task beginning with `@` from naming a file.
  args.push("--", task.startsWith("@") ? ` ${task}` : task);
  return args;
}

/**
 * The child's environment: the parent's, marked as a child running `agent`,
 * with the tools it may call and, for a bounded agent, the file holding its
 * task. Nothing the parent inherited from an outer child survives. With
 * `sessionProfile` (T120), the profile the launching session uses, which the
 * child's own sub-agents follow (lib/session-profile.ts); a session on the
 * default passes none.
 */
export function childEnv(agent: AgentDef, taskFile?: string, base: NodeJS.ProcessEnv = process.env, sessionProfile?: string): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = { ...base, JHON_PI_CHILD: "1", JHON_PI_AGENT: agent.name };
  delete env.JHON_PI_TOOLS;
  delete env.JHON_PI_TASK_FILE;
  delete env[SESSION_PROFILE_ENV];
  if (agent.tools.length) env.JHON_PI_TOOLS = agent.tools.join(",");
  if (taskFile) env.JHON_PI_TASK_FILE = taskFile;
  if (sessionProfile) env[SESSION_PROFILE_ENV] = sessionProfile;
  return env;
}

export type ChildResult = { ok: boolean; text: string };

const LIMIT = 50_000;
const clip = (t: string) => (t.length > LIMIT ? `${t.slice(0, LIMIT)}\n… (truncated at ${LIMIT} characters)` : t);

/**
 * Run `agent` on `task` in `cwd` as a child pi process; its output, or why it
 * failed. With `session`, the child's session is saved there, its meta file
 * written first so that a child that fails is still found.
 */
/** One record of pi's JSON event stream (docs/json.md). */
export type ChildEvent = { type?: unknown; [key: string]: unknown };

/** The text of an assistant message's text blocks, or "" for any other message. */
function assistantText(message: unknown): string {
  const m = message as { role?: unknown; content?: unknown } | undefined;
  if (m?.role !== "assistant" || !Array.isArray(m.content)) return "";
  return m.content.filter((b: any) => b?.type === "text" && typeof b.text === "string").map((b: any) => b.text).join("");
}

/** How a sub-agent runs, beyond its task and cwd. */
export type RunOptions = {
  /** Cancels the child. */
  signal?: AbortSignal;
  /** Where its session is saved; unsaved when absent. */
  session?: ChildSession | null;
  /** Receives each of pi's JSON events as it arrives; the child prints its answer instead when absent. */
  onEvent?: (event: ChildEvent) => void;
  /** What a continued agent's scope guard reads: the task it was launched with, or the last continue that carried its own `## Allowed edit surfaces` (continuedScope); the task when absent. */
  scope?: string;
  /** The child is stopped, and fails, past this many seconds. */
  timeoutSeconds?: number;
  /** The exact skill paths the parent resolved for this task; the child loads only these. */
  skills?: string[];
  /** The profile the launching session uses (T120), which the child's own sub-agents follow; none on the default. */
  sessionProfile?: string;
};

/**
 * Runs the sub-agent to its end. With onEvent the child streams pi's JSONL
 * events, each handed over as it arrives, and the answer is the last
 * assistant message's text; without it the child prints its answer. A
 * bounded agent's scope guard reads `scope`, the task it was launched with,
 * which a continued agent keeps while `task` is the new message, unless a
 * continue carried a corrected list (continuedScope). Past its
 * timeout the child is stopped and the run fails, keeping what it had said.
 */
export function runAgent(agent: AgentDef, task: string, cwd: string, options: RunOptions = {}): Promise<ChildResult> {
  const { signal, onEvent, scope = task, timeoutSeconds, skills, sessionProfile } = options;
  let session = options.session;
  const program = process.env.JHON_PI_BIN || "pi";
  if (session) {
    try {
      mkdirSync(session.dir, { recursive: true });
      writeFileSync(join(session.dir, `${session.id}.meta.json`), `${JSON.stringify(session.meta)}\n`);
    } catch {
      session = null; // the parent's session directory is not writable: the child runs unsaved
    }
  }
  const taskDir = agent.bounded ? mkdtempSync(join(tmpdir(), "jhon-pi-task-")) : undefined;
  const taskFile = taskDir ? join(taskDir, "task.md") : undefined;
  if (taskFile) writeFileSync(taskFile, scope, { mode: 0o600 });
  return new Promise((resolve) => {
    let out = "", err = "";
    let settled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const started = Date.now();
    let turns = 0;
    const done = (r: ChildResult, outcome: SubagentOutcome) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      if (taskDir) rmSync(taskDir, { recursive: true, force: true });
      recordEvent("jhon.subagent", subagentAttributes(agent, outcome, Date.now() - started, onEvent ? turns : undefined, session?.id), outcome === "completed" ? "INFO" : "WARN");
      resolve(r);
    };
    // Computed once, before the child starts: its system prompt stays byte-identical for the whole run.
    const agentDir = process.env.PI_CODING_AGENT_DIR || join(homedir(), ".pi", "agent");
    const base: ChildBase = { skills, context: childContext(cwd, agentDir, agent.tools) };
    const child = spawn(program, childArgs(agent, task, session, onEvent ? "json" : "print", base), { cwd, env: childEnv(agent, taskFile, process.env, sessionProfile), stdio: ["ignore", "pipe", "pipe"] });
    // JSONL framing: records end at LF only (docs/json.md), so the decoder splits on "\n" itself.
    let pending = "", answer = "";
    const record = (line: string) => {
      if (!line.trim()) return;
      let event: ChildEvent;
      try { event = JSON.parse(line.replace(/\r$/, "")); } catch { return; }
      if (event.type === "message_end" && (event.message as { role?: unknown } | undefined)?.role === "assistant") turns++;
      if (event.type === "message_end") answer = assistantText(event.message) || answer;
      try { onEvent?.(event); } catch { /* a viewer's failure never stops the run */ }
    };
    const abort = () => { child.kill("SIGTERM"); done({ ok: false, text: `jhon: the sub-agent "${agent.name}" was cancelled` }, "cancelled"); };
    if (timeoutSeconds && timeoutSeconds > 0) {
      timer = setTimeout(() => {
        child.kill("SIGTERM");
        const said = (onEvent ? answer : out).trim();
        done({ ok: false, text: clip(`jhon: the sub-agent "${agent.name}" ran past its ${timeoutSeconds}s timeout and was stopped${said ? `; it had said:\n${said}` : ""}`) }, "timeout");
      }, timeoutSeconds * 1000);
      timer.unref?.();
    }
    if (signal?.aborted) abort();
    signal?.addEventListener("abort", abort, { once: true });
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (d: string) => {
      if (!onEvent) { out += d; return; }
      pending += d;
      const lines = pending.split("\n");
      pending = lines.pop() ?? "";
      for (const line of lines) record(line);
    });
    child.stderr.on("data", (d) => { err += d; });
    child.on("error", (e) => done({ ok: false, text: `jhon: could not run ${program} for "${agent.name}": ${e.message}` }, "failed"));
    child.on("close", (code, sig) => {
      signal?.removeEventListener("abort", abort);
      if (onEvent) { record(pending); out = answer; }
      const text = out.trim();
      if (code === 0) {
        return done({ ok: true, text: clip(text || "(the sub-agent printed nothing)") }, "completed");
      }
      done({ ok: false, text: clip(childFailure(agent, code, sig, [text, err.trim()].filter(Boolean).join("\n"))) }, "failed");
    });
  });
}

/**
 * What the parent hears of a child that exited non-zero: its exit and what it
 * printed. A child runs on an explicit --model, so pi never falls back for it:
 * with no credential for the model's provider pi ends the run before any call
 * (`No API key found for <provider>.`). The result then says what that line
 * proves, the provider without a credential, and the model the agent was
 * launched on, whichever provider the line names (T130, L920 B1, L926).
 */
export function childFailure(agent: Pick<AgentDef, "name" | "model">, code: number | null, sig: string | null, why: string): string {
  const head = `jhon: the sub-agent "${agent.name}" exited ${sig ? `on ${sig}` : code}`;
  const provider = /^No API key found for (.+?)\.$/m.exec(why)?.[1];
  if (provider) return `${head}: pi found no credential for the provider ${provider}; the agent was launched on the model ${agent.model}.\n${why}`;
  return `${head}${why ? `:\n${why}` : ""}`;
}

/** How a sub-agent's run ended: done, past its timeout, cancelled, or failed. Events from before T155 also say `context-cap` (the child cap T122 had), and from before T131 `turn-cap`, which the engine still counts. */
export type SubagentOutcome = "completed" | "context-cap" | "timeout" | "cancelled" | "failed";

/** A sub-agent's run as its jhon.subagent event says it; turns only when they were counted (a JSON run). */
export function subagentAttributes(agent: AgentDef, outcome: SubagentOutcome, durationMs: number, turns?: number, session?: string): Attributes {
  return {
    "gen_ai.agent.name": agent.name,
    "jhon.outcome": outcome,
    "jhon.duration_ms": durationMs,
    ...(turns !== undefined ? { "jhon.turns": turns } : {}),
    ...(session ? { "session.id": session } : {}),
  };
}

/** A saved sub-agent to continue: its definition; its session; where it ran; and its scope, the task it was launched with or the last continue that carried its own `## Allowed edit surfaces`. */
export type Continuation = { agent: AgentDef; session: ChildSession; cwd: string; scope: string };

type SessionRecord = { type?: unknown; id?: unknown; cwd?: unknown; message?: { role?: unknown; content?: unknown } };

/** The text of a user message's content: a string, or its text blocks. */
function userText(content: unknown): string {
  if (typeof content === "string") return content;
  return Array.isArray(content) ? content.filter((b: any) => b?.type === "text" && typeof b.text === "string").map((b: any) => b.text).join("") : "";
}

/**
 * The saved sub-agent `id` of the parent session saved at `parent.file`
 * (childSession), to run again on its own session. It is refused, naming
 * why, when the parent is not saved or no such sub-agent was saved.
 */
export function continuation(agentDir: string, parent: ParentSession | undefined, id: string): Continuation {
  const file = parent?.file;
  if (!file || !file.endsWith(".jsonl")) throw new Error("jhon: this session is not saved, so its sub-agents cannot be continued; launch a fresh one with a handoff");
  const dir = join(file.slice(0, -".jsonl".length), "subagents");
  let meta: ChildSession["meta"];
  try { meta = JSON.parse(readFileSync(join(dir, `${id}.meta.json`), "utf8")); } catch {
    throw new Error(`jhon: no saved sub-agent ${id} in this session; subagent_continue takes the id subagent returned`);
  }
  const agent = resolveAgent(agentDir, meta.agentType);
  let records: SessionRecord[] = [];
  try {
    const name = readdirSync(dir).find((f) => f === `${id}.jsonl` || f.endsWith(`_${id}.jsonl`));
    if (name) records = readFileSync(join(dir, name), "utf8").split("\n").filter((l) => l.trim()).flatMap((l) => { try { return [JSON.parse(l)]; } catch { return []; } });
  } catch { /* no transcript: refused below */ }
  const header = records.find((r) => r.type === "session");
  if (!header || header.id !== id || typeof header.cwd !== "string") throw new Error(`jhon: the sub-agent ${id} left no session to continue; launch a fresh ${agent.name} with a handoff`);
  const messages = records.filter((r) => r.type === "message");
  // The task it was launched with, then each continue that carried its own list: the last one wins.
  // Only user messages count, which the launching side wrote; nothing the child said or read sets its scope.
  const [task = "", ...continues] = messages.filter((r) => r.message?.role === "user").map((r) => userText(r.message?.content));
  // T134: the launch's first message carried the brief and, after it, the blocks launchContext added; the scope is the brief.
  const scope = continues.reduce((kept, message) => continuedScope(kept, message), withoutLaunchContext(task.trimStart()));
  return { agent, session: { dir, id, meta }, cwd: header.cwd, scope };
}
