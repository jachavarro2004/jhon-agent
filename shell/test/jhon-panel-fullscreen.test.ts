/**
 * L174: the LDD detail (alt+j) and /jhon:profile are work views, drawn near
 * full screen as the Gentle Shell draws its profiles view (gentle-ai.ts
 * RoutingTablePanel.renderFrame, showRoutingTablePanel): the whole terminal,
 * a rounded frame in the border role with `│ ` margins, the view's tab row and
 * its rule (the active tab's `━` in the accent), the title row, the body, then
 * G's dim key hints joined by two spaces and two feedback rows, blank rows
 * filling the rest. /jhon:grants stays the small card, as Customize.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { JhonPanel, type ProfilesData } from "../lib/ui/jhon-panel.ts";
import { FULLSCREEN_OVERLAY, renderFullscreenView } from "../lib/ui/fullscreen-view.ts";

const plain = { fg: (_r: string, t: string) => t, bold: (t: string) => t };
const tagged = { fg: (r: string, t: string) => `<${r}>${t}</${r}>`, bold: (t: string) => t };
const flush = () => new Promise((r) => setImmediate(r));
const profiles = (): ProfilesData => ({ active: "default", file: "~/.config/jhon/profiles.json", profiles: [
	{ name: "default", delegation: "inline", agents: [{ agent: "worker", runtime: "pi", model: "claude-bridge/claude-opus-5-5@medium", source: "agents.yaml tiers.x.strong" }] },
] });
const deps = (view: "change" | "profiles" | "grants", loadChange: () => Promise<unknown>, rows = 30) => ({
	theme: plain, runtime: "pi", view, load: async () => profiles(), use: async () => undefined, loadChange,
	height: () => Math.floor(rows * 0.85) - 2, rows: () => rows,
	loadGrants: async () => ({ root: "", classes: [], grants: [] }), grant: async () => {}, revoke: async () => {},
	loadTests: async () => ({}), startTests: () => ({ id: "j", startedAt: 0 }), stopTests: () => {},
	onClose() {}, requestRender() {},
});
const status = { change: "rail-redesign", route: { level: "standard", tier: "medium", reasons: [] }, notes: Array.from({ length: 60 }, (_, i) => `note ${i + 1}`) };

test("the full-screen frame (design 10): the whole terminal, ╭─╮ in border, the tab and its ━ run, the title, the body one cell in, the keys and two feedback rows at the bottom", () => {
	for (const width of [160, 100, 60]) {
		const options = { width, rows: 20, tab: "LDD", title: "LDD · rail-redesign", sentence: "⎇ wt", body: ["one", "two"], hints: ["t tests", "esc close"], feedback: ["saved"] };
		const lines = renderFullscreenView({ ...options, theme: tagged });
		const plainLines = renderFullscreenView({ ...options, theme: plain });
		assert.equal(plainLines.length, 20, "the whole terminal's rows");
		for (const line of plainLines) assert.equal(visibleWidth(line), width, `${width}: ${line}`);
		assert.equal(plainLines[0], `╭${"─".repeat(width - 2)}╮`);
		assert.equal(plainLines.at(-1), `╰${"─".repeat(width - 2)}╯`);
		assert.match(plainLines[1]!, /^│ {2}LDD +│$/, "the view's tab one cell in");
		assert.match(plainLines[2]!, /^│ ━━━━━─+ │$/, "the rule, the tab's ━ under it and a cell past each side");
		assert.match(plainLines[3]!, /^│ {2}LDD · rail-redesign {2}⎇ wt +│$/);
		assert.deepEqual(plainLines.slice(4, 7).map((line) => line.slice(2, -2).trimEnd()), ["", " one", " two"]);
		assert.deepEqual(plainLines.slice(-5, -1).map((line) => line.slice(2, -2).trimEnd()), ["", " t tests  esc close", "saved", ""], "the keys and feedback at the bottom");
		assert.ok(plainLines.slice(7, -5).every((line) => /^│ +│$/.test(line)), "blank rows between the body and the keys");
		if (width < 160) continue; // the tags count as columns: the roles are read where they fit
		assert.match(lines[0]!, /^<border>╭/);
		assert.match(lines[2]!, /<accent>━━━━━<\/accent>/);
		assert.match(lines[3]!, /<accent>LDD · rail-redesign<\/accent> {2}<muted>⎇ wt<\/muted>/);
		assert.match(lines.at(-4)!, /<muted>t<\/muted><dim> tests<\/dim>  <muted>esc<\/muted><dim> close<\/dim>/);
	}
	assert.deepEqual(FULLSCREEN_OVERLAY, { overlay: true, overlayOptions: { anchor: "center", width: "100%", maxHeight: "100%", margin: 0 } }, "every row and column");
	const ascii = renderFullscreenView({ theme: plain, width: 40, rows: 10, tab: "LDD", title: "LDD · x", body: [], hints: ["esc close"], ascii: true });
	assert.ok(ascii.every((line) => /^[\x20-\x7e]*$/.test(line)), ascii.join("\n"));
});

test("the hints keep `esc close` when they do not fit; the rest drop from the end", () => {
	const lines = renderFullscreenView({ theme: plain, width: 30, rows: 12, tab: "LDD", title: "LDD", body: [], hints: ["↑/↓ scroll", "t tests", "d diff", "o tasks.md", "r refresh", "esc close"] });
	const hints = lines.at(-4)!.slice(2, -2).trimEnd();
	assert.match(hints, /esc close$/);
	assert.ok(visibleWidth(hints) <= 26);
});

test("alt+j's LDD detail opens near full screen as G's profiles view; a long body scrolls inside it", async () => {
	const panel = new JhonPanel(deps("change", async () => status, 30));
	panel.open();
	await flush();
	for (const width of [160, 100]) {
		const lines = panel.render(width);
		assert.equal(lines.length, 30, `${width}: the terminal's rows`);
		for (const line of lines) assert.equal(visibleWidth(line), width);
		assert.match(lines[1]!, /^│ {2}LDD +│$/);
		assert.match(lines[3]!, /^│ {2}LDD · rail-redesign +│$/);
		const text = lines.join("\n");
		assert.match(text, /│ {2}↑\/↓ scroll {2}t tests {2}r refresh {2}esc close +│/);
		assert.match(text, /note 1\b/);
	}
	panel.handleInput("j");
	assert.doesNotMatch(panel.render(100).join("\n"), /│ {2}standard route/, "it scrolled");
});

test("/jhon:profile opens near full screen as G's profiles view; /jhon:grants stays the small card", async () => {
	const panel = new JhonPanel(deps("profiles", async () => ({}), 30));
	panel.open();
	await flush();
	const lines = panel.render(120);
	assert.equal(lines.length, 30);
	for (const line of lines) assert.equal(visibleWidth(line), 120, line);
	assert.match(lines[1]!, /^│ {2}default ⊙★ {2}\+ New +│$/, "the profiles as tabs at the top (L695)");
	assert.match(lines[2]!, /^│ ━+─+ │$/);
	assert.match(lines.join("\n"), /▸ Orchestrator/);
	assert.match(lines.join("\n"), /esc close +│/);
	const mode = new JhonPanel(deps("grants", async () => ({}), 30));
	mode.open();
	await flush();
	const card = mode.render(80);
	assert.ok(card.length < 30, "the grants are not full screen");
	assert.match(card[1]!, /^│ Grants +esc │$/, "the card's title row, as Customize's");
});
