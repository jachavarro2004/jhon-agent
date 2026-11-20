/**
 * The Settings overlay as the Gentle Shell's Customization view (T15.12e,
 * L153 answer 6, L154): G's Commands card, `Settings … esc`, CATEGORIES and
 * `<Category> · n/m`, each setting one line with its values inline, the
 * Preview of the highlighted row and `Click · <keys>`, at most 24 rows; under
 * 60 columns the categories take the card until one is opened. G's keys:
 * ↑/↓ (j/k) move, → Tab Enter or Space open a category, ←/→ change a
 * setting's value and apply it, Tab returns, Enter edits a free value.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-settings-view.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { customizeRows as settingsRows } from "../lib/ui/customize/registry.ts";
import { SETTINGS_TWO_PANES, SettingsView, type SettingRow } from "../lib/ui/settings-view.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { memorySettings } from "./fake-jhon-settings.ts";

const plain = { fg: (_c: string, t: string) => t, bold: (t: string) => t };
const tagged = { fg: (c: string, t: string) => `<${c}>${t}</${c}>`, bg: (c: string, t: string) => `[${c}]${t}[/${c}]`, bold: (t: string) => `<b>${t}</b>` };
const ENTER = "\r";
const ESC = "\x1b";
const DOWN = "\x1b[B";
const UP = "\x1b[A";
const BACKSPACE = "\x7f";
const TAB = "\t";
const RIGHT = "\x1b[C";
const LEFT = "\x1b[D";

function rows(applied: Array<[string, string]>): SettingRow[] {
	const values: Record<string, string> = { animations: "full", icons: "geometric", dblesc: "off", compact: "60", panel: "alt+j" };
	const row = (group: string, key: string, label: string, options?: string[], note?: string, preview?: SettingRow["preview"]): SettingRow => ({
		group, key, label, note, options, preview,
		value: () => values[key]!,
		apply: (next) => {
			if (next === "bad" || (key === "icons" && next === "emoji" && values["dblesc"] === "on")) return "jhon settings: refused";
			applied.push([key, next]);
			values[key] = next;
			return undefined;
		},
	});
	return [
		row("Appearance", "animations", "Animations", ["full", "off"], "The prompt's petal: full, or off", (v, ascii) => ({ caption: "static animation sample", lines: [ascii ? "* Working..." : v === "full" ? "◆ ◈ ◇ ◈ Working…" : "◆ Working…"] })),
		row("Appearance", "icons", "Tool icons", ["geometric", "emoji"]),
		row("Behaviour", "dblesc", "Double Esc", ["on", "off"]),
		row("Behaviour", "compact", "Compact at (%)", undefined, undefined, (v) => ({ caption: "what happens", lines: [`At ${v}% the session compacts.`] })),
		row("Keys", "panel", "Jhon panel", undefined, "Applies at the next start; off turns it off"),
	];
}

function view(options: { group?: string; height?: number; ascii?: boolean } = {}) {
	const applied: Array<[string, string]> = [];
	let closed = 0;
	const settings = new SettingsView({ rows: rows(applied), theme: plain, height: () => options.height ?? 30, group: options.group, ascii: () => options.ascii ?? false, onClose: () => { closed++; }, requestRender() {} });
	return { settings, applied, closed: () => closed };
}

const type = (v: SettingsView, text: string) => { for (const ch of text) v.handleInput(ch); };
const plainLines = (v: SettingsView, width = 80) => v.render(width).map(stripAnsi);
/** The footer: the last row inside the card. */
const footer = (v: SettingsView, width = 80) => plainLines(v, width).at(-2)!;

test("Settings is G's Customization card: `Settings … esc`, CATEGORIES │ `<Category> · n/m`, the values inline, the Preview and `Click · <keys>`, 24 rows at most", () => {
	const { settings } = view();
	for (const width of [120, 80]) {
		const lines = plainLines(settings, width);
		const text = lines.join("\n");
		assert.equal(lines.length, 24, "G caps the card at 24 rows");
		assert.match(lines[0]!, /^╭─+╮$/);
		assert.match(lines[1]!, /^│ Customize +esc │$/);
		assert.match(lines[2]!, /^│ CATEGORIES +│ Appearance · 1\/2 +│$/);
		assert.match(lines[3]!, /^│ +│$/);
		assert.match(lines[4]!, /^│ ▸ Appearance +│ {3}Animations  ● full  ○ off +│$/, `${width}: the selected category beside its first setting`);
		assert.match(lines[5]!, /^│ {3}Behaviour +│ {3}Tool icons  ● geometric  ○ emoji +│$/);
		assert.match(lines[6]!, /^│ {3}Keys +│ +│$/);
		assert.doesNotMatch(text, /Double Esc|Jhon panel/, "only the selected category's settings");
		assert.match(text, /│ Preview · full · static animation sample +│/);
		assert.match(text, /│ ◆ ◈ ◇ ◈ Working… +│/);
		assert.match(footer(settings, width), /^│ Click · ←\/→ or Tab panes · ↑\/↓ moves · Enter applies · Esc closes +│$/);
		assert.match(lines.at(-3)!, /^│ +│$/);
		assert.match(lines.at(-1)!, /^╰─+╯$/);
		for (const line of settings.render(width)) assert.equal(visibleWidth(line), width, `${width}: ${stripAnsi(line)}`);
	}
});

test("ASCII: the + - | card, radios * o, the cursor >, the divider |", () => {
	const { settings } = view({ ascii: true });
	settings.handleInput(TAB);
	const lines = plainLines(settings);
	assert.match(lines[0]!, /^\+-+\+$/);
	assert.match(lines[1]!, /^\| Customize +esc \|$/);
	assert.match(lines.join("\n"), /\| > Animations  \* full  o off/);
	for (const line of lines) assert.match(line, /^[\x20-\x7e]*$/, line);
});

test("the colour roles, G's: the title row and CATEGORIES accent, the selected row on selectedBg with ▸ accent, ● accent, ○ muted", () => {
	const settings = new SettingsView({ rows: rows([]), theme: tagged, height: () => 30, group: "appearance", onClose() {}, requestRender() {} });
	const text = settings.render(400).join("\n");
	assert.match(text, /<accent>Customize +/);
	assert.match(text, /<accent>CATEGORIES +<\/accent>/);
	assert.match(text, /<accent>Appearance · 1\/2 +/);
	assert.match(text, /\[selectedBg\]<accent>▸ <\/accent><text>Animations  <\/text><accent>● full<\/accent>  <muted>○ off<\/muted>/);
	assert.match(text, /<muted>○ emoji<\/muted>/);
	assert.match(text, /<border>╭─/);
});

test("G's keys: ↑/↓ (j/k) wrap through the categories; →, Tab, Enter or Space open one; ↑/↓ wrap through its settings; Tab or ← on a free value come back", () => {
	const { settings } = view();
	settings.handleInput(DOWN);
	let text = plainLines(settings).join("\n");
	assert.match(text, /│ ▸ Behaviour +│ {3}Compact at \(%\) +60/);
	assert.match(text, /│ {3}Appearance +│ {3}Double Esc +○ on  ● off/);
	assert.match(text, /Behaviour · 1\/2/);
	settings.handleInput(UP); settings.handleInput(UP);
	assert.match(plainLines(settings).join("\n"), /▸ Keys/, "up from the first wraps to the last, as G's");
	settings.handleInput("j");
	assert.match(plainLines(settings).join("\n"), /▸ Appearance/);
	for (const into of [RIGHT, TAB, ENTER, " "]) {
		settings.handleInput(into);
		assert.match(footer(settings), /^│ Click · ↑\/↓ setting · ←\/→ changes it · Tab categories · Esc closes +│$/, "a setting focused");
		settings.handleInput(TAB);
		assert.match(footer(settings), /Tab panes/);
	}
	settings.handleInput(RIGHT);
	settings.handleInput("k");
	assert.match(plainLines(settings).join("\n"), /Appearance · 2\/2/, "k wraps to the last setting");
	settings.handleInput("k"); settings.handleInput(TAB); settings.handleInput("j"); settings.handleInput(RIGHT); settings.handleInput("j");
	assert.match(plainLines(settings).join("\n"), /▸ Compact at \(%\) +60/);
	settings.handleInput(LEFT);
	assert.match(footer(settings), /Tab panes/, "← on a free value goes back to the categories");
});

test("←/→ on a setting step its value and apply it at once; ← past the first value goes back to the categories; a refusal notifies, as G's", () => {
	const { settings, applied } = view({ group: "appearance" });
	settings.handleInput(RIGHT);
	assert.deepEqual(applied, [["animations", "off"]]);
	assert.match(plainLines(settings).join("\n"), /▸ Animations  ○ full  ● off/);
	settings.handleInput(RIGHT);
	assert.deepEqual(applied, [["animations", "off"]], "nothing past the last value");
	settings.handleInput(LEFT);
	assert.deepEqual(applied, [["animations", "off"], ["animations", "full"]]);
	settings.handleInput(LEFT);
	assert.match(footer(settings), /Tab panes/, "← past the first value: the categories");
	assert.equal(applied.length, 2);
	const errors: string[] = [];
	const refused = new SettingsView({ rows: [{ group: "A", key: "k", label: "Mode", options: ["ok", "bad"], value: () => "ok", apply: (v) => v === "bad" ? "jhon settings: refused" : undefined }], theme: plain, height: () => 24, group: "a", onError: (message) => errors.push(message), onClose() {}, requestRender() {} });
	const before = plainLines(refused).join("\n");
	refused.handleInput(RIGHT);
	assert.deepEqual(errors, ["jhon settings: refused"], "a refused value notifies, as G's onError");
	assert.equal(plainLines(refused).join("\n"), before, "the Preview headline is unchanged");
});

test("Enter on a free value edits it in place, in the Preview's rows: typing, backspace, Enter saves, Esc cancels without closing; Esc closes", () => {
	const { settings, applied, closed } = view({ group: "behaviour" });
	settings.handleInput(DOWN);
	settings.handleInput(ENTER);
	assert.match(plainLines(settings).join("\n"), /│ Compact at \(%\): 60▏ +│\n│ Enter applies · Esc cancels +│/);
	settings.handleInput(BACKSPACE);
	type(settings, "5");
	settings.handleInput(ENTER);
	assert.deepEqual(applied, [["compact", "65"]]);
	settings.handleInput(ENTER);
	settings.handleInput(ESC);
	assert.equal(closed(), 0, "esc cancels the edit");
	assert.doesNotMatch(plainLines(settings).join("\n"), /▏/);
	settings.handleInput(ESC);
	assert.equal(closed(), 1);
});

test("the Preview's headline is a focused setting's `<label> — <note>`, else `Preview · <value> · <caption>`", () => {
	const { settings } = view({ group: "appearance" });
	assert.match(plainLines(settings).join("\n"), /│ Animations — The prompt's petal: full, or off +│\n│ ◆ ◈ ◇ ◈ Working… +│/);
	const keys = view({ group: "keys" });
	assert.match(plainLines(keys.settings).join("\n"), /│ Preview · alt\+j +│\n│ Applies at the next start; off turns it off +│/);
});

test(`under ${SETTINGS_TWO_PANES} columns the categories take the card until one is opened, as G's`, () => {
	const { settings } = view();
	const lines = plainLines(settings, 50);
	assert.match(lines[2]!, /^│ Categories · pick one → +│$/);
	assert.match(lines[4]!, /^│ ▸ Appearance +│$/);
	assert.match(footer(settings, 50), /^│ Click · ↑\/↓ category · → opens it · Esc closes +│$/);
	settings.handleInput(RIGHT);
	const open = plainLines(settings, 50);
	assert.match(open[2]!, /^│ Appearance · 1\/2 +│$/);
	assert.match(open[4]!, /^│ ▸ Animations  ● full  ○ off +│$/);
	for (const line of settings.render(50)) assert.equal(visibleWidth(line), 50);
});

test("a setting whose values do not fit shows the current one between arrows with its place", () => {
	const settings = new SettingsView({ rows: [{ group: "A", key: "k", label: "Sound", options: ["none", "builtin:success", "builtin:error", "builtin:attention"], value: () => "builtin:error" }], theme: plain, height: () => 24, group: "a", onClose() {}, requestRender() {} });
	assert.match(plainLines(settings, 64).join("\n"), /▸ Sound  ‹ builtin:error › 3\/4/);
});

test("G's pointer: a click on a category opens it, a click on a value applies it, a move paints the hovered target", () => {
	const { settings, applied } = view();
	const lines = settings.render(80);
	const at = (text: string) => { const y = lines.findIndex((line) => stripAnsi(line).includes(text)); return { y, x: stripAnsi(lines[y]!).indexOf(text) }; };
	const event = (type: "click" | "move", x: number, y: number) => ({ type, button: type === "move" ? "none" : "left", x, y, width: 80, height: lines.length, shift: false, alt: false, ctrl: false }) as never;
	const off = at("○ off");
	settings.handleMouse(event("click", off.x + 1, off.y));
	assert.deepEqual(applied, [["animations", "off"]]);
	const fresh = settings.render(80);
	const behaviour = fresh.findIndex((line) => stripAnsi(line).includes("Behaviour"));
	const sgr = { fg: (c: string, t: string) => c === "warning" ? `\x1b[33m${t}\x1b[39m` : t };
	const hoverTheme = new SettingsView({ rows: rows([]), theme: sgr, height: () => 30, onClose() {}, requestRender() {} });
	const tl = hoverTheme.render(80);
	const by = tl.findIndex((line) => stripAnsi(line).includes("Behaviour"));
	assert.deepEqual(hoverTheme.handleMouse({ type: "move", button: "none", x: 5, y: by, width: 80, height: tl.length, shift: false, alt: false, ctrl: false } as never), { handled: true, render: true });
	assert.match(hoverTheme.render(80)[by]!, /\x1b\[33mBehaviour\x1b\[39m/, "the hover role");
	settings.handleMouse(event("click", 5, behaviour));
	assert.match(plainLines(settings).join("\n"), /Behaviour · 1\/2/);
});

test("a short overlay keeps the cursor row in view", () => {
	const many: SettingRow[] = Array.from({ length: 12 }, (_, i) => ({ group: "Keys", key: `k${i}`, label: `Key ${i}`, value: () => "off" }));
	const settings = new SettingsView({ rows: many, theme: plain, height: () => 12, group: "keys", onClose() {}, requestRender() {} });
	for (let i = 0; i < 10; i++) settings.handleInput(DOWN);
	const lines = plainLines(settings);
	assert.equal(lines.length, 12);
	assert.match(lines.join("\n"), /▸ Key 10/);
	assert.match(lines.join("\n"), /Keys · 11\/12/);
});

// The real rows at the owner's sizes: 186x40, 120x36, 80x24 and a 60-column sheet.
test("the real Settings at 186, 120, 80 and 60 columns: previews for animations, icons, ascii, status, keys and numbers", () => {
	const noop = () => {};
	const real = settingsRows({ settings: memorySettings(), animations: noop, doubleEsc: noop, status: noop, redraw: noop });
	for (const [cols, termRows] of [[186, 40], [120, 36], [80, 24], [60, 24]] as const) {
		// G's card: 70% wide, at least 60 columns, 85% of the rows less the border, at most 24.
		const width = Math.min(cols, Math.max(60, Math.floor(cols * 0.7)));
		const height = Math.min(24, Math.floor(termRows * 0.85) - 2);
		const open = (group: string, down = 0) => {
			const v = new SettingsView({ rows: real, theme: plain, height: () => height, group, onClose() {}, requestRender() {} });
			for (let i = 0; i < down; i++) v.handleInput(DOWN);
			const lines = v.render(width).map(stripAnsi);
			assert.equal(lines.length, height, `${cols}x${termRows}`);
			for (const line of lines) assert.equal(visibleWidth(line), width, `${cols}: ${line}`);
			return lines.join("\n");
		};
		const at = `${cols}x${termRows}`;
		let text = open("appearance");
		assert.match(text, /Customize +esc/, at);
		assert.match(text, /CATEGORIES +│ Appearance · 1\/8/, at);
		assert.match(text, /▸ Animations +● full  ○ off/, at);
		assert.match(text, /力 {2}Working… {3}the petal glows/, at);
		text = open("appearance", 1);
		assert.match(text, /▸ ASCII glyphs +○ on  ● off/, at);

		assert.match(text, /╭─╮ ▸ ▾ ▰▱ … ·/, at);
		text = open("appearance", 2);
		assert.match(text, /▸ Tool icons +● geometric  ○ emoji/, at);

		assert.match(text, /▤ read {2}⌕ search {2}◍ web {2}\$ shell {2}✎ edit/, at);
		text = open("appearance", 3);
		assert.match(text, /▸ Cards +● float  ○ neon/, at);

		assert.match(text, / ▎ ⧗ Background job · npm test/, at);
		text = open("appearance", 4);
		assert.match(text, /▸ Surface +(● transparent|‹ transparent › 1\/2)/, at);

		text = open("appearance", 5);
		assert.match(text, /▸ Density +(● comfortable  ○ compact  ○ minimal|‹ comfortable › 1\/3)/, at);
		assert.match(text, /the title row/, at);
		text = open("appearance", 6);
		assert.match(text, /▸ Status line +● auto  ○ hidden/, at);
		text = open("behaviour");
		assert.match(text, /▸ Compact at \(%\) +60/, at);
		assert.match(text, /Preview · 60 · what happens/, at);
		assert.match(text, /At 60% of the context window the session compacts;/, at);
		text = open("keys");
		assert.match(text, /Keys · 1\/2/, at);
		assert.match(text, /Preview · alt\+j · what the key opens/, at);
		assert.match(text, /alt\+j opens the LDD status\./, at);
		text = open("appearance", 1);
		assert.match(text, /Click · ↑\/↓ setting /, at, "a choice row's footer");
	}
});

// T15.6, T15.7 (L96): Settings lists every shell setting the engine has, no
// more and no fewer; the rest are `jhon setup`'s (test/setup pins those).
// The engine's list is the tree's own binary, build/jhon, as the guard tests
// run it (extension.test.ts): the pre-commit hook and `jhon test` build it
// before the suite; JHON_TEST_BIN names another.
test("the Settings rows are exactly the shell.* keys `jhon settings list --json` gives", async (t) => {
	const { spawnSync } = await import("node:child_process");
	const { mkdtempSync, rmSync } = await import("node:fs");
	const { tmpdir } = await import("node:os");
	const { dirname, join } = await import("node:path");
	const { fileURLToPath } = await import("node:url");
	const { customizeRows: settingsRows } = await import("../lib/ui/customize/registry.ts");
	const { memorySettings } = await import("./fake-jhon-settings.ts");
	const bin = process.env.JHON_TEST_BIN || join(dirname(fileURLToPath(import.meta.url)), "..", "..", "build", "jhon");
	const home = mkdtempSync(join(tmpdir(), "jhon-settings-rows-"));
	t.after(() => rmSync(home, { recursive: true, force: true }));
	const run = spawnSync(bin, ["settings", "list", "--json"], { encoding: "utf8", env: { PATH: process.env.PATH, HOME: home, JHON_SETTINGS: join(home, "settings.toml") } });
	assert.equal(run.status, 0, `${bin}: ${run.error?.message ?? run.stderr}`);
	const engine = new Set((JSON.parse(run.stdout).settings as Record<string, string>[]).map((entry) => entry["key"]!).filter((k) => k.startsWith("shell.")));
	const noop = () => {};
	const rowsShown = settingsRows({ settings: memorySettings(), animations: noop, doubleEsc: noop, status: noop, redraw: noop });
	assert.deepEqual([...new Set(rowsShown.map((row) => row.group))], ["Appearance", "Behaviour", "Keys"]);
	// Action rows (Reset appearance) save no key of their own.
	const settingRows = rowsShown.filter((row) => !row.action);
	assert.equal(settingRows.length, engine.size, "one row per engine shell.* key");
	const keyLabels = rowsShown.filter((row) => row.group === "Keys").map((row) => row.label);
	// T169 (L1438): the panel sheet's and the palette's rows went with them; T174 (L1462): Todos, Changes and Usage too.
	assert.deepEqual(keyLabels, ["LDD status", "Agents"]);
	const shown = new Set(settingRows.map((row) => row["key"]));
	assert.deepEqual([...shown].filter((k) => !engine.has(k)).sort(), [], "rows the engine does not know");
	assert.deepEqual([...engine].filter((k) => !shown.has(k)).sort(), [], "engine keys Settings does not show");
});

// T15.13: the Status line row's note names what it hides now, the status bar
// and the rail's Status sections (STATUS is the rail's LDD section).
test("the Status line row's note names the status bar and the rail's Status sections, not the old STATUS card", () => {
	const noop = () => {};
	const real = settingsRows({ settings: memorySettings(), animations: noop, doubleEsc: noop, status: noop, redraw: noop });
	const status = real.find((row) => row.key === "shell.appearance.status")!;
	assert.equal(status.note, "The status bar and the rail's Status sections, where they fit; or none.");
});
