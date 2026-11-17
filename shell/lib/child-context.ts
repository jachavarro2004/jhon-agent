/**
 * shell/lib/child-context.ts — the lean base a sub-agent child starts from (T115f).
 *
 * A child pi process runs with `--no-skills` and `--no-context-files` (agents.ts
 * childArgs), so it carries neither pi's skills catalog nor the raw context
 * files. What it needs from them is passed in once, at spawn:
 *
 * - its body without the orchestrator-only sections (ORCHESTRATOR_ONLY), and
 *   without the memory protocol when it may call no memory tool;
 * - the project's context files (pi's own discovery, loadProjectContextFiles)
 *   filtered the same way, rendered as pi renders them;
 * - the exact skill paths the parent resolved, as `--skill` arguments;
 * - the rule that a file already read in this child session is not read again.
 *
 * Everything is computed before the child starts and passed as arguments, so
 * the child's system prompt stays byte-identical for its whole run.
 */

import { loadProjectContextFiles } from "@earendil-works/pi-coding-agent";

/** Sections (by heading text) only the orchestrator acts on: talking to the owner, delegation, delivery. */
export const ORCHESTRATOR_ONLY = ["Routing"];

/** Sections that carry the memory protocol, dropped for a child with no memory tool. */
export const MEMORY_SECTIONS = ["Engram", "Engram Persistent Memory"];

/** Explicit markers a context file may use around orchestrator-only prose. */
const MARKED = /<!--\s*(?:jhon:)?orchestrator-only\s*-->[\s\S]*?<!--\s*\/(?:jhon:)?orchestrator-only\s*-->\n?/g;

/** The rule a child follows so a handoff by path is not paid twice. */
export const NO_REREAD = "Do not re-read a skill file, spec section, or other file you already read in this session unless it changed since; use what you read.";

/** True when one of `tools` is a memory tool (Engram's mem_*). */
export function hasMemoryTools(tools: string[]): boolean {
  return tools.some((t) => /(^|__)mem_[a-z_]+$/.test(t));
}

/**
 * `text` without the Markdown sections whose heading is one of `headings`
 * (each up to the next heading of the same or a higher level) and without
 * marker-delimited orchestrator-only blocks. Headings inside code fences do
 * not count.
 */
export function withoutSections(text: string, headings: string[]): string {
  const names = new Set(headings.map((h) => h.toLowerCase()));
  const out: string[] = [];
  let fence = false;
  let dropping = 0; // the level of the section being dropped, 0 when keeping
  for (const line of text.replace(MARKED, "").split("\n")) {
    if (/^\s*(```|~~~)/.test(line)) fence = !fence;
    const h = fence ? null : /^(#{1,6})\s+(.*?)\s*#*\s*$/.exec(line);
    if (h) {
      const level = h[1].length;
      if (dropping && level <= dropping) dropping = 0;
      if (!dropping && names.has(h[2].toLowerCase())) dropping = level;
    }
    if (!dropping) out.push(line);
  }
  return out.join("\n").replace(/\n{3,}/g, "\n\n").trim();
}

/** The headings a child with `tools` never receives. */
export function childDroppedSections(tools: string[]): string[] {
  return hasMemoryTools(tools) ? ORCHESTRATOR_ONLY : [...ORCHESTRATOR_ONLY, ...MEMORY_SECTIONS];
}

/** A child's body: its prompt without what only the orchestrator or a memory-using agent reads. */
export function leanBody(body: string, tools: string[]): string {
  return body ? withoutSections(body, childDroppedSections(tools)) : "";
}

/** A context file as pi discovers it. */
export type ContextFile = { path: string; content: string };

/** The context files, filtered for a child with `tools`, rendered as pi renders its project context; "" when none is left. */
export function renderChildContext(files: ContextFile[], tools: string[]): string {
  const drop = childDroppedSections(tools);
  const kept = files.map((f) => ({ path: f.path, content: withoutSections(f.content, drop) })).filter((f) => f.content);
  if (!kept.length) return "";
  return [
    "Project-specific instructions and guidelines:",
    ...kept.map(({ path, content }) => `<project_instructions path="${path}">\n${content}\n</project_instructions>`),
  ].join("\n\n");
}

/** The context files pi would load for a session in `cwd` on `agentDir`; none when they cannot be read. */
export function discoverContextFiles(cwd: string, agentDir: string): ContextFile[] {
  try {
    return loadProjectContextFiles({ cwd, agentDir }).map((f: ContextFile) => ({ path: f.path, content: f.content }));
  } catch {
    return [];
  }
}

/** What a child gets beyond its body: the filtered context and the no-re-read rule, as one appended block. */
export function childContext(cwd: string, agentDir: string, tools: string[]): string {
  const context = renderChildContext(discoverContextFiles(cwd, agentDir), tools);
  return context ? `${context}\n\n${NO_REREAD}` : NO_REREAD;
}
