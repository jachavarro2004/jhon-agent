/**
 * The frame primitives (T174-S2): clip and fit, rules and pane dividers, the
 * tinted block of design/shell-tui, the conversation card in its two styles,
 * the overlay box, the meter, the hover role and the glyph set. Rows are
 * checked at 180 and 100 columns with a plain theme (test/rows.ts).
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import {
	BAND_ROLE, BOX_PADDING, CARD_STYLE, CARD_TONE, GLYPHS, HOVER_ROLE, STATES, TINT,
	asciiText, cardAwaitingResult, cardInnerWidth, cardStyle, clip, drawCard, fit, glyph, joinPanes, markCardResult,
	meterTone, paintHover, paintMeter, paintSurface, paintSurfaceRows, panelInnerWidth, plainPainter, renderBox, renderCard,
	renderDivider, renderMeter, renderPick, renderRule, renderTintedBlock, setCardStyle, spread, stripAnsi, surfaceOpener,
	type Card, type CardStyle,
} from "../lib/ui/primitives/index.ts";
import { useMemorySettings } from "./fake-jhon-settings.ts";
import { SNAPSHOT_WIDTHS, assertRows, padded, plainTheme, rowsAt, spread as spreadRow } from "./rows.ts";

const paint = plainPainter();
const tagged = { fg: (role: string, text: string) => `<${role}>${text}</${role}>`, bold: (text: string) => `<b>${text}</b>` };
// A theme that paints backgrounds: the opener names the role, so a row says which tint it sits on.
const tinted = {
	...plainTheme,
	bg: (role: string, text: string) => `\x1b[48;5;${role.length}m${text}\x1b[49m`,
};
const roleOf = (row: string) => /^ ?\x1b\[48;5;(\d+)m/.exec(row)?.[1];
const ROLE_CODE = (role: string) => String(role.length);
function withStyle<T>(style: CardStyle, run: () => T): T {
	const previous = cardStyle();
	setCardStyle(style);
	try { return run(); } finally { setCardStyle(previous); }
}

// --- clip and fit -------------------------------------------------------------

test("clip cuts at display width with the ellipsis, wide glyphs counted twice; ASCII cuts with ~", () => {
	assert.equal(clip("billing/api/export.py", 10), "billing/a…");
	assert.equal(clip("billing/api/export.py", 10, { ascii: true }), "billing/a~");
	assert.equal(clip("short", 10), "short", "text that fits is unchanged");
	assert.equal(clip("力力力力", 5), "力力…", "never splits a wide glyph");
	assert.equal(visibleWidth(clip("力力力力", 4)), 3);
	assert.equal(clip("abcdef", 4, { ellipsis: "" }), "abcd", "no ellipsis: a hard cut");
	assert.equal(clip("abc", 0), "");
	assert.equal(stripAnsi(clip("\x1b[31mred text here\x1b[0m", 6)), "red t…", "painted text keeps its escapes whole");
});

test("fit gives exactly width cells: padded when short, clipped when long", () => {
	assert.equal(fit("ok", 5), "ok   ");
	assert.equal(fit("力 ok", 6), "力 ok ");
	assert.equal(fit("overflowing", 6), "overf…");
	for (const width of [0, 1, 2, 7, 100, 180]) assert.equal(visibleWidth(fit("力 a long line of text", width)), width);
});

test("spread ends the right part at width and cuts the left first", () => {
	assert.equal(spread("left", "right", 15), "left      right");
	assert.equal(spread("a long left part", "right", 15), "a long l… right");
	assert.equal(spread("left", "a right part far too long", 10), "left      ", "a right part that cannot fit drops");
});

// --- glyphs ---------------------------------------------------------------------

test("the state glyphs stay ✓ · ◉ ● ◐ ✗, each with a one-cell ASCII form", () => {
	assert.deepEqual(Object.values(STATES).map((state) => state.glyph), ["✓", "·", "◉", "●", "◐", "✗", "?"]);
	for (const state of Object.values(STATES)) assert.match(state.ascii, /^[\x20-\x7e]$/);
});

test("every glyph is one cell with a one-cell ASCII form, except the two-cell brand", () => {
	for (const [name, [unicode, ascii]] of Object.entries(GLYPHS)) {
		if (name === "brand") continue;
		assert.equal(visibleWidth(unicode), 1, name);
		assert.match(ascii, /^[\x20-\x7e]$/, name);
	}
	assert.equal(glyph("brand"), "力");
	assert.equal(glyph("brand", true), "jhon");
	assert.equal(glyph("heavyRule"), "━");
	assert.equal(glyph("heavyRule", true), "=");
	assert.equal(asciiText("━━ ✓ done · ◐ working"), "== + done - ~ working");
});

// --- rules and pane dividers --------------------------------------------------------

test("a plain rule fills the width; a labelled rule is design 04's Running dock", () => {
	assertRows((width) => [renderRule({ width, paint })], (width) => ["─".repeat(width)]);
	assertRows((width) => [renderRule({ width, label: "Running", note: "2 agents · 1 job", paint })],
		(width) => [`── Running  2 agents · 1 job ${"─".repeat(width - 29)}`]);
	assert.equal(renderRule({ width: 12, label: "Running", note: "2 agents · 1 job", paint }), "── Running ─", "the note gives way first");
	assert.equal(renderRule({ width: 8, label: "Running", paint }), "── Run… ");
	assert.equal(renderRule({ width: 6, label: "Running", ascii: true, paint }), "-- R~ ");
});

test("a rule can open with a heavy run in accent: the tab underline of the full-screen views", () => {
	const row = renderRule({ width: 20, heavy: 10, paint: { ...paint, fg: (role, text) => `<${role}>${text}</${role}>` } });
	assert.equal(row, `<accent>${"━".repeat(10)}</accent><faint>${"─".repeat(10)}</faint>`);
	assert.equal(stripAnsi(renderRule({ width: 6, heavy: 10, ascii: true, paint })), "======");
});

test("a pane divider is one faint column; joinPanes is design 01's split, the panel right of the rule", () => {
	assert.deepEqual(renderDivider(3, { paint }), ["│", "│", "│"]);
	assert.deepEqual(renderDivider(2, { ascii: true, paint }), ["|", "|"]);
	const conversation = ["Slice 2's endpoint answers the CSV.", "", "Sending slice 2 to the Prism review."];
	const panel = ["◆ add-invoice-export", "standard · strict TDD · risk medium"];
	assertRows((width) => joinPanes({ left: conversation, right: panel, leftWidth: width - 46, width, paint }), (width) => [
		`${padded(conversation[0]!, width - 46)} │ ${padded(panel[0]!, 43)}`,
		`${padded("", width - 46)} │ ${padded(panel[1]!, 43)}`,
		`${padded(conversation[2]!, width - 46)} │ ${padded("", 43)}`,
	]);
	const tight = joinPanes({ left: ["a long conversation row"], right: ["a long panel row"], leftWidth: 10, width: 20, paint });
	assert.deepEqual(tight, ["a long co… │ a long…"], "each pane clips to its own width");
});

// --- the tinted block (design/shell-tui 01's cards) ---------------------------------------

const BASH = { icon: "$", title: "pytest tests/billing -k export", right: "✓ 2.3s", body: ["14 passed in 2.31s"] };
const FINDING = "✗ high      billing/api/export.py:88  an empty range answers 200 with a header-only CSV; spec S4 asks for 422";

test("a tinted block: the header one cell in with its state at the right, the body three cells in", () => {
	assertRows((width, theme) => renderTintedBlock({ width, ...BASH, theme }), (width) => [
		spreadRow(" $ pytest tests/billing -k export", "✓ 2.3s ", width),
		padded("   14 passed in 2.31s", width),
	]);
	assertRows((width, theme) => renderTintedBlock({ width, icon: "◇", title: "Prism review  slice 2 Export endpoint", right: "✗ changes requested", body: [FINDING], theme }), {
		180: [spreadRow(" ◇ Prism review  slice 2 Export endpoint", "✗ changes requested ", 180), padded(`   ${FINDING}`, 180)],
		100: [spreadRow(" ◇ Prism review  slice 2 Export endpoint", "✗ changes requested ", 100), `   ${FINDING.slice(0, 94)}…  `],
	});
	const header = renderTintedBlock({ width: 40, icon: "✗", title: "refused  $ git push origin feat/add-invoice-export", right: "safety guard", tone: "error", theme: plainTheme })[0]!;
	assert.equal(header, " ✗ refused  $ git push o…  safety guard ", "the title gives way to the state");
	assert.equal(renderTintedBlock({ width: 20, ...BASH, theme: plainTheme })[0], " $ pytest tests/bi… ", "too narrow for the state: the title alone");
	for (const width of [0, 1, 3, 5, 6, 12]) {
		for (const row of renderTintedBlock({ width, ...BASH, theme: plainTheme })) assert.equal(visibleWidth(row), width);
	}
});

test("the tint roles: a block sits on Box (toolSuccessBg); an error's header on errorContainer (toolErrorBg)", () => {
	assert.deepEqual(TINT, { box: "toolSuccessBg", error: "toolErrorBg", band: "customMessageBg", strong: "userMessageBg", sel: "selectedBg" });
	const normal = renderTintedBlock({ width: 60, ...BASH, theme: tinted });
	assert.deepEqual(normal.map(roleOf), [ROLE_CODE(TINT.box), ROLE_CODE(TINT.box)]);
	const refusal = renderTintedBlock({ width: 60, ...BASH, tone: "error", theme: tinted });
	assert.deepEqual(refusal.map(roleOf), [ROLE_CODE(TINT.error), ROLE_CODE(TINT.box)]);
	assert.ok(normal.every((row) => row.endsWith("\x1b[49m")), "the tint closes at the row's end");
	const icon = renderTintedBlock({ width: 60, ...BASH, theme: { ...tagged } })[0]!;
	assert.match(icon, /^ <b><accent>\$<\/accent><\/b> /, "the icon is bold accent");
	assert.match(renderTintedBlock({ width: 60, ...BASH, tone: "error", theme: tagged })[0]!, /^ <b><error>\$<\/error><\/b> /, "bold bad on an error");
});

test("surfaces: the opener re-arms after every reset in the row; a theme that cannot paint leaves rows as they are", () => {
	const open = surfaceOpener(tinted, BAND_ROLE);
	assert.equal(open, `\x1b[48;5;${BAND_ROLE.length}m`);
	assert.equal(paintSurface("a\x1b[0mb", open), `${open}a\x1b[0m${open}b\x1b[49m`);
	assert.equal(surfaceOpener(plainTheme, BAND_ROLE), "");
	assert.equal(surfaceOpener({ ...plainTheme, bg: () => { throw new Error("no role"); } }, BAND_ROLE), "");
	assert.deepEqual(paintSurfaceRows(["x"], plainTheme, BAND_ROLE, 4), ["x"]);
	assert.deepEqual(paintSurfaceRows(["x"], tinted, BAND_ROLE, 4).map(stripAnsi), ["x   "]);
});

// --- the conversation card -------------------------------------------------------------

const NOTICE: Card = { title: "Saved", subtitle: "settings", body: ["shell.appearance.cards is neon now.", "", "It applies to every new card."], tone: CARD_TONE.INFO };

test("a notice card without a background is the rounded outline: title in the top rule, body inside, a closing rule", () => {
	assertRows((width, theme) => renderCard(NOTICE, theme, width, { expanded: true }), (width) => [
		`╭─ 力 Saved · settings ${"─".repeat(width - 24)}╮`,
		`│ ${padded("shell.appearance.cards is neon now.", width - 4)} │`,
		`│ ${padded("", width - 4)} │`,
		`│ ${padded("It applies to every new card.", width - 4)} │`,
		`╰${"─".repeat(width - 2)}╯`,
	]);
	assertRows((width, theme) => renderCard(NOTICE, theme, width, { expanded: false, hint: "ctrl+o expand" }), (width) => [
		`╭─ 力 Saved · settings ${"─".repeat(width - 39)} ctrl+o expand ╮`,
		`│ ${padded("shell.appearance.cards is neon now.…", width - 4)} │`,
		`╰${"─".repeat(width - 2)}╯`,
	], "collapsed: the first line and an ellipsis");
});

test("the outline paints the frame in the tone, an info title in accent", () => {
	const info = renderCard({ ...NOTICE, body: ["x"] }, tagged, 40, { expanded: true });
	assert.match(info[0]!, /^<border>╭<\/border><border>─ <\/border><accent>力 Saved<\/accent> <muted>·<\/muted> <muted>settings<\/muted><border> ─+<\/border><border>╮<\/border>$/);
	assert.match(info[1]!, /^<border>│<\/border> <text>x<\/text> +<border>│<\/border>$/);
	const error = renderCard({ ...NOTICE, subtitle: undefined, tone: CARD_TONE.ERROR, body: [] }, tagged, 40, { expanded: true, hint: "ctrl+o" });
	assert.match(error[0]!, /^<error>╭<\/error><error>─ <\/error><error>力 Saved<\/error><error> ─+<\/error> <dim>ctrl\+o<\/dim> <error>╮<\/error>$/);
	assert.equal(error.length, 2);
});

test("the hint goes before the title is cut; nothing is ever wider than the width", () => {
	assert.equal(stripAnsi(renderCard(NOTICE, plainTheme, 30, { expanded: false, hint: "ctrl+o expand" })[0]!), "╭─ 力 Saved · settings ──────╮");
	for (const width of [0, 1, 2, 3, 4, 5, 8, 16, 30]) {
		for (const line of renderCard({ ...NOTICE, glyph: "\u{1F339}︎" }, plainTheme, width, { expanded: true, hint: "ctrl+o to expand" })) assert.equal(visibleWidth(line), width);
	}
	assert.deepEqual(renderCard({ ...NOTICE, body: ["ab", "", "c d"] }, plainTheme, 0, { expanded: true }), Array(7).fill(""), "a nonpositive width keeps the row count of one cell's wrap");
	assert.deepEqual(renderCard(NOTICE, plainTheme, 0, { expanded: true, previewRows: 3 }), [], "a preview draws nothing");
});

test("the float style: a tinted panel inside a one-cell margin, a ▎ bar, a blank row above the heading and the body", () => {
	const rows = withStyle(CARD_STYLE.FLOAT, () => renderCard({ ...NOTICE, body: ["first", "second"] }, tinted, 60, { expanded: false, previewRows: 3, hint: "ctrl+o expand" }));
	const plain = rows.map(stripAnsi);
	assert.deepEqual(plain, [
		padded(" ▎", 60),
		spreadRow(" ▎ 力 Saved · settings", "ctrl+o expand    ", 60),
		padded(" ▎", 60),
		padded(" ▎ first", 60),
		padded(" ▎ second", 60),
		padded(" ▎", 60),
	]);
	assert.ok(rows.every((row) => roleOf(row) === ROLE_CODE(TINT.box) && row.endsWith("\x1b[49m ")), "info on toolSuccessBg, a clear margin on both sides");
	const pending = withStyle(CARD_STYLE.FLOAT, () => renderCard({ ...NOTICE, tone: CARD_TONE.WARNING }, tinted, 40, { expanded: false, previewRows: 3 }));
	assert.ok(pending.every((row) => roleOf(row) === ROLE_CODE("toolPendingBg")));
	const error = withStyle(CARD_STYLE.FLOAT, () => renderCard({ ...NOTICE, tone: CARD_TONE.ERROR }, tinted, 40, { expanded: false, previewRows: 3 }));
	assert.ok(error.every((row) => roleOf(row) === ROLE_CODE(TINT.error)));
	assert.equal(withStyle(CARD_STYLE.FLOAT, () => renderCard(NOTICE, tinted, 9, { expanded: true, previewRows: 3 }))[0]!.startsWith("╭"), true, "under ten columns the outline");
	assert.equal(panelInnerWidth(tinted, 50), 44);
	assert.equal(panelInnerWidth(plainTheme, 50), 46);
	assert.equal(cardStyle(), CARD_STYLE.FLOAT, "float is the default style");
});

test("a preview keeps its row budget after wrapping, in both styles", () => {
	const value: Card = { ...NOTICE, body: ["one\ntwo\nthree\nfour"] };
	for (const style of ["float", "neon"] as const) {
		const rows = withStyle(style, () => renderCard(value, tinted, 60, { expanded: false, previewRows: 3 })).map(stripAnsi);
		assert.ok(rows.some((row) => row.includes("three")) && !rows.some((row) => row.includes("four")), style);
	}
});

test("shell.appearance.ascii draws both styles in ASCII", (t) => {
	useMemorySettings(t, { "shell.appearance.ascii": true });
	const neon = withStyle(CARD_STYLE.NEON, () => renderCard({ ...NOTICE, body: ["x"] }, tinted, 40, { expanded: true, previewRows: 3 })).map(stripAnsi);
	assert.deepEqual(neon, [`+- jhon Saved - settings ${"-".repeat(14)}+`, `| x${" ".repeat(36)}|`, `+${"-".repeat(38)}+`]);
	const float = withStyle(CARD_STYLE.FLOAT, () => renderCard({ ...NOTICE, body: ["x"] }, tinted, 40, { expanded: true, previewRows: 3 })).map(stripAnsi);
	assert.ok(float.every((row) => row.startsWith(" | ")), float.join("\n"));
	for (const width of [1, 2, 3, 4, 6]) assert.match(stripAnsi(renderCard(NOTICE, plainTheme, width, { expanded: true }).join("")), /^[\x20-\x7e]*$/, `${width}`);
});

test("drawCard: a tool card's call row, its running line until a result exists, then the result closes it", () => {
	const draw = (running: boolean) => drawCard(CARD_TONE.WARNING, plainTheme, 40, (frame, inner) => ({
		head: frame.topRows({ title: "bash $ make test", glyph: "$" }, inner, "ctrl+o"),
		...(running ? { body: [frame.running(inner)], bottom: frame.close(inner) } : {}),
	}));
	assert.deepEqual(draw(true), [`╭─ $ bash $ make test ${"─".repeat(9)} ctrl+o ╮`, `│ running…${" ".repeat(28)} │`, `╰${"─".repeat(38)}╯`]);
	assert.deepEqual(draw(false), [`╭─ $ bash $ make test ${"─".repeat(9)} ctrl+o ╮`]);
	const result = drawCard(CARD_TONE.SUCCESS, plainTheme, 40, (frame, inner) => ({ body: ["ok"].map((line) => frame.line(line, inner)), bottom: frame.close(inner), afterHeading: true }));
	assert.deepEqual(result, [`│ ok${" ".repeat(34)} │`, `╰${"─".repeat(38)}╯`]);
	assert.equal(cardInnerWidth(40), 36);
	const floatResult = withStyle(CARD_STYLE.FLOAT, () => drawCard(CARD_TONE.SUCCESS, tinted, 40, (frame, inner) => ({ body: [frame.line("ok", inner)], bottom: frame.close(inner), afterHeading: true }))).map(stripAnsi);
	assert.deepEqual(floatResult, [padded(" ▎", 40), padded(" ▎ ok", 40), padded(" ▎", 40)], "after a heading the float body keeps its blank row");
});

test("a long call continues inside the same frame, the hint reserved first, no text lost", () => {
	const title = "bash $ printf 界é alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike";
	for (const width of [60, 100, 180]) {
		const rows = drawCard(CARD_TONE.SUCCESS, plainTheme, width, (frame, inner) => ({ head: frame.topRows({ title, glyph: "$" }, inner, "ctrl+o expand") }));
		assert.ok(rows.every((row) => visibleWidth(row) === width));
		assert.match(rows[0]!, /ctrl\+o expand ╮$/);
		const words = rows.map((row, index) => index === 0 ? row.replace(/^╭─ \$ /, "").replace(/ ─+ ctrl\+o expand ╮$/, "") : row.replace(/^│ /, "").replace(/ +│$/, ""));
		assert.equal(words.join(" ").replace(/\s+/g, " ").trim(), title);
	}
	for (const width of [0, 1, 4, 7]) {
		const rows = drawCard(CARD_TONE.SUCCESS, plainTheme, width, (frame, inner) => ({ head: frame.topRows({ title, glyph: "$" }, inner, "ctrl+o expand") }));
		assert.ok(rows.every((row) => visibleWidth(row) <= width && !row.includes("ctrl+o")));
	}
});

test("a call card awaits its result until pi finishes the row or a result renderer marks the shared state", () => {
	const state = {};
	assert.equal(cardAwaitingResult({ isPartial: true, state }), true);
	assert.equal(cardAwaitingResult({ state }), true);
	assert.equal(cardAwaitingResult({ isPartial: false, state }), false);
	markCardResult(state);
	assert.equal(cardAwaitingResult({ isPartial: true, state }), false);
	assert.deepEqual(Object.keys(state), [], "the mark is not ordinary row state");
	// A row state that is no object cannot carry the mark: marking it does nothing, and the call keeps waiting.
	const notObjects = [undefined, null, 1, "state"];
	notObjects.forEach((value) => markCardResult(value));
	assert.deepEqual(notObjects.map((value) => cardAwaitingResult({ isPartial: true, state: value })), [true, true, true, true]);
});

// --- the overlay box, the meter, hover ---------------------------------------------------

test("the overlay box: a rounded border role frame with one cell of padding, every row fitted", () => {
	assertRows((width, theme) => renderBox(["Customize", "a row far too long for the box ".repeat(8)], width, theme), (width) => [
		`╭${"─".repeat(width - 2)}╮`,
		`│ ${padded("Customize", width - BOX_PADDING)} │`,
		`│ ${clip("a row far too long for the box ".repeat(8), width - BOX_PADDING)} │`,
		`╰${"─".repeat(width - 2)}╯`,
	]);
	assert.deepEqual(renderBox(["x"], 6, tagged, true), ["<border>+----+</border>", "<border>|</border> x  <border>|</border>", "<border>+----+</border>"]);
});

test("a pick row: the cursor and accent, or the whole row on the selection tint when the theme has one", () => {
	assert.equal(renderPick("Appearance", 12, true, tagged), "<accent>▸ </accent><text>Appearance</text>");
	assert.equal(renderPick("Appearance", 14, false, tagged), "  <text>Appearance  </text>", "the label pads to the width");
	assert.equal(renderPick("Appearance", 12, false), "  Appearance");
	assert.equal(stripAnsi(renderPick("Appearance", 14, true, tinted)), "  Appearance  ");
	assert.equal(roleOf(renderPick("Appearance", 14, true, tinted)), ROLE_CODE(TINT.sel));
	assert.equal(renderPick("a\x07b", 8, false), "  a\\x07b", "a control byte shows escaped");
	assert.equal(stripAnsi(renderPick("Permissions", 8, true, undefined, true)), "> Permi~");
});

test("the meter: eight cells on one ladder, ok under 70, warn from 70, bad from 90", () => {
	assert.equal(renderMeter(45), "▰▰▰▰▱▱▱▱");
	assert.equal(renderMeter(null), "▱▱▱▱▱▱▱▱");
	assert.equal(renderMeter(100, 4, true), "####");
	assert.deepEqual([45, 69, 70, 89, 90, null].map(meterTone), ["success", "success", "warning", "warning", "error", "dim"]);
	assert.equal(paintMeter(75, tagged), "<warning>▰▰▰▰▰▰</warning><border>▱▱</border>");
});

test("hover paints one role, warning, over any idle role", () => {
	assert.equal(HOVER_ROLE, "warning");
	assert.equal(paintHover(tagged, "refresh", true, "dim"), "<warning>refresh</warning>");
	assert.equal(paintHover(tagged, "refresh", false, "dim"), "<dim>refresh</dim>");
	assert.equal(paintHover(tagged, "refresh", false), "refresh");
});

test("the snapshot helper draws at the design's widths with a plain theme", () => {
	assert.deepEqual([...SNAPSHOT_WIDTHS], [180, 100]);
	assert.deepEqual(rowsAt((width) => [`\x1b[1m${"x".repeat(width)}\x1b[0m`], 100), ["x".repeat(100)]);
});
