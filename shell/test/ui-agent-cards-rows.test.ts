// T174-S7: the agent cards on design/shell-tui 01, as exact rows at the
// conversation's width beside the pane (133 columns at 180) and at 100: the
// Prism review card (`◇ Prism review  <change>`, the verdict at the right,
// the facets, the challenger and the arbiter, each kept finding, the record),
// the delegation card (`→ Delegating · agent · model · effort`, `◐ running` at
// the right, the background line under it) and the thinking label; their
// expanded (ctrl+o), ASCII, emoji, minimal and neon (outline) forms.
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { initTheme, keyHint } from "@earendil-works/pi-coding-agent";
import { visibleWidth } from "@earendil-works/pi-tui";
import { installShellSettings } from "../lib/settings.ts";
import { setCardStyle, stripAnsi } from "../lib/ui/primitives/index.ts";
import { reviewRenderers } from "../lib/ui/review-card.ts";
import { delegationRenderers } from "../lib/ui/delegation-card.ts";
import { installThinkingLabel, thinkingLabel } from "../lib/ui/thinking-label.ts";
import { memorySettings } from "./fake-jhon-settings.ts";
import { assertCardRows, padded, spread } from "./rows.ts";

initTheme("dark");
installShellSettings(memorySettings({}));
setCardStyle("float");

const EXPAND = stripAnsi(keyHint("app.tools.expand", "to expand"));
const COLLAPSE = stripAnsi(keyHint("app.tools.expand", "to collapse"));

interface Row {
	args: Record<string, unknown>;
	text?: string;
	details?: unknown;
	isError?: boolean;
	isPartial?: boolean;
	expanded?: boolean;
}

type Renderers = { renderCall: Function; renderResult: Function };

/** One tool row as pi draws it: the call component, then the result component, sharing one row state. */
function card(make: () => Renderers, row: Row) {
	return (width: number, theme: unknown): string[] => {
		const r = make() as any;
		const context = {
			args: row.args, toolCallId: "rows-agent", state: {}, cwd: "/repo", executionStarted: true, argsComplete: true,
			isPartial: row.isPartial ?? false, expanded: row.expanded ?? false, showImages: false, isError: row.isError ?? false, invalidate() {},
		};
		const call = r.renderCall(row.args, theme, context);
		const result = row.text === undefined ? undefined
			: r.renderResult({ content: [{ type: "text", text: row.text }], details: row.details }, { expanded: context.expanded, isPartial: context.isPartial }, theme, context);
		return [...call.render(width), ...(result?.render(width) ?? [])];
	};
}

function withSettings(t: { after(fn: () => void): void }, values: Record<string, unknown>, style: "float" | "neon" = "float"): void {
	const previous = installShellSettings(memorySettings(values));
	setCardStyle(style);
	t.after(() => { installShellSettings(previous); setCardStyle("float"); });
}

/** A body row: the card's margin and its three-cell inset. */
const body = (text: string, width: number) => padded(`    ${text}`, width);
/** The cells a body row's text gets at width: the margins, the inset and the end. */
const room = (width: number) => width - 2 - 3 - 2;
/** Plain text cut to n cells with `…` (or `~` in ASCII), as a card cuts a row. */
const cut = (text: string, n: number, mark = "…") => (text.length <= n ? text : `${text.slice(0, n - 1)}${mark}`);

// --- the Prism review card ------------------------------------------------------------------

const CLAIM = "an empty range answers 200 with a header-only CSV; spec S4 asks for 422";
const RECORD = "odd/changes/add-invoice-export/review-prism.md";
/** The details jhon_review returns for design 01's review: four facets, two findings, one kept. */
const REVIEWED = {
	status: "recorded", record: RECORD, verdict: "fail", change: "add-invoice-export",
	facets: [{ facet: "safety", status: "completed" }, { facet: "clarity", status: "completed" }, { facet: "behavior", status: "completed" }, { facet: "failure", status: "completed" }],
	findings: [
		{ id: "failure-1", facet: "failure", severity: "CRITICAL", location: "billing/api/export.py:88", claim: CLAIM, set: "correction_set" },
		{ id: "safety-1", facet: "safety", severity: "WARNING", location: "billing/api/auth.py:12", claim: "the token is read twice", set: "refuted" },
	],
	challenger: { findings: 2, refuted: 1 },
	arbiter: { asked: 1, confirmed: 1 },
};
const REVIEW_TEXT = `Prism native review: verdict fail · facets safety, clarity, behavior, failure · record ${RECORD}`;
const REVIEW: Row = { args: {}, text: REVIEW_TEXT, details: REVIEWED };
const FACETS = "facets      ✓ safety   ✓ clarity   ✓ behavior   ✗ failure 1 finding";
const CHALLENGER = "challenger  kept 1 of 2 findings · arbiter agreed";
const KEPT = `✗ critical  billing/api/export.py:88  ${CLAIM}`;

test("the Prism review card: the change, the verdict at the right, facets, challenger, each kept finding, the record", () => {
	assertCardRows(card(reviewRenderers, REVIEW), (w) => [
		spread("  ◇ Prism review  add-invoice-export", `${EXPAND}  ✗ changes requested  `, w),
		body(FACETS, w),
		body(CHALLENGER, w),
		body(cut(KEPT, room(w)), w),
		body(`record      ${RECORD}`, w),
	], "review");
});

test("ctrl+o shows the full findings: every claim whole, the refuted ones too", () => {
	const wrapped = (w: number) => {
		// The claim wraps under the location's column, twelve cells in.
		const first = `✗ critical  billing/api/export.py:88  ${CLAIM}`;
		if (first.length <= room(w)) return [body(first, w)];
		const words = CLAIM.split(" ");
		const head = `✗ critical  billing/api/export.py:88  `;
		let line = head;
		const rows: string[] = [];
		for (const word of words) {
			const next = line === head || line.trim() === "" ? `${line}${word}` : `${line} ${word}`;
			if (next.length > room(w)) { rows.push(line); line = `${" ".repeat(12)}${word}`; } else line = next;
		}
		rows.push(line);
		return rows.map((row) => body(row, w));
	};
	assertCardRows(card(reviewRenderers, { ...REVIEW, expanded: true }), (w) => [
		spread("  ◇ Prism review  add-invoice-export", `${COLLAPSE}  ✗ changes requested  `, w),
		body(FACETS, w),
		body(CHALLENGER, w),
		...wrapped(w),
		body("refuted     billing/api/auth.py:12  the token is read twice", w),
		body(`record      ${RECORD}`, w),
	], "review expanded");
});

test("an approved review: `✓ approved`, every facet clean, no finding to show", () => {
	const approved = { ...REVIEWED, verdict: "pass", findings: [], challenger: undefined, arbiter: undefined };
	assertCardRows(card(reviewRenderers, { args: {}, text: REVIEW_TEXT, details: approved }), (w) => [
		spread("  ◇ Prism review  add-invoice-export", "✓ approved  ", w),
		body("facets      ✓ safety   ✓ clarity   ✓ behavior   ✓ failure", w),
		body(`record      ${RECORD}`, w),
	], "approved");
});

test("the arbiter's word: `agreed` when it confirmed all it was asked, else how many; an unavailable facet says so", () => {
	const split = {
		...REVIEWED,
		facets: [{ facet: "safety", status: "unavailable", reason: "the completion failed: timeout" }, { facet: "failure", status: "completed" }],
		findings: [
			{ id: "failure-1", facet: "failure", severity: "CRITICAL", location: "a.py:1", claim: "one", set: "correction_set" },
			{ id: "failure-2", facet: "failure", severity: "BLOCKER", location: "a.py:2", claim: "two", set: "for_verify" },
		],
		challenger: { findings: 2, refuted: 0 },
		arbiter: { asked: 2, confirmed: 1 },
	};
	assertCardRows(card(reviewRenderers, { args: {}, text: REVIEW_TEXT, details: split }), (w) => [
		spread("  ◇ Prism review  add-invoice-export", `${EXPAND}  ✗ changes requested  `, w),
		body("facets      ✗ safety unavailable   ✗ failure 2 findings", w),
		body("challenger  kept 2 of 2 findings · arbiter confirmed 1 of 2", w),
		body("✗ critical  a.py:1  one", w),
		body("◐ blocker   a.py:2  two", w),
		body(`record      ${RECORD}`, w),
	], "split");
});

test("a review still running: `◐ running` at the right; one that fell back to its agents says so with the tool's words", () => {
	assertCardRows(card(reviewRenderers, { args: {}, isPartial: true }), (w) => [spread("  ◇ Prism review", "◐ running  ", w)], "running");
	const fallback = "jhon_review could not run natively: no pi model for prism-safety in the active profile.\nRun the Prism review with its agents instead: launch the prism-safety agents.";
	assertCardRows(card(reviewRenderers, { args: {}, text: fallback, details: { status: "fallback" } }), (w) => [
		spread("  ◇ Prism review", "◐ agents instead  ", w),
		body("jhon_review could not run natively: no pi model for prism-safety in the active profile.", w),
		body("Run the Prism review with its agents instead: launch the prism-safety agents.", w),
	], "fallback");
	const refused = "jhon review start refused (exit 2): no change selected";
	assertCardRows(card(reviewRenderers, { args: {}, text: refused, details: { status: "refused" } }), (w) => [
		spread("  ◇ Prism review", "✗ failed  ", w),
		body(refused, w),
	], "refused");
});

test("the review card in ASCII, with emoji icons, and in minimal density", (t) => {
	withSettings(t, { "shell.appearance.ascii": true });
	assertCardRows(card(reviewRenderers, REVIEW), (w) => [
		spread("  a Prism review  add-invoice-export", `${EXPAND}  x changes requested  `, w),
		body("facets      + safety   + clarity   + behavior   x failure 1 finding", w),
		body("challenger  kept 1 of 2 findings - arbiter agreed", w),
		body(cut(`x critical  billing/api/export.py:88  ${CLAIM}`, room(w), "~"), w),
		body(`record      ${RECORD}`, w),
	], "ascii review");
});

test("the review card with emoji icons and in minimal density", (t) => {
	withSettings(t, { "shell.appearance.icons": "emoji", "shell.appearance.density": "minimal" });
	assertCardRows(card(reviewRenderers, REVIEW), (w) => [
		spread("  🤖 Prism review  add-invoice-export", `${EXPAND}  ✗ changes requested  `, w),
		body(FACETS, w),
		body(cut(KEPT, room(w)), w),
	], "emoji minimal review");
});

test("the review card in neon: the outline, the verdict in its top edge", (t) => {
	withSettings(t, {}, "neon");
	const rows = card(reviewRenderers, REVIEW)(133, { fg: (_r: string, s: string) => s, bold: (s: string) => s }).map(stripAnsi);
	assert.match(rows[0]!, /^ ╭─ ◇ Prism review {2}add-invoice-export ─+ .+ {2}✗ changes requested ─╮ $/);
	assert.match(rows[1]!, /^ │ facets {6}✓ safety/);
	assert.match(rows.at(-1)!, /^ ╰─+╯ $/);
	for (const row of rows) assert.equal(visibleWidth(row), 133);
});

// --- the delegation card ------------------------------------------------------------------

const PROFILE = (agent: string) => (agent === "jhon-builder" ? { model: "claude-opus-5-5", thinking: "high" } : undefined);
const STARTED = "Started background sub-agent agent-3 (jhon-builder).";
const LONG = `${STARTED} Its answer arrives as a message when it finishes; task_result reads it, task_stop cancels it. Its completion is pushed automatically; continue other work or end the turn.`;
const statuses = new Map<string, string>([["agent-3", "running"]]);
const delegating = () => delegationRenderers("Delegating", "agent", PROFILE, (task) => statuses.get(task) as any) as unknown as Renderers;
const DELEGATION: Row = { args: { agent: "jhon-builder", task: "slice 3" }, text: STARTED, details: { task: "agent-3", owner: "s1" } };

test("the delegation card: `→ Delegating · agent · model · effort`, `◐ running` at the right, the background line under it", () => {
	statuses.set("agent-3", "running");
	assertCardRows(card(delegating, DELEGATION), (w) => [
		spread("  → Delegating · jhon-builder · claude-opus-5-5 · high", "◐ running  ", w),
		body(STARTED, w),
	], "delegating");
});

test("the delegation card follows its agent: `✓ done` once it answered, `✗ failed` when it could not", (t) => {
	t.after(() => statuses.set("agent-3", "running"));
	statuses.set("agent-3", "completed");
	assertCardRows(card(delegating, DELEGATION), (w) => [spread("  → Delegating · jhon-builder · claude-opus-5-5 · high", "✓ done  ", w), body(STARTED, w)], "done");
	statuses.set("agent-3", "failed");
	assertCardRows(card(delegating, DELEGATION), (w) => [spread("  → Delegating · jhon-builder · claude-opus-5-5 · high", "✗ failed  ", w), body(STARTED, w)], "failed");
	statuses.set("agent-3", "stopped");
	assertCardRows(card(delegating, DELEGATION), (w) => [spread("  → Delegating · jhon-builder · claude-opus-5-5 · high", "✗ stopped  ", w), body(STARTED, w)], "stopped");
});

test("a delegation's call before its result: `◐ running`, no body; an unknown agent names only itself", () => {
	assertCardRows(card(delegating, { args: { agent: "jhon-builder" }, isPartial: true }), (w) => [spread("  → Delegating · jhon-builder · claude-opus-5-5 · high", "◐ running  ", w)], "launching");
	assertCardRows(card(delegating, { args: { agent: "nobody" }, isPartial: true }), (w) => [spread("  → Delegating · nobody", "◐ running  ", w)], "unknown");
});

test("the background line keeps every word: a long one wraps; ctrl+o shows the rest of a foreground answer", () => {
	for (const w of [133, 100]) {
		const rows = card(delegating, { ...DELEGATION, text: LONG })(w, { fg: (_r: string, s: string) => s, bold: (s: string) => s }).map(stripAnsi);
		assert.equal(rows.slice(1).map((row) => row.trim()).join(" "), LONG, `every word at ${w}`);
		assert.ok(rows.length > 2, `it wraps at ${w}`);
	}
	const continuing = () => delegationRenderers("Continuing agent", "id", PROFILE) as unknown as Renderers;
	const answer = "Done: slice 3 is green.\n\n4/4 tasks · 3 files";
	assertCardRows(card(continuing, { args: { id: "jhon-builder-3" }, text: answer }), (w) => [
		spread("  → Continuing agent · jhon-builder-3", `${EXPAND}  ✓ done  `, w),
		body("Done: slice 3 is green.", w),
	], "foreground answer");
	assertCardRows(card(continuing, { args: { id: "jhon-builder-3" }, text: answer, expanded: true }), (w) => [
		spread("  → Continuing agent · jhon-builder-3", `${COLLAPSE}  ✓ done  `, w),
		body("Done: slice 3 is green.", w),
		body("", w),
		body("4/4 tasks · 3 files", w),
	], "foreground answer expanded");
});

test("a delegation that failed: the error band and its reason", () => {
	const why = "jhon: agent-3 is still running; its answer arrives as a message";
	assertCardRows(card(delegating, { args: { agent: "jhon-builder" }, text: why, isError: true }), (w) => [
		spread("  → Delegating · jhon-builder · claude-opus-5-5 · high", "✗ failed  ", w),
		body(why, w),
	], "error");
});

test("the delegation card in ASCII and in neon", (t) => {
	withSettings(t, { "shell.appearance.ascii": true });
	statuses.set("agent-3", "running");
	assertCardRows(card(delegating, DELEGATION), (w) => [
		spread("  -> Delegating - jhon-builder - claude-opus-5-5 - high", "~ running  ", w),
		body(STARTED, w),
	], "ascii delegating");
	setCardStyle("neon");
	const rows = card(delegating, DELEGATION)(100, { fg: (_r: string, s: string) => s, bold: (s: string) => s }).map(stripAnsi);
	assert.match(rows[0]!, /^ \+- -> Delegating - jhon-builder - claude-opus-5-5 - high -+ ~ running -\+ $/);
	assert.match(rows[1]!, /^ \| Started background sub-agent agent-3 \(jhon-builder\)\. +\| $/);
	assert.match(rows[2]!, /^ \+-+\+ $/);
});

// --- the thinking label ------------------------------------------------------------------

test("the thinking label: `▸ Thinking…` as design 01 draws it, `> Thinking...` in ASCII", () => {
	assert.equal(thinkingLabel(false), "▸ Thinking…");
	assert.equal(thinkingLabel(true), "> Thinking...");
});

test("the shell sets pi's hidden-thinking label in its TUI, follows the ASCII setting, and gives pi its own back", async (t) => {
	const handlers = new Map<string, Function[]>();
	const pi = { on(name: string, handler: Function) { handlers.set(name, [...(handlers.get(name) ?? []), handler]); } };
	const labels: Array<string | undefined> = [];
	const ctx = { mode: "tui", ui: { setHiddenThinkingLabel: (label?: string) => { labels.push(label); } } };
	installThinkingLabel(pi as any, {});
	const fire = async (name: string, c: unknown = ctx) => { for (const h of handlers.get(name) ?? []) await h({}, c); };
	await fire("session_start");
	assert.deepEqual(labels, ["▸ Thinking…"]);
	await fire("agent_start");
	assert.deepEqual(labels, ["▸ Thinking…"], "set again only when it changed");
	withSettings(t, { "shell.appearance.ascii": true });
	await fire("agent_start");
	assert.deepEqual(labels, ["▸ Thinking…", "> Thinking..."]);
	await fire("session_shutdown");
	assert.deepEqual(labels, ["▸ Thinking…", "> Thinking...", undefined], "pi's own label back");
	// Print mode, a host without the call, a child and a shell turned off leave pi's label alone.
	labels.length = 0;
	await fire("session_start", { mode: "print", ui: ctx.ui });
	await fire("session_start", { mode: "tui", ui: {} });
	assert.deepEqual(labels, []);
	for (const env of [{ JHON_PI_CHILD: "1" }, { JHON_SHELL: "0" }]) {
		const quiet = new Map<string, Function[]>();
		installThinkingLabel({ on: (n: string, h: Function) => quiet.set(n, [...(quiet.get(n) ?? []), h]) } as any, env);
		assert.equal(quiet.size, 0, JSON.stringify(env));
	}
});
