import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import * as prompt from "../lib/ui/shell-prompt.ts";
import {
	framePromptLines as frame,
	resolvePromptLayout,
	scanWorkingText,
	PROMPT_STATE,
	petalGlyph,
	petalTone,
	SHELL_PULSE_MS,
	SHELL_SCANNER_STEPS,
	withPromptHint,
	type PromptFrameOptions,
} from "../lib/ui/shell-prompt.ts";

test("scanner reflects a symmetric light wave across working text at Pi's original cadence", () => {
	const fg = (role: string, char: string) => `<${role}>${char}</${role}>`;
	assert.equal(SHELL_PULSE_MS, 80);
	assert.equal(SHELL_SCANNER_STEPS, 15);
	for (const word of ["working…", "Thinking…"]) {
		for (let tick = 0; tick < SHELL_SCANNER_STEPS; tick++) {
			const frame = scanWorkingText(word, tick, fg);
			assert.equal(frame.replace(/<[^>]+>/g, ""), word);
		}
		assert.match(scanWorkingText(word, 0, fg), /^(<muted>.<\/muted>)+$/);
		assert.match(scanWorkingText(word, 3, fg), new RegExp(`^<borderAccent>${word[0]}</borderAccent><accent>${word[1]}</accent><thinkingHigh>${word[2]}</thinkingHigh>`));
		assert.match(scanWorkingText(word, 4, fg), new RegExp(`^<accent>${word[0]}</accent><borderAccent>${word[1]}</borderAccent><accent>${word[2]}</accent>`));
		assert.match(scanWorkingText(word, 5, fg), new RegExp(`^<thinkingHigh>${word[0]}</thinkingHigh><accent>${word[1]}</accent><borderAccent>${word[2]}</borderAccent>`));
		assert.match(scanWorkingText(word, 14, fg), /^(<muted>.<\/muted>)+$/);
		assert.equal(scanWorkingText(word, SHELL_SCANNER_STEPS, fg), scanWorkingText(word, 0, fg));
	}
	for (const width of [1, 8, 20, 40]) {
		for (let tick = 0; tick < SHELL_SCANNER_STEPS; tick++) {
			const lines = framePromptLines(editorLines(Math.max(4, width)), width, options({ state: PROMPT_STATE.WORKING, tick, fg: (_role, text) => text }));
			assert.ok(lines.every((line) => visibleWidth(line) <= width));
		}
	}
});

test("compact banner pulse rises from ink through fresh color to a bright tip then fades", () => {
	const tones = Array.from({ length: 8 }, (_, tick) => petalTone(PROMPT_STATE.WORKING, tick));
	assert.deepEqual(tones, ["mdQuoteBorder", "thinkingHigh", "accent", "borderAccent", "accent", "thinkingHigh", "mdQuoteBorder", "mdQuoteBorder"]);
	assert.equal(petalTone(PROMPT_STATE.WORKING, 8), tones[0]);
});

// The shell prompt turns pi's editor output (a top rule, padded content
// lines, a bottom rule) into the Gentle Shell's panel editor: a rule-line
// header with a petal that shows the agent state and the hint at its right
// end, `›` rows and a blank last row (T15.12d, L158); under 10 columns it
// keeps a rounded frame. Framing is pure: the editor's lines in, new ones out.

const CURSOR = "\x1b[7m \x1b[0m";

/** The fullscreen chrome's editor, G's panel layout. */
function framePromptLines(lines: string[], width: number, opts: PromptFrameOptions): string[] {
	return frame(lines, width, opts, resolvePromptLayout(width, undefined, undefined, true));
}

function options(overrides: Partial<PromptFrameOptions> = {}): PromptFrameOptions {
	return {
		state: PROMPT_STATE.IDLE,
		tick: 0,
		borderColor: (text) => text,
		fg: (color, text) => `<${color}>${text}</${color}>`,
		...overrides,
	};
}

test("framePromptLines sets the petal in bold when the theme offers it", () => {
	const lines = framePromptLines(editorLines(40), 40, options({ bold: (text) => `*${text}*` }));
	assert.match(lines[0], /<borderAccent>\*力\*<\/borderAccent>/);
});

function editorLines(width: number, content: string[] = [` ${CURSOR}`]): string[] {
	const inner = width - 2;
	return ["─".repeat(inner), ...content.map((line) => line + " ".repeat(inner - visibleWidth(line))), "─".repeat(inner)];
}

test("framePromptLines draws the rule-line header, the › rows and G's blank last row, every line at width; a rounded frame under 4 columns", () => {
	const width = 40;
	const lines = framePromptLines(editorLines(width), width, options({ fg: (_c, t) => t }));
	assert.equal(lines.length, 3);
	for (const line of lines) assert.equal(visibleWidth(line), width, `line "${stripAnsi(line)}" is not ${width} wide`);
	assert.match(stripAnsi(lines[0]), /^─ 力 ready for you ─+$/);
	assert.match(stripAnsi(lines[1]), /^› /);
	assert.equal(lines[2], " ".repeat(width), "the last row is blank, as the Gentle Shell's");
	const narrow = framePromptLines(["─", " ", "─"], 3, options({ fg: (_c, t) => t })).map(stripAnsi);
	assert.deepEqual([narrow[0], narrow[2]], ["╭─╮", "╰─╯"]);
});

test("framePromptLines paints the rule with the editor border color, the petal with the state tone and the › mark in the accent, bold, as the Gentle Shell's (T15.12c)", () => {
	const lines = framePromptLines(editorLines(40), 40, options({ borderColor: (text) => `[b]${text}[/b]`, bold: (text) => `*${text}*` }));
	assert.match(lines[0], /^\[b\]─ \[\/b\]<borderAccent>\*力\*<\/borderAccent> <muted>ready for you<\/muted> \[b\]─+\[\/b\]\[b\]─\[\/b\]$/);
	assert.match(lines[1], /^<accent>\*›\*<\/accent> /);
});

// T169 (L1438): the panel sheet is gone, and the rule's [ Panel ] button with it.
test("framePromptLines draws the panel editor's rule with no [ Panel ], idle, working and with a hint, every line at width (T169)", () => {
	const plain = framePromptLines(editorLines(60), 60, options({ fg: (_c, t) => t })).map(stripAnsi);
	assert.match(plain[0]!, /^─ 力 ready for you ─+$/);
	for (const width of [10, 14, 20, 40, 60, 100]) {
		for (const line of framePromptLines(editorLines(Math.max(10, width)), width, options({ fg: (_c, t) => t }))) assert.equal(visibleWidth(line), width, `${width}: ${stripAnsi(line)}`);
	}
	assert.match(framePromptLines(editorLines(60), 60, options({ fg: (_c, t) => t, state: PROMPT_STATE.WORKING })).map(stripAnsi)[0]!, /^─ \S working… ─+$/);
	const hinted = framePromptLines(editorLines(60), 60, options({ fg: (_c, t) => t, escHint: "esc again clears it" })).map(stripAnsi);
	assert.match(hinted[0]!, /^─ 力 ready for you ─+ esc again clears it ─$/);
	assert.equal(hinted[2], " ".repeat(60));
	assert.equal("PANEL_BUTTON" in prompt, false, "no [ Panel ] button to draw");
});

test("petalTone rests bright, walks the rose ramp while working, and turns to warning when queued", () => {
	assert.equal(petalTone(PROMPT_STATE.IDLE, 2), "borderAccent");
	assert.deepEqual([0, 1, 2, 3, 4].map((tick) => petalTone(PROMPT_STATE.WORKING, tick)), ["mdQuoteBorder", "thinkingHigh", "accent", "borderAccent", "accent"]);
	assert.equal(petalTone(PROMPT_STATE.QUEUED, 1), "warning");
});

// G's petal is its brand ✿, its working frames flowers of the same family;
// Jhon's brand 力 takes every one of them (L146, T15.12g): working turns its tone.
test("petalGlyph is the brand 力 in every state, as G's ✿ and its flowers", () => {
	assert.equal(petalGlyph(PROMPT_STATE.IDLE, 3), "力");
	assert.deepEqual([0, 1, 2, 3, 4].map((tick) => petalGlyph(PROMPT_STATE.WORKING, tick)), ["力", "力", "力", "力", "力"]);
	assert.equal(petalGlyph(PROMPT_STATE.QUEUED, 1), "力");
});

test("framePromptLines scans the working word in the header and says queued", () => {
	const plain = (_color: string, text: string) => text;
	const working = framePromptLines(editorLines(40), 40, options({ state: PROMPT_STATE.WORKING, tick: 3 }));
	assert.match(working[0], /<borderAccent>力<\/borderAccent>/);
	assert.match(working[0], /<borderAccent>w<\/borderAccent><accent>o<\/accent><thinkingHigh>r<\/thinkingHigh>/);
	const workingPlain = framePromptLines(editorLines(40), 40, options({ state: PROMPT_STATE.WORKING, tick: 3, fg: plain }));
	assert.match(stripAnsi(workingPlain[0]), /^─ 力 working… ─+$/);
	assert.equal(visibleWidth(workingPlain[0]), 40);

	const queued = framePromptLines(editorLines(40), 40, options({ state: PROMPT_STATE.QUEUED }));
	assert.match(queued[0], /<warning>力<\/warning>/);
	assert.match(queued[0], /<muted>queued<\/muted>/);
	const queuedPlain = framePromptLines(editorLines(40), 40, options({ state: PROMPT_STATE.QUEUED, fg: plain }));
	assert.match(stripAnsi(queuedPlain[0]), /^─ 力 queued ─+$/);
});

test("framePromptLines keeps the editor scroll indicators: the top one as the header's label, the bottom one at its right end", () => {
	const width = 40;
	const inner = width - 2;
	const top = `─── ↑ 2 more ${"─".repeat(inner - 13)}`;
	const bottom = `─── ↓ 3 more ${"─".repeat(inner - 13)}`;
	const lines = framePromptLines([top, ` x${" ".repeat(inner - 2)}`, bottom], width, options({ fg: (_c, t) => t }));
	assert.match(stripAnsi(lines[0]), /^─ 力 ↑ 2 more ─+ ↓ 3 more ─$/);
	assert.equal(lines[2], " ".repeat(width));
	for (const line of lines) assert.equal(visibleWidth(line), width);
});

test("framePromptLines shows an explicit escHint (esc again) at the rule's right end, overriding the editor's own scroll indicator", () => {
	const width = 40;
	const inner = width - 2;
	const bottom = `─── ↓ 3 more ${"─".repeat(inner - 13)}`;
	const lines = framePromptLines(
		["─".repeat(inner), ` x${" ".repeat(inner - 2)}`, bottom],
		width,
		options({ fg: (_c, t) => t, escHint: "esc again stops it" }),
	);
	assert.equal(stripAnsi(lines[0]), "─".repeat(width), "as G's panelPromptRule: when label and hint do not fit, the rule blanks (no clipping)");
	assert.match(stripAnsi(framePromptLines(["─".repeat(78), ` x${" ".repeat(76)}`, "─".repeat(78)], 80, options({ fg: (_c, t) => t, escHint: "esc again stops it" }))[0]!), /^─ 力 ready for you ─+ esc again stops it ─$/);
	assert.equal(lines[2], " ".repeat(width), "no hint row of its own");
	for (const line of lines) assert.equal(visibleWidth(line), width);
});

test("outside the fullscreen chrome the prompt is G's card: neon a rounded frame with the hint in its bottom rule, float tinted ▎ rows with the state on the first", () => {
	const plain = (_c: string, t: string) => t;
	const neon = frame(editorLines(40), 40, options({ fg: plain, escHint: "esc again" }), resolvePromptLayout(40, "neon")).map(stripAnsi);
	assert.match(neon[0]!, /^╭─ 力 ─+╮$/);
	assert.match(neon[1]!, /^│ /);
	assert.match(neon[2]!, /^╰─ esc again ─+╯$/);
	const bg = (_role: string, text: string) => `\x1b[48;5;236m${text}\x1b[49m`;
	const layout = resolvePromptLayout(40, "float", bg);
	assert.equal(layout.background, "\x1b[48;5;236m");
	assert.equal(layout.prefixWidth, 3);
	const float = frame(editorLines(40), 40, options({ fg: plain, bg, escHint: "esc again" }), layout);
	assert.equal(float.length, 3);
	for (const line of float) assert.equal(visibleWidth(line), 40);
	assert.match(stripAnsi(float[0]!), /^ ▎ 力 ready for you · esc again +$/);
	assert.match(stripAnsi(float[1]!), /^ ▎ /);
	assert.match(float[1]!, /\x1b\[48;5;236m/, "the float rows are tinted");
	assert.equal(resolvePromptLayout(40, "float").background, "", "no theme background: the neon frame");
});

test("withPromptHint places a dim hint after the cursor on an empty editor line", () => {
	const inner = 38;
	const line = ` ${CURSOR}${" ".repeat(inner - 2)}`;
	const hinted = withPromptHint(line, "write, or / for a command", (color, text) => `<${color}>${text}</${color}>`);
	assert.match(hinted, /\x1b\[7m \x1b\[0m <dim>write, or \/ for a command<\/dim> +$/);
	const hintedPlain = withPromptHint(line, "write, or / for a command", (_c, t) => t);
	assert.equal(visibleWidth(hintedPlain), inner);
});

test("withPromptHint finds pi-tui 1.4.0's fake cursor, marked with APC sequences", () => {
	const inner = 40;
	const cursor = "\x1b_pi:c\x07\x1b_pi:fc\x07 \x1b_pi:/fc\x07";
	const line = ` ${cursor}${" ".repeat(inner - 2)}`;
	const hinted = withPromptHint(line, "write, or / for a command", (_c, t) => t);
	assert.ok(hinted.endsWith(`\x1b_pi:/fc\x07 write, or / for a command${" ".repeat(inner - 2 - 26)}`), JSON.stringify(hinted));
	assert.equal(visibleWidth(hinted), inner);
});

test("withPromptHint leaves the line alone when the hint does not fit", () => {
	const line = ` ${CURSOR}${" ".repeat(6)}`;
	const hinted = withPromptHint(line, "write, or / for a command", (_c, t) => t);
	assert.equal(hinted, line);
});

test("the working prompt stays transparent at narrow and normal widths", () => {
	for (const width of [0, 1, 2, 3, 8, 40]) {
		const lines = framePromptLines(["──", ` ${CURSOR}界`, "──"], width, options({
			state: PROMPT_STATE.WORKING, fg: (_c, t) => t,
		}));
		for (const [row, line] of lines.entries()) {
			assert.ok(visibleWidth(line) <= width);
			let bg = false, cell = 0;
			for (const token of line.match(/\x1b\[[\d;]*m|[^\x1b]/gu) ?? []) {
				if (token.startsWith("\x1b")) {
					for (const code of token.slice(2, -1).split(";").map(Number)) {
						if (code === 0 || code === 49) bg = false;
						if (code === 44) bg = true;
					}
				} else {
					assert.equal(bg, false, `row ${row}, cell ${cell} must remain transparent`);
					cell += visibleWidth(token);
				}
			}
			assert.equal(bg, false);
		}
	}
});

test("the transparent prompt preserves pi's cursor reset", () => {
	const lines = framePromptLines(editorLines(40), 40, options({ fg: (_c, t) => t }));
	assert.ok(lines[1].includes(CURSOR));
	assert.doesNotMatch(lines.join("\n"), /\x1b\[44m/);
});
