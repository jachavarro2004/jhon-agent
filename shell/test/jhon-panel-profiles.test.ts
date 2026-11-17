/**
 * The profile panel as the owner's mockup (T117d, L694, L695, L720): profiles
 * as tabs with their delegation icon and the default star, the legend, the
 * Orchestrator row with its mode word, the agents' models and efforts, a
 * draft edited with w / enter / e and saved with s, u and d, delete on x.
 *
 *   jhon test
 */
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { JhonPanel, modelsOffered, registryModels } from "../lib/ui/jhon-panel.ts";
import { ProfilesCard, type ProfilesData, type ProfilesDeps } from "../lib/ui/jhon-panel-profiles.ts";

const plain = { fg: (_r: string, t: string) => t, bold: (t: string) => t };
const flush = async () => { for (let i = 0; i < 4; i++) await new Promise((r) => setImmediate(r)); };
const row = (agent: string, model: string, source = "profile agent x") => ({ agent, runtime: "pi", model, source });
const yaml = (agent: string, model: string) => ({ agent, runtime: "pi", model, source: "agents.yaml tiers.x.strong" });

function fixture(): ProfilesData {
	return {
		active: "current", file: "~/.config/jhon/profiles.json",
		efforts: { pi: ["off", "minimal", "low", "medium", "high", "xhigh", "max"] },
		profiles: [
			{ name: "current", provider: "claude-bridge", delegation: "inline", agents: [
				row("jhon-orchestrator", "claude-bridge/claude-opus-5-5@high"),
				row("review-refuter", "claude-bridge/claude-opus-5-5@low"),
				row("gentle-ai-worker", "claude-bridge/claude-opus-5-5@low"),
				{ agent: "gentle-ai-worker", runtime: "claude-code", model: "claude-opus-5-5@medium", source: "agents.yaml" },
			] },
			{ name: "default", provider: "claude-bridge", delegation: "inline", agents: [
				yaml("jhon-orchestrator", "claude-bridge/claude-opus-5-5@medium"), yaml("review-refuter", "claude-bridge/claude-opus-5-5@medium"),
			] },
			{ name: "fast", provider: "deepseek", delegation: "writer", agents: [
				row("jhon-orchestrator", "deepseek/deepseek-v4@high"), row("review-refuter", "glm/glm-5@low"),
			] },
		],
	};
}

function card(over: Partial<ProfilesDeps> = {}) {
	const calls: string[] = [];
	let data = fixture();
	const deps: ProfilesDeps = {
		runtime: "pi",
		load: async () => data,
		useNow: async (n) => { calls.push(`useNow ${n}`); return undefined; },
		makeDefault: async (n) => { calls.push(`makeDefault ${n}`); data = { ...data, active: n }; return undefined; },
		create: async (n, from) => { calls.push(`create ${n} ${from}`); data = { ...data, profiles: [...data.profiles, { ...data.profiles.find((p) => p.name === from)!, name: n }] }; },
		set: async (p, a, rt, v) => { calls.push(`set ${p} ${a} ${rt} ${v}`); },
		mode: async (p, m) => { calls.push(`mode ${p} ${m}`); },
		remove: async (n) => { calls.push(`remove ${n}`); data = { ...data, profiles: data.profiles.filter((p) => p.name !== n) }; },
		models: () => ["claude-bridge/claude-opus-5-5", "claude-bridge/claude-sonnet-5-5", "deepseek/deepseek-v4"],
		requestRender() {},
		...over,
	};
	const c = new ProfilesCard(deps);
	const text = (width = 96, ascii = false) => c.lines(plain, width, ascii).join("\n");
	const keys = async (...ks: string[]) => { for (const k of ks) { c.handleInput(k); await flush(); } };
	return { c, calls, text, keys };
}

const LEFT = "\x1b[D", RIGHT = "\x1b[C", DOWN = "\x1b[B", UP = "\x1b[A", ENTER = "\r";

test("profiles are tabs: name, mode icon, ★ on the default, + New last; the selected one underlined", async () => {
	const { c, text } = card();
	c.reload();
	await flush();
	const lines = text().split("\n");
	assert.match(lines[0]!, /^ current ⊙★ {2,}default ⊙ {2,}fast ⇄ {2,}\+ New $/);
	assert.match(lines[1]!, /^━{12}─+$/, "the selected tab underlined, a rule under the rest");
	assert.match(lines[2]!, /^current {2,}Used in this session\. Default for new sessions\. {2,}⊙ inline · ⇄ writer · w switches$/);
	assert.ok(c.lines(plain, 96).every((l) => visibleWidth(l) <= 96));
});

test("T120: the profile this session uses as its own carries ● on its tab while it is not the default, and the legend names the mark; the keys are as they were", async () => {
	let own: string | undefined = "fast";
	const { c, text, keys } = card({ sessionProfile: () => own });
	c.reload();
	await flush();
	const lines = text(120).split("\n");
	assert.match(lines[0]!, /^ current ⊙★ {2,}default ⊙ {2,}fast ⇄● {2,}\+ New $/);
	assert.match(lines[2]!, /^current {2,}Default for new sessions\. {2,}⊙ inline · ⇄ writer · w switches · ● this session only$/);
	await keys(RIGHT, RIGHT);
	assert.match(text(120).split("\n")[2]!, /^fast {2,}Used in this session\. /);
	assert.match(text(120, true).split("\n")[0]!, /fast =@/, "ASCII");
	assert.match(c.keys().map((k) => k.join(" ")).join("  "), /^←→ profile {2}↑↓ agent {2}enter model {2}e effort {2}w mode {2}s save {2}u use$/);
	// The session's profile is the default (after a d): no mark, and both sentences.
	own = "current";
	c.reload();
	await flush();
	assert.match(text(120).split("\n")[0]!, /^ current ⊙★ {2,}default ⊙ {2,}fast ⇄ {2,}\+ New $/);
	assert.doesNotMatch(text(120), /this session only/);
});

test("the Orchestrator row first with model, effort and mode word; then Agent/Model/Effort and one row per agent of this runtime", async () => {
	const { c, text } = card();
	c.reload();
	await flush();
	const t = text();
	assert.match(t, /\n▸ Orchestrator {2,}claude-bridge\/claude-opus-5-5 {2,}high {2}⊙ inline\n/);
	assert.match(t, /\n {2}Agent {2,}Model ▾ {2,}Effort ▾\n {2}review-refuter {2,}claude-bridge\/claude-opus-5-5 {2,}low\n {2}gentle-ai-worker {2,}claude-bridge\/claude-opus-5-5 {2,}low\n/);
	assert.doesNotMatch(t, /jhon-orchestrator|claude-opus-5-5 · medium|set every agent|fallbacks|tier/);
	await keys(c, DOWN);
	assert.match(text(), /\n {2}Orchestrator .*\n[\s\S]*\n▸ review-refuter /, "↓ moves to the agents");
});

async function keys(c: ProfilesCard, ...ks: string[]) { for (const k of ks) { c.handleInput(k); await flush(); } }

test("←→ move between tabs; a writer profile shows ⇄ writer; + New is reachable", async () => {
	const { c, text } = card();
	c.reload();
	await flush();
	await keys(c, RIGHT, RIGHT);
	let t = text();
	assert.match(t.split("\n")[0]!, /^ current ⊙★ {2}default ⊙ {2}fast ⇄ /);
	assert.match(t.split("\n")[1]!, /^─{23}━{8}─+$/, "fast's tab underlined");
	assert.match(t, /\nfast {2,}Not in use\./);
	assert.match(t, /▸ Orchestrator {2,}deepseek\/deepseek-v4 {2,}high {2}⇄ writer/);
	await keys(c, RIGHT, RIGHT);
	t = text();
	assert.match(t.split("\n")[1]!, /^─{31}━{7}─*$/, "+ New selected, the last tab");
	await keys(c, LEFT, LEFT, LEFT, LEFT, LEFT);
	assert.match(text().split("\n")[1]!, /^━{12}/, "← stops at the first tab");
});

test("w toggles the draft's mode without calling deps; s saves it: deps.mode and deps.set once each; Save dim until dirty, Saved after", async () => {
	const { c, calls, text } = card();
	c.reload();
	await flush();
	assert.match(text(), /\[ u · Use in this session \] {2}\[ d · Make default \] {2}\[ s · Save \]/);
	assert.match(c.keys().map((k) => k.join(" ")).join("  "), /^←→ profile {2}↑↓ agent {2}enter model {2}e effort {2}w mode {2}s save {2}u use$/);
	await keys(c, "w");
	assert.deepEqual(calls, []);
	assert.match(text(), /▸ Orchestrator .*⇄ writer/);
	assert.match(text().split("\n")[0]!, /current ⇄★/, "the tab shows the draft's mode");
	await keys(c, DOWN, "e", "\x1b[A", ENTER);
	assert.deepEqual(calls, [], "an effort edit changes the draft only");
	assert.match(text(), /▸ review-refuter {2,}claude-bridge\/claude-opus-5-5 {2,}minimal/);
	await keys(c, "s");
	assert.deepEqual(calls, ["mode current writer", "set current review-refuter pi claude-bridge/claude-opus-5-5@minimal"]);
	assert.match(text(), /Saved$/m);
	await keys(c, "s");
	assert.equal(calls.length, 2, "a clean draft saves nothing");
});

test("enter picks a model for the row, keeping its effort, into the draft", async () => {
	const { c, calls, text } = card();
	c.reload();
	await flush();
	await keys(c, ENTER);
	assert.match(text(), /model: type to filter▏ 3 available/);
	await keys(c, "s", "o", "n", ENTER);
	assert.match(text(), /▸ Orchestrator {2,}claude-bridge\/claude-sonnet-5-5 {2,}high/);
	assert.deepEqual(calls, []);
	await keys(c, "s");
	assert.deepEqual(calls, ["set current jhon-orchestrator pi claude-bridge/claude-sonnet-5-5@high"]);
});

test("u uses the profile in this session, d makes it the default", async () => {
	const { c, calls, text } = card();
	c.reload();
	await flush();
	await keys(c, RIGHT, RIGHT, "u");
	assert.deepEqual(calls, ["useNow fast"]);
	assert.match(text(), /\nfast {2,}Used in this session\./);
	await keys(c, "d");
	assert.deepEqual(calls, ["useNow fast", "makeDefault fast"]);
	assert.match(text().split("\n")[0]!, /current ⊙ {2}default ⊙ {2}fast ⇄★/);
});

test("a note longer than the card wraps onto further rows, at most three and none past the width; a short one is one row, as before", async () => {
	const refusal = "far/model cannot run in this pi (provider not loaded, or no credential). To use it: make it the default with d, then restart pi.";
	let why = refusal;
	const { c } = card({ useNow: async () => why, makeDefault: async () => "no" });
	c.reload();
	await flush();
	await keys(c, RIGHT, RIGHT, "u");
	let lines = c.lines(plain, 96);
	assert.deepEqual(lines.slice(-2), ["Could not use fast: far/model cannot run in this pi (provider not loaded, or no credential). To", "use it: make it the default with d, then restart pi."]);
	assert.match(lines.at(-3)!, /^\[ u · Use in this session \]/, "under the actions bar");
	assert.equal(c.lines(plain, 200).at(-1), `Could not use fast: ${refusal}`, "one row where it fits");
	// A message of any length takes three rows at most, the last cut.
	why = "word ".repeat(200).trim();
	await keys(c, "u");
	lines = c.lines(plain, 96);
	assert.match(lines.at(-4)!, /^\[ u · Use in this session \]/);
	assert.match(lines.at(-3)!, /^Could not use fast: word word /);
	assert.match(lines.at(-1)!, /word…$/);
	assert.ok(lines.every((l) => visibleWidth(l) <= 96));
	assert.match(c.lines(plain, 96, true).at(-1)!, /word~$/, "ASCII");
	// A short note, another action's: one row, the card's last.
	await keys(c, "d");
	lines = c.lines(plain, 96);
	assert.equal(lines.at(-1), "Could not make fast the default: no");
	assert.match(lines.at(-2)!, /^\[ u · Use in this session \]/);
});

test("default is agents.yaml: w, enter, e and s are refused with a note", async () => {
	const { c, calls, text } = card();
	c.reload();
	await flush();
	await keys(c, RIGHT);
	for (const k of ["w", ENTER, "e", "s"]) {
		await keys(c, k);
		assert.match(text(), /default is agents\.yaml's models, read-only: n makes an editable copy\./, k);
	}
	assert.deepEqual(calls, []);
	assert.match(text(), /▸ Orchestrator .*⊙ inline/);
});

test("x deletes the selected profile after y; any other key cancels; n creates a copy as a new tab", async () => {
	const { c, calls, text } = card();
	c.reload();
	await flush();
	await keys(c, RIGHT, RIGHT, "x");
	assert.match(text(), /Delete profile fast\? y deletes · any other key cancels/);
	await keys(c, "n");
	assert.match(text(), /Cancelled: nothing changed\./);
	await keys(c, "x", "y");
	assert.deepEqual(calls, ["remove fast"]);
	assert.doesNotMatch(text().split("\n")[0]!, /fast/);
	await keys(c, "n", "r", "a", "p", ENTER);
	assert.equal(calls.at(-1), "create rap default");
	assert.match(text().split("\n")[0]!, /rap ⊙ {2}\+ New/);
	await keys(c, RIGHT, ENTER);
	assert.match(text(), /new profile name: ▏/, "enter on + New names a new profile");
});

test("the panel frames the card full screen with the hints line and draws in ASCII", async () => {
	const data = fixture();
	const panel = new JhonPanel({ theme: plain, runtime: "pi", view: "profiles", load: async () => data, use: async () => undefined, loadChange: async () => ({}), height: () => 40, rows: () => 30,
		loadGrants: async () => ({ root: "", classes: [], grants: [] }), grant: async () => {}, revoke: async () => {},
		loadTests: async () => ({}), startTests: () => ({ id: "j", startedAt: 0 }), stopTests: () => {}, onClose() {}, requestRender() {} });
	panel.open();
	await flush();
	const lines = panel.render(100);
	assert.equal(lines.length, 30);
	for (const l of lines) assert.equal(visibleWidth(l), 100, l);
	assert.match(lines[1]!, /^│ {2}current ⊙★ /);
	assert.match(lines.join("\n"), /←→ profile {2}↑↓ agent {2}enter model {2}e effort {2}w mode {2}s save {2}u use {2}esc close/);
	const ascii = new JhonPanel({ theme: plain, runtime: "pi", view: "profiles", ascii: true, load: async () => data, use: async () => undefined, loadChange: async () => ({}), height: () => 40, rows: () => 30,
		loadGrants: async () => ({ root: "", classes: [], grants: [] }), grant: async () => {}, revoke: async () => {},
		loadTests: async () => ({}), startTests: () => ({ id: "j", startedAt: 0 }), stopTests: () => {}, onClose() {}, requestRender() {} });
	ascii.open();
	await flush();
	const a = ascii.render(100).join("\n");
	assert.ok(/^[\x20-\x7e\n]*$/.test(a), a);
	assert.match(a, /current o\* /);
	assert.match(a, /o inline - = writer - w switches/);
});

// T130 (L902, L909): the model list is pi's registry united with the models the profiles name.
const registryOf = (...ids: string[]) => ({ getAvailable: () => ids.map((id) => ({ provider: id.slice(0, id.indexOf("/")), id: id.slice(id.indexOf("/") + 1) })) });
const BRIDGE = Array.from({ length: 15 }, (_, i) => `claude-bridge/claude-model-${String(i).padStart(2, "0")}`);
const NAMED = ["claude-bridge/claude-opus-5-5", "deepseek/deepseek-v4", "glm/glm-5"];

test("the models offered for pi are every registry model and every profile-named one: provider/id, sorted, none twice (T130)", () => {
	const registry = registryOf(...BRIDGE, "deepseek/deepseek-v4", "claude-bridge/claude-opus-5-5", "nan/alpha-flash");
	const offered = modelsOffered("pi", fixture(), () => registry);
	assert.deepEqual(offered, [...new Set([...BRIDGE, "nan/alpha-flash", ...NAMED])].sort());
	assert.equal(offered.length, 19, "15 + 1 from the registry alone, 3 the profiles name (two of them in the registry too)");
});

test("a model a profile names and the registry lacks is still offered (T130)", () => {
	const offered = modelsOffered("pi", fixture(), () => registryOf("nan/alpha-flash"));
	assert.deepEqual(offered, ["claude-bridge/claude-opus-5-5", "deepseek/deepseek-v4", "glm/glm-5", "nan/alpha-flash"]);
});

test("another runtime's column lists only the models its profiles name, whatever pi's registry holds (T130)", () => {
	assert.deepEqual(modelsOffered("claude-code", fixture(), () => registryOf(...BRIDGE)), ["claude-opus-5-5"]);
});

test("a registry that cannot be read leaves the profile-named models, and nothing is thrown (T130)", () => {
	const broken = [
		() => undefined,
		() => ({}),
		() => ({ getAvailable: () => { throw new Error("registry down"); } }),
		() => { throw new Error("this ctx is stale"); },
		() => ({ getAvailable: () => "not a list" }),
	];
	for (const read of broken) assert.deepEqual(modelsOffered("pi", fixture(), read), NAMED);
});

test("over a long list the picker counts every model, pages as the cursor moves, filters, and enter chooses (T130)", async () => {
	const all = modelsOffered("pi", fixture(), () => registryOf(...BRIDGE, "nan/alpha-flash", "nan/beta-flash"));
	assert.equal(all.length, 20);
	const { c, text, keys } = card({ models: () => all });
	c.reload();
	await flush();
	await keys(ENTER);
	assert.match(text(), /model: type to filter▏ 20 available/, "the count is the list's length");
	const shown = () => text().split("\n").filter((l) => /^ {4}(▸ | {2})\S+\/\S+$/.test(l)).map((l) => l.trim().replace(/^▸ /, ""));
	assert.equal(shown().length, 12, "twelve rows at a time");
	assert.match(text(), /▸ claude-bridge\/claude-opus-5-5/, "the cursor opens on the row's own model");
	await keys(...Array<string>(19).fill(UP));
	assert.deepEqual(shown(), all.slice(0, 12), "the first twelve: the rest is below the window");
	await keys(...Array<string>(19).fill(DOWN));
	assert.match(text(), /▸ nan\/beta-flash/, "the cursor reaches the last model, the window following it");
	assert.deepEqual(shown(), all.slice(8), "the last twelve");
	await keys("n", "a", "n", "/");
	assert.match(text(), /model: nan\/▏ 2 available/, "the filter counts what it leaves");
	assert.deepEqual(shown(), ["nan/alpha-flash", "nan/beta-flash"]);
	await keys(DOWN, ENTER);
	assert.match(text(), /Orchestrator +nan\/beta-flash +high/, "enter chooses, the effort kept");
});

test("s on a model this session's pi cannot run (its provider not loaded, or without a credential) says when it runs, promising no next session; one it can run says nothing more (T130, L920)", async () => {
	const had = card({ inSession: () => true });
	had.c.reload();
	await flush();
	await had.keys(ENTER, DOWN, ENTER, "s");
	assert.deepEqual(had.calls, ["set current jhon-orchestrator pi claude-bridge/claude-sonnet-5-5@high"]);
	assert.match(had.text(), /Saved/);
	assert.doesNotMatch(had.text(), /available models/);
	const lacks = card({ inSession: (model) => model !== "claude-bridge/claude-sonnet-5-5" });
	lacks.c.reload();
	await flush();
	await lacks.keys(ENTER, DOWN, ENTER, "s");
	assert.deepEqual(lacks.calls, ["set current jhon-orchestrator pi claude-bridge/claude-sonnet-5-5@high"]);
	assert.match(lacks.text(), /Saved/);
	assert.match(lacks.text(160), /^claude-bridge\/claude-sonnet-5-5 is not among this session's available models: it runs once pi lists it, with its provider loaded and a credential for it\.$/m);
	const unknown = card();
	unknown.c.reload();
	await flush();
	await unknown.keys(ENTER, DOWN, ENTER, "s");
	assert.doesNotMatch(unknown.text(), /available models/, "a shell that cannot tell says nothing");
});

// L920: the verifier's cases for the registry's list.
test("registryModels drops malformed entries, is [] for an empty list and undefined for a promise (L920)", () => {
	const malformed = [null, 7, "claude-bridge/claude-opus-5-5", {}, { provider: "nan" }, { id: "alpha-flash" }, { provider: 1, id: "x" }, { provider: "nan", id: 2 }, { provider: "nan", id: "alpha-flash" }];
	assert.deepEqual(registryModels(() => ({ getAvailable: () => malformed })), ["nan/alpha-flash"]);
	assert.deepEqual(registryModels(() => ({ getAvailable: () => [] })), []);
	assert.equal(registryModels(() => ({ getAvailable: () => Promise.resolve([{ provider: "nan", id: "alpha-flash" }]) })), undefined);
});

test("modelsOffered offers a duplicate once, and 200 models as a sorted list without duplicates (L920)", () => {
	assert.deepEqual(modelsOffered("pi", { ...fixture(), profiles: [] }, () => registryOf("nan/alpha-flash", "nan/alpha-flash", "nan/beta-flash", "nan/alpha-flash")), ["nan/alpha-flash", "nan/beta-flash"]);
	const many = Array.from({ length: 200 }, (_, i) => `p${i % 7}/m-${String((i * 37) % 200).padStart(3, "0")}`);
	const offered = modelsOffered("pi", { ...fixture(), profiles: [] }, () => registryOf(...many, ...many.slice(0, 50)));
	assert.equal(offered.length, new Set(many).size);
	assert.deepEqual(offered, [...new Set(many)].sort());
});
