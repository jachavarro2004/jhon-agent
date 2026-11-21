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
import { framePrompt, PROMPT_INDENT } from "../lib/ui/shell-prompt.ts";

const plain = { fg: (_c: string, t: string) => t, bold: (t: string) => t, strikethrough: (t: string) => t };
/** The 256-colour background the plain theme paints with, and its reset. */
const [BG_OPEN, BG_CLOSE] = ["\x1b[48;5;236m", "\x1b[49m"];

// The top row and the status bar are lib/ui/bars.ts's (T174-S3):
// test/ui-bars.test.ts holds them.

test("the prompt in sections: a rule, ❯ and the input, a rule; continuation rows keep the input's column (T174-S8)", () => {
	assert.equal(PROMPT_INDENT, 3, "a space, the mark, a space");
	const plainTheme = { fg: (_c: string, t: string) => t, bold: (t: string) => t };
	const lines = framePrompt(40, { rows: ["─".repeat(37), "hello", "─".repeat(37)], inputRows: 1, border: (t) => t, theme: plainTheme });
	assert.deepEqual(lines.map((l) => visibleWidth(l)), [40, 40, 40]);
	assert.deepEqual(lines, ["─".repeat(40), ` ❯ hello${" ".repeat(32)}`, "─".repeat(40)]);
	const two = framePrompt(40, { rows: ["─".repeat(37), "a", "b", "─".repeat(37)], inputRows: 2, border: (t) => t, theme: plainTheme });
	assert.match(two[2]!, /^ {3}b/, "continuation rows keep the input's column");
	assert.deepEqual(framePrompt(4, { rows: ["────", "ab", "────"], inputRows: 1, border: (t) => t, theme: plainTheme }), ["────", "ab", "────"], "too narrow for the mark: pi's rows alone");
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
