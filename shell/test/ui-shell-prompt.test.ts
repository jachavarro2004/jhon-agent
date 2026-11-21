/**
 * The pieces of the prompt and the working line (lib/ui/shell-prompt.ts,
 * T174-S8): the light across the working word, the petal's tone, the rules,
 * the placeholder and what the agent does. Whole rows are
 * test/ui-prompt-rows.test.ts's.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-shell-prompt.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { stripAnsi } from "../lib/ui/primitives/index.ts";
import {
	createWorkingActivity,
	interruptHint,
	petalTone,
	promptInputRow,
	PROMPT_HINT,
	PROMPT_STATE,
	renderPromptRule,
	renderWorkingLine,
	scanWorkingText,
	SHELL_PULSE_MS,
	SHELL_SCANNER_STEPS,
	withPlaceholder,
} from "../lib/ui/shell-prompt.ts";

const tag = (role: string, text: string) => `<${role}>${text}</${role}>`;
const plain = { fg: (_role: string, text: string) => text };

test("the light crosses the working word one cell per frame at 80 ms, entering from the left, and leaves the text unchanged", () => {
	assert.equal(SHELL_PULSE_MS, 80);
	assert.equal(SHELL_SCANNER_STEPS, 15);
	for (const word of ["Working…", "Working..."]) {
		for (let tick = 0; tick < SHELL_SCANNER_STEPS; tick++) assert.equal(scanWorkingText(word, tick, tag).replace(/<[^>]+>/g, ""), word);
		assert.match(scanWorkingText(word, 0, tag), /^(<muted>.<\/muted>)+$/, "frame 0: the light is still before the word");
		assert.ok(scanWorkingText(word, 3, tag).startsWith("<borderAccent>W</borderAccent><accent>o</accent><thinkingHigh>r</thinkingHigh><muted>k</muted>"));
		assert.ok(scanWorkingText(word, 4, tag).startsWith("<accent>W</accent><borderAccent>o</borderAccent><accent>r</accent><thinkingHigh>k</thinkingHigh>"));
		assert.equal(scanWorkingText(word, SHELL_SCANNER_STEPS, tag), scanWorkingText(word, 0, tag), "the pass repeats");
	}
});

test("the petal walks the rose ramp while working, turns to warning while a message waits, and rests bright", () => {
	assert.deepEqual(Array.from({ length: 9 }, (_, tick) => petalTone(PROMPT_STATE.WORKING, tick)),
		["mdQuoteBorder", "thinkingHigh", "accent", "borderAccent", "accent", "thinkingHigh", "mdQuoteBorder", "mdQuoteBorder", "mdQuoteBorder"]);
	assert.deepEqual([petalTone("queued", 2), petalTone("idle", 2)], ["warning", "borderAccent"], "a waiting message warns; at rest the petal is bright");
});

test("the working line paints the design's roles: 力 bold, Working… italic in the accent, the time muted, the keys muted and their word dim", () => {
	const theme = { fg: tag, bold: (t: string) => `*${t}*`, italic: (t: string) => `/${t}/` };
	const line = renderWorkingLine(80, { state: PROMPT_STATE.WORKING, tick: 0, elapsedMs: 38_000, doubleEsc: true }, theme);
	assert.ok(line.startsWith(" <accent>*力*</accent> /<accent>Working…</accent>/<dim>  thinking · </dim><muted>38s</muted>"), line);
	assert.ok(line.endsWith("<muted>esc esc</muted><dim> interrupts</dim> "), line);
	const queued = renderWorkingLine(80, { state: PROMPT_STATE.QUEUED, tick: 0, elapsedMs: 0, doubleEsc: false }, theme);
	assert.ok(queued.startsWith(" <warning>*力*</warning>"), "the petal warns while a message waits");
	assert.match(queued, /<dim> · <\/dim><warning>queued<\/warning>/);
	const idle = renderWorkingLine(80, { state: PROMPT_STATE.IDLE, tick: 0 }, theme);
	assert.ok(idle.startsWith(" <borderAccent>*力*</borderAccent><muted> ready for you</muted>"), idle);
	assert.equal(visibleWidth(idle.replace(/<[^>]+>|[*/]/g, "")), 80);
	const animated = renderWorkingLine(80, { state: PROMPT_STATE.WORKING, tick: 3, animate: true, elapsedMs: 0 }, theme);
	assert.ok(animated.startsWith(" <borderAccent>*力*</borderAccent> /<borderAccent>W</borderAccent><accent>o</accent>"), "animated: the petal's ramp and the light");
});

test("the interrupt hint follows the double-esc setting", () => {
	assert.deepEqual(interruptHint(true), ["esc esc", "interrupts"]);
	assert.deepEqual(interruptHint(false), ["esc", "interrupts"]);
});

test("an open esc confirmation takes the right end, idle too", () => {
	assert.match(renderWorkingLine(60, { state: PROMPT_STATE.IDLE, tick: 0, escHint: "esc again clears it" }, plain), /^ 力 ready for you +esc again clears it $/);
	assert.match(renderWorkingLine(60, { state: PROMPT_STATE.WORKING, tick: 0, escHint: "esc again stops it" }, plain), /^ 力 Working…  thinking · 0s +esc again stops it $/);
});

test("a rule is exactly the width, a scroll note at its right end when it fits", () => {
	assert.equal(renderPromptRule(20, (t) => t), "─".repeat(20));
	assert.equal(renderPromptRule(20, (t) => t, "↓ 3 more"), `${"─".repeat(9)} ↓ 3 more ─`);
	assert.equal(renderPromptRule(10, (t) => t, "↓ 3 more"), "─".repeat(10), "no room: the plain rule");
	assert.equal(renderPromptRule(20, (t) => t, "↓ 3 more", true), `${"-".repeat(9)} v 3 more -`);
	assert.equal(renderPromptRule(6, (t) => `[${t}]`), "[──────]", "painted in pi's border colour");
});

test("an input row is the mark or its indent, then pi's row, cut and padded to the width", () => {
	assert.equal(promptInputRow("hello", 12, "❯"), " ❯ hello    ");
	assert.equal(promptInputRow("hello", 12, undefined), "   hello    ");
	assert.equal(stripAnsi(promptInputRow("hello world", 8, "❯")), " ❯ hello", "cut (with a reset, so no colour leaks)");
});

test("the placeholder sits one cell after the cursor of an empty row, dim, and only when it fits", () => {
	const cursor = "\x1b_pi:fc\x07 \x1b_pi:/fc\x07";
	const row = `${cursor}${" ".repeat(39)}`;
	const shown = withPlaceholder(row, PROMPT_HINT, (t) => `<dim>${t}</dim>`);
	assert.equal(shown, `${cursor} <dim>${PROMPT_HINT}</dim>${" ".repeat(39 - PROMPT_HINT.length - 1)}`);
	assert.equal(visibleWidth(withPlaceholder(row, PROMPT_HINT, (t) => t)), 40);
	const legacy = `\x1b[7m \x1b[0m${" ".repeat(39)}`;
	assert.ok(withPlaceholder(legacy, PROMPT_HINT, (t) => t).startsWith(`\x1b[7m \x1b[0m ${PROMPT_HINT}`), "pi-tui's older SGR cursor too");
	const narrow = `${cursor}${" ".repeat(6)}`;
	assert.equal(withPlaceholder(narrow, PROMPT_HINT, (t) => t), narrow);
});

test("what the agent does: thinking, then writing, the tool that runs last, and thinking again when the tools end", () => {
	const activity = createWorkingActivity();
	activity.start();
	assert.equal(activity.current(), "thinking");
	activity.stream("text_delta");
	assert.equal(activity.current(), "writing");
	activity.stream("thinking_start");
	assert.equal(activity.current(), "thinking");
	activity.stream("toolcall_delta");
	assert.equal(activity.current(), "thinking", "a tool call being written changes nothing yet");
	activity.toolStart("a", "bash");
	activity.toolStart("b", "read");
	assert.equal(activity.current(), "read");
	activity.toolEnd("b");
	assert.equal(activity.current(), "bash");
	activity.toolEnd("a");
	assert.equal(activity.current(), "thinking");
	activity.toolStart("c", "edit");
	activity.start();
	assert.equal(activity.current(), "thinking", "a new turn forgets the last one's tools");
});
