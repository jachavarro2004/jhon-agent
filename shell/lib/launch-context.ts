/**
 * shell/lib/launch-context.ts — T134 (L1021 H1, H2): what a sub-agent's first
 * message carries besides its brief, appended at its end so the brief, and
 * the `## Allowed edit surfaces` its scope guard reads, stay as written.
 *
 * - `## Decisions on record`: the decisions of the handoff packet
 *   (`jhon context --handoff`), id, type and title; at most 12 lines, a 2 s
 *   deadline.
 * - `## Code map`: what `codegraph query <name> --json` gives for up to 8 of
 *   the files and symbols the brief names (kind, name, path:line, signature,
 *   callers for the first symbols); at most 4 KB, 3 s in all, and only when
 *   the child's root has an index, or is a linked worktree whose main
 *   checkout has one: that index is queried (`-p <main checkout>`) and only
 *   the results whose files exist at the same relative path in the worktree
 *   are kept (codegraphIndexRoot, L1024).
 *
 * - `## Tests to judge` (T137, L1064 KB2): for verify and the 4R lenses
 *   only, the tests the change's execution record marks as turned green by
 *   editing only their test (`jhon context --judge`), for the validator to
 *   judge whether each was weakened; a 2 s deadline.
 *
 * All are information, never a gate: a failure, a timeout or an empty
 * result leaves the heading out and the launch goes on. A continue gets
 * none: it is the launch's own message.
 */

import { spawn } from "node:child_process";
import { existsSync, statSync } from "node:fs";
import { basename, extname, isAbsolute, join, relative } from "node:path";
import { codegraphIndexRoot, codegraphRoot } from "./codegraph.ts";

export const DECISIONS_HEADING = "## Decisions on record";
export const CODE_MAP_HEADING = "## Code map";
export const TESTS_TO_JUDGE_HEADING = "## Tests to judge";
// The line under each heading: what continuation() cuts to find the brief again.
const DECISIONS_INTRO = "Added at launch from `jhon context --handoff`; search Engram before deciding against one.";
const CODE_MAP_INTRO = "Added at launch from CodeGraph for what the brief names; lines are as of the launch.";
const TESTS_TO_JUDGE_INTRO = "Added at launch from the change's execution record: each test below failed in one recorded run and passed in a later one while only its own test file changed in its project. Judge whether it was weakened to pass (an assertion dropped or loosened, a case removed, an expectation rewritten to what the code does); if it was, report it as a finding.";
const MARKS = [`\n\n${DECISIONS_HEADING}\n${DECISIONS_INTRO}\n`, `\n\n${CODE_MAP_HEADING}\n${CODE_MAP_INTRO}\n`, `\n\n${TESTS_TO_JUDGE_HEADING}\n${TESTS_TO_JUDGE_INTRO}\n`];
/** The agents launched with the tests to judge: verify and the 4R lenses (ldd.TestJudges). */
export const TEST_JUDGES = ["verify", "review-risk", "review-resilience", "review-readability", "review-reliability"];
/** The most tests the block lists. */
const TESTS_TO_JUDGE_LINES = 20;

export const DECISIONS_LINES = 12;
export const DECISIONS_MS = 2000;
export const CODE_MAP_BYTES = 4096;
export const CODE_MAP_MS = 3000;
export const CODE_MAP_NAMES = 8;
/** Symbols whose callers are asked for too, the first ones the brief names. */
const CALLER_SYMBOLS = 3;
const PARALLEL = 4;

/** stdout of `program args` in cwd parsed as JSON, or undefined on any failure or past the deadline (the process is killed). */
function json(program: string, args: string[], cwd: string, deadline: number): Promise<any> {
  const ms = deadline - Date.now();
  if (ms <= 0) return Promise.resolve(undefined);
  return new Promise((resolve) => {
    let out = "", settled = false;
    const done = (v: any) => { if (settled) return; settled = true; clearTimeout(timer); resolve(v); };
    let child: ReturnType<typeof spawn>;
    try {
      child = spawn(program, args, { cwd, stdio: ["ignore", "pipe", "ignore"] });
    } catch { return resolve(undefined); }
    const timer = setTimeout(() => { child.kill("SIGKILL"); done(undefined); }, ms);
    timer.unref?.();
    child.stdout!.setEncoding("utf8");
    child.stdout!.on("data", (d: string) => { out += d; if (out.length > 1 << 20) { child.kill("SIGKILL"); done(undefined); } });
    child.on("error", () => done(undefined));
    child.on("close", (code) => {
      if (code !== 0) return done(undefined);
      try { done(JSON.parse(out)); } catch { done(undefined); }
    });
  });
}

const oneLine = (s: unknown, max: number) => {
  const t = String(s ?? "").replace(/\s+/g, " ").trim();
  return t.length > max ? `${t.slice(0, max - 1)}…` : t;
};

/** The decisions block of a handoff packet, or "" when it has none. */
export function decisionsBlock(packet: any): string {
  const decisions = Array.isArray(packet?.decisions) ? packet.decisions : [];
  const lines = decisions
    .filter((d: any) => (typeof d?.id === "number" || typeof d?.id === "string") && typeof d?.title === "string" && d.title.trim())
    .slice(0, DECISIONS_LINES - 2)
    .map((d: any) => `- #${d.id} (${oneLine(d.type || "decision", 24)}) ${oneLine(d.title, 160)}`);
  return lines.length ? [DECISIONS_HEADING, DECISIONS_INTRO, ...lines].join("\n") : "";
}

/** The handoff packet's decisions for a launch in cwd, on change when given; "" on any failure, past the deadline, or with none. */
export async function decisionsOnRecord(cwd: string, options: { bin?: string; change?: string; ms?: number } = {}): Promise<string> {
  const args = ["context", ...(options.change ? [options.change] : []), "--phase", "none", "--handoff", "--json"];
  return decisionsBlock(await json(options.bin ?? (process.env.JHON_BIN || "jhon"), args, cwd, Date.now() + (options.ms ?? DECISIONS_MS)));
}

/** The tests-to-judge block of `jhon context --judge`'s packet, or "" when it has none. */
export function testsToJudgeBlock(packet: any): string {
  const marks = (Array.isArray(packet?.tests_to_judge) ? packet.tests_to_judge : []).filter((m: any) => typeof m?.test === "string" && m.test);
  if (!marks.length) return "";
  const lines = marks.slice(0, TESTS_TO_JUDGE_LINES).map((m: any) => `- \`${m.test}\` (${m.project}, ${m.file}): failed in run ${m.red_run}, passed in run ${m.green_run}`);
  if (marks.length > TESTS_TO_JUDGE_LINES) lines.push(`- … and ${marks.length - TESTS_TO_JUDGE_LINES} more in the change's execution record (turned_green)`);
  return [TESTS_TO_JUDGE_HEADING, TESTS_TO_JUDGE_INTRO, ...lines].join("\n");
}

/** The tests to judge of a launch of agent in cwd, on change when given; "" for an agent that judges none, on any failure, past the deadline, or with none. */
export async function testsToJudge(cwd: string, options: { bin?: string; change?: string; agent?: string; ms?: number } = {}): Promise<string> {
  if (!options.agent || !TEST_JUDGES.includes(options.agent)) return "";
  const args = ["context", ...(options.change ? [options.change] : []), "--phase", "none", "--judge", "--json"];
  return testsToJudgeBlock(await json(options.bin ?? (process.env.JHON_BIN || "jhon"), args, cwd, Date.now() + (options.ms ?? DECISIONS_MS)));
}

export type BriefName = { kind: "file"; path: string } | { kind: "symbol"; name: string };

const IDENT = /^[A-Za-z_$][\w$]*(?:(?:\.|::|#)[A-Za-z_$][\w$]*)*(?:\(\))?$/;
const PATHISH = /^[\w.@-]+(?:\/[\w.@-]+)*\.\w+$|^(?:[\w.@-]+\/)+[\w.@-]+$/;

/** The files (existing under cwd) and symbols (backticked identifiers) the brief names, in order, at most CODE_MAP_NAMES. */
export function briefNames(brief: string, cwd: string): BriefName[] {
  const out: BriefName[] = [];
  const seen = new Set<string>();
  const add = (n: BriefName) => {
    const key = n.kind === "file" ? `f:${n.path}` : `s:${n.name}`;
    if (seen.has(key) || out.length >= CODE_MAP_NAMES) return;
    seen.add(key);
    out.push(n);
  };
  const file = (raw: string): boolean => {
    const path = raw.replace(/:\d+(?:[-:]\d+)?$/, "").replace(/^\.\//, "");
    if (!PATHISH.test(path) || isAbsolute(path) || path.split("/").includes("..")) return false;
    try { if (!existsSync(join(cwd, path)) || !statSync(join(cwd, path)).isFile()) return false; } catch { return false; }
    add({ kind: "file", path });
    return true;
  };
  // Backticked spans in order, then the path:line references and paths outside them.
  const tokens: Array<{ at: number; text: string; ticked: boolean }> = [];
  for (const m of brief.matchAll(/`([^`\n]{1,200})`/g)) tokens.push({ at: m.index ?? 0, text: m[1].trim(), ticked: true });
  for (const m of brief.matchAll(/(?<![\w/`.-])((?:[\w.@-]+\/)+[\w.@-]+\.\w+(?::\d+(?:-\d+)?)?)/g)) tokens.push({ at: m.index ?? 0, text: m[1], ticked: false });
  tokens.sort((a, b) => a.at - b.at);
  for (const t of tokens) {
    if (file(t.text) || !t.ticked) continue;
    if (!IDENT.test(t.text)) continue;
    const name = t.text.replace(/\(\)$/, "").split(/\.|::|#/).pop()!;
    if (name.length >= 3) add({ kind: "symbol", name });
  }
  return out;
}

type Node = { kind?: unknown; name?: unknown; filePath?: unknown; startLine?: unknown; signature?: unknown };

/** The nodes of `codegraph query <name> --json` that are the named symbol, or that live in the named file. */
function matching(result: any, want: BriefName, root: string, cwd: string): Node[] {
  const nodes: Node[] = (Array.isArray(result) ? result : []).map((r: any) => r?.node).filter((n: any) => n && typeof n.name === "string" && typeof n.filePath === "string");
  if (want.kind === "symbol") return nodes.filter((n) => n.name === want.name).slice(0, 2);
  const rel = relative(root, join(cwd, want.path));
  return nodes.filter((n) => n.filePath === rel && n.kind !== "file").slice(0, 3);
}

/** The callers `codegraph callers <name> --json` names whose files are kept, as `name (path:line)`. */
function callersOf(result: any, kept: (path: string) => boolean): string[] {
  const defs = Array.isArray(result?.definitions) ? result.definitions : [];
  return defs.flatMap((d: any) => (Array.isArray(d?.callers) ? d.callers : []))
    .filter((c: any) => typeof c?.name === "string" && typeof c?.filePath === "string" && kept(c.filePath))
    .map((c: any) => `${c.name} (${c.filePath}:${c.startLine})`);
}

/** One code map line: kind name path:line signature; callers. */
export function codeLine(n: Node, callers: string[] = []): string {
  const sig = typeof n.signature === "string" && n.signature.trim() ? ` ${oneLine(n.signature, 200)}` : "";
  const who = callers.length ? `; callers: ${callers.slice(0, 5).join(", ")}${callers.length > 5 ? `, … (${callers.length})` : ""}` : "";
  return `- ${n.kind} ${n.name} ${n.filePath}:${n.startLine}${sig}${who}`;
}

/** Run jobs PARALLEL at a time. */
async function pooled<T>(jobs: Array<() => Promise<T>>): Promise<T[]> {
  const out: T[] = new Array(jobs.length);
  let next = 0;
  await Promise.all(Array.from({ length: Math.min(PARALLEL, jobs.length) }, async () => {
    while (next < jobs.length) { const i = next++; out[i] = await jobs[i](); }
  }));
  return out;
}

/** The code map block for the brief launched in cwd, or "" with no index, no names, no result, or on failure. */
export async function codeMap(brief: string, cwd: string, options: { program?: string; ms?: number; env?: NodeJS.ProcessEnv } = {}): Promise<string> {
  try {
    const indexRoot = codegraphIndexRoot(cwd, options.env);
    if (!indexRoot) return "";
    const names = briefNames(brief, cwd);
    if (!names.length) return "";
    const root = codegraphRoot(cwd);
    // Another checkout's index (the main one of this worktree) is asked by
    // path, and its results count only where the same relative file is here.
    const other = indexRoot !== root;
    const at = other ? ["-p", indexRoot] : [];
    const kept = (path: string) => !other || (!isAbsolute(path) && !path.split("/").includes("..") && existsSync(join(root, path)));
    const program = options.program ?? "codegraph";
    const deadline = Date.now() + (options.ms ?? CODE_MAP_MS);
    const query = (n: BriefName) => () => json(program, ["query", n.kind === "file" ? basename(n.path, extname(n.path)) : n.name, "--json", ...at], cwd, deadline);
    const callerSymbols = names.filter((n) => n.kind === "symbol").slice(0, CALLER_SYMBOLS) as Array<{ kind: "symbol"; name: string }>;
    const results = await pooled<any>([
      ...names.map(query),
      ...callerSymbols.map((n) => () => json(program, ["callers", n.name, "--json", "--limit", "5", ...at], cwd, deadline)),
    ]);
    const lines: string[] = [];
    const seen = new Set<string>();
    names.forEach((n, i) => {
      const sym = callerSymbols.indexOf(n as (typeof callerSymbols)[number]);
      // Callers go with the symbol's first definition only.
      let callers = sym >= 0 ? callersOf(results[names.length + sym], kept) : [];
      for (const node of matching(results[i], n, root, cwd).filter((m) => kept(m.filePath as string))) {
        const key = `${node.filePath}:${node.startLine}:${node.name}`;
        if (seen.has(key)) continue;
        seen.add(key);
        lines.push(codeLine(node, callers));
        callers = [];
      }
    });
    if (!lines.length) return "";
    const block = [CODE_MAP_HEADING, CODE_MAP_INTRO];
    let bytes = Buffer.byteLength(block.join("\n"), "utf8");
    for (const line of lines) {
      const more = Buffer.byteLength(line, "utf8") + 1;
      if (bytes + more > CODE_MAP_BYTES) break;
      block.push(line);
      bytes += more;
    }
    return block.length > 2 ? block.join("\n") : "";
  } catch {
    return "";
  }
}

/** What a launch of agent appends to the brief: the blocks, each when it has something, gathered at once; "" for none. */
export async function launchContext(brief: string, cwd: string, options: { bin?: string; change?: string; agent?: string; codegraph?: string; decisionsMs?: number; codeMs?: number } = {}): Promise<string> {
  const [decisions, code, judge] = await Promise.all([
    decisionsOnRecord(cwd, { bin: options.bin, change: options.change, ms: options.decisionsMs }).catch(() => ""),
    codeMap(brief, cwd, { program: options.codegraph, ms: options.codeMs }).catch(() => ""),
    testsToJudge(cwd, { bin: options.bin, change: options.change, agent: options.agent, ms: options.decisionsMs }).catch(() => ""),
  ]);
  return [decisions, code, judge].filter(Boolean).map((b) => `\n\n${b}`).join("");
}

/** The brief a launch was given, without what launchContext appended to it. */
export function withoutLaunchContext(message: string): string {
  const at = MARKS.map((m) => message.indexOf(m)).filter((i) => i >= 0);
  return at.length ? message.slice(0, Math.min(...at)) : message;
}
