import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import {
	CARD_STYLE, CARD_TONE, cardAwaitingResult, cardRunningLine, cardStyle, cardTopRows, markCardResult, renderCard, setCardStyle,
	type Card,
} from "../lib/ui/shell-card.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { useMemorySettings } from "./fake-jhon-settings.ts";

// Cards are how Jhon's notices look: the same rounded frame as the prompt,
// with the title in the notice tone. They collapse to one body line when pi
// asks for it.

const plainTheme = {
	fg(_color: string, text: string) {
		return text;
	},
};

const taggedTheme = {
	fg(color: string, text: string) {
		return `<${color}>${text}</${color}>`;
	},
};

const ansiTheme = {
	fg(_color: string, text: string) {
		return `\x1b[35m${text}\x1b[0m`;
	},
};

function card(overrides: Partial<Card> = {}): Card {
	return {
		title: "Jhon",
		subtitle: "review preflight",
		body: ["Receipt-driven development is enabled, and this worktree holds an unreviewed candidate.", "", "Call the jhon_review tool with inspect and follow the transition it returns."],
		tone: CARD_TONE.INFO,
		...overrides,
	};
}

test("renderCard draws the rounded frame with the title in the top rule and wraps the body inside", () => {
	const lines = renderCard(card(), plainTheme, 48, { expanded: true }).map(stripAnsi);
	assert.match(lines[0], /^╭─ 力 Jhon · review preflight ─+╮$/);
	assert.match(lines[1], /^│ Receipt-driven development is enabled, and +│$/);
	for (const line of lines) assert.equal(visibleWidth(line), 48, `"${line}" is not 48 wide`);
	assert.ok(lines.some((line) => /^│ +│$/.test(line)), "blank body lines keep the frame");
	assert.ok(lines.some((line) => line.includes("jhon_review")), "every paragraph is rendered when expanded");
	assert.match(lines[lines.length - 1], /^╰─+╯$/);
});

test("renderCard collapses to the frame and the first body line with an expand hint", () => {
	const lines = renderCard(card(), plainTheme, 60, { expanded: false }).map(stripAnsi);
	assert.equal(lines.length, 3);
	assert.match(lines[1], /^│ Receipt-driven development is enabled.*… +│$/);
	assert.equal(visibleWidth(lines[1]), 60);
});

test("renderCard uses the card tone across the full frame while preserving content roles", () => {
	// INFO paints the rose frame: the rounded border in the plain border role,
	// the title in accent — the same look every sidebar card already used.
	const info = renderCard(card(), taggedTheme, 80, { expanded: true });
	assert.match(info[0], /^<border>╭<\/border><border>─ <\/border><accent>力 Jhon<\/accent> <muted>·<\/muted> <muted>review preflight<\/muted><border> ─+<\/border><border>╮<\/border>$/);
	assert.match(info[1], /^<border>│<\/border> <text>.*<border>│<\/border>$/);
	assert.match(info[info.length - 1], /^<border>╰<\/border><border>─+╯<\/border>$/);

	const warning = renderCard(card({ tone: CARD_TONE.WARNING, subtitle: undefined }), taggedTheme, 80, { expanded: true });
	assert.match(warning[0], /^<warning>╭<\/warning>/);
	assert.match(warning[0], /<warning>力 Jhon<\/warning>/);
	assert.match(warning[1], /^<warning>│<\/warning> /);
	assert.match(warning[warning.length - 1], /^<warning>╰<\/warning>/);

	const error = renderCard(card({ tone: CARD_TONE.ERROR }), taggedTheme, 80, { expanded: true, hint: "ctrl+o to expand" });
	assert.match(error[0], /^<error>╭<\/error><error>─ <\/error><error>力 Jhon<\/error> <muted>·<\/muted> <muted>review preflight<\/muted><error> ─+<\/error> <dim>ctrl\+o to expand<\/dim> <error>╮<\/error>$/);
	assert.match(error[1], /^<error>│<\/error> <text>.*<error>│<\/error>$/);
	assert.match(error[error.length - 1], /^<error>╰<\/error><error>─+╯<\/error>$/);
	assert.equal(CARD_TONE.SUCCESS, "success");
});

test("renderCard places a hint at the right end of the top rule without background fill", () => {
	const lines = renderCard(card(), plainTheme, 60, { expanded: false, hint: "ctrl+o expand" });
	assert.match(stripAnsi(lines[0]), /^╭─ 力 Jhon · review preflight ─+ ctrl\+o expand ╮$/);
	assert.equal(visibleWidth(lines[0]), 60);

	assert.doesNotMatch(lines.join("\n"), /\x1b\[44m/);
});

test("cards remain transparent across content resets and narrow widths", () => {
	for (const width of [0, 1, 2, 3, 4, 8, 40]) {
		const lines = renderCard(card({ body: ["red\x1b[0m blue", ""] }), ansiTheme, width, { expanded: true });
		for (const [row, line] of lines.entries()) {
			let painted = false, column = 0;
			for (const token of line.match(/\x1b\[[\d;]*m|[^\x1b]/gu) ?? []) {
				if (token.startsWith("\x1b")) {
					for (const code of token.slice(2, -1).split(";").map(Number)) {
						if (code === 0 || code === 49) painted = false;
						if (code === 44) painted = true;
					}
				} else {
					assert.equal(painted, false, `row ${row}, cell ${column}`);
					column += visibleWidth(token);
				}
			}
			assert.equal(painted, false, "background must not leak into host padding");
			assert.equal(visibleWidth(line), width);
		}
	}
});

test("renderCard accepts a custom glyph and an empty body", () => {
	const lines = renderCard(card({ glyph: "✎", body: [] }), plainTheme, 40, { expanded: true }).map(stripAnsi);
	assert.equal(lines.length, 2);
	assert.match(lines[0], /^╭─ ✎ Jhon · review preflight ─+╮$/);
	assert.match(lines[1], /^╰─+╯$/);
});

test("renderCard keeps the top rule at width with a two-cell glyph", () => {
	const lines = renderCard(card({ glyph: "\u{1F339}\uFE0E" }), plainTheme, 60, { expanded: false, hint: "ctrl+o expand" });
	for (const line of lines) assert.equal(visibleWidth(line), 60, `"${stripAnsi(line)}" is not 60 wide`);
});

test("renderCard drops the hint before truncating title content", () => {
	const lines = renderCard(card(), plainTheme, 35, { expanded: false, hint: "ctrl+o expand" }).map(stripAnsi);
	assert.equal(lines[0], "╭─ 力 Jhon · review preflight ────╮");
	assert.ok(!lines[0].includes("ctrl+o"));
	assert.equal(visibleWidth(lines[0]), 35);
});

test("renderCard truncates ANSI-styled title content at display width", () => {
	const lines = renderCard(
		card({ glyph: "\u{1F339}\uFE0E", title: "Jhon review", subtitle: "completed · review acknowledge approved" }),
		ansiTheme,
		30,
		{ expanded: false, hint: "ctrl+o to expand" },
	);
	assert.match(stripAnsi(lines[0]), /^╭─ 🌹︎ Jhon review.*╮$/);
	assert.ok(!stripAnsi(lines[0]).includes("ctrl+o"));
	for (const line of lines) assert.equal(visibleWidth(line), 30, `"${stripAnsi(line)}" is not 30 wide`);
});

test("tool heading continuation reserves configured hint columns before ANSI and Unicode wrapping", () => {
	const title = "bash $ printf 界e\u0301 alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november oscar papa";
	const hint = "\x1b[36mconfigured key to expand\x1b[0m";
	const value = card({ title: `\x1b[1m${title}\x1b[0m`, subtitle: undefined, glyph: "🌹︎", body: [] });
	for (const width of [60, 80, 120]) {
		const lines = cardTopRows(value, ansiTheme, width, hint);
		assert.match(stripAnsi(lines[0]), /configured key to expand ╮$/);
		assert.match(stripAnsi(lines[0]), /^╭─ 🌹︎ bash \$ printf/);
		assert.ok(lines.length > 1, "long heading continues inside the same frame");
		assert.ok(lines.every((line) => visibleWidth(line) === width));
		const content = lines.map(stripAnsi).map((line, index) => index === 0
			? line.replace(/^╭─ 🌹︎ /, "").replace(/ ─+ configured key to expand ╮$/, "")
			: line.replace(/^│ /, "").replace(/ +│$/, "")).join(" ").replace(/\s+/g, " ").trim();
		assert.equal(content, title, "hint reservation must not lose any command text");
	}
	for (const width of [0, 1, 2, 3, 4, 5, 6, 7, 8]) {
		const lines = cardTopRows(value, ansiTheme, width, hint);
		assert.ok(lines.every((line) => visibleWidth(line) <= width));
		assert.ok(!lines.some((line) => stripAnsi(line).includes("configured key")));
	}
});

// G's two card styles (L153 answer 1): float by default, neon by the setting.
const bgTheme = {
	fg(color: string, text: string) {
		return `<${color}>${text}</${color}>`;
	},
	bg(color: string, text: string) {
		return `\x1b[48;5;${color.length}m${text}\x1b[49m`;
	},
};
function withStyle<T>(style: "float" | "neon", run: () => T): T {
	const previous = cardStyle();
	setCardStyle(style);
	try {
		return run();
	} finally {
		setCardStyle(previous);
	}
}
const plainRows = (lines: string[]) => lines.map((line) => stripAnsi(line.replace(/<\/?[a-zA-Z]+>/g, "")));

test("the card style is float until shell.appearance.cards picks neon (G's default)", () => {
	assert.equal(cardStyle(), CARD_STYLE.FLOAT);
	assert.deepEqual(Object.values(CARD_STYLE).sort(), ["float", "neon"]);
});

test("a float notice: a tinted panel inside a one-column margin, a ▎ bar in its tone, a blank row above the heading and before the body", () => {
	const lines = withStyle("float", () => renderCard(card({ body: ["first", "second"] }), { ...bgTheme, fg: (_role: string, text: string) => text }, 60, { expanded: false, previewRows: 3, hint: "ctrl+o expand" }));
	const plain = plainRows(lines);
	assert.deepEqual(plain.map((line) => line.slice(0, 3)), [" ▎ ", " ▎ ", " ▎ ", " ▎ ", " ▎ ", " ▎ "]);
	assert.match(plain[1], /^ ▎ 力 Jhon · review preflight +ctrl\+o expand {4}$/);
	assert.match(plain[3], /^ ▎ first +$/);
	assert.match(plain[4], /^ ▎ second +$/);
	for (const line of plain) assert.equal(visibleWidth(line), 60);
	assert.ok(lines.every((line) => line.startsWith(" \x1b[48;5;13m") && line.endsWith("\x1b[49m ")), "INFO paints toolSuccessBg");
	const warning = withStyle("float", () => renderCard(card({ tone: CARD_TONE.WARNING }), bgTheme, 40, { expanded: false, previewRows: 3 }));
	assert.ok(warning.every((line) => line.startsWith(" \x1b[48;5;13m")), "warning paints toolPendingBg");
	assert.match(warning[1]!, /<warning>▎<\/warning>/);
	const error = withStyle("float", () => renderCard(card({ tone: CARD_TONE.ERROR }), bgTheme, 40, { expanded: false, previewRows: 3 }));
	assert.ok(error.every((line) => line.startsWith(" \x1b[48;5;11m")), "error paints toolErrorBg");
});

test("a neon notice: the rounded outline with the title and hint in its top rule, the body inside, a closing rule", () => {
	const plain = plainRows(withStyle("neon", () => renderCard(card({ body: ["first", "second", "third", "fourth"] }), bgTheme, 60, { expanded: false, previewRows: 3, hint: "ctrl+o expand" })));
	assert.match(plain[0], /^╭─ 力 Jhon · review preflight ─+ ctrl\+o expand ╮$/);
	assert.deepEqual(plain.slice(1, 4).map((line) => line.replace(/ +│$/, "")), ["│ first", "│ second", "│ third"]);
	assert.match(plain[4], /^╰─+╯$/);
	assert.equal(plain.length, 5);
});

test("notice previews keep a configurable physical-row budget in both styles; a theme without backgrounds draws the outline", () => {
	const value = card({ body: ["first useful row", "界🌹e\u0301".repeat(80), "last"], tone: CARD_TONE.ERROR });
	for (const style of ["float", "neon"] as const) {
		for (const width of [0, 1, 2, 3, 4, 5, 6, 7, 8, 24, 100]) {
			const lines = withStyle(style, () => renderCard(value, { ...bgTheme, fg: (_role: string, text: string) => text }, width, { expanded: false, previewRows: 3 }));
			assert.ok(lines.length <= 7, "padding, heading, a blank, three preview rows, the closing row");
			assert.ok(lines.every((line) => visibleWidth(line) <= width));
			if (width === 0) assert.deepEqual(lines, []);
		}
	}
	const multiline = renderCard(card({ body: ["one\ntwo\nthree\nfour"] }), plainTheme, 60, { expanded: false, previewRows: 3 });
	assert.match(stripAnsi(multiline[0]), /^╭─ 力 Jhon/, "no background: the outline");
	assert.equal(multiline.length, 5);
	assert.doesNotMatch(multiline.join("\n"), /four/);
});

test("shell.appearance.ascii draws both card styles in ASCII", (t) => {
	useMemorySettings(t, { "shell.appearance.ascii": true });
	{
		const neon = plainRows(withStyle("neon", () => renderCard(card({ glyph: "*", body: ["x"] }), bgTheme, 40, { expanded: true, previewRows: 3 })));
		assert.match(neon[0], /^\+- \* Jhon - review preflight -+\+$/, "the subtitle separator reads - (T15.12g)");
		assert.match(neon[1], /^\| x +\|$/);
		assert.match(neon.at(-1)!, /^\+-+\+$/);
		const float = plainRows(withStyle("float", () => renderCard(card({ glyph: "*", body: ["x"] }), bgTheme, 40, { expanded: true, previewRows: 3 })));
		assert.ok(float.every((line) => line.startsWith(" | ")), float.join("\n"));
	}
});

test("legacy renderCard keeps its empty-row shape at nonpositive widths while opt-in previews stay empty", () => {
	// Agents, Todos and Status call renderCard without their own width guard, so
	// their row count at width zero must remain the pre-preview shape: every
	// row is present and empty. Only the opt-in previewRows path returns nothing.
	const paragraphs = card({ subtitle: "2 running", body: ["ab", "", "c d"] });
	const empty = card({ glyph: "✎", subtitle: undefined, body: [], tone: CARD_TONE.WARNING });
	for (const width of [0, -1, -7]) {
		for (const theme of [plainTheme, taggedTheme, ansiTheme]) {
			assert.deepEqual(renderCard(paragraphs, theme, width, { expanded: true }), ["", "", "", "", "", "", ""], `expanded at ${width}`);
			assert.deepEqual(renderCard(paragraphs, theme, width, { expanded: true, hint: "ctrl+o expand" }), ["", "", "", "", "", "", ""], `hinted at ${width}`);
			assert.deepEqual(renderCard(paragraphs, theme, width, { expanded: false }), ["", "", ""], `collapsed at ${width}`);
			assert.deepEqual(renderCard(empty, theme, width, { expanded: true }), ["", ""], `empty body at ${width}`);
			assert.deepEqual(renderCard(paragraphs, theme, width, { expanded: false, previewRows: 3 }), [], `preview at ${width}`);
			assert.deepEqual(renderCard(paragraphs, theme, width, { expanded: true, previewRows: 3 }), [], `expanded preview at ${width}`);
		}
	}
});

test("renderCard never exceeds extremely narrow supplied widths", () => {
	for (const width of [0, 1, 2, 3, 4, 5, 8, 16]) {
		const lines = renderCard(card({ glyph: "\u{1F339}\uFE0E" }), ansiTheme, width, { expanded: true, hint: "ctrl+o to expand" });
		for (const line of lines) assert.equal(visibleWidth(line), width, `"${stripAnsi(line)}" is not ${width} wide`);
	}
});

// Float style: the same rows become a borderless panel. Content cells keep a
// tone-matched background, the frame turns into a tone accent bar and spaces,
// and a one-column margin stays outside the panel on both sides.
test("a call card awaits its result until pi finishes the row or a result renderer marks it", () => {
	const state = {};
	assert.equal(cardAwaitingResult({ isPartial: true, state }), true);
	assert.equal(cardAwaitingResult({ state }), true, "an unknown lifecycle is still pending");
	assert.equal(cardAwaitingResult({ isPartial: true }), true, "no shared state: only the final flag closes it");
	assert.equal(cardAwaitingResult({ isPartial: false, state }), false, "a final result always exists");
	markCardResult(state);
	assert.equal(cardAwaitingResult({ isPartial: true, state }), false, "a partial result component closes the frame");
	assert.deepEqual(Object.keys(state), [], "the mark never shows up as ordinary row state");
	for (const value of [undefined, null, 1, "state"]) {
		assert.doesNotThrow(() => markCardResult(value));
		assert.equal(cardAwaitingResult({ isPartial: true, state: value }), true);
	}
	assert.equal(cardRunningLine(CARD_TONE.WARNING, plainTheme, 20), `│ running…${" ".repeat(8)} │`);
	for (const width of [0, 1, 2, 3, 4, 5, 6, 7, 8]) assert.ok(visibleWidth(cardRunningLine(CARD_TONE.WARNING, plainTheme, width)) <= width);
});
