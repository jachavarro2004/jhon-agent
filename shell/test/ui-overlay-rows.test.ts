/**
 * T174-S13: the ask view (design/shell-tui 08) and the overlay frame
 * (05 Customize, 12 Grants; 06, 10, 11 and 13 full screen) as exact rows at
 * the artboards' sizes. The ask view draws in the dock beside the right pane,
 * so at the card widths (133 at 180 columns, 100 in a narrow terminal); the
 * cards at their artboard widths (Customize 104, Grants 96); the full-screen
 * views at 180×50. pi cannot dim what lies behind an overlay (L1469): the
 * card is drawn over the screen as it is.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { OTHER } from "../lib/ask.ts";
import { AskView, type AskItem } from "../lib/ui/ask-view.ts";
import { renderFullscreenView } from "../lib/ui/fullscreen-view.ts";
import { CARD_OVERLAY, GRANTS_OVERLAY, closeWithRepaint, renderOverlayCard } from "../lib/ui/overlay.ts";
import { SettingsView, type SettingRow } from "../lib/ui/settings-view.ts";
import { assertCardRows, padded, plainTheme, rowsAt, spread } from "./rows.ts";

const ITEMS: AskItem[] = [
	{ value: "422 with a problem body — Spec S4 as written: the client sees why, and the button can say it.", label: "422 with a problem body", description: "Spec S4 as written: the client sees why, and the button can say it." },
	{ value: "200 with a header-only CSV — Today's behavior; the Prism review flagged it as a finding.", label: "200 with a header-only CSV", description: "Today's behavior; the Prism review flagged it as a finding." },
	{ value: "204 No Content — No body at all; the download button must handle it.", label: "204 No Content", description: "No body at all; the download button must handle it." },
	{ value: OTHER, label: OTHER },
];

function ask(o: { ascii?: boolean; multi?: boolean; items?: AskItem[]; done?: (v: string | undefined) => void; requestRender?: () => void } = {}) {
	return new AskView({
		step: 1, total: 2, title: "Empty range", question: "How should the export answer when the date range has no invoices?",
		items: o.items ?? ITEMS, multi: o.multi ?? false, theme: plainTheme, done: o.done ?? (() => {}), ascii: o.ascii ?? false,
		...(o.requestRender ? { requestRender: o.requestRender } : {}),
	});
}

test("the ask view is artboard 08: between two accent rules, one cell in, the focus ❯, descriptions four cells under their option, the hint", () => {
	assertCardRows((width) => ask().render(width), (width) => [
		"─".repeat(width),
		padded(" [1/2]  ▸ Empty range", width),
		padded("", width),
		padded(" How should the export answer when the date range has no invoices?", width),
		padded(" ❯ 422 with a problem body", width),
		padded("     Spec S4 as written: the client sees why, and the button can say it.", width),
		padded("   200 with a header-only CSV", width),
		padded("     Today's behavior; the Prism review flagged it as a finding.", width),
		padded("   204 No Content", width),
		padded("     No body at all; the download button must handle it.", width),
		padded(`   ${OTHER}`, width),
		padded("", width),
		padded(" ↑↓ move · enter select · esc cancel", width),
		"─".repeat(width),
	], "ask view");
});

test("the ask view's roles: rules and question in the accent, the step dim, the focused option bold, descriptions and hint dim", () => {
	const tagged = { fg: (role: string, text: string) => `<${role}>${text}</${role}>`, bold: (text: string) => `<b>${text}</b>` };
	const rows = new AskView({ step: 1, total: 2, title: "Empty range", question: "Which?", items: ITEMS, multi: false, theme: tagged, done() {} }).render(133);
	assert.match(rows[0]!, /^<accent>─+<\/accent>$/);
	assert.match(rows[1]!, /^ <dim>\[1\/2\]<\/dim>  <accent><b>▸ Empty range<\/b><\/accent> +$/);
	assert.match(rows[3]!, /^ <accent><b>Which\?<\/b><\/accent> +$/);
	assert.match(rows[4]!, /^ <accent><b>❯ <\/b><\/accent><text><b>422 with a problem body<\/b><\/text> +$/);
	assert.match(rows[5]!, /^ {5}<dim>Spec S4 as written/);
	assert.match(rows[6]!, /^ {3}<text>200 with a header-only CSV<\/text> +$/);
	assert.match(rows[12]!, /^ <dim>↑↓ move · enter select · esc cancel<\/dim> +$/);
});

test("the ask view in ASCII, a multiple choice's hint, and narrow widths: every row exactly the width", () => {
	assert.deepEqual(rowsAt((w) => ask({ ascii: true }).render(w), 100).map((r) => r.trimEnd()).filter(Boolean).slice(0, 4), [
		"-".repeat(100), " [1/2]  > Empty range", " How should the export answer when the date range has no invoices?", " > 422 with a problem body",
	]);
	assert.equal(rowsAt((w) => ask({ ascii: true }).render(w), 100).at(-2)!.trimEnd(), " up/down move - enter select - esc cancel");
	assert.equal(rowsAt((w) => ask({ multi: true }).render(w), 100).at(-2)!.trimEnd(), " ↑↓ move · space toggle · enter select · esc cancel");
	const narrow = rowsAt((w) => ask().render(w), 30);
	assert.deepEqual(narrow.slice(3, 9).map((r) => r.trimEnd()), [
		" How should the export answer", " when the date range has no", " invoices?", " ❯ 422 with a problem body", "     Spec S4 as written: the", "     client sees why, and the",
	]);
	for (const width of [1, 2, 5, 12, 30, 100, 133]) for (const row of ask().render(width)) assert.equal(visibleWidth(row), width, `${width}: ${JSON.stringify(row)}`);
});

test("the ask view's keys and pointer: ↑↓ move, enter picks, space toggles a multiple choice, esc dismisses; a click picks, a move paints the label in the hover role; each key asks for a render", () => {
	const picked: Array<string | undefined> = [];
	let renders = 0;
	const v = ask({ done: (value) => picked.push(value), requestRender: () => renders++ });
	v.handleInput("\x1b[B");
	v.handleInput("\x1b[B");
	v.handleInput("\x1b[B");
	v.handleInput("\x1b[B");
	assert.equal(rowsAt((w) => v.render(w), 100)[10]!.trimEnd(), ` ❯ ${OTHER}`, "the focus stops at the last option");
	v.handleInput("\x1b[A");
	v.handleInput(" ");
	assert.deepEqual(picked, [], "space picks nothing in a single choice");
	v.handleInput("\r");
	assert.deepEqual(picked, [ITEMS[2]!.value]);
	assert.equal(renders, 7);
	const multi: Array<string | undefined> = [];
	const m = ask({ multi: true, done: (value) => multi.push(value) });
	m.handleInput(" ");
	assert.deepEqual(multi, [ITEMS[0]!.value]);
	const gone: Array<string | undefined> = [];
	ask({ done: (value) => gone.push(value) }).handleInput("\x1b");
	assert.deepEqual(gone, [undefined]);
	const hover = { fg: (role: string, text: string) => role === "warning" ? `<hover>${text}</hover>` : text, bold: (t: string) => t };
	const clicked: Array<string | undefined> = [];
	const c = new AskView({ step: 1, total: 1, title: "T", question: "Q", items: ITEMS, multi: false, theme: hover, done: (value) => clicked.push(value) });
	const rows = c.render(100);
	const at = (type: "move" | "press" | "click", y: number) => ({ type, button: type === "move" ? "none" : "left", x: 4, y, width: 100, height: rows.length, shift: false, alt: false, ctrl: false }) as never;
	assert.deepEqual(c.handleMouse(at("move", 7)), { handled: true, render: true }, "a move over a description hovers its option");
	assert.match(c.render(100)[6]!, /<hover>200 with a header-only CSV<\/hover>/);
	assert.deepEqual(c.handleMouse(at("move", 7)), { handled: true, render: false }, "the same option: no render");
	assert.deepEqual(c.handleMouse(at("press", 6)), { handled: true, render: false }, "the press is taken, so pi sends the click");
	assert.equal(c.handleMouse(at("press", 1)), undefined, "a press off the options is not");
	c.handleMouse(at("click", 6));
	assert.deepEqual(clicked, [ITEMS[1]!.value]);
	assert.equal(c.handleMouse({ ...(at("click", 6) as object), width: 90 } as never), undefined, "a click from another width is not");
});

test("the overlay card: a rounded frame, `<title> … esc` one cell in, the body two cells in; ASCII + - |", () => {
	const card = (width: number, ascii = false) => renderOverlayCard({ title: "Grants", body: ["", "Active", "  push"], width, theme: plainTheme, ascii }).map((r) => r);
	for (const width of [104, 96]) {
		assert.deepEqual(card(width), [
			`╭${"─".repeat(width - 2)}╮`,
			`│ ${spread("Grants", "esc", width - 4)} │`,
			`│  ${padded("", width - 6)}  │`,
			`│  ${padded("Active", width - 6)}  │`,
			`│  ${padded("  push", width - 6)}  │`,
			`╰${"─".repeat(width - 2)}╯`,
		], `${width}`);
	}
	assert.deepEqual(card(20, true), ["+------------------+", "| Grants       esc |", "|                  |", "|  Active          |", "|    push          |", "+------------------+"]);
	const long = renderOverlayCard({ title: "T", body: ["x".repeat(200)], width: 30, theme: plainTheme });
	for (const row of long) assert.equal(visibleWidth(row), 30);
	assert.match(long[2]!, /^│  x+…  │$/);
});

test("the cards' sizes are the artboards': Customize 104 columns, Grants 96, both at most 85% of the rows; a narrower terminal takes the whole width", () => {
	assert.deepEqual(CARD_OVERLAY, { overlay: true, overlayOptions: { anchor: "center", width: 104, maxHeight: "85%" } });
	assert.deepEqual(GRANTS_OVERLAY, { overlay: true, overlayOptions: { anchor: "center", width: 96, maxHeight: "85%" } });
});

test("closing an overlay repaints every cell: done first, then a forced render; a failed paint never breaks the close, a failed done is not hidden", () => {
	const calls: string[] = [];
	closeWithRepaint({ requestRender: (force?: boolean) => calls.push(`render:${force === true}`) }, (r: string | null) => calls.push(`done:${r}`))(null);
	assert.deepEqual(calls, ["done:null", "render:true"]);
	const seen: unknown[] = [];
	assert.doesNotThrow(() => closeWithRepaint({ requestRender: () => { throw new Error("EIO"); } }, (r: unknown) => seen.push(r))("x"));
	assert.deepEqual(seen, ["x"]);
	assert.throws(() => closeWithRepaint({ requestRender: () => {} }, () => { throw new Error("teardown"); })(null), /teardown/);
});

// Artboard 05's rows, the card at 104 columns: its first fifteen rows.
const CUSTOMIZE_05 = [
	"╭──────────────────────────────────────────────────────────────────────────────────────────────────────╮",
	"│ Customize                                                                                        esc │",
	"│  CATEGORIES           │ Appearance · 5/9                                                             │",
	"│                       │                                                                              │",
	"│  ▸ Appearance         │   Theme         ● rose-ink (current)                                         │",
	"│    Behaviour          │   Animations    ● full  ○ off                                                │",
	"│    Keys               │   ASCII glyphs  ○ on  ● off                                                  │",
	"│    Permissions        │   Tool icons    ● geometric  ○ emoji                                         │",
	"│                       │ ▸ Cards         ● tint  ○ outline  ○ band                                    │",
	"│                       │   Surface       ● transparent  ○ tinted                                      │",
	"│                       │   Density       ○ comfortable  ● compact  ○ minimal                          │",
	"│                       │   Status line   ● auto  ○ hidden                                             │",
	"│                       │   Reset         to defaults                                                  │",
	"│                       │                                                                              │",
	"│                       │                                                                              │",
];

test("Customize in the new card is artboard 05's: CATEGORIES two cells in, the divider, `<Category> · n/m` and the values inline", () => {
	const choice = (label: string, value: string, options: string[]): SettingRow => ({ group: "Appearance", key: label, label, value: () => value, options, apply: () => undefined });
	const rows: SettingRow[] = [
		choice("Theme", "rose-ink (current)", ["rose-ink (current)"]),
		choice("Animations", "full", ["full", "off"]),
		choice("ASCII glyphs", "off", ["on", "off"]),
		choice("Tool icons", "geometric", ["geometric", "emoji"]),
		choice("Cards", "tint", ["tint", "outline", "band"]),
		choice("Surface", "transparent", ["transparent", "tinted"]),
		choice("Density", "compact", ["comfortable", "compact", "minimal"]),
		choice("Status line", "auto", ["auto", "hidden"]),
		{ group: "Appearance", key: "reset", label: "Reset", value: () => "to defaults", action: true, apply: () => undefined },
		...["Behaviour", "Keys", "Permissions"].map((group) => ({ group, key: group, label: group, value: () => "x" })),
	];
	const view = new SettingsView({ rows, theme: plainTheme, group: "appearance", height: () => 40, onClose() {}, requestRender() {} });
	for (let i = 0; i < 4; i++) view.handleInput("\x1b[B");
	assert.deepEqual(rowsAt((w) => view.render(w), 104).slice(0, CUSTOMIZE_05.length), CUSTOMIZE_05);
});

test("a full-screen view is artboard 10's frame: the tab and its heavy run, the title and its sentence two cells apart, the keys on the fourth row from the bottom, key muted and action dim", () => {
	const tagged = { fg: (role: string, text: string) => role === "muted" || role === "dim" ? `<${role}>${text}</${role}>` : text };
	const rows = renderFullscreenView({ theme: plainTheme, width: 180, rows: 50, tab: "LDD", title: "◆ add-invoice-export", sentence: "the change this session works on", body: ["standard route · strict TDD"], hints: ["t tests", "o tasks.md", "r refresh", "esc close"] });
	assert.equal(rows.length, 50);
	assert.equal(rows[0], `╭${"─".repeat(178)}╮`);
	assert.equal(rows[1], `│ ${padded(" LDD", 176)} │`);
	assert.equal(rows[2], `│ ${padded(`━━━━━${"─".repeat(171)}`, 176)} │`);
	assert.equal(rows[3], `│ ${padded(" ◆ add-invoice-export  the change this session works on", 176)} │`);
	assert.equal(rows[5], `│ ${padded(" standard route · strict TDD", 176)} │`);
	assert.equal(rows[46], `│ ${padded(" t tests  o tasks.md  r refresh  esc close", 176)} │`);
	assert.equal(rows[49], `╰${"─".repeat(178)}╯`);
	const painted = renderFullscreenView({ theme: tagged, width: 100, rows: 20, tab: "LDD", title: "T", body: [], hints: ["t tests", "esc close"] });
	assert.match(painted[16]!, /<muted>t<\/muted><dim> tests<\/dim>  <muted>esc<\/muted><dim> close<\/dim>/);
});
