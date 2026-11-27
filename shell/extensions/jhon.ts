/**
 * shell/extensions/jhon.ts — Jhon's extension for pi, the entry point pi loads.
 *
 * `jhon start pi` runs plain pi on Jhon's own agent directory (the renderer,
 * the pi projection), whose settings load this file. It does these things:
 *
 * - The main session receives the orchestrator's prompt (the body, the
 *   persona, and the always-on contracts the renderer wrote to the file
 *   `jhon.json` names), appended to `systemPromptOptions.appendSystemPrompt`,
 *   which pi renders into the prompt every provider receives (pi-claude-bridge
 *   0.13 forwards that prompt whole; 0.9 forwarded only these options). A
 *   sub-agent, a child pi process (JHON_PI_CHILD=1), receives its own body
 *   through its definition instead.
 * - Every tool call is judged by Jhon's guards before it runs (pi/guards.ts),
 *   the same hooks Claude Code runs. A guard's refusal blocks the
 *   call; its ask is put to the owner when the main session has a UI, and is a
 *   refusal otherwise. A missing `jhon.json` fails closed.
 * - The `inspect` tool gives the reviewers (the `inspect` capability) their
 *   read-only inspectors without a shell: the inspect guard admits the
 *   command, which then runs as the argv its whitespace split gives.
 * - The `subagent` tool runs one of the agents in <agent dir>/agents as a
 *   child pi process (pi/agents.ts); the child loads this file too, so the
 *   guards judge its calls. `subagent_continue` runs a saved one again on its
 *   own session.
 * - The main session's background tasks (lib/background-tasks-tools.ts):
 *   commands, monitors, and background sub-agents in one registry, read and
 *   stopped with task_list, task_result, and task_stop, and shown by
 *   /jhon:agents (sub-agents) and /jhon:jobs (commands and monitors).
 * - The `ask_user` tool puts one to four questions, with described options,
 *   multiple choice, and typed answers, to the owner in the interactive main
 *   session, and fails where no one can answer (pi/ask.ts).
 * - The `m365_read` tool reads Microsoft 365 (the HUs in SharePoint), which
 *   pi-claude-bridge never reaches, through a headless Claude Code limited
 *   on its command line to the read tools (lib/m365.ts).
 *
 * - The main session reads and writes the project's file memory, the one
 *   Claude Code keeps (lib/memory.ts): its index MEMORY.md is appended to
 *   the prompt on every turn, so either runtime picks up what the other saved.
 * - The main session records how it runs (SESSION_ENTRY: pi's mode and the
 *   orchestrator) so `jhon audit` states its kind.
 * - A session may use a profile of its own (T120, lib/session-profile.ts):
 *   the panel's `u` records it in the session, and its sub-agents, its
 *   orchestrator's model, its delegation mode and its header follow it; the
 *   default profile, which `d` sets, is what every other session reads.
 *
 * MCP servers (Engram memory, codegraph) are pi's own: the renderer writes
 * them to <agent dir>/mcp.json, and a child, sharing the agent directory,
 * connects them too.
 *
 * The colors are rose ink: Jhon's own fixed values, which the render seeds.
 * Following a desktop palette is an opt-in integration (L197, T55): only with
 * integrations.theme.palette = "desktop" ("ryoku", its older name) does `jhon palette` rewrite
 * <agent dir>/themes/rose-ink.json from it whenever the wallpaper changes
 * (lib/rose-ink.ts; pi hot-reloads it), and, where one stands
 * (lib/desktop-palette.ts), the header is the rashin poster Neovim draws,
 * painted in those colors. Off, pi keeps its own header and nothing reads
 * the desktop's files.
 *
 * /jhon:status [<change>] opens the LDD detail (the change, its route and
 * risk, and its actions), never a toast; /jhon:grants the project's grants
 * (the permission mode is Customize's, L214);
 * /jhon:profile <args> runs the jhon CLI; /jhon:dashboard opens the
 * dashboard at this session, starting `jhon dashboard` when it does not
 * answer. Every one runs without blocking pi.
 */

import { spawn, spawnSync } from "node:child_process";
import { existsSync, readFileSync, realpathSync } from "node:fs";
import { homedir, userInfo } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { PALETTE_PATH, NVIM_SPEC_PATH, poster, readPalette, readWordmark } from "../lib/desktop-palette.ts";
import { ownerShellNote } from "../lib/owner-shell.ts";
import { appendSessionOnce } from "../lib/ui/append-system-prompt.ts";
import { followPalette } from "../lib/rose-ink.ts";
import { askHosts, listenForHosts } from "../lib/host.ts";
import { CONFIG_FILE, claudeCall, contractMismatch, formatEdited, inspect, judge, loadConfig } from "../lib/guards.ts";
import { wireSandbox } from "../lib/sandbox.ts";
import { childSession, continuation, continuedScope, knownAgents, refuseTask, resolveAgent, runAgent, type AgentDef, type ChildEvent, type ChildResult, type ChildSession, type RunOptions } from "../lib/agents.ts";
import { activityLine } from "../lib/background-tasks.ts";
import { askUser, parseQuestions, MAX_HEADER, MAX_OPTIONS, MAX_QUESTIONS, MIN_OPTIONS } from "../lib/ask.ts";
import { activeProfileName, profileDelegation } from "../lib/profiles.ts";
import { delegationPrompt, followMode, profileModeActions, writerFile } from "../lib/delegation-mode.ts";
import { ORCHESTRATOR, SESSION_PROFILE_CHANGED, SESSION_PROFILE_ENTRY, SESSION_PROFILE_ENV, SessionProfile, lostProfileNotice, orchestratorSwitchNotice, piRows, sessionMark, sessionProfileOnBranch, unavailableRefusal, type PiRow } from "../lib/session-profile.ts";
import { WriterBudget } from "../lib/writer-guard.ts";
import { ChildContextBudget, ChildReadBudget, readBudgetApplies, readBudgetKind } from "../lib/child-budget.ts";
import { waitingForOwner } from "../lib/attention.ts";
import { formatTaskDuration, registerBackgroundTasks } from "../lib/background-tasks-tools.ts";
import { counted } from "../lib/ui-use.ts";
import { shellSettings } from "../lib/settings.ts";
import { FULLSCREEN_OVERLAY } from "../lib/ui/fullscreen-view.ts";
import { memoryDir, projectRoot } from "../lib/memory.ts";
import { gitReadEnv } from "../lib/git-read.ts";
import { nativeReviewDisabled, registryCompleter, runReview } from "../lib/review-tool.ts";
import { m365Read } from "../lib/m365.ts";
import { delegationRenderers, type DelegationProfile, type DelegationStatus } from "../lib/ui/delegation-card.ts";
import { reviewRenderers } from "../lib/ui/review-card.ts";
import { createWorktree, sameRepoWorktree, worktreeNote } from "../lib/worktree.ts";
import { completeWord, lddChanges, PROFILE_ACTIONS } from "../lib/ui/jhon-commands.ts";
import { LDD_SKILL, lddCompletions, lddPrompt } from "../lib/ldd-command.ts";
import { commandPrompts } from "../lib/command-prompt.ts";
import { STUDIO_SETTING, VIDEO_DESCRIPTION, offForSession, studioSkill, videoPrompt } from "../lib/studio-command.ts";
import { ModelChoice, settingsDefaults, UNCHOSEN_ENDED, UNCHOSEN_HINT, UNCHOSEN_NO, UNCHOSEN_YES, unchosenQuestion } from "../lib/model-choice.ts";
import { JhonPanel, jhonPanelKey, modelsOffered, registryModels, type ProfilesData, type View } from "../lib/ui/jhon-panel.ts";
import { compactAt } from "../lib/ui/bars.ts";
import { FRESH_SESSION_ENTRY, FreshSessionLatch } from "../lib/fresh-session.ts";
import { MainCompaction } from "../lib/compaction.ts";
import { BACKSTOP_ENTRY, ContextBackstop } from "../lib/context-backstop.ts";
import { lddFromStatus, lddLanes, lddOfSession, readLddStatus, refreshSelection, selectedChange, type Lane } from "../lib/ui/ldd-status.ts";
import { launchContext } from "../lib/launch-context.ts";
import { recentSessionRoots } from "../lib/ui/session-changes.ts";
import { registerSubagentWorktree } from "../lib/ui/session-worktree-registry.ts";
import { closeWithRepaint, GRANTS_OVERLAY } from "../lib/ui/overlay.ts";
import { openInExternalEditor } from "../lib/ui/jhon-panel-editor.ts";
import { execFile } from "node:child_process";
import { recordEvent } from "../lib/telemetry.ts";
import { lddReminder, reminderAnswer } from "../lib/ldd-reminder.ts";
import { parseSessionBrief, sessionBriefs } from "../lib/session-brief.ts";
import { codegraphIndexed, withoutCodegraph } from "../lib/codegraph.ts";
import { ROUTE_MESSAGE, SessionPrompt, compactSkills, memoryIndexPrompt, splitRoute } from "../lib/session-prompt.ts";
import { ToolPolicy, enabledOnBranch, toolsMoreTool } from "../lib/tools-deferred.ts";
import { CHANGE_ASK_MS, FULL_MESSAGE, changeMoment, fullOnBranch, fullTrigger, setContextLevel } from "../lib/lean-context.ts";
import { AnnounceSendBack, engineStopRunner } from "../lib/announce-stop.ts";
import { fromTheShell } from "../lib/from-the-shell.ts";

export { CONFIG_FILE, claudeCall, inspect, judge, loadConfig } from "../lib/guards.ts";
export { activeProfileName } from "../lib/profiles.ts";
export type { CallContext, ClaudeCall, Decision, JhonPiConfig } from "../lib/guards.ts";

/** The entry a main session records at its start: pi's mode and the agent it runs as; `jhon audit` reads it. */
export const SESSION_ENTRY = "jhon.session/v1";

/** Where `jhon dashboard` answers (its default port). */
export const DASHBOARD = "http://127.0.0.1:7480";

/** What /jhon:dashboard reaches the world through, so a test fakes each step. */
export interface DashboardDeps {
  /** Whether the dashboard answers at url. */
  probe(url: string): Promise<boolean>;
  /** Starts `jhon dashboard` detached; false when it could not. */
  start(): Promise<boolean>;
  /** Opens url in the browser. */
  open(url: string): void;
  /** Whether a browser can open here (hasDisplay). */
  display: boolean;
  wait(ms: number): Promise<void>;
  /** Where the dashboard answers: DASHBOARD unless a test points elsewhere. */
  base?: string;
  /** Whether the Jhon dashboard answering at base is an older build than this shell's jhon (dashboardIsStale). */
  stale?(base: string): Promise<boolean>;
}

/** `jhon dashboard`'s arguments to answer at base: its port, 7480 when base names none. */
export function dashboardStartArgs(base: string): string[] {
  let port = "";
  try { port = new URL(base).port; } catch { port = ""; }
  return ["dashboard", "--port", port || "7480"];
}

/** Whether a Jhon dashboard answers at url: a 200 that says so (X-Jhon-Dashboard), not any server on the port. */
export async function probeDashboard(url: string, get: (url: string, init?: RequestInit) => Promise<Response> = fetch): Promise<boolean> {
  try {
    const res = await get(url, { signal: AbortSignal.timeout(1000) });
    return res.ok && res.headers.get("x-jhon-dashboard") === "1";
  } catch {
    return false;
  }
}

/**
 * Whether the Jhon dashboard at base runs another build than jhon (T15.16,
 * L180): it has no /api/version (a release before it) or names another
 * binary. False when it cannot be told: nothing is replaced on a guess.
 */
export async function dashboardIsStale(base: string, jhon: string | undefined, get: (url: string, init?: RequestInit) => Promise<Response> = fetch): Promise<boolean> {
  if (!jhon) return false;
  try {
    const res = await get(`${base}/api/version`, { signal: AbortSignal.timeout(1000) });
    if (res.headers.get("x-jhon-dashboard") !== "1") return false;
    if (!res.ok) return true;
    const v = (await res.json()) as { binary?: unknown };
    return typeof v.binary === "string" && v.binary !== jhon;
  } catch {
    return false;
  }
}

/** The real path of the jhon bin names (a path, or a command on PATH); undefined when not found. */
export function resolveJhon(bin: string, env: NodeJS.ProcessEnv = process.env): string | undefined {
  const candidates = bin.includes("/") ? [bin] : (env.PATH ?? "").split(":").filter(Boolean).map((d) => join(d, bin));
  for (const c of candidates) {
    try { return realpathSync(c); } catch { /* the next one */ }
  }
  return undefined;
}

/** Whether a browser can open: always on macOS, an X11 or Wayland display elsewhere. */
export function hasDisplay(env: NodeJS.ProcessEnv = process.env, platform: string = process.platform): boolean {
  return platform === "darwin" || Boolean(env.DISPLAY || env.WAYLAND_DISPLAY);
}

/**
 * /jhon:dashboard (L138, answer 5): the dashboard at this session's page
 * (`#/s/<id>`, its home with no id), started detached when it does not
 * answer and asked again every 250 ms for 2 s. One from an older build
 * (stale) is replaced the same way: `jhon dashboard` stops it when it is
 * proven Jhon's and serves in its place, asked every 250 ms for 6 s; when it
 * is not replaced it is opened all the same, and the owner told how. With
 * no display the address is said instead of a browser opened. What to tell
 * the owner.
 */
export async function openDashboard(id: string | undefined, deps: DashboardDeps): Promise<{ level: "info" | "error"; message: string }> {
  const base = deps.base ?? DASHBOARD;
  const url = id ? `${base}/#/s/${id}` : `${base}/`;
  let note = "";
  if (await deps.probe(`${base}/`)) {
    if (deps.stale && (await deps.stale(base))) {
      let started = false;
      try { started = await deps.start(); } catch { started = false; }
      let replaced = false;
      for (let i = 0; started && i < 24 && !replaced; i++) {
        await deps.wait(250);
        replaced = !(await deps.stale(base));
      }
      if (!replaced) note = `; an older dashboard answers there and was not replaced: run \`jhon ${dashboardStartArgs(base).join(" ")}\` in a terminal to see why`;
    }
  } else {
    let started = false;
    try { started = await deps.start(); } catch { started = false; }
    if (!started) return { level: "error", message: "jhon dashboard did not start: run `jhon dashboard` in a terminal to see why" };
    let up = false;
    for (let i = 0; i < 8 && !up; i++) {
      await deps.wait(250);
      up = await deps.probe(`${base}/`);
    }
    if (!up) return { level: "error", message: `jhon dashboard did not answer at ${base} after 2 s: run \`jhon dashboard\` in a terminal to see why` };
  }
  if (!deps.display) return { level: "info", message: `Jhon dashboard: ${url} (no display here to open a browser: open the address yourself)${note}` };
  deps.open(url);
  return { level: "info", message: `Jhon dashboard: ${url} (opened in your browser)${note}` };
}

/** True in a child pi process the `subagent` tool started. */
export const isChild = (env: NodeJS.ProcessEnv = process.env) => env.JHON_PI_CHILD === "1";

/**
 * A child's limits, as the parent's subagent tool set them (agents.ts): the
 * tools it may call and a bounded agent's task. A task file that cannot be
 * read leaves the scope as an empty task, which names no surface, so every
 * write in a repository is refused rather than allowed.
 */
export function childLimits(env: NodeJS.ProcessEnv = process.env): { tools?: string[]; launchPrompt?: string } {
  if (!isChild(env)) return {};
  const out: { tools?: string[]; launchPrompt?: string } = {};
  if (env.JHON_PI_TOOLS) out.tools = env.JHON_PI_TOOLS.split(",").filter(Boolean);
  if (env.JHON_PI_TASK_FILE) {
    try { out.launchPrompt = readFileSync(env.JHON_PI_TASK_FILE, "utf8"); } catch { out.launchPrompt = "## Allowed edit surfaces\n"; }
  }
  return out;
}

/** The prompt is appended to the structured prompt options, which reach every provider, pi-claude-bridge included, once. */
function appendOnce(options: { appendSystemPrompt?: string } | undefined, text: string): void {
  if (!options || !text || (options.appendSystemPrompt ?? "").includes(text)) return;
  options.appendSystemPrompt = options.appendSystemPrompt ? `${options.appendSystemPrompt}\n\n${text}` : text;
}

const git = (cwd: string, ...args: string[]) => {
  const r = spawnSync("git", ["-C", cwd, ...args], { encoding: "utf8", env: gitReadEnv() });
  return r.status === 0 ? r.stdout.trim() : "";
};

const text = (t: string) => ({ content: [{ type: "text", text: t }], details: undefined });

/**
 * A child's skills (T115f follow-up, L709): children start with --no-skills.
 * Agent definitions declare no skills field, so the rule is: a skill this
 * session resolved whose `skills/<name>/` path the agent's body names (jhon-builder,
 * jhon-checker and jhon-scout name the LDD skill's references), then the exact SKILL.md
 * paths the call passes. A passed path that is not a SKILL.md file is refused.
 */
export function childSkills(body: string, resolved: ReadonlyMap<string, string>, passed?: unknown): string[] {
  const out: string[] = [];
  for (const m of (body ?? "").matchAll(/skills\/([a-z0-9][a-z0-9-]*)\//g)) {
    const path = resolved.get(m[1]);
    if (path && !out.includes(path)) out.push(path);
  }
  if (passed === undefined) return out;
  if (!Array.isArray(passed)) throw new Error("skills: a list of SKILL.md paths");
  for (const p of passed) {
    if (typeof p !== "string" || basename(p) !== "SKILL.md" || !existsSync(p)) throw new Error(`skills: ${String(p)} is not a SKILL.md file`);
    if (!out.includes(p)) out.push(p);
  }
  return out;
}

/** The timeout_seconds parameter of the tools that run a sub-agent. */
const TIMEOUT = { type: "integer", minimum: 1, description: "Stop the sub-agent, and fail the call, after this many seconds; by default it runs until it returns." };

/** A valid timeout_seconds, or undefined when none was given. */
/** T81: a tool's `cwd`, the change's worktree; only a worktree of the session's own repository. */
const CWD = { type: "string", description: "The change's worktree (a git worktree of this repository) to run in; omit it for the session's folder." };
function at(ctx: any, dir: unknown): string {
  const session = ctx?.cwd ?? process.cwd();
  if (dir === undefined) return session;
  if (typeof dir !== "string" || !dir.trim()) throw new Error("jhon: cwd must name the change's worktree");
  return sameRepoWorktree(session, resolve(session, dir));
}

function timeoutOf(value: unknown): number | undefined {
  if (value === undefined) return undefined;
  if (typeof value !== "number" || !Number.isInteger(value) || value < 1) throw new Error("timeout_seconds must be a whole number of seconds, 1 or more");
  return value;
}

/** Lines of activity a running foreground sub-agent shows. */
const PROGRESS_LINES = 8;

/**
 * Run a sub-agent in the foreground. When pi passes onUpdate, the child
 * streams its events and the call shows what it is doing as it works: its
 * elapsed time and its latest activity (lib/background-tasks.ts).
 */
function foreground(agent: AgentDef, task: string, cwd: string, options: RunOptions, onUpdate: unknown): Promise<ChildResult> {
  if (typeof onUpdate !== "function") return runAgent(agent, task, cwd, options);
  const started = Date.now();
  const lines: string[] = [];
  const onEvent = (event: ChildEvent) => {
    const line = activityLine(event);
    if (!line) return;
    lines.push(line);
    if (lines.length > PROGRESS_LINES) lines.shift();
    onUpdate({ content: [{ type: "text", text: [`${agent.name} is working (${formatTaskDuration(Date.now() - started)})`, ...lines].join("\n") }], details: undefined });
  };
  return runAgent(agent, task, cwd, { ...options, onEvent });
}

/** A foreground sub-agent's answer, or its failure, naming the id subagent_continue takes when its session is saved. */
function answered(r: ChildResult, session: ChildSession | null): ReturnType<typeof text> {
  const out = session ? `${r.text}\n\n(sub-agent id ${session.id}: subagent_continue continues it)` : r.text;
  if (!r.ok) throw new Error(out);
  return text(out);
}

/** The session entry that records the owner's answer to a guard's question. */
export const AUTHORIZATION_ENTRY = "jhon-owner-authorization";

/** Each class an ask names: `push: git push; gh-write: gh pr create` → push, gh-write. */
export function askClasses(ask: string): string[] {
  return [...new Set(ask.split("; ").map((part) => part.split(":")[0]!.trim()).filter(Boolean))];
}

export const ANSWER_ONCE = "Yes, just this once (recorded in the session)";
export const ANSWER_GRANT = "Yes, and grant it for 30 min (recorded in the grants' history)";
export const ANSWER_NO = "No";
export const ANSWER_SESSION = "Yes, and allow writes in this folder for this session (recorded in the grants' history)";

/** Whether cwd is in a git repository: a folder that is in none is no project, and takes no grant for a while (T57a). */
function inRepo(cwd: string): boolean {
  return spawnSync("git", ["-C", cwd, "rev-parse", "--show-toplevel"], { encoding: "utf8", env: gitReadEnv() }).status === 0;
}

/** Whether `jhon grant --session` could take cwd as its folder: never /, HOME or a folder above it, nor jhon's state or a folder in or above it (grant.SessionFolder). */
function sessionGrantable(cwd: string): boolean {
  const real = (p: string) => { try { return realpathSync(p); } catch { return p; } };
  const within = (p: string, r: string) => p === r || p.startsWith(r.endsWith("/") ? r : `${r}/`);
  const xdg = process.env.XDG_STATE_HOME;
  const state = real(join(xdg && xdg.startsWith("/") ? xdg : join(homedir(), ".local", "state"), "jhon"));
  const dir = real(cwd), home = real(homedir());
  return dir !== "/" && !within(home, dir) && !within(state, dir) && !within(dir, state);
}

/**
 * The owner's answer to a guard's question, always on record: once (the
 * session records it), a 30-minute grant of the classes (jhon grant, the
 * grants' history), or no. In a folder that is no project, a write outside
 * the scratch roots is granted for the session instead (`jhon grant
 * outside-write --session`, revoked when the session ends), and nothing
 * else is granted for a while.
 */
async function ownerAnswer(ctx: any, ask: string, bin: string, session: string): Promise<{ allowed: boolean; scope?: "once" | "grant" | "session"; classes: string[]; error?: string }> {
  const classes = askClasses(ask);
  const cwd = ctx?.cwd ?? process.cwd();
  const folder = !inRepo(cwd);
  const options = !folder ? [ANSWER_ONCE, ANSWER_GRANT, ANSWER_NO]
    : session && sessionGrantable(cwd) && classes.length === 1 && classes[0] === "outside-write" ? [ANSWER_ONCE, ANSWER_SESSION, ANSWER_NO] : [ANSWER_ONCE, ANSWER_NO];
  const choice = await ctx.ui.select(`Jhon safety guard · ${ask}`, options);
  if (choice === ANSWER_ONCE) return { allowed: true, scope: "once", classes };
  if (!options.includes(choice) || (choice !== ANSWER_GRANT && choice !== ANSWER_SESSION)) return { allowed: false, classes };
  const r = choice === ANSWER_SESSION
    ? spawnSync(bin, ["grant", "outside-write", "--session", session], { cwd, encoding: "utf8" })
    : spawnSync(bin, ["grant", ...classes, "--for", "30m"], { cwd, encoding: "utf8" });
  if (r.status !== 0) return { allowed: false, classes, error: `${r.stderr || r.stdout || r.error?.message || "exit " + r.status}`.trim() };
  return { allowed: true, scope: choice === ANSWER_SESSION ? "session" : "grant", classes };
}

/**
 * `jhon <args>`'s JSON: jhon status exits 1 when it selects no change and
 * still prints it, so output that parses is the answer; none is the error.
 */
function jhonJson(bin: string, args: string[], cwd: string): Promise<any> {
  return new Promise((resolve, reject) =>
    execFile(bin, args, { cwd, encoding: "utf8", maxBuffer: 16 << 20 }, (error, stdout, stderr) => {
      try { resolve(JSON.parse(stdout)); } catch { reject(new Error(`${stderr || error?.message || "jhon printed nothing"}`.trim())); }
    }));
}

/**
 * Where the session's change is, as STATUS finds it (lddOfSession): pi's
 * folder, the worktree the session worked in last that selects one, or, for
 * a session that worked in none yet, the repository's worktree whose
 * tasks.md moved last; else pi's folder. Each folder's `jhon status --json`
 * is read once, and the one of the folder it names comes back as status, so
 * the panel shows it without reading it again.
 */
export async function changeRoot(ctx: any, cwd: string, bin = process.env.JHON_BIN || "jhon"): Promise<{ root: string; worktree?: string; status?: any }> {
  let roots: string[] = [];
  try { roots = recentSessionRoots(ctx.sessionManager.getSessionId(), ctx.sessionManager.getEntries()); } catch { /* No entries. */ }
  const read = new Map<string, any>();
  const found = await lddOfSession(cwd, roots, async (dir) => {
    const status = await jhonJson(bin, ["status", "--json"], dir);
    read.set(dir, status);
    return typeof status?.change === "string" ? lddFromStatus(status.change, status) : undefined;
  }, existsSync);
  const root = found?.root ?? cwd;
  return { root, ...(found?.worktree ? { worktree: found.worktree } : {}), ...(read.has(root) ? { status: read.get(root) } : {}) };
}

/**
 * The session's change, the one STATUS shows (T29): its selection refreshed
 * (extensions/ui.ts), both updated together, with its lanes; shared marks
 * it. Without STATUS (no UI), the panel selects it as STATUS would
 * (changeRoot).
 */
export async function sessionChange(ctx: any, cwd: string, bin = process.env.JHON_BIN || "jhon"): Promise<{ root: string; worktree?: string; change?: string; lanes?: Lane[]; status?: any; shared?: boolean }> {
  let session: string | undefined;
  try { session = ctx?.sessionManager?.getSessionId?.(); } catch { /* No session. */ }
  const shared = session ? await refreshSelection(session) : undefined;
  if (!shared) return changeRoot(ctx, cwd, bin);
  const s = shared.status;
  return { root: s?.root ?? cwd, shared: true, ...(s?.worktree ? { worktree: s.worktree } : {}), ...(s ? { change: s.change, lanes: s.lanes ?? [] } : {}) };
}

/** A word quoted for sh. */
function shellQuote(word: string): string {
  return /^[\w@%+=:,./-]+$/.test(word) ? word : `'${word.replace(/'/g, `'\\''`)}'`;
}

/** The base an affected run compares with: the remote's default branch (origin/HEAD), else main. */
function defaultBase(cwd: string): string {
  const r = spawnSync("git", ["-C", cwd, "rev-parse", "--abbrev-ref", "origin/HEAD"], { encoding: "utf8", env: gitReadEnv() });
  const ref = r.status === 0 ? r.stdout.trim().replace(/^origin\//, "") : "";
  return ref && ref !== "origin/HEAD" ? ref : "main";
}

export default function jhon(pi: any): void {
  // Its commands and shortcuts are counted (lib/ui-use.ts).
  pi = counted(pi);
  const agentDir = process.env.PI_CODING_AGENT_DIR || join(homedir(), ".pi", "agent");
  const config = loadConfig(agentDir);
  // The binary and this shell ship in one release; a binary that speaks another contract judges no call.
  const mismatch = config ? contractMismatch(config) : null;
  const child = isChild();
  const limits = childLimits();
  // A multiplexer bridge may announce its host at any time (jhon:host, lib/host.ts).
  if (!child) listenForHosts(pi.events);
  let stopPalette: (() => void) | undefined;
  // Only paths: nothing is read from them unless the settings follow the desktop palette.
  const colors = PALETTE_PATH(), spec = NVIM_SPEC_PATH();
  const bin = process.env.JHON_BIN || "jhon";
  // T128b (L894): the OS sandbox around bash, bash_background and monitor, pinned at each session start by the binary the guards call.
  const sandbox = wireSandbox(pi, { jhon: config && !mismatch ? config.jhon : undefined });
  // Claude Code's config dir, where the project memory both runtimes share lives.
  const claudeConfig = { CLAUDE_CONFIG_DIR: process.env.CLAUDE_CONFIG_DIR };
  // T115c/T115d: the prompt and the tool list, decided once per session (lib/session-prompt.ts, lib/tools-deferred.ts).
  const sessionPrompt = new SessionPrompt();
  const toolPolicy = new ToolPolicy(!child, (cwd) => codegraphIndexed(cwd));
  // T146h: whether this session started lean, and whether its full context has loaded since;
  // decided at its first run, from session-brief's context. fullBefore: a resumed branch holds it.
  let lean: { on: boolean; loaded: boolean } | undefined;
  let fullBefore = false;
  let leanSkills = "";
  // T115f follow-up (L709): the skills this session resolved, by name; a child gets only the ones its definition names.
  const resolvedSkills = new Map<string, string>();
  // T130 (L920 B1): the session never calls a model nobody chose (lib/model-choice.ts). A child
  // runs on an explicit --model, which pi never replaces.
  // The named defaults are the agent folder's and the project's (L926).
  const modelChoice = child ? undefined : new ModelChoice((cwd) => settingsDefaults(agentDir, cwd));
  /** Whether the owner's question is open: one at a time (L942 G1). */
  let asking = false;
  /** The held session's line, said to the owner; undefined when the session is on a chosen model. */
  const held = (ctx: any): string | undefined => {
    const line = modelChoice?.refused();
    if (line) ctx?.ui?.notify?.(`${line} ${UNCHOSEN_HINT}`, "error");
    return line;
  };
  if (modelChoice) {
    pi.on("model_select", async (event: any) => { modelChoice.picked(event); return undefined; });
    // L926 BREAK 2: held, a compaction and a tree summary would call the model too; pi cancels them here.
    pi.on("session_before_compact", async (_event: any, ctx: any) => (held(ctx) ? { cancel: true } : undefined));
    pi.on("session_before_tree", async (event: any, ctx: any) => (event?.preparation?.userWantsSummary && held(ctx) ? { cancel: true } : undefined));
  }
  // The jhon CLI's output, read without blocking pi: the panel's views and the session's profile.
  const jhonRun = (args: string[], cwd: string) => new Promise<string>((resolve, reject) =>
    execFile(bin, args, { cwd, encoding: "utf8", maxBuffer: 8 << 20 }, (error, stdout, stderr) =>
      error ? reject(new Error(`${stderr || stdout || error.message}`.trim())) : resolve(stdout)));
  // T120 (L980): the profile this session uses, apart from the default (lib/session-profile.ts). A
  // child hears its parent session's in its environment, so the sub-agents it launches follow it.
  const sessionProfile = new SessionProfile(async (cwd) => JSON.parse(await jhonRun(["profile", "json"], cwd)));
  if (child) sessionProfile.start(process.env[SESSION_PROFILE_ENV]);
  /** The header poster's facts, kept so its profile follows the session's. */
  let headerFacts: { profile: string } | undefined;
  /** The session's profile changed: its entry, the event the status bar hears, and the header. */
  const recordSessionProfile = (ctx: any, name: string | undefined) => {
    let session = "";
    try { session = ctx?.sessionManager?.getSessionId?.() ?? ""; } catch { /* No session. */ }
    pi.appendEntry?.(SESSION_PROFILE_ENTRY, { profile: name ?? null });
    pi.events?.emit?.(SESSION_PROFILE_CHANGED, { sessionId: session, ...(name ? { profile: name } : {}) });
    if (headerFacts) headerFacts.profile = sessionMark(name, activeProfileName());
  };
  /** What a change of the session's profile reaches to keep its delegation mode with it (lib/delegation-mode.ts). */
  const modeDeps = (ctx: any) => ({
    delegationOf: (name: string) => profileDelegation(name),
    sessionMode: () => sessionPrompt.frozenDelegation(),
    resetPrompt: () => sessionPrompt.reset(),
    notify: (text: string) => ctx?.ui?.notify?.(text, "info"),
  });
  /** L980 A9: the session's profile is gone (renamed or deleted): it is back on the default, said in one line. A child's parent says it. */
  const lostSessionProfile = (ctx: any, was: string) => {
    if (child) return;
    const active = activeProfileName();
    recordSessionProfile(ctx, undefined);
    ctx?.ui?.notify?.(lostProfileNotice(was, active), "warning");
    followMode(modeDeps(ctx), active);
  };
  /** A2: the agent as this session's profile runs it, the profile read again at each launch; itself in a session on the default, which reads nothing. */
  const profiled = async (agent: AgentDef, ctx: any): Promise<AgentDef> => {
    const name = sessionProfile.current();
    if (!name) return agent;
    const read = await sessionProfile.refresh(ctx?.cwd ?? process.cwd());
    if (read.state === "missing") lostSessionProfile(ctx, read.was);
    // A profile that cannot be read now and never was: no launch on a model this session did not choose.
    if (read.state === "unread" && !sessionProfile.known()) throw new Error(`jhon: this session uses the profile ${name}, which cannot be read (${read.why}), so "${agent.name}" was not launched; run \`jhon profile json\` to see why`);
    return sessionProfile.agent(agent);
  };
  /**
   * A3: the running orchestrator on the profile's model (pi.setModel) and thinking level
   * (pi.setThinkingLevel); pi saves neither as a default. A registry that cannot be read switches
   * nothing and says nothing.
   */
  const switchOrchestrator = async (ctx: any, row: PiRow): Promise<{ refused?: string; notice?: string }> => {
    const slash = row.model.indexOf("/");
    const provider = row.model.slice(0, slash), id = row.model.slice(slash + 1);
    const registry = ctx?.modelRegistry;
    if (slash <= 0 || typeof registry?.find !== "function" || typeof pi.setModel !== "function") return {};
    let changed = false;
    if (ctx?.model?.provider !== provider || ctx?.model?.id !== id) {
      let why = "pi has no such model, or no credential for its provider";
      let set = false;
      try { const model = registry.find(provider, id); set = Boolean(model) && (await pi.setModel(model)) === true; } catch (error) { why = (error as Error)?.message ?? String(error); }
      if (!set) return { refused: `the orchestrator could not switch to ${row.model} (${why}); nothing changed.` };
      changed = true;
    }
    let level: unknown;
    try { level = pi.getThinkingLevel?.(); } catch { level = undefined; }
    const setter = typeof pi.setThinkingLevel === "function";
    // pi picks a level of its own when the model changes, so the profile's is set after it.
    const moves = Boolean(row.thinking) && (changed || level !== row.thinking);
    if (moves && setter) pi.setThinkingLevel(row.thinking);
    return changed || moves ? { notice: orchestratorSwitchNotice(row, moves && !setter) } : {};
  };
  /**
   * u (L980 A1, A3, A7): the profile becomes this session's, and nothing global is written.
   * Refused, with nothing changed, when this pi cannot run one of the profile's models; else the
   * orchestrator switches and the session records the profile. The switch's notice is the
   * caller's to say, in one notice with the mode's (lib/delegation-mode.ts followMode).
   */
  const useInSession = async (ctx: any, name: string, inSession: (model: string) => boolean): Promise<{ refused?: string; notice?: string }> => {
    const rows = piRows(JSON.parse(await jhonRun(["profile", "json"], ctx?.cwd ?? process.cwd())), name);
    if (!rows) return { refused: `jhon has no profile named ${name}.` };
    const unavailable = [...new Set([...rows.values()].map((row) => row.model))].filter((model) => !inSession(model));
    if (unavailable.length) return { refused: unavailableRefusal(unavailable) };
    const orchestrator = rows.get(ORCHESTRATOR);
    const switched = orchestrator ? await switchOrchestrator(ctx, orchestrator) : {};
    if (switched.refused) return { refused: switched.refused };
    sessionProfile.use(name, rows);
    recordSessionProfile(ctx, name);
    return switched.notice ? { notice: switched.notice } : {};
  };
  pi.on("session_start", async (event: any, ctx: any) => {
    let unchosen = modelChoice?.start(ctx, process.argv.slice(2));
    // L926 GAP 3: the model the owner picked in this process goes back on a session pi started on
    // its fallback (/new); pi sets it only with a credential, and saves nothing to the settings.
    const pick = unchosen ? ModelChoice.pick() : undefined;
    if (pick) {
      try {
        const model = ctx?.modelRegistry?.find?.(pick.provider, pick.id);
        if (model && await pi.setModel?.(model)) { modelChoice!.picked({ source: "set", model: pick }); unchosen = undefined; }
      } catch { /* not set: held below */ }
    }
    if (unchosen) {
      // With a UI the owner reads it and chooses; without one the run ends here, before any prompt.
      // A /new on a held session holds the next one for the same reason: said once (L930); a
      // startup, a resume and a reload draw the screen anew and say it (L942).
      if (ctx?.hasUI) { if (ModelChoice.announce(unchosen, event?.reason === "new")) ctx.ui?.notify?.(`${unchosen} ${UNCHOSEN_HINT}`, "error"); }
      else { process.stderr.write(`${unchosen} ${UNCHOSEN_ENDED}\n`); process.exit(1); }
    } else if (modelChoice) ModelChoice.announce(undefined);
    sessionPrompt.reset();
    let branch: unknown[] | undefined;
    try { branch = ctx?.sessionManager?.getBranch?.(); } catch { branch = undefined; }
    toolPolicy.reset(child ? [] : enabledOnBranch(branch));
    lean = undefined;
    fullBefore = fullOnBranch(branch);
    leanSkills = "";
    if (!child) setContextLevel(undefined);
    // T120 (L980 A6): the session's profile is its branch's: a resumed session comes back with it, a
    // new one has none; one that no longer exists is said here (A9).
    headerFacts = undefined;
    if (!child) {
      sessionProfile.start(sessionProfileOnBranch(branch));
      const read = await sessionProfile.refresh(ctx?.cwd ?? process.cwd());
      if (read.state === "missing") lostSessionProfile(ctx, read.was);
    }
    // pi's reason (startup, reload, new, resume, fork) tells the audit a reload from a reopening.
    // Bridges loaded before this one answer the query; later ones announce themselves.
    if (!child) askHosts(pi.events);
    if (!child && config) pi.appendEntry?.(SESSION_ENTRY, { mode: ctx?.mode ?? "unknown", agent: "jhon-orchestrator", reason: event?.reason ?? "unknown" });
    // Rose ink follows the wallpaper only when the owner opted in: the main
    // session keeps the theme file current through `jhon palette` (one writer
    // for every surface) and says each change as jhon:palette-changed.
    const palette = shellSettings().string("integrations.theme.palette");
    if (child || (palette !== "desktop" && palette !== "ryoku")) return undefined;
    if (config?.jhon && !stopPalette) stopPalette = followPalette({ jhon: config.jhon, agentDir, colors, active: () => { try { return ctx?.ui?.theme?.name; } catch { return undefined; } }, palette: "desktop", emit: (name, data) => pi.events?.emit?.(name, data) });
    if (!readPalette(colors)) return undefined;
    if (ctx?.mode === "tui" && ctx.ui?.setHeader) {
      const cwd = ctx.cwd ?? process.cwd();
      const top = git(cwd, "rev-parse", "--show-toplevel");
      const facts = {
        user: userInfo().username, project: basename(top || cwd), branch: git(cwd, "branch", "--show-current") || "-",
        // T120: the session's profile, marked while it differs from the default.
        profile: sessionMark(sessionProfile.current(), activeProfileName()),
      };
      headerFacts = facts;
      const art = readWordmark(spec);
      ctx.ui.setHeader((_tui: unknown, theme: any) => ({
        render: (width: number) => poster({ width, art, facts, paint: (role, text) => theme.fg(role, text) }),
        invalidate() {},
      }));
    }
    return undefined;
  });

  // Jhon's views (lib/ui/jhon-panel.ts): the change's detail, the grants, the profiles.
  // named: the change /jhon:status <change> asks for, read where pi is.
  const openPanel = (ctx: any, view: View, named?: string) => ctx.ui.custom((tui: any, theme: any, _keys: unknown, done: (r: null) => void) => {
    const cwd = ctx?.cwd ?? process.cwd();
    const close = closeWithRepaint(tui, done);
    // A model this session's registry does not list as available (provider not loaded, or no credential); an unreadable registry says nothing.
    const inSession = (model: string) => registryModels(() => ctx?.modelRegistry)?.includes(model) ?? true;
    const profileActions = profileModeActions({
      // d: `profile use` writes settings' pi.profile: load them again for the header and status bar.
      use: async (name) => { await jhonRun(["profile", "use", name], cwd); shellSettings().refresh(); },
      // u (T120): this session only; nothing global is written.
      useInSession: (name) => useInSession(ctx, name, inSession),
      setMode: (profile, mode) => jhonRun(["profile", "mode", profile, mode], cwd),
      ...modeDeps(ctx),
    });
    const panel = new JhonPanel({
      theme,
      runtime: "pi",
      view,
      load: async () => JSON.parse(await jhonRun(["profile", "json"], cwd)) as ProfilesData,
      // T117c, T120: `u` uses the profile in this session only (its sub-agents, its
      // orchestrator's model, and its mode: a mode change rebuilds the prompt once, with
      // a notice); `d` (and `use`) makes it the default for new sessions, with no rebuild;
      // `w` then `s` saves a mode for the next session.
      useNow: profileActions.useNow,
      sessionProfile: () => sessionProfile.current(),
      makeDefault: profileActions.makeDefault,
      use: profileActions.makeDefault,
      setMode: profileActions.mode,
      createProfile: async (name, from) => { await jhonRun(["profile", "new", name, "--from", from], cwd); },
      setModel: async (profile, agent, runtime, value) => { await jhonRun(["profile", "set", profile, agent, runtime, value || "-"], cwd); },
      removeProfile: async (name) => { await jhonRun(["profile", "rm", name], cwd); },
      // pi's own registry says what pi can run, united with the models the profiles name (T130);
      // another runtime, or a registry that cannot be read, the profile-named ones.
      models: (runtime, data) => modelsOffered(runtime, data, () => ctx?.modelRegistry),
      // A saved model the session cannot run yet is said so.
      inSession,
      // Change and Tests work where the change is: pi's folder, else the worktree it is in.
      loadChange: async () => {
        // The change STATUS shows, read whole by name where it is; named, the one asked for.
        const where = named ? { root: cwd } as Awaited<ReturnType<typeof sessionChange>> : await sessionChange(ctx, cwd, bin);
        const which = named ?? where.change;
        const status = where.status ?? await jhonJson(bin, ["status", ...(which ? [which] : []), "--json"], where.root);
        // Every lane beside it: STATUS's own lanes when it shows this change; else read here.
        const shown = typeof status?.change === "string" ? { change: status.change, slices: [], root: where.root } : undefined;
        // Its risk row: jhon risk's tier and the files that set it; without it, the route's tier.
        const [lanes, risk] = await Promise.all([
          where.lanes && !named ? where.lanes : lddLanes(cwd, shown, (dir) => readLddStatus(dir)).catch(() => []),
          typeof status?.change === "string" ? jhonJson(bin, ["risk", "--json"], where.root).catch(() => undefined) : undefined,
        ]);
        return { ...status, root: where.root, ...(where.worktree ? { worktree: where.worktree } : {}), ...(lanes.length ? { lanes } : {}), ...(risk?.tier ? { risk } : {}) };
      },
      // o: tasks.md in the owner's editor, the panel closed around it; with no editor, the panel says its path.
      openFile: (path) => {
        if (!(process.env.VISUAL || process.env.EDITOR)?.trim()) return "No editor: set $VISUAL or $EDITOR.";
        close(null);
        setImmediate(() => {
          const failed = openInExternalEditor(tui, path, process.env, spawnSync, cwd);
          if (failed) ctx.ui.notify(failed, "warning");
        });
        return undefined;
      },
      // The owner's own key press runs jhon grant, as `!` does in Claude Code; no agent reaches it.
      loadGrants: async () => JSON.parse(await jhonRun(["grant", "--list", "--json"], cwd)),
      grant: async (cls, duration) => { await jhonRun(["grant", cls, "--for", duration], cwd); },
      revoke: async (cls) => { await jhonRun(["grant", "--revoke", cls], cwd); },
      loadTests: async () => {
        const where = await sessionChange(ctx, cwd, bin);
        const status = where.status ?? await jhonJson(bin, ["status", ...(where.change ? [where.change] : []), "--json"], where.root);
        if (typeof status?.change !== "string") return {};
        const base = defaultBase(where.root);
        const plan = await jhonJson(bin, ["executions", "run", status.change, "--affected", base, "--dry-run", "--json"], where.root);
        return { change: status.change, base, last: status.tests ?? undefined, projects: plan.projects ?? [], timeoutMinutes: plan.timeout_minutes, root: where.root, worktree: where.worktree };
      },
      // The owner's run: BACKGROUND shows it; its end comes back to the panel, never to the agent.
      startTests: (data, onEnd) => {
        if (!background) throw new Error("background tasks are unavailable in this session");
        const command = [bin, "executions", "run", data.change!, "--affected", data.base!].map(shellQuote).join(" ");
        const job = background.startOwnerCommand(command, data.root ?? cwd, ctx?.sessionManager?.getSessionId?.() ?? "", `tests of ${data.change} (you)`, (j) => onEnd(j.status === "exited" ? j.exitCode ?? null : null));
        return { id: job.id, startedAt: job.startedAt };
      },
      stopTests: (id) => background?.stopOwnerCommand(id),
      ascii: shellSettings().ascii(),
      // G's detail card: 85% of the rows less the card's border, the card itself at most 24.
      height: () => Math.max(0, Math.floor((tui.terminal?.rows ?? 40) * 0.85) - 2),
      // The LDD detail and the profiles take the whole terminal, as G's profiles view (L174).
      rows: () => tui.terminal?.rows ?? 40,
      onClose: () => close(null),
      requestRender: () => tui.requestRender(),
    });
    panel.open();
    return panel;
  }, view === "grants" ? GRANTS_OVERLAY : FULLSCREEN_OVERLAY);
  if (!child) {
    // Each view its own /jhon:<option> (L194): the LDD detail, as alt+j, and the grants;
    // the permission mode changes only in Customize → Permissions (L214).
    // T8f: a command's prompt runs as a typed one; under `pi -p` the command waits for its run (lib/command-prompt.ts).
    const sendPrompt = commandPrompts(pi);
    pi.registerCommand("jhon:status", {
      description: "The LDD status of the session's change, or the one named",
      getArgumentCompletions: (prefix: string) => completeWord(lddChanges(process.cwd()), prefix),
      handler: async (args: string, ctx: any) => { await openPanel(ctx, "change", String(args ?? "").trim() || undefined); },
    });
    // pi's /jhon-ldd (L194): the shell's command in place of the prompt template
    // the render wrote; the same prompt, its actions and the change completed.
    pi.registerCommand("jhon-ldd", {
      description: "Runs an LDD action on a change; with no action it continues",
      getArgumentCompletions: (prefix: string) => { try { return lddCompletions(readFileSync(LDD_SKILL, "utf8"), prefix, lddChanges(process.cwd())); } catch { return null; } },
      handler: async (args: string, ctx: any) => {
        let skill: string;
        try { skill = readFileSync(LDD_SKILL, "utf8"); } catch (error) { return void ctx?.ui?.notify?.(`jhon-ldd: ${LDD_SKILL} cannot be read (${(error as Error).message}); run \`jhon sync\``, "error"); }
        // As a typed prompt: while the agent works it steers the turn, as Enter
        // on the old template did; a prompt pi refuses (a compaction running) is said once.
        try { await sendPrompt(ctx, lddPrompt(skill, String(args ?? ""))); }
        catch (error) { ctx?.ui?.notify?.(`jhon-ldd: ${(error as Error).message}; run it again when pi is ready`, "warning"); }
      },
    });
    // odd/tasks/jhon-studio.md S3: jhon-studio's video skill, only while integrations.studio is on
    // (off, nothing about video is registered); its skill is read where jhon-studio says, at each use.
    // T237: nor while `jhon start pi` kept it off for the session (a jhon-studio that needs a newer jhon).
    if (shellSettings().bool(STUDIO_SETTING) && !offForSession(STUDIO_SETTING)) pi.registerCommand("jhon:video", {
      description: VIDEO_DESCRIPTION,
      handler: async (args: string, ctx: any) => {
        const skill = await studioSkill();
        if ("error" in skill) return void ctx?.ui?.notify?.(`jhon:video: ${skill.error}; run \`jhon doctor\`, or turn it off with \`jhon settings set ${STUDIO_SETTING} false\``, "error");
        try { await sendPrompt(ctx, videoPrompt(skill.text, String(args ?? ""))); }
        catch (error) { ctx?.ui?.notify?.(`jhon:video: ${(error as Error).message}; run it again when pi is ready`, "warning"); }
      },
    });
    pi.registerCommand("jhon:grants", {
      description: "The grants for this project: give, revoke, duration",
      handler: async (_args: string, ctx: any) => { await openPanel(ctx, "grants"); },
    });
    const panelKey = jhonPanelKey();
    // The rail's LDD section expanded: its key opens the LDD detail (L96, L113, L157).
    if (panelKey) pi.registerShortcut?.(panelKey, { description: "Opens the LDD status, as /jhon:status (the rail's LDD section expanded)", handler: async (ctx: any) => { await openPanel(ctx, "change"); } });
  }

  // The jhon CLI's own answer, read without blocking pi: what it printed, and whether it exited 0.
  const jhonText = (args: string[], cwd: string) => new Promise<{ text: string; ok: boolean }>((resolve) =>
    execFile(bin, args, { cwd, encoding: "utf8", maxBuffer: 8 << 20 }, (error, stdout, stderr) =>
      resolve({ text: `${stdout ?? ""}${stderr ?? ""}`.trim() || `jhon ${args[0]}: ${error ? error.message : "exited 0"}`, ok: !error })));
  pi.registerCommand("jhon:profile", {
    description: "Model profiles: opens them, or list, show [<name>], use <name>",
    getArgumentCompletions: (prefix: string) => completeWord(PROFILE_ACTIONS, prefix),
    handler: async (args: string, ctx: any) => {
      const words = String(args ?? "").trim().split(/\s+/).filter(Boolean);
      if (!child && !words.length) return void (await openPanel(ctx, "profiles"));
      const r = await jhonText(["profile", ...words], ctx?.cwd ?? process.cwd());
      ctx.ui.notify(r.text, r.ok ? "info" : "error");
    },
  });
  if (!child) {
    pi.registerCommand("jhon:dashboard", {
      description: "Opens this session's audit in the dashboard, starting it if needed",
      handler: async (_args: string, ctx: any) => {
        const sm = ctx?.sessionManager;
        let id: string | undefined;
        try { id = sm?.getSessionFile?.() ? sm.getSessionId?.() : undefined; } catch { id = undefined; }
        // A test points the shell at a dashboard of its own, never the owner's.
        const base = process.env.JHON_DASHBOARD_URL || DASHBOARD;
        const r = await openDashboard(id, {
          probe: (url) => probeDashboard(url),
          stale: (at) => dashboardIsStale(at, resolveJhon(bin)),
          // The server outlives the command and pi.
          start: () => new Promise<boolean>((resolve) => {
            const server = spawn(bin, dashboardStartArgs(base), { cwd: ctx?.cwd ?? process.cwd(), detached: true, stdio: "ignore" });
            server.once("spawn", () => resolve(true));
            server.once("error", () => resolve(false));
            server.unref();
          }),
          open: (url) => {
            const browser = spawn(process.platform === "darwin" ? "open" : "xdg-open", [url], { detached: true, stdio: "ignore" });
            browser.on("error", () => {});
            browser.unref();
          },
          display: hasDisplay(),
          wait: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
          base,
        });
        ctx.ui.notify(r.message, r.level);
      },
    });
  }

  // The main session compacts at shell.behaviour.compact_at of its window (60%
  // by default; JHON_SHELL_COMPACT_AT overrides it) or past shell.behaviour.compact_tokens,
  // whichever is first: after a reply, before an idle prompt and between the turns
  // of a long run (T121, lib/compaction.ts). A child never compacts here.
  const compaction = child ? undefined : new MainCompaction({
    at: () => compactAt(),
    budget: () => shellSettings().compactTokens(),
    record: (name, attributes) => recordEvent(name, attributes as any),
    send: (message, options) => pi.sendMessage?.(message, options),
  });
  // One session per change (S35): past half the window or the token budget
  // (T115a), whichever is first, the owner and the agent hear once that a
  // fresh session with a handoff would pay.
  const fresh = child ? undefined : new FreshSessionLatch();
  // T115e (L715): the ladder's first rung, a delegation signal (lib/context-backstop.ts).
  const backstop = child ? undefined : new ContextBackstop();
  // T117e (L746): per owner request, the writer guard's lines.
  const written = child ? undefined : new WriterBudget();
  // T122 (L762 F2), T125 (L783 F5): a child's own context budget (a wrap-up notice, never a cap: T155), and a scout's or builder's CodeGraph-first read budget (lib/child-budget.ts).
  const childBudget = child ? new ChildContextBudget() : undefined;
  const childReads = child && readBudgetApplies(process.env.JHON_PI_AGENT, limits.tools) ? new ChildReadBudget(readBudgetKind(process.env.JHON_PI_AGENT)) : undefined;
  // T135 (L1055, L1063): a main-session reply that announces an action and stops, or is empty, is sent back once per owner turn (lib/announce-stop.ts).
  const announce = child || !config ? undefined : new AnnounceSendBack(engineStopRunner(config.jhon));
  if (announce) pi.on("agent_end", async (event: any) => { announce.ended(event?.messages); return undefined; });
  // A request starts with the owner's message (pi's input event, source interactive or rpc);
  // the extensions' messages (notices, sub-agent completions, follow-ups) come as "extension".
  if (!child) pi.on("input", async (event: any, ctx: any) => {
    // T130 (L920 B1): no prompt reaches a model nobody chose; the owner's yes or a pick lifts it.
    const unchosen = modelChoice?.refused();
    if (unchosen) {
      // L930 N2: the owner's own prompt asks whether this session runs on pi's fallback. Yes makes it
      // their choice as a pick does (pi fires no model_select for the model it already stands on) and
      // that prompt goes through; no, or a dismissed dialog, refuses that prompt, whatever happened
      // to the hold meanwhile (the dialog showed the line: it is not said again). One question at a
      // time (L942 G1): a prompt that arrives while one is open, another extension's input, and a
      // session that cannot ask are refused with the line.
      const fallback = modelChoice!.fallback();
      if (asking || event?.source === "extension" || !ctx?.hasUI || !fallback || typeof ctx.ui?.select !== "function") { held(ctx); return { action: "handled" }; }
      let answer: unknown;
      asking = true;
      try { answer = await ctx.ui.select(unchosenQuestion(unchosen, fallback), [UNCHOSEN_YES, UNCHOSEN_NO]); } catch { answer = undefined; } finally { asking = false; }
      if (answer !== UNCHOSEN_YES) return { action: "handled" };
      if (modelChoice!.refused()) modelChoice!.picked({ source: "set", model: fallback });
    }
    if (event?.source !== "extension") { written!.reset(); announce?.ownerTurn(); }
    // T121: a session already over budget (a resume, a smaller model) compacts before the prompt runs.
    await compaction!.beforePrompt(event, ctx);
    return undefined;
  });
  pi.on("agent_settled", async (event: any, ctx: any) => {
    if (child) return undefined;
    // T124 (L762 F4): while or once a compaction runs, the usage pi reports is the
    // pre-compaction one; the notices wait for a reply that measures the compacted context.
    if (compaction!.compacting) return undefined;
    const usage = ctx?.getContextUsage?.();
    if (compaction!.afterReply(ctx, usage)) return undefined;
    // The reply's one read serves both; two notices on one reply are one.
    const delegate = backstop?.observe(usage);
    const handoff = fresh?.context(usage?.percent, usage?.tokens);
    const notice = [delegate, handoff].filter(Boolean).join("\n\n");
    if (notice) {
      ctx.ui?.notify?.(notice, "info");
      pi.sendMessage?.({ customType: handoff ? FRESH_SESSION_ENTRY : BACKSTOP_ENTRY, content: fromTheShell(notice), display: false }, { deliverAs: "nextTurn" });
    }
    // T135: the send-back is the shell's follow-up message, never the owner's, and says so (T194); a failure lets the turn end.
    const sendBack = await announce?.settled(event?.aborted === true, ctx?.cwd ?? process.cwd());
    if (sendBack) {
      try { pi.sendUserMessage?.(fromTheShell(sendBack), ctx?.isIdle?.() === false ? { deliverAs: "followUp" } : undefined); } catch { /* the turn just ends */ }
    }
    return undefined;
  });

  // T31 D: in a project that uses LDD, the binding lead and the project's rules Claude Code's session gets (lib/session-brief.ts).
  // timeoutMs bounds a mid-session ask (T192 S7): past it the engine is stopped and the answer is unknown.
  const briefOf = (cwd: string, timeoutMs?: number) => new Promise<string>((resolve, reject) => {
    if (!config) return reject(new Error("no jhon.json"));
    const run = spawn(config.jhon, ["hook", "session-brief", "jhon-orchestrator"], { cwd, stdio: ["pipe", "pipe", "ignore"] });
    const timer = timeoutMs === undefined ? undefined : setTimeout(() => { try { run.kill("SIGKILL"); } catch { /* gone already */ } reject(new Error("session-brief timed out")); }, timeoutMs);
    let out = "";
    run.stdout.setEncoding("utf8").on("data", (d: string) => { out += d; });
    run.once("error", (error: Error) => { clearTimeout(timer); reject(error); });
    run.once("close", (code: number | null) => { clearTimeout(timer); if (code === 0) resolve(out); else reject(new Error(`session-brief exited ${code}`)); });
    run.stdin.on("error", () => {});
    run.stdin.end(JSON.stringify({ cwd }));
  });
  const briefs = child || !config ? undefined : sessionBriefs(briefOf);

  pi.on("before_agent_start", async (event: any, ctx: any) => {
    // L926 GAP 2: a run that did not come through the input (another extension's triggerTurn) is
    // aborted while it starts; pi stops it at agent_start, which it awaits before the first request.
    if (held(ctx)) { try { ctx?.abort?.(); } catch { /* the input refusal still stands */ } return undefined; }
    const options = event.systemPromptOptions;
    const cwd = ctx?.cwd ?? process.cwd();
    // T42c: codegraph's tool and contract only in a repository it indexed;
    // T115c: the main session's deferred tools stay out (lib/tools-deferred.ts).
    if (Array.isArray(options?.selectedTools)) {
      const kept = toolPolicy.filter(options.selectedTools, cwd);
      if (kept) options.selectedTools.splice(0, options.selectedTools.length, ...kept);
    }
    if (child || !config || !existsSync(config.orchestrator)) return undefined;
    // T117c, T120: the delegation mode of the session's profile, its own or else the default, read once per session with the prompt (the writer guard reads the same).
    const mode = sessionPrompt.delegation(() => profileDelegation(sessionProfile.current() ?? activeProfileName()));
    for (const skill of options?.skills ?? []) if (typeof skill?.name === "string" && typeof skill?.filePath === "string") resolvedSkills.set(skill.name, skill.filePath);
    compactSkills(options);
    let session = "";
    try { session = ctx?.sessionManager?.getSessionId?.() ?? ""; } catch { session = ""; }
    const brief = await briefs?.(session, cwd);
    const rules = brief && "lead" in brief ? brief : undefined;
    const split = rules ? splitRoute(rules.brief) : undefined;
    // T146h T2: no change, or a small one, starts with the lean prompt; Jhon's own tools and the
    // skills list wait for the full context (L3), but the delegation tools are offered from the
    // first request (T192 S3). A failed brief, or no lean file, starts full.
    if (lean === undefined) {
      const on = brief?.context === "lean" && typeof config.lean === "string" && existsSync(config.lean);
      lean = { on, loaded: on && fullBefore };
      toolPolicy.setLean(on && !lean.loaded);
      setContextLevel(on && !lean.loaded ? "lean" : "full");
    }
    if (lean.on && !lean.loaded) {
      if (Array.isArray(options?.selectedTools)) {
        const kept = toolPolicy.filter(options.selectedTools, cwd);
        if (kept) options.selectedTools.splice(0, options.selectedTools.length, ...kept);
      }
      if (Array.isArray(options?.skills) && options.skills.length) {
        leanSkills = options.skills.filter((k: any) => typeof k?.name === "string").map((k: any) => `- ${k.name}: ${k.description ?? ""} (${k.filePath ?? ""})`).join("\n");
        options.skills.splice(0, options.skills.length);
      }
    }
    // T115d (L707): the appended prompt is the one of the session's first run
    // here: a rewritten orchestrator file or a re-read brief waits for the
    // next session, so the cached prefix holds on every turn.
    const [head, ...rest] = sessionPrompt.parts(cwd, () => {
      const orchestrator = lean?.on ? readFileSync(config.lean!, "utf8").trim() : fullPrompt(cwd, mode, ctx);
      return [
        rules ? `${rules.lead}\n\n${orchestrator}` : orchestrator,
        // Where the engine says LDD is not set up yet (no layout), its one line
        // (T64, T72: the text Claude Code's session start gets) says so instead of the project's rules;
        // a failed brief says nothing (L477).
        split ? split.rules : brief && "inactive" in brief ? brief.inactive : "",
        ownerShellNote(process.env.SHELL) ?? "",
        // T45b: the memory index, read once per session, in the session section after the static text.
        memoryIndexPrompt(memoryDir(projectRoot(cwd), claudeConfig)),
      ];
    });
    const memory = rest.pop()!;
    appendOnce(options, head);
    for (const part of rest) if (part) appendOnce(options, part);
    appendSessionOnce(options, memory);
    // The route follows the diff: a persisted message, sent when it changes.
    const route = sessionPrompt.routeChange(split?.route);
    return route ? { message: { customType: ROUTE_MESSAGE, content: fromTheShell(route), display: false } } : undefined;
  });

  // The orchestrator's full prompt: writer mode's rules over the marked region, or the organic
  // rules whole, said once; codegraph's contract only where it indexed the repository.
  const fullPrompt = (cwd: string, mode: "inline" | "writer", ctx: any): string => {
    const organic = readFileSync(config!.orchestrator, "utf8");
    const spliced = delegationPrompt(organic, mode, writerFile(config!.orchestrator));
    if (spliced.note) ctx?.ui?.notify?.(spliced.note, "warning");
    return toolPolicy.codegraphOn(cwd) ? spliced.text : withoutCodegraph(spliced.text);
  };

  // T146h T3: a lean session's full context loads once, when `jhon status` says the change is not
  // small (through bash, inspect or the jhon MCP server's status tool, T192 S5) or a sub-agent is
  // launched: one persisted message, never a system-prompt swap (T115d), and Jhon's deferred tools come back.
  // T192 S7: or when the agent makes or selects a change (lib/lean-context.ts changeMoment) and the
  // engine, asked as at the start (session-brief's context), says it is not small: before its next edit.
  const loadFull = (ctx: any) => {
    if (!lean) return;
    lean.loaded = true;
    toolPolicy.setLean(false);
    setContextLevel("full");
    const cwd = ctx?.cwd ?? process.cwd();
    let text = "";
    try { text = fullPrompt(cwd, sessionPrompt.frozenDelegation() ?? "inline", ctx); } catch { text = ""; }
    if (leanSkills) text += `\n\n## Skills\n\nRead a skill's file when its task applies:\n${leanSkills}`;
    const all = (pi.getAllTools?.() ?? []).map((t: any) => t.name).filter((n: unknown): n is string => typeof n === "string");
    if (all.length) pi.setActiveTools?.(toolPolicy.filter(all, cwd) ?? all);
    if (text) pi.sendMessage?.({ customType: FULL_MESSAGE, content: fromTheShell(`Jhon: this work needs the full context; it binds from now on.\n\n${text}`), display: false }, { deliverAs: "steer" });
  };
  let asked: Promise<unknown> = Promise.resolve();
  if (!child && config) pi.on("tool_result", async (event: any, ctx: any) => {
    if (!lean?.on || lean.loaded) return undefined;
    const output = (event.content ?? []).map((c: any) => (c?.type === "text" && typeof c.text === "string" ? c.text : "")).join("\n");
    if (fullTrigger(event.toolName, event.input, output)) { loadFull(ctx); return undefined; }
    const where = changeMoment(event.toolName, event.input, ctx?.cwd ?? process.cwd());
    if (!where) return undefined;
    // One ask at a time; one that waited finds the session full already and asks nothing.
    const ask = asked.then(async () => {
      if (!lean?.on || lean.loaded) return;
      const brief = parseSessionBrief(await briefOf(where, CHANGE_ASK_MS).catch(() => ""));
      if (brief?.context === "full" && !lean.loaded) loadFull(ctx);
    });
    asked = ask.catch(() => {});
    await asked;
    return undefined;
  });

  // T45b: pi connects its MCP servers after before_agent_start, so their tools
  // join the active set after the filter above; each request filters them
  // again, before the provider reads the active tools. T115d: the decision is
  // the session's, so after the first request nothing is set again.
  pi.on("context", async (_event: any, ctx: any) => {
    const active: string[] | undefined = pi.getActiveTools?.();
    const kept = active && toolPolicy.filter(active, ctx?.cwd ?? process.cwd());
    if (kept) pi.setActiveTools?.(kept);
    return undefined;
  });

  // T115c: the deferred tools, reached on request.
  if (!child) pi.registerTool(toolsMoreTool(pi, toolPolicy));

  // T118c: the native Prism review, one call; its completions run through pi's model registry.
  if (!child) pi.registerTool({
    name: "jhon_review",
    label: "Prism review",
    // T174-S7: the Prism review card, drawn from the result's details (lib/ui/review-card.ts).
    ...reviewRenderers(),
    description: "Prism review in one call: facets, challenger, arbiter, record. Takes a few minutes. Returns the verdict, or which review agents to launch.",
    parameters: { type: "object", additionalProperties: false, properties: { base: { type: "string" }, change: { type: "string" } } },
    async execute(_id: string, params: { base?: unknown; change?: unknown }, signal: AbortSignal | undefined, _onUpdate: unknown, ctx: any) {
      if (!config) throw new Error(`jhon: ${join(agentDir, CONFIG_FILE)} is missing or invalid; run \`jhon sync\``);
      if (mismatch) throw new Error(mismatch);
      const str = (v: unknown) => (typeof v === "string" && v.trim() ? v.trim() : undefined);
      const r = await runReview({ base: str(params.base), change: str(params.change) },
        { bin: config.jhon, cwd: ctx?.cwd ?? process.cwd(), completer: registryCompleter(ctx?.modelRegistry), signal: signal ?? ctx?.signal, disabled: nativeReviewDisabled(process.env) });
      return { content: [{ type: "text", text: r.text }], details: r.details };
    },
  });

  // A child is never stopped for its turns (T131) nor its context (T155).
  pi.on("turn_end", async (_event: any, ctx: any) => {
    // T121 (L762 F1): a long run over budget compacts between turns and carries on.
    compaction?.afterTurn(_event, ctx);
    return undefined;
  });

  // The sessions whose owner granted a folder's writes; each grant ends with the session.
  const grantedSessions = new Set<string>();
  pi.on("session_shutdown", async (_event: any, ctx: any) => {
    for (const id of grantedSessions) spawnSync(bin, ["grant", "--revoke", "outside-write", "--session", id], { cwd: ctx?.cwd ?? process.cwd(), encoding: "utf8" });
    grantedSessions.clear();
    return undefined;
  });

  const guardCall = async (event: any, ctx: any): Promise<any> => {
    const input = (event.input ?? {}) as Record<string, unknown>;
    if (!config) {
      return claudeCall(event.toolName, input) === null ? undefined
        : { block: true, reason: `jhon: ${join(agentDir, CONFIG_FILE)} is missing or invalid, so no guard can judge this call; run \`jhon sync\`` };
    }
    if (mismatch) return claudeCall(event.toolName, input) === null ? undefined : { block: true, reason: mismatch };
    // T117c (L720), T117e (L746): in writer mode the main session writes inline at most
    // ~20 lines per owner request, summed over its calls; odd/** always passes, uncounted.
    if (written && sessionPrompt.frozenDelegation() === "writer") {
      const here = ctx?.cwd ?? process.cwd();
      const why = written.check(event.toolName, input, here, () => projectRoot(here), typeof event.toolCallId === "string" ? event.toolCallId : undefined);
      if (why) return { block: true, reason: why };
    }
    let sessionId = "";
    try { sessionId = ctx?.sessionManager?.getSessionId?.() ?? ""; } catch { /* No session. */ }
    const d = await judge(event.toolName, input, { config, cwd: ctx.cwd ?? process.cwd(), child, interactive: !child && ctx.hasUI === true, sessionId, ...limits });
    if (!d) return undefined;
    if ("block" in d) return d;
    if (!child && ctx.hasUI) {
      // The short label (L87): the class, not its detail, so a multiplexer's row stays readable.
      const answer = await waitingForOwner(pi.events, `permission: ${askClasses(d.ask).join(", ")}`, () => ownerAnswer(ctx, d.ask, bin, sessionId));
      if (answer.scope === "session") grantedSessions.add(sessionId);
      if (answer.allowed) {
        // The owner's answer is on record: in the session, and a grant in the grants' history.
        pi.appendEntry?.(AUTHORIZATION_ENTRY, { ask: d.ask, classes: answer.classes, tool: event.toolName, scope: answer.scope, at: new Date().toISOString() });
        return undefined;
      }
      if (answer.error) return { block: true, reason: `${d.ask}: the owner chose to grant it, but jhon grant failed: ${answer.error}` };
    }
    return { block: true, reason: `${d.ask}: the owner did not authorize it` };
  };
  pi.on("tool_call", async (event: any, ctx: any) => {
    let r = await guardCall(event, ctx);
    // T128b: the guards judged the command as the model wrote it; what they let through
    // runs in the sandbox, or is refused where sandbox.mode is on and it cannot run.
    if (!r) {
      const refused = await sandbox.gate(event.toolName, ctx?.cwd ?? process.cwd());
      if (refused) r = { block: true, reason: refused };
    }
    // A call the writer guard counted but a later guard refused never ran: taken back.
    if (r && "block" in r) written?.settle(event.toolCallId, true);
    return r;
  });

  // T31 E: the main session's first code edit in an LDD project with no change selected gets one reminder (lib/ldd-reminder.ts).
  const remindLdd = child || !config ? undefined : lddReminder(async (cwd) => {
    // The engine says where LDD is active, and whether a change is selected.
    return reminderAnswer(await jhonJson(config.jhon, ["status", "--json"], cwd));
  });

  // A file the agent edited or wrote is rewritten by its project's formatter.
  pi.on("tool_result", async (event: any, ctx: any) => {
    // The agent read the archive's notice in its output; the owner sees it once as a notice.
    if (fresh && !event.isError && event.toolName === "bash") {
      const output = (event.content ?? []).map((c: any) => (c?.type === "text" && typeof c.text === "string" ? c.text : "")).join("\n");
      const line = fresh.archive(event.input?.command, output);
      if (line) ctx.ui?.notify?.(line, "info");
    }
    if (!config || mismatch || event.isError) return undefined;
    const input = (event.input ?? {}) as Record<string, unknown>;
    const cwd = ctx.cwd ?? process.cwd();
    const notes = [await formatEdited(config, event.toolName, input, cwd), await remindLdd?.(event.toolName, input, cwd)].filter(Boolean);
    return notes.length ? { content: [...(event.content ?? []), ...notes.map((text) => ({ type: "text", text }))] } : undefined;
  });

  // T115e: past the backstop mid-turn, the agent reads it appended to this
  // tool result (the newest message), so the cached prefix holds; registered
  // last, it reads the content the handlers before it gave (pi chains them).
  if (backstop) {
    pi.on("session_compact", async () => { backstop.compacted(); return undefined; });
    pi.on("session_start", async () => { backstop.reset(); return undefined; });
    pi.on("tool_result", async (event: any, ctx: any) => {
      written?.settle(event.toolCallId, event.isError === true);
      const notices = [backstop.measure(ctx)].filter(Boolean) as string[];
      if (!notices.length) return undefined;
      for (const notice of notices) ctx.ui?.notify?.(notice, "info");
      return { content: [...(event.content ?? []), ...notices.map((text) => ({ type: "text", text }))] };
    });
  }
  // T122: a child's budget notices ride the same channel; its system prompt never changes.
  if (childBudget) {
    pi.on("tool_result", async (event: any, ctx: any) => {
      const notices = [childBudget.measure(ctx), childReads?.result(event)].filter(Boolean) as string[];
      if (!notices.length) return undefined;
      return { content: [...(event.content ?? []), ...notices.map((text) => ({ type: "text", text }))] };
    });
  }

  pi.registerTool({
    name: "inspect",
    label: "Inspect",
    description: `Run one read-only inspector (${(config?.inspectAllow ?? []).join(", ") || "none configured"}). No shell: arguments split on spaces; quotes, pipes, redirections and chains are refused. E.g. git log --oneline -20 -- src/a.ts. cwd: the change's worktree, from the main checkout.`,
    parameters: { type: "object", additionalProperties: false, required: ["command"], properties: { command: { type: "string", description: "The inspector and its arguments, e.g. `git diff main...HEAD -- src/a.ts`." }, cwd: CWD } },
    async execute(_id: string, params: { command?: unknown; cwd?: unknown }, _signal: unknown, _onUpdate: unknown, ctx: any) {
      if (!config) throw new Error(`jhon: ${join(agentDir, CONFIG_FILE)} is missing or invalid; run \`jhon sync\``);
      if (mismatch) throw new Error(mismatch);
      if (typeof params.command !== "string" || !params.command.trim()) throw new Error("inspect needs a command");
      const cwd = at(ctx, params.cwd);
      // L917 C3: the program the model names starts in the sandbox with sandbox.mode on, or not at all where it cannot run.
      const r = await inspect(params.command, config, cwd, (program, args) => sandbox.startArgv(program, args, ctx?.cwd ?? process.cwd()));
      if (!r.ok) throw new Error(r.text);
      return text(r.text);
    },
  });

  const agents = knownAgents(agentDir);
  // Background work is the main session's (lib/background-tasks-tools.ts); a child waits on its own.
  const background = registerBackgroundTasks(pi, { sandbox: sandbox.jobOperations });
  // In the main session every sub-agent runs in the background (L649): the
  // owner sees it on the agents rail and keeps talking. A sub-agent has no
  // background tasks, so the ones it starts answer in the call.
  const launch = background
    ? "It runs in the background: it returns a task id at once; its answer arrives as a message (do not poll or sleep). Launch several in one turn to run them in parallel; task_result reads one, task_stop cancels it."
    : "It returns its final answer.";
  const relaunch = background ? "It runs in the background, as subagent does." : "It returns its final answer.";
  // The card names what the child runs on: its agent's model and thinking, the session's profile's when it has one (as last read).
  const profile: DelegationProfile = (name) => {
    try { const a = sessionProfile.agent(resolveAgent(agentDir, name)); return { model: a.model, thinking: a.thinking }; } catch { return undefined; }
  };
  // T174-S7: the card's state follows the background agent a launch started.
  const delegated: DelegationStatus | undefined = background ? (task, owner) => background.agentRun(task, owner)?.status : undefined;

  // A model trained on other harnesses may still ask for a foreground run
  // (background: false, mode: "foreground"/"default"/"task"): it runs in the background anyway, and says so (T119, L716).
  // T195: the short label a background sub-agent is named by on the rail; any other value is none.
  const labelOf = (value: unknown) => (typeof value === "string" ? value : undefined);
  const PUSHED = "Its completion is pushed automatically; continue other work or end the turn.";
  const forcedBackground = (params: { background?: unknown; mode?: unknown }) =>
    params.background === false || (params.mode !== undefined && params.mode !== "background")
      ? " Foreground was requested; in the main session every sub-agent runs in the background."
      : "";

  pi.registerTool({
    name: "subagent",
    label: "Sub-agent",
    ...delegationRenderers("Delegating", "agent", profile, delegated),
    description: `Run a named sub-agent on a self-contained task. ${launch} Agents: ${agents.length ? agents.join(", ") : "(none installed)"}. A bounded agent's task needs a "## Allowed edit surfaces" section listing the files it may write. worktree: true gives a parallel writer its own git worktree on a new branch: integrate the branch afterwards. cwd: the change's worktree, from the main checkout.`,
    parameters: {
      // No additionalProperties: false: an old `background: false` or `mode` still validates, and runs in the background (T119).
      type: "object", required: ["agent", "task"],
      properties: {
        agent: { type: "string", description: "The sub-agent's name." },
        task: { type: "string", description: "The complete handoff: goal, context, constraints, and the expected return." },
        worktree: { type: "boolean", description: "Run it in a git worktree of its own (beside the repository, on a new branch from HEAD), so parallel writers never share a tree." },
        cwd: CWD,
        timeout_seconds: TIMEOUT,
        skills: { type: "array", items: { type: "string" }, description: "Exact SKILL.md paths the sub-agent also loads, beyond the ones its definition names." },
        label: { type: "string", description: "What it does, a few words (e.g. \"web: categories page\"), shown on the agents rail; else the brief's first phrase." },
      },
    },
    async execute(id: string, params: { agent?: unknown; task?: unknown; worktree?: unknown; cwd?: unknown; timeout_seconds?: unknown; skills?: unknown; label?: unknown; background?: unknown; mode?: unknown }, signal: AbortSignal | undefined, onUpdate: unknown, ctx: any) {
      if (typeof params.agent !== "string" || typeof params.task !== "string") throw new Error("subagent needs an agent and a task");
      const timeoutSeconds = timeoutOf(params.timeout_seconds);
      const rendered = resolveAgent(agentDir, params.agent);
      const skills = childSkills(rendered.body, resolvedSkills, params.skills);
      const refusal = refuseTask(rendered, params.task);
      if (refusal) throw new Error(refusal);
      // T120: a session that uses a profile of its own launches on that profile's model and level.
      const agent = await profiled(rendered, ctx);
      const sessionProfileName = sessionProfile.current();
      // The child's session is saved beside this one, linked to it and to this call, for `jhon audit`.
      const sm = ctx?.sessionManager;
      const session = childSession(agent, { file: sm?.getSessionFile?.(), id: sm?.getSessionId?.() }, String(id ?? ""));
      // A parallel writer gets a tree of its own; the orchestrator integrates its branch.
      const base = at(ctx, params.cwd);
      const tree = params.worktree === true ? createWorktree(base, agent.name) : undefined;
      const cwd = tree?.cwd ?? base;
      // T132: where the child works is one of the session's roots, where the LDD section looks for its change; the session's own folder adds nothing.
      registerSubagentWorktree(pi, sm, ctx?.cwd ?? process.cwd(), tree?.dir ?? base);
      // T134 (L1021 H1, H2): the decisions on record and the code map follow the brief, bounded; the scope guard still reads the brief.
      const shown = selectedChange(sm?.getSessionId?.() ?? "");
      const change = shown && (shown.root ?? ctx?.cwd) === cwd ? shown.change : undefined;
      // T137: jhon-checker and the Prism facets are launched with the tests to judge too.
      const task = params.task + await launchContext(params.task, cwd, { bin, change, agent: agent.name }).catch(() => "");
      const scope = params.task;
      if (background) {
        const run = background.startAgent(agent, task, cwd, sm?.getSessionId?.() ?? "", { session, scope, timeoutSeconds, skills, sessionProfile: sessionProfileName, label: labelOf(params.label) });
        const started = text(`Started background sub-agent ${run.id} (${agent.name}).${forcedBackground(params)} Its answer arrives as a message when it finishes; task_result reads it, task_stop cancels it. ${PUSHED}${tree ? ` ${worktreeNote(tree)}` : ""}`);
        return { ...started, details: { task: run.id, owner: sm?.getSessionId?.() ?? "" } };
      }
      const r = await foreground(agent, task, cwd, { signal: signal ?? ctx?.signal, session, scope, timeoutSeconds, skills, sessionProfile: sessionProfileName }, onUpdate);
      const result = answered(r, session);
      if (tree) result.content.push({ type: "text", text: worktreeNote(tree) });
      return result;
    },
  });

  pi.registerTool({
    name: "subagent_continue",
    label: "Continue a sub-agent",
    ...delegationRenderers("Continuing agent", "id", profile, delegated),
    description: `Continue a sub-agent that stopped short (a resolved refusal, an answered question) in its own session, keeping what it read and did: give the id subagent returned (task id or sub-agent id) and the message it needs. ${relaunch} Its scope carries over; one stopped at its context cap gets a fresh one with a handoff instead.`,
    parameters: {
      type: "object", required: ["id", "message"],
      properties: {
        id: { type: "string", description: "The id subagent returned." },
        message: { type: "string", description: "What it needs to go on: the answer, the decision, or the resolved refusal. A `## Allowed edit surfaces` replaces its list." },
        timeout_seconds: TIMEOUT,
        label: { type: "string", description: "What it does now, a few words; else the message's first phrase." },
      },
    },
    async execute(_id: string, params: { id?: unknown; message?: unknown; timeout_seconds?: unknown; label?: unknown; background?: unknown; mode?: unknown }, signal: AbortSignal | undefined, onUpdate: unknown, ctx: any) {
      if (typeof params.id !== "string" || typeof params.message !== "string" || !params.message.trim()) throw new Error("subagent_continue needs an id and a message");
      const timeoutSeconds = timeoutOf(params.timeout_seconds);
      const sm = ctx?.sessionManager;
      const owner = sm?.getSessionId?.() ?? "";
      // A background sub-agent is named by its task id; its session by the call that launched it.
      const run = background?.agentRun(params.id, owner);
      if (run?.status === "running") throw new Error(`jhon: ${run.id} is still running; its answer arrives as a message`);
      if (run && !run.childSessionId) throw new Error(`jhon: ${run.id} ran without a saved session; launch a fresh ${run.agent} with a handoff`);
      const c = continuation(agentDir, { file: sm?.getSessionFile?.(), id: owner }, run?.childSessionId ?? params.id);
      // T120: continued, it runs on the session's profile as a fresh launch does.
      const agent = await profiled(c.agent, ctx);
      const sessionProfileName = sessionProfile.current();
      if (background) {
        const skills = childSkills(c.agent.body, resolvedSkills);
        const next = background.startAgent(agent, params.message, c.cwd, owner, { session: c.session, scope: continuedScope(c.scope, params.message), timeoutSeconds, skills, sessionProfile: sessionProfileName, label: labelOf(params.label) });
        const continued = text(`Continued sub-agent ${c.session.id} (${c.agent.name}) in the background as ${next.id}.${forcedBackground(params)} Its answer arrives as a message when it finishes; task_result reads it, task_stop cancels it. ${PUSHED}`);
        return { ...continued, details: { task: next.id, owner } };
      }
      return answered(await foreground(agent, params.message, c.cwd, { signal: signal ?? ctx?.signal, session: c.session, scope: continuedScope(c.scope, params.message), timeoutSeconds, skills: childSkills(c.agent.body, resolvedSkills), sessionProfile: sessionProfileName }, onUpdate), c.session);
    },
  });

  pi.registerTool({
    name: "m365_read",
    label: "Read Microsoft 365",
    description: "Find and read a document in Microsoft 365 (SharePoint, OneDrive), such as the HUs, through a headless Claude Code that can only search and read: give the document and the parts you need, e.g. \"HU-ABC-001 in HU_ACME_2026_VF01012026_STORIES.docx: its acceptance criteria and business rules, verbatim\". Returns the text it read with the document's name, path, and last-modified date. It takes minutes; copy what the change needs into its artifacts.",
    parameters: {
      type: "object", additionalProperties: false, required: ["request"],
      properties: { request: { type: "string", description: "The document and the parts of it to return." } },
    },
    async execute(_id: string, params: { request?: unknown }, signal: AbortSignal | undefined, _onUpdate: unknown, ctx: any) {
      if (typeof params.request !== "string" || !params.request.trim()) throw new Error("m365_read needs a request");
      const r = await m365Read(params.request, { bin: process.env.JHON_CLAUDE_BIN || "claude", model: process.env.JHON_M365_MODEL || "sonnet", signal: signal ?? ctx?.signal });
      if (!r.ok) throw new Error(r.text);
      return text(r.text);
    },
  });

  pi.registerTool({
    name: "ask_user",
    label: "Ask the owner",
    description: `Ask the owner 1 to ${MAX_QUESTIONS} questions and wait, questions and options in the owner's reply language. Each takes ${MIN_OPTIONS} to ${MAX_OPTIONS} options or none for a free answer; a typed answer is offered; never add one. multiSelect allows several picks. Recommended option first, label ending "(Recommended)" translated. Only the interactive main session asks.`,
    parameters: {
      type: "object", additionalProperties: false, required: ["questions"],
      properties: {
        questions: {
          type: "array", minItems: 1, maxItems: MAX_QUESTIONS,
          items: {
            type: "object", additionalProperties: false, required: ["question"],
            properties: {
              question: { type: "string", description: "The question, complete enough to answer without other context." },
              header: { type: "string", maxLength: MAX_HEADER, description: "A short label for it, e.g. \"Library\"." },
              options: {
                type: "array", minItems: MIN_OPTIONS, maxItems: MAX_OPTIONS, description: "The choices.",
                items: {
                  type: "object", additionalProperties: false, required: ["label"],
                  properties: {
                    label: { type: "string", description: "The choice, in a few words." },
                    description: { type: "string", description: "What choosing it means, and its trade-off." },
                  },
                },
              },
              multiSelect: { type: "boolean", description: "The owner may pick several options." },
            },
          },
        },
      },
    },
    async execute(_id: string, params: { questions?: unknown }, signal: AbortSignal | undefined, _onUpdate: unknown, ctx: any) {
      // A refusal (bad questions, a child, no UI) asks no one, so it waits for no one.
      const asks = !child && ctx?.hasUI && ctx.ui && typeof parseQuestions(params) !== "string";
      const r = asks ? await waitingForOwner(pi.events, "question", () => askUser(params, ctx, child, signal)) : await askUser(params, ctx, child, signal);
      if (!r.ok) throw new Error(r.text);
      return text(r.text);
    },
  });
}
