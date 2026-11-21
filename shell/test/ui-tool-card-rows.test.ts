// T174-S6: the tool cards on design/shell-tui 01, as exact rows at the
// conversation's width beside the pane (133 columns at 180) and at 100: the
// tool card (a tinted block, the state at the right of the header), the bash
// card (`$ command`, its output, `✓ 2.3s`), the numbered edit diff, and a
// guard's refusal (`✗ refused  <call>  … safety guard`, the reason and the
// fix); their expanded (ctrl+o), ASCII and neon (outline) forms.
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { initTheme, keyHint } from "@earendil-works/pi-coding-agent";
import { visibleWidth } from "@earendil-works/pi-tui";
import { createQuietToolRenderer } from "../extensions/quiet.ts";
import { installShellSettings } from "../lib/settings.ts";
import { setCardStyle, stripAnsi } from "../lib/ui/primitives/index.ts";
import { memorySettings } from "./fake-jhon-settings.ts";
import { assertCardRows, padded, spread } from "./rows.ts";

initTheme("dark");
installShellSettings(memorySettings({}));
setCardStyle("float");

const EXPAND = stripAnsi(keyHint("app.tools.expand", "to expand"));
const COLLAPSE = stripAnsi(keyHint("app.tools.expand", "to collapse"));

type Tool = Parameters<typeof createQuietToolRenderer>[0];

interface Row {
	args: Record<string, unknown>;
	text?: string;
	details?: unknown;
	isError?: boolean;
	isPartial?: boolean;
	expanded?: boolean;
	durationMs?: number;
}

/** One tool row as pi draws it: the call component, then the result component, sharing one row state. */
function card(tool: Tool, row: Row) {
	return (width: number, theme: unknown): string[] => {
		const r = createQuietToolRenderer(tool) as any;
		const context = {
			args: row.args, toolCallId: `rows-${tool}`, state: {}, cwd: "/repo", executionStarted: true, argsComplete: true,
			isPartial: row.isPartial ?? false, expanded: row.expanded ?? false, showImages: false, isError: row.isError ?? false,
			durationMs: row.durationMs, invalidate() {},
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

const BASH: Row = { args: { command: "pytest tests/billing -k export" }, text: "14 passed in 2.31s", durationMs: 2300 };
const DIFF = [" 86 rows = invoices_in(range_)", "-87 return csv_response(rows)", "+87 if not rows:", "+88     raise EmptyRange(range_)  # 422, spec S4", "+89 return csv_response(rows)"].join("\n");
const EDIT: Row = { args: { path: "billing/api/export.py", edits: [] }, text: "Successfully replaced text in billing/api/export.py.", details: { diff: DIFF } };
const PUSH: Row = {
	args: { command: "git push origin feat/add-invoice-export" }, isError: true,
	text: "safety-guard: git push needs the owner's grant push for this project; nothing was pushed. The owner gives it in /jhon:grants.\n",
};
const SCOPE_REASON = "Edit /home/dev/src/lumen/api/invoices.go is outside the allowed edit surfaces (export/stream.go, export/stream_test.go)";
const SCOPE_FIX = "stop and return status: blocked naming the file and why the change needs it, so the orchestrator can widen the list (contracts/efficiency.md, \"Writer scope\")";
const SCOPE: Row = { args: { path: "/home/dev/src/lumen/api/invoices.go", edits: [] }, isError: true, text: `scope-guard: ${SCOPE_REASON}; ${SCOPE_FIX}` };

test("the bash card: `$ command` in the header, `✓ 2.3s` at its right, the output under it", () => {
	assertCardRows(card("bash", BASH), (w) => [
		spread("  $ pytest tests/billing -k export", "✓ 2.3s  ", w),
		body("14 passed in 2.31s", w),
	], "bash");
});

test("the edit card: the path, `+3 −1` at the right, then the numbered diff", () => {
	assertCardRows(card("edit", EDIT), (w) => [
		spread("  ✎ edit  billing/api/export.py", "+3 −1  ", w),
		body(" 86   rows = invoices_in(range_)", w),
		body(" 87 − return csv_response(rows)", w),
		body(" 87 + if not rows:", w),
		body(" 88 +     raise EmptyRange(range_)  # 422, spec S4", w),
		body(" 89 + return csv_response(rows)", w),
	], "edit");
});

test("a guard's refusal: `✗ refused  $ <command>` with the guard at the right, then the reason and the fix", () => {
	assertCardRows(card("bash", PUSH), (w) => [
		spread("  ✗ refused  $ git push origin feat/add-invoice-export", "safety guard  ", w),
		body("git push needs the owner's grant push for this project; nothing was pushed.", w),
		body("The owner gives it in /jhon:grants.", w),
	], "refusal");
});

test("a long refusal: collapsed, each row cut and the expand key at the right; expanded, the whole reason", () => {
	const fix = `S${SCOPE_FIX.slice(1)}`;
	assertCardRows(card("edit", SCOPE), {
		133: [
			spread("  ✗ refused  edit /home/dev/src/lumen/api/invoices.go", `${EXPAND}  scope guard  `, 133),
			body(SCOPE_REASON, 133),
			body(`${fix.slice(0, 125)}…`, 133),
		],
		100: [
			spread("  ✗ refused  edit /home/dev/src/lumen/api/invoices.go", `${EXPAND}  scope guard  `, 100),
			body(`${SCOPE_REASON.slice(0, 92)}…`, 100),
			body(`${fix.slice(0, 92)}…`, 100),
		],
	}, "refusal collapsed");
	assertCardRows(card("edit", { ...SCOPE, expanded: true }), {
		133: [
			spread("  ✗ refused  edit /home/dev/src/lumen/api/invoices.go", `${COLLAPSE}  scope guard  `, 133),
			body(SCOPE_REASON, 133),
			body("Stop and return status: blocked naming the file and why the change needs it, so the orchestrator can widen the list", 133),
			body("(contracts/efficiency.md, \"Writer scope\")", 133),
		],
		100: [
			spread("  ✗ refused  edit /home/dev/src/lumen/api/invoices.go", `${COLLAPSE}  scope guard  `, 100),
			body("Edit /home/dev/src/lumen/api/invoices.go is outside the allowed edit surfaces", 100),
			body("(export/stream.go, export/stream_test.go)", 100),
			body("Stop and return status: blocked naming the file and why the change needs it, so the", 100),
			body("orchestrator can widen the list (contracts/efficiency.md, \"Writer scope\")", 100),
		],
	}, "refusal expanded");
});

test("the read card: the path, `↳ 61 lines` at the right, the first three lines, and the expand key", () => {
	const text = Array.from({ length: 61 }, (_, i) => `line ${i + 1}`).join("\n");
	assertCardRows(card("read", { args: { path: "odd/changes/add-invoice-export/specs/export.md" }, text }), (w) => [
		spread("  ▤ read  odd/changes/add-invoice-export/specs/export.md", `${EXPAND}  ↳ 61 lines  `, w),
		body("line 1", w), body("line 2", w), body("line 3", w),
	], "read");
});

test("a bash card with a longer output: the last five lines under `... (N earlier lines, <key>)`; expanded, all of it", () => {
	const row: Row = { args: { command: "seq 1 8" }, text: "1\n2\n3\n4\n5\n6\n7\n8", durationMs: 400 };
	assertCardRows(card("bash", row), (w) => [
		spread("  $ seq 1 8", "✓ 0.4s  ", w),
		body(`... (3 earlier lines, ${EXPAND})`, w),
		...["4", "5", "6", "7", "8"].map((n) => body(n, w)),
	], "bash collapsed");
	assertCardRows(card("bash", { ...row, expanded: true }), (w) => [
		spread("  $ seq 1 8", `${COLLAPSE}  ✓ 0.4s  `, w),
		...["1", "2", "3", "4", "5", "6", "7", "8"].map((n) => body(n, w)),
	], "bash expanded");
});

test("a multi-line command keeps every line: the first in the header, the rest under it", () => {
	const command = "cat <<'EOF' > notes.txt\nfirst\nEOF\nwc -l notes.txt";
	assertCardRows(card("bash", { args: { command }, text: "1 notes.txt", durationMs: 100 }), (w) => [
		spread("  $ cat <<'EOF' > notes.txt", "✓ 0.1s  ", w),
		body("first", w), body("EOF", w), body("wc -l notes.txt", w),
		body("1 notes.txt", w),
	], "heredoc");
});

test("a failed bash: `✗ 1.2s` at the right, Jhon's headline and the first failure, then the output", () => {
	const text = "FAILED test_a.py::test_x - assert 1 == 2\n1 failed, 3 passed\n\nCommand exited with code 1";
	assertCardRows(card("bash", { args: { command: "pytest" }, text, isError: true, durationMs: 1200 }), (w) => [
		spread("  $ pytest", "✗ 1.2s  ", w),
		body("exited 1 · 1 failed · 3 passed", w),
		body("✗ test_a.py::test_x — assert 1 == 2", w),
		body("FAILED test_a.py::test_x - assert 1 == 2", w),
		body("1 failed, 3 passed", w),
	], "failed bash");
});

test("a call still running: `◐ running` at the right of its header", () => {
	assertCardRows(card("bash", { args: { command: "pytest tests/billing -k range" }, isPartial: true }), (w) => [
		spread("  $ pytest tests/billing -k range", "◐ running  ", w),
	], "running");
});

test("ASCII: the same rows with the icons as letters and every glyph in its ASCII form", (t) => {
	withSettings(t, { "shell.appearance.ascii": true });
	assertCardRows(card("bash", BASH), (w) => [spread("  $ pytest tests/billing -k export", "+ 2.3s  ", w), body("14 passed in 2.31s", w)], "ascii bash");
	assertCardRows(card("edit", EDIT), (w) => [
		spread("  e edit  billing/api/export.py", "+3 -1  ", w),
		body(" 86   rows = invoices_in(range_)", w),
		body(" 87 - return csv_response(rows)", w),
		body(" 87 + if not rows:", w),
		body(" 88 +     raise EmptyRange(range_)  # 422, spec S4", w),
		body(" 89 + return csv_response(rows)", w),
	], "ascii edit");
	assertCardRows(card("bash", PUSH), (w) => [
		spread("  x refused  $ git push origin feat/add-invoice-export", "safety guard  ", w),
		body("git push needs the owner's grant push for this project; nothing was pushed.", w),
		body("The owner gives it in /jhon:grants.", w),
	], "ascii refusal");
	assertCardRows(card("read", { args: { path: "a.md" }, text: "one" }), (w) => [spread("  r read  a.md", "> 1 line  ", w), body("one", w)], "ascii read");
	const failed = "FAILED test_a.py::test_x - assert 1 == 2\n1 failed, 3 passed\n\nCommand exited with code 1";
	assertCardRows(card("bash", { args: { command: "pytest" }, text: failed, isError: true, durationMs: 1200 }), (w) => [
		spread("  $ pytest", "x 1.2s  ", w),
		body("exited 1 - 1 failed - 3 passed", w),
		body("x test_a.py::test_x - assert 1 == 2", w),
		body("FAILED test_a.py::test_x - assert 1 == 2", w),
		body("1 failed, 3 passed", w),
	], "ascii failed bash");
	assertCardRows(card("bash", { args: { command: "pytest" }, isPartial: true }), (w) => [spread("  $ pytest", "~ running  ", w)], "ascii running");
});

test("neon: design 02's outline, the title and the state in the top edge", (t) => {
	withSettings(t, {}, "neon");
	const top = (left: string, right: string, w: number) => {
		const head = ` ╭─ ${left} `;
		const tail = ` ${right} ─╮ `;
		return `${head}${"─".repeat(w - visibleWidth(head) - visibleWidth(tail))}${tail}`;
	};
	const side = (text: string, w: number) => ` │ ${padded(text, w - 6)} │ `;
	const bottom = (w: number) => ` ╰${"─".repeat(w - 4)}╯ `;
	assertCardRows(card("bash", BASH), (w) => [top("$ pytest tests/billing -k export", "✓ 2.3s", w), side("14 passed in 2.31s", w), bottom(w)], "neon bash");
	assertCardRows(card("bash", PUSH), (w) => [
		top("✗ refused  $ git push origin feat/add-invoice-export", "safety guard", w),
		side("git push needs the owner's grant push for this project; nothing was pushed.", w),
		side("The owner gives it in /jhon:grants.", w),
		bottom(w),
	], "neon refusal");
	assertCardRows(card("bash", { args: { command: "pytest" }, isPartial: true }), (w) => [top("$ pytest", "◐ running", w), bottom(w)], "neon running closes itself");
});

test("neon in ASCII: the outline in + - |", (t) => {
	withSettings(t, { "shell.appearance.ascii": true }, "neon");
	const rows = card("bash", BASH)(100, { fg: (_r: string, s: string) => s, bold: (s: string) => s }).map(stripAnsi);
	assert.match(rows[0]!, /^ \+- \$ pytest tests\/billing -k export -+ \+ 2\.3s -\+ $/);
	assert.match(rows[1]!, /^ \| 14 passed in 2\.31s +\| $/);
	assert.match(rows[2]!, /^ \+-+\+ $/);
});

test("minimal density: bash quiet, `↳ N lines` at the right and the last lines, no duration", (t) => {
	withSettings(t, { "shell.appearance.density": "minimal" });
	const text = Array.from({ length: 8 }, (_, i) => `out ${i + 1}`).join("\n");
	assertCardRows(card("bash", { args: { command: "ls" }, text, durationMs: 400 }), (w) => [
		spread("  $ ls", `${EXPAND}  ↳ 8 lines  `, w),
		body("out 6", w), body("out 7", w), body("out 8", w),
	], "minimal bash");
});

test("the tints: a card on Box; a refusal's header on errorContainer, its rows on Box", () => {
	const tinted = { fg: (_r: string, s: string) => s, bold: (s: string) => s, bg: (role: string, s: string) => `\x1b[48;5;${role === "toolErrorBg" ? 52 : 22}m${s}\x1b[49m` };
	const refusal = card("bash", PUSH)(100, tinted);
	assert.ok(refusal[0]!.startsWith(" \x1b[48;5;52m"), "the refusal's header band");
	assert.ok(refusal.slice(1).every((row) => row.startsWith(" \x1b[48;5;22m")), "its rows on Box");
	const bash = card("bash", BASH)(100, tinted);
	assert.ok(bash.every((row) => row.startsWith(" \x1b[48;5;22m") && row.endsWith("\x1b[49m ")), "a card on Box, a cell in from each side");
	for (const width of [0, 1, 2, 3, 4, 5, 8, 16, 40]) {
		for (const row of [...card("bash", PUSH)(width, tinted), ...card("edit", EDIT)(width, tinted)]) assert.equal(visibleWidth(row), width, `every row exactly ${width} cells`);
	}
});
