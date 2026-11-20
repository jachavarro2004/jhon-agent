/**
 * docs/media/captures.ts — the shell's own views, rendered for the media.
 *
 * Each capture calls a real render function of shell/lib/ui with fake data
 * built the way the shell's tests build it (shell/test/), painted by pi's
 * own Theme class loaded from rose ink's theme file in truecolor: the rows
 * are exactly what the terminal would receive. Times are fixed (`NOW`), the
 * home folder is fake, the settings are the defaults (never the owner's),
 * and every project, branch and file name is invented: no client data.
 */
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { SHELL } from "./paths.ts";

import { ShellSettings, installShellSettings } from "../../shell/lib/settings.ts";
import { renderAgentsSection, renderJobsSection, renderLddPanel, type RunningJobDisplay } from "../../shell/lib/ui/panel.ts";
import { PANE_TEXT } from "../../shell/lib/ui/pane.ts";
import type { ThemeLike } from "../../shell/lib/ui/primitives/index.ts";
import { renderStatusBar, renderTopRow, type BarModel } from "../../shell/lib/ui/bars.ts";
import { lddFromStatus } from "../../shell/lib/ui/ldd-status.ts";
import { AskView } from "../../shell/lib/ui/ask-view.ts";

/** A fixed clock: 2026-11-16 10:00 UTC. */
export const NOW = Date.parse("2026-11-16T10:00:00Z");
const HOME = "/home/dev";

/** A theme the shell's renderers accept: pi's Theme over rose ink, in truecolor. */
export interface MediaTheme extends ThemeLike {
  bg(color: string, text: string): string;
  underline(text: string): string;
}

/** pi's own Theme class, loaded from rose ink's file (no pi run, no pi config read). */
export async function loadTheme(themePath: string): Promise<MediaTheme> {
  const url = pathToFileURL(join(SHELL, "node_modules/@earendil-works/pi-coding-agent/dist/modes/interactive/theme/theme.js")).href;
  const mod = (await import(url)) as { loadThemeFromPath(path: string, mode: string): MediaTheme };
  return mod.loadThemeFromPath(themePath, "truecolor");
}

/** The shell's settings at their defaults: never the owner's, never a jhon run. */
export function defaultSettings(): void {
  installShellSettings(new ShellSettings(undefined, "media build: the defaults", () => NOW));
}

/** One capture: its file name, a title for the frame, its width in cells and its painted rows. */
export interface Capture {
  name: string;
  title: string;
  cols: number;
  lines: string[];
}

/** The bars' data: the invented project ~/src/lumen mid-change, in the sandbox. */
const BARS: BarModel = {
  cwd: `${HOME}/src/lumen`, branch: "feat/csv-export", sandbox: "on",
  modelId: "claude-opus-5-5", effort: "high", contextPercent: 38, contextWindow: 200_000, costTotal: 1.84, subscription: true,
};

/** A change in the shape `jhon status --json` reports (as ui-ldd-status.test.ts builds it). */
const LDD = lddFromStatus("invoice-csv-export", {
  route: { level: "verified", tier: "medium" }, next: "apply", strict_tdd: { value: true },
  tasks: {
    total: 9, completed: 5, slice: "Slice 2: stream large exports",
    slices: [
      { name: "Slice 1: the CSV writer", total: 4, completed: 4 },
      { name: "Slice 2: stream large exports", total: 3, completed: 1, delivers: "S4", tasks: [{ text: "2.1 chunked reader", done: true }, { text: "2.2 stream to the response", done: false }, { text: "2.3 back-pressure test", done: false }] },
      { name: "Slice 3: export button", total: 2, completed: 0 },
    ],
  },
  tests: { timestamp: "2026-11-16T09:52:00Z", passed: true, failed: [], scope: "affected", commit: "50e3ff2daea42318fbca2eec33f512f9220c946e" },
})!;

const JOBS: RunningJobDisplay[] = [
  { id: "agent-1", kind: "agent", label: "jhon-builder: stream to the response", startedAt: NOW - 252_000, tokens: 48_200, step: "bash · go test ./export/..." },
  { id: "agent-2", kind: "agent", label: "jhon-scout: where invoices are read", startedAt: NOW - 95_000, tokens: 12_900 },
  { id: "agent-3", kind: "agent", label: "reviewer: slice 1", startedAt: NOW - 900_000, endedAt: NOW - 30_000, status: "completed", exitCode: 0 },
  { id: "job-1", kind: "command", label: "go test ./... -run Export", startedAt: NOW - 41_000 },
  { id: "job-2", kind: "monitor", label: "watch: tail -f logs/export.log", startedAt: NOW - 600_000 },
];

/*
 * The engine's own words, for the screens that show them. Each was printed by
 * the real `jhon` binary (built from this checkout) in a sandbox: HOME, XDG_*
 * and JHON_SETTINGS in a scratch folder, JHON_ENGRAM=/dev/null/engram, and the
 * invented project ~/src/lumen (a Go module, `export.Row`, its change
 * invoice-csv-export under `delivery: change`, two slices, 2.1 unchecked). The
 * scratch folder's path is written as /home/dev, and the grant's times are set
 * to the media clock (NOW); nothing else is changed. Recorded 2026-11-16:
 *   EXEC_RED / EXEC_GREEN  `jhon executions run invoice-csv-export --affected main`,
 *                          the test failing (Row without the amount), then passing
 *   SCOPE_REFUSAL          `jhon hook scope-guard --agent jhon-builder` on a jhon-builder's Edit of
 *                          api/invoices.go, its launch prompt allowing export/stream.go
 *                          and export/stream_test.go (as test/guards/scope_guard_test.go)
 *   ROUTE_REFUSAL          `jhon hook route-guard` on the session's `gh pr create --fill`
 *   AUDIT_OUT              `jhon audit <session>.jsonl` of an invented Claude Code session
 *                          that edits export/csv.go, commits and pushes to main with no run
 * When the engine's wording changes, record them again the same way.
 */
const EXEC_RED: readonly string[] = ["==> .: go test ./...","--- FAIL: TestRowHasTheAmount (0.00s)","    csv_test.go:7: Row = \"INV-7,Acme Ltd\", want the amount in the last column","FAIL","FAIL\texample.com/lumen/export\t0.001s","FAIL","<== .: exit 1","==> . (lint): go vet ./...","<== . (lint): exit 0","==> . (format): ! gofmt -l . | grep .","<== . (format): exit 0","","record     openspec/changes/invoice-csv-export/execution-record.json","scope      full (affected since main)","candidate  50e3ff2daea42318fbca2eec33f512f9220c946e  tree 93227b3d782a53d509ea19fdc43d2c75447322b9  clean false","fail       .","pass       . (lint)","pass       . (format)","note       1 changed file(s) under openspec/ ignored: the change's record is not project input","note       every project selected: export/csv_test.go is outside every project (root configuration, a lockfile, CI, or a shared file)","jhon executions: fail, exit 1 (2 of 3 passed; record openspec/changes/invoice-csv-export/execution-record.json)"];
const EXEC_GREEN: readonly string[] = ["==> .: go test ./...","ok  \texample.com/lumen/export\t0.001s","<== .: exit 0","==> . (lint): go vet ./...","<== . (lint): exit 0","==> . (format): ! gofmt -l . | grep .","<== . (format): exit 0","","record     openspec/changes/invoice-csv-export/execution-record.json","scope      full (affected since main)","candidate  50e3ff2daea42318fbca2eec33f512f9220c946e  tree 93227b3d782a53d509ea19fdc43d2c75447322b9  clean false","pass       .","pass       . (lint)","pass       . (format)","note       2 changed file(s) under openspec/ ignored: the change's record is not project input","note       every project selected: export/csv.go is outside every project (root configuration, a lockfile, CI, or a shared file)","note       every project selected: export/csv_test.go is outside every project (root configuration, a lockfile, CI, or a shared file)","note       the last recorded test run took: . 4 s; a run that narrows takes less","jhon executions: pass, exit 0 (3 of 3 passed; record openspec/changes/invoice-csv-export/execution-record.json)"];
const AUDIT_OUT: readonly string[] = ["audit  5e7c1a90-0000-4000-8000-00000000c5a1  (claude-code 2.1.304)","  session  sessionKind: interactive","  suspect    delivered-untested [heuristic]              5e7c1a90-0000-4000-8000-00000000c5a1.jsonl:8  main (main)  indeterminate","             a push no change can be tied to: no launch block or openspec path names one, and no earlier jhon executions run or jhon deliver check in this agent does, so no test run can be read for it","             evidence: git push origin HEAD:main","  timing  main  turns 4  model 1m (20s/turn)  tools 1m  cache 88%","  dialogue  owner answers 0","  dialogue  ask-tool calls 0  questions per call 1:0 2:0 3:0 4+:0  prose questions 0 (lower bound)","  integrations  orchestrator  launches 1  memory read 0  memory write 0  codegraph 0  plain reads 1  codegraph share 0.0%  todo 0","  outcomes  change              elapsed  active  agent-time  main-time  main-files  launches  verify  replans  relaunches  tests","    invoice-csv-export  2m       2m      0s          2m         1           0         0       0        0           0","  session  elapsed 3m  active 3m  unattributed 20s  resumes 0","  ldd  invoice-csv-export  slices 2  owner messages 0  per slice 0","0 violation(s), 1 suspect(s)"];
const SCOPE_REFUSAL: readonly string[] = ["scope-guard: Edit /home/dev/src/lumen/api/invoices.go is outside the allowed edit surfaces (export/stream.go, export/stream_test.go); stop and return status: blocked naming the file and why the change needs it, so the orchestrator can widen the list (contracts/efficiency.md, \"Writer scope\")"];
const ROUTE_REFUSAL: readonly string[] = ["route-guard: delivery: change opens one PR per change, after its last slice: invoice-csv-export has 1 unchecked task(s) (2.1 chunked reader). Commit each slice on the branch and open the PR when every task is done."];
const MODE_GRANT: Array<[string, string[]]> = [
  ["jhon mode", ["normal for /home/dev/src/lumen"]],
  ["jhon grant push --for 30m", ["granted push for /home/dev/src/lumen until 2026-11-16T10:28:00.000Z"]],
  ["jhon grant --list", ["push           /home/dev/src/lumen  live until 2026-11-16T10:28:00.000Z"]],
];

/** Tabs to spaces at the terminal's 8-column stops. */
function untab(line: string): string {
  let out = "";
  for (const ch of line) out += ch === "\t" ? " ".repeat(8 - (out.length % 8)) : ch;
  return out;
}

/** A line as a terminal `cols` wide shows it: wrapped hard at its width. */
function wrapAt(line: string, cols: number): string[] {
  const s = untab(line);
  if (s.length <= cols) return [s];
  const rows: string[] = [];
  for (let i = 0; i < s.length; i += cols) rows.push(s.slice(i, i + cols));
  return rows;
}

/** A typed command, as the panel shows a prompt: `$` in the accent, the command bold. */
const typed = (theme: MediaTheme, command: string) => `${theme.fg("accent", "$")} ${theme.bold(command)}`;

/** The rows a terminal `cols` wide shows for a command and its output; `keep` picks the lines shown, the rest marked by one dim `…`. */
function terminal(theme: MediaTheme, cols: number, command: string, output: readonly string[], keep: (line: string) => boolean = () => true): string[] {
  const rows = [typed(theme, command)];
  let skipped = false;
  for (const line of output) {
    if (!keep(line)) {
      if (!skipped) rows.push(theme.fg("dim", "…"));
      skipped = true;
      continue;
    }
    skipped = false;
    rows.push(...wrapAt(line, cols));
  }
  return rows;
}

/**
 * A tool call the guards refused, as the shell draws it: its tool card
 * (shell/extensions/quiet.ts, the call row and the error result, expanded
 * with ctrl+o so the whole reason shows), on pi's default keybindings and
 * this theme as pi's global one.
 */
async function refusedCard(theme: MediaTheme, tool: "edit" | "bash", args: Record<string, unknown>, reason: string, width: number): Promise<string[]> {
  const pi = join(SHELL, "node_modules/@earendil-works");
  const keys = (await import(pathToFileURL(join(pi, "pi-coding-agent/dist/core/keybindings.js")).href)) as { KeybindingsManager: new () => unknown };
  const tui = (await import(pathToFileURL(join(pi, "pi-tui/dist/index.js")).href)) as { setKeybindings(k: unknown): void };
  const themes = (await import(pathToFileURL(join(pi, "pi-coding-agent/dist/modes/interactive/theme/theme.js")).href)) as { setThemeInstance(t: unknown): void };
  tui.setKeybindings(new keys.KeybindingsManager());
  themes.setThemeInstance(theme);
  const { createQuietToolRenderer } = await import("../../shell/extensions/quiet.ts");
  const r = createQuietToolRenderer(tool) as unknown as {
    renderCall(args: unknown, theme: unknown, ctx: unknown): { render(w: number): string[] };
    renderResult(result: unknown, options: unknown, theme: unknown, ctx: unknown): { render(w: number): string[] };
  };
  const ctx = { toolCallId: `media-${tool}`, args, isPartial: false, isError: true, executionStarted: false, expanded: true, state: {} };
  return [
    ...r.renderCall(args, theme, ctx).render(width),
    ...r.renderResult({ content: [{ type: "text", text: reason }] }, { isPartial: false, expanded: true }, theme, ctx).render(width),
  ];
}

/** Every feature capture, rendered through the shell's own functions. */
export async function captures(theme: MediaTheme): Promise<Capture[]> {
  defaultSettings();
  const wide = 112;
  const out: Capture[] = [];

  // The top row and the status bar as a terminal `wide` columns wide gets them
  // (the status bar's narrow form, under 140), a blank row between them.
  out.push({
    name: "shell-bar", title: "the shell's bars", cols: wide,
    lines: [renderTopRow(BARS, theme, wide, { home: HOME }), "", renderStatusBar(BARS, theme, wide)],
  });

  // The right pane's sections at its width (shell/lib/ui/panel.ts): the LDD
  // section, then Agents and Jobs a blank row apart, as the pane stacks them.
  const pane = PANE_TEXT;
  out.push({
    name: "ldd-status", title: "LDD status", cols: pane,
    lines: renderLddPanel(LDD, theme, pane, { now: NOW }),
  });

  out.push({
    name: "agents-jobs", title: "agents and jobs", cols: pane,
    lines: [...renderAgentsSection(JOBS, theme, pane, { now: NOW }), "", ...renderJobsSection(JOBS, theme, pane, { now: NOW })],
  });

  const pw = 84;
  // A repo with no LDD yet: at the first code change the session asks once how
  // to deliver and test (prose/skills/ldd/references/init.md, step 3: one
  // batched question, each option naming the recommended one), through the
  // ask tool, which the shell draws as AskView. The wording is the agent's;
  // the choices are the ones init.md names.
  const ask = new AskView({
    step: 1, total: 2, title: "Delivery", multi: false, theme, done() {},
    question: "How should this project deliver its changes?",
    items: [
      { value: "slice", label: "Each slice on its own (Recommended)", description: "each slice is delivered to the default branch on its own; no delivery key" },
      { value: "change", label: "The whole change in one merge", description: "after its last slice: delivery: change" },
    ],
  });
  out.push({ name: "ldd-question", title: "the first code change", cols: pw, lines: ask.render(pw) });

  // The guards' refusals: the shell's tool card, whose result row shows the
  // engine's reason cut to the width, then the same call fed to the hook, its
  // whole answer as the engine prints it.
  out.push({
    name: "guard-refusal", title: "a refused tool call", cols: wide,
    lines: [...await refusedCard(theme, "edit", { path: `${HOME}/src/lumen/api/invoices.go`, edits: [{ oldText: "h.store.All()", newText: "h.store.Each(ctx)" }] }, SCOPE_REFUSAL.join("\n"), wide),
      "", ...terminal(theme, wide, "jhon hook scope-guard --agent jhon-builder < call.json", SCOPE_REFUSAL)],
  });
  out.push({
    name: "route-refusal", title: "route-guard", cols: wide,
    lines: [...await refusedCard(theme, "bash", { command: "gh pr create --fill" }, ROUTE_REFUSAL.join("\n"), wide),
      "", ...terminal(theme, wide, "jhon hook route-guard < call.json", ROUTE_REFUSAL)],
  });

  // The engine's commands in a terminal: what each printed, the lint and
  // format checks, the candidate commit and the notes of a run left out (…).
  const run = "jhon executions run invoice-csv-export --affected main";
  const shown = (line: string) => !/^(==>|<==) \. \((lint|format)\)/.test(line) && !/^(candidate|note) /.test(line);
  out.push({
    name: "executions-red-green", title: "jhon executions", cols: wide,
    lines: [...terminal(theme, wide, run, EXEC_RED, shown), "", ...terminal(theme, wide, run, EXEC_GREEN, shown)],
  });
  out.push({
    name: "mode-grant", title: "jhon mode and jhon grant", cols: pw,
    lines: MODE_GRANT.flatMap(([cmd, said], i) => [...(i ? [""] : []), ...terminal(theme, pw, cmd, said)]),
  });
  out.push({ name: "audit", title: "jhon audit", cols: wide, lines: terminal(theme, wide, "jhon audit ~/.claude/projects/-home-dev-src-lumen/5e7c1a90-0000-4000-8000-00000000c5a1.jsonl", AUDIT_OUT) });
  return out;
}
