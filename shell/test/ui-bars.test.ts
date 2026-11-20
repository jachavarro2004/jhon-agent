/**
 * The top row and the status bar (T174-S3), drawn to design/shell-tui 01 (180
 * columns) and 04 (the narrow form, 100): the top row is `力 repo  path` and
 * nothing else; the status bar is branch │ sandbox on the left and
 * model · effort │ ctx │ cost on the right. Row snapshots through test/rows.ts.
 *
 * Truncation order (written down here, pinned below): under 140 columns the
 * narrow form drops the meter and caps the branch at 18 cells. When a row
 * still does not fit, the branch shortens first (down to 8 cells), then
 * these go, one at a time: the meter, the profile, the compaction note,
 * model · effort, sandbox, the branch, ctx, the mode's badge. The cost is
 * the last to go.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-bars.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test, { type TestContext } from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { BRANCH_MIN_CELLS, NARROW_BELOW, NARROW_BRANCH_CELLS, renderStatusBar, renderTopRow, tildePath, type BarModel } from "../lib/ui/bars.ts";
import { chromeSurface, setChromeSurface } from "../lib/ui/shell-panel.ts";
import { stripAnsi } from "../lib/ui/primitives/index.ts";
import { assertRows, plainTheme, spread, padded } from "./rows.ts";
import { useMemorySettings } from "./fake-jhon-settings.ts";

// The design's story (design/shell-tui/.src/parts.py): acme_app, mid-slice 3.
const acme = (overrides: Partial<BarModel> = {}): BarModel => ({
	cwd: "/home/dev/acme_app", branch: "feat/add-invoice-export", sandbox: "on",
	modelId: "opus-5-5", effort: "high", contextPercent: 38, contextWindow: 1_000_000, costTotal: 1.42, subscription: false,
	...overrides,
});
// A home that is not the path's, so the top row shows it whole, as the artboard does.
const ELSEWHERE = "/home/owner";
const tagged = { fg: (role: string, text: string) => `<${role}>${text}</${role}>`, bold: (text: string) => `<b>${text}</b>` };

function surface(t: TestContext, value: "transparent" | "tinted") {
	const before = chromeSurface();
	setChromeSurface(value);
	t.after(() => setChromeSurface(before));
}

test("the top row is the design's: ` 力 acme_app  /home/dev/acme_app` and nothing else, at 180 and 100 columns", () => {
	assertRows((width, theme) => [renderTopRow(acme(), theme, width, { home: ELSEWHERE })], (width) => [padded(" 力 acme_app  /home/dev/acme_app", width)], "top row");
});

test("the top row shows the home as ~", () => {
	assertRows((width, theme) => [renderTopRow(acme(), theme, width, { home: "/home/dev" })], (width) => [padded(" 力 acme_app  ~/acme_app", width)], "top row");
	assert.equal(tildePath("/home/dev", "/home/dev"), "~");
	assert.equal(tildePath("/home/dev/", "/home/dev"), "~");
	assert.equal(tildePath("/home/devel/x", "/home/dev"), "/home/devel/x", "a sibling folder that shares the prefix is not the home");
	assert.equal(tildePath("/srv/acme", "/home/dev"), "/srv/acme");
});

test("the top row carries no session name, model, profile or anything else of the model", () => {
	const row = stripAnsi(renderTopRow(acme({ profile: "team", mode: "yolo" }), plainTheme, 180, { home: "/home/dev" }));
	assert.equal(row.trimEnd(), " 力 acme_app  ~/acme_app");
});

test("the top row's roles: 力 accent bold, the name text bold, the path dim", () => {
	const row = renderTopRow(acme(), tagged, 180, { home: "/home/dev" });
	assert.match(row, /^ <b><accent>力<\/accent><\/b> <b><text>acme_app<\/text><\/b> {2}<dim>~\/acme_app<\/dim> +$/);
});

test("narrowing, the top row clips the path first, then the name; always exactly the width", () => {
	const at = (width: number) => stripAnsi(renderTopRow(acme({ cwd: "/srv/projects/acme_app" }), plainTheme, width, { home: ELSEWHERE }));
	assert.equal(at(30), " 力 acme_app  /srv/projects/a…");
	assert.equal(at(16), " 力 acme_app  /…");
	assert.equal(at(14), " 力 acme_app  ", "no room for one path cell beside its ellipsis: the path goes");
	assert.equal(at(8), " 力 acm…");
	for (let width = 0; width <= 60; width++) assert.equal(visibleWidth(at(width)), width, `${width}`);
});

test("under a multiplexer that shows the path, the top row names the project alone (L112)", () => {
	assert.equal(stripAnsi(renderTopRow(acme({ hostShowsPath: true }), plainTheme, 100, { home: ELSEWHERE })), padded(" 力 acme_app", 100));
});

test("the status bar is the design's at 180 (01) and in the narrow form at 100 (04)", () => {
	assertRows((width, theme) => [renderStatusBar(acme(), theme, width)], {
		180: [spread(" ⎇ feat/add-invoice-export │ sandbox on", "opus-5-5 · high │ ctx ▰▰▰▱▱▱▱▱ 38% │ $1.42 ", 180)],
		100: [spread(" ⎇ feat/add-invoice-… │ sandbox on", "opus-5-5 · high │ ctx 38% │ $1.42 ", 100)],
	}, "status bar");
});

test("the narrow form starts under 140 columns: no meter, the branch capped at 18 cells", () => {
	assert.equal(NARROW_BELOW, 140);
	assert.equal(NARROW_BRANCH_CELLS, 18);
	assert.match(stripAnsi(renderStatusBar(acme(), plainTheme, 140)), /feat\/add-invoice-export .* ctx ▰▰▰▱▱▱▱▱ 38%/);
	assert.match(stripAnsi(renderStatusBar(acme(), plainTheme, 139)), /feat\/add-invoice-… .* ctx 38%/);
	assert.match(stripAnsi(renderStatusBar(acme(), plainTheme, 100, { narrow: false })), /feat\/add-invoice-export .* ctx ▰▰▰▱▱▱▱▱ 38%/, "the form can be asked for");
	assert.match(stripAnsi(renderStatusBar(acme({ branch: "main" }), plainTheme, 100)), /^ ⎇ main │ sandbox on +/, "a short branch stays whole");
});

test("the status bar's roles: ⎇ accent, the branch text, │ faint, sandbox dim, on ok, model text, effort muted, ctx dim, the meter in its tone, % and cost text", () => {
	const row = renderStatusBar(acme(), tagged, 400);
	for (const piece of [
		/^ <accent>⎇<\/accent> <text>feat\/add-invoice-export<\/text>/, /<mdHr> │ <\/mdHr><dim>sandbox<\/dim> <success>on<\/success>/,
		/<text>opus-5-5<\/text><dim> · <\/dim><muted>high<\/muted><mdHr> │ <\/mdHr>/,
		/<dim>ctx<\/dim> <success>▰▰▰▱▱▱▱▱<\/success> <text>38%<\/text><mdHr> │ <\/mdHr><text>\$1\.42<\/text> $/,
	]) assert.match(row, piece);
	assert.match(renderStatusBar(acme({ contextPercent: 75 }), tagged, 400), /<warning>▰▰▰▰▰▰▱▱<\/warning>/, "warn from 70");
	assert.match(renderStatusBar(acme({ contextPercent: 95 }), tagged, 400), /<error>▰▰▰▰▰▰▰▰<\/error>/, "bad from 90");
	assert.match(renderStatusBar(acme({ sandbox: "off" }), tagged, 400), /<dim>sandbox<\/dim> <warning>off<\/warning>/, "off is a warning");
});

test("the status bar's facts: sandbox off, unknown sandbox, no branch, no effort, unknown context, a subscription's cost, the profile", () => {
	const at = (model: BarModel, width = 180) => stripAnsi(renderStatusBar(model, plainTheme, width));
	assert.match(at(acme({ sandbox: "off" })), /│ sandbox off +opus/);
	assert.match(at(acme({ sandbox: undefined })), /^ ⎇ feat\/add-invoice-export +opus/, "unread: no sandbox segment");
	assert.match(at(acme({ branch: null })), /^ sandbox on +opus/, "no branch: no ⎇");
	assert.match(at(acme({ effort: undefined })), / opus-5-5 │ ctx/);
	assert.match(at(acme({ effort: "xhigh" })), / opus-5-5 · extra high │ ctx/, "pi's thinking levels in the owner's words");
	assert.match(at(acme({ contextPercent: null })), /ctx ▱▱▱▱▱▱▱▱ \?% │/);
	assert.match(at(acme({ subscription: true })), /│ \$1\.42 sub $/);
	assert.match(at(acme({ costTotal: 0.004 })), /│ \$0\.004 $/);
	assert.match(at(acme({ profile: "team" })), / opus-5-5 · high · team │ ctx/, "an active profile beside the model (T120)");
	for (const width of [180, 100]) assert.doesNotMatch(at(acme(), width), /⧗|job|agent/, "jobs and agents are the right pane's (S5)");
});

test("the owner's mode wears its badge after sandbox; normal shows none (S31)", () => {
	assert.match(stripAnsi(renderStatusBar(acme({ mode: "yolo" }), plainTheme, 180)), /^ ⎇ feat\/add-invoice-export │ sandbox on │ YOLO +opus/);
	assert.match(renderStatusBar(acme({ mode: "yolo" }), tagged, 400), /<mdHr> │ <\/mdHr><b><warning>YOLO<\/warning><\/b>/);
	assert.match(renderStatusBar(acme({ mode: "super-yolo" }), tagged, 400), /<b><error>SUPER YOLO<\/error><\/b>/);
	assert.doesNotMatch(stripAnsi(renderStatusBar(acme({ mode: "normal" }), plainTheme, 180)), /YOLO|│ +│/);
});

test("a filling context says where the session compacts, ten points before it", (t) => {
	useMemorySettings(t, {});
	const at = (percent: number) => stripAnsi(renderStatusBar(acme({ contextPercent: percent, contextWindow: 200_000 }), plainTheme, 180));
	assert.doesNotMatch(at(45), /compacts/);
	assert.match(at(55), /ctx ▰▰▰▰▱▱▱▱ 55% · compacts at 60% │ \$1\.42 $/);
});

test("truncation: the branch shortens first; then the meter, the profile, the note, model · effort, sandbox, the branch, ctx and the badge go, in that order; the cost goes last", (t) => {
	useMemorySettings(t, {});
	const data = acme({ mode: "yolo", profile: "team", contextPercent: 55, contextWindow: 200_000 });
	const facts: Array<[string, RegExp]> = [
		["meter", /▰/], ["profile", /· team/], ["note", /compacts/], ["model", /opus-5-5/], ["sandbox", /sandbox/],
		["branch", /⎇/], ["ctx", /ctx/], ["badge", /YOLO/], ["cost", /\$1\.42/],
	];
	for (const narrow of [false, true]) {
		let gone = 0;
		let shortened = false;
		for (let width = 220; width >= 0; width--) {
			const row = stripAnsi(renderStatusBar(data, plainTheme, width, { narrow }));
			assert.equal(visibleWidth(row), width, `${width}: ${row}`);
			const shown = facts.map(([, fact]) => fact.test(row));
			while (gone < facts.length && !shown[gone]) gone++;
			for (let i = 0; i < facts.length; i++) assert.equal(shown[i], i >= gone, `narrow ${narrow} ${width}: ${facts[i]![0]} ${i < gone ? "came back" : "went out of order"}: ${JSON.stringify(row)}`);
			if (row.includes("feat/add-invoice-export")) assert.ok(!shortened, `${width}: the branch grew back`);
			else if (shown[5]) shortened = true;
			if (gone > 0) assert.ok(shortened || narrow, `${width}: the meter went before the branch shortened: ${row}`);
			if (shown[5]) assert.ok(/⎇ \S{7}/.test(row), `${width}: the branch keeps ${BRANCH_MIN_CELLS} cells while it shows: ${row}`);
			if (width >= 7) assert.match(row, /\$1\.42/, `${width}: the cost stays`);
		}
		assert.equal(gone, facts.length, "at the end even the cost went");
	}
	assert.equal(stripAnsi(renderStatusBar(data, plainTheme, 9)), "   $1.42 ", "the cost alone, at the right");
	assert.equal(stripAnsi(renderStatusBar(data, plainTheme, 6)), " $1.… ", "then the cost clipped");
});

test("tinted, both bars sit on the band; transparent, on the terminal's own background", (t) => {
	const BG = "\x1b[48;5;236m";
	const painted = { ...plainTheme, bg: (_role: string, text: string) => `${BG}${text}\x1b[49m` };
	surface(t, "tinted");
	assert.ok(renderTopRow(acme(), painted, 100).startsWith(BG));
	assert.ok(renderStatusBar(acme(), painted, 100).startsWith(BG));
	setChromeSurface("transparent");
	assert.ok(!renderTopRow(acme(), painted, 100).includes(BG));
	assert.ok(!renderStatusBar(acme(), painted, 100).includes(BG));
});

test("ASCII (shell.appearance.ascii): ⎇ reads @, │ reads |, the meter # and -, · reads -, … reads ~, no brand mark (L165); still exactly the width", (t) => {
	useMemorySettings(t, { "shell.appearance.ascii": true });
	assertRows((width, theme) => [renderTopRow(acme(), theme, width, { home: "/home/dev" })], (width) => [padded(" acme_app  ~/acme_app", width)], "ascii top row");
	assertRows((width, theme) => [renderStatusBar(acme({ mode: "yolo" }), theme, width)], {
		180: [spread(" @ feat/add-invoice-export | sandbox on | YOLO", "opus-5-5 - high | ctx ###----- 38% | $1.42 ", 180)],
		100: [spread(" @ feat/add-invoice-~ | sandbox on | YOLO", "opus-5-5 - high | ctx 38% | $1.42 ", 100)],
	}, "ascii status bar");
	for (let width = 0; width <= 180; width += 7) {
		const rows = [renderTopRow(acme({ cwd: "/srv/a-very-long-folder/acme_app" }), plainTheme, width), renderStatusBar(acme({ mode: "yolo", profile: "team", contextPercent: 55, contextWindow: 200_000 }), plainTheme, width)].map(stripAnsi);
		for (const row of rows) {
			assert.match(row, /^[\x20-\x7e]*$/, `${width}: ${row}`);
			assert.equal(visibleWidth(row), width);
		}
	}
	assert.equal(stripAnsi(renderStatusBar(acme(), plainTheme, 100, { ascii: false })).slice(0, 3), " ⎇ ", "the flag can be given");
});
