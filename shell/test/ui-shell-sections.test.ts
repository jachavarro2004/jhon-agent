/**
 * The sections style (T32, L96-L97): the minimalist chrome beside the Gentle
 * Shell. A thin identity line on top, the prompt under a rule-line header, one
 * full-width status line on its own band under everything (the right pane's
 * sections are test/ui-panel.test.ts's, T174-S5). It is the only style
 * (L76: float and neon are gone).
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { BAND_ROLE } from "../lib/ui/primitives/index.ts";
import { renderStatusBar } from "../lib/ui/bars.ts";
import { setChromeSurface } from "../lib/ui/shell-panel.ts";
import { framePromptLines, resolvePromptLayout, PROMPT_STATE } from "../lib/ui/shell-prompt.ts";

const plain = { fg: (_c: string, t: string) => t, bold: (t: string) => t, strikethrough: (t: string) => t };
const BG_OPEN = "\x1b[48;5;236m";
const BG_CLOSE = "\x1b[49m";

// The top row and the status bar are lib/ui/bars.ts's (T174-S3):
// test/ui-bars.test.ts holds them.

test("the prompt in sections: a rule-line header with the petal and state, then › and the input", () => {
	const layout = resolvePromptLayout(40, undefined, undefined, true);
	assert.equal(layout.panel, true);
	assert.equal(layout.prefixWidth, 2);
	assert.equal(layout.nativeWidth, 38);
	const lines = framePromptLines(["─".repeat(38), "hello", "─".repeat(38)], 40, { state: PROMPT_STATE.IDLE, tick: 0, borderColor: (t) => t, fg: (_c, t) => t }, layout);
	assert.deepEqual(lines.map((l) => visibleWidth(l)), [40, 40, 40]);
	assert.equal(lines[0], `─ 力 ready for you ${"─".repeat(21)}`, "力 is two cells");
	assert.equal(lines[1], `› hello${" ".repeat(33)}`);
	assert.equal(lines[2].trim(), "");
	const working = framePromptLines(["─".repeat(38), "a", "b", "─".repeat(38)], 40, { state: PROMPT_STATE.QUEUED, tick: 1, borderColor: (t) => t, fg: (_c, t) => t }, layout);
	assert.match(working[0], /^─ 力 queued ─+$/, "the brand petal, Jhon's state words");
	assert.match(working[2], /^ {2}b/, "continuation rows keep the input's column");
	assert.equal(resolvePromptLayout(3, undefined, undefined, true).panel, undefined, "under 4 columns the prompt keeps a frame, as G's");
	assert.equal(resolvePromptLayout(40).panel, undefined, "outside the fullscreen chrome the prompt is a card, as G's");
});

// T34 (L98): the band is darker than the page (rose ink's `band`, Mix(page,
// frame, 0.5)); pi reads it as customMessageBg. Notices are cards in the
// card style now (L153 answer 1, ui-primitives-frame.test.ts).
test("a tinted status bar and rail paint one band role: pi's customMessageBg", () => {
	assert.equal(BAND_ROLE, "customMessageBg");
	const roles: string[] = [];
	setChromeSurface("tinted");
	try {
		const bg = (c: string, t: string) => { roles.push(c); return `${BG_OPEN}${t}${BG_CLOSE}`; };
		renderStatusBar({ cwd: "/repo", branch: "main", modelId: "m", contextPercent: 10, costTotal: 0 }, { ...plain, bg }, 60);
	} finally { setChromeSurface("transparent"); }
	assert.deepEqual([...new Set(roles)], [BAND_ROLE]);
});
