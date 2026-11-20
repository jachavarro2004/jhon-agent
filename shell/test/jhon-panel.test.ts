/**
 * The /jhon:profile overlay (lib/ui/jhon-panel-profiles.ts): the profiles as
 * tabs, the default one, each agent's model, and d making the chosen one the
 * default (the mockup's layout, T117d; more in jhon-panel-profiles.test.ts).
 *
 *   jhon test
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { JhonPanel, jhonPanelKey, modelText, type ProfilesData } from "../lib/ui/jhon-panel.ts";
import { changeCardLines } from "../lib/ui/jhon-panel-change.ts";
import { visibleWidth } from "@earendil-works/pi-tui";
import { envOverrides, memorySettings } from "./fake-jhon-settings.ts";

// The settings `jhon settings` would give the shell under an environment (S38).
const envSettings = (env: NodeJS.ProcessEnv) => memorySettings({}, undefined, envOverrides(env));

const plain = { fg: (_r: string, t: string) => t, bold: (t: string) => t };
const row = (agent: string, runtime: string, model: string, source = "agents.yaml tiers.x.strong") => ({ agent, runtime, model, source });
const data = (active: string): ProfilesData => ({ active, file: "~/.config/jhon/profiles.json", profiles: [
  { name: "default", delegation: "inline", agents: [row("jhon-orchestrator", "pi", "claude-bridge/claude-opus-5-5@high"), row("worker", "pi", "claude-bridge/claude-opus-5-5@medium"), row("worker", "claude-code", "claude-opus-5-5@medium")] },
  { name: "jhon", delegation: "inline", agents: [row("jhon-orchestrator", "pi", "claude-bridge/claude-opus-5-5@high"), row("worker", "pi", "claude-bridge/claude-fable-5-1@high", "profile agent worker.pi"), row("worker", "claude-code", "claude-opus-5-5@medium")] },
] });
const flush = () => new Promise((r) => setImmediate(r));
/** A full-screen view's hints row (G's profiles view, L174): `<key> <action>` joined by two spaces, esc last. */
const hintRow = (lines: string[]) => lines.find((l) => /\besc (close|done)\b/.test(l)) ?? "";
const noGrants = { loadGrants: async () => ({ root: "", classes: [], grants: [] }), grant: async () => {}, revoke: async () => {}, ...noTestsDeps() };
function noTestsDeps() {
  return { loadTests: async () => ({}), startTests: () => ({ id: "job-1", startedAt: 0 }), stopTests: () => {} };
}

test("the model reads with its effort in words", () => {
  assert.equal(modelText("claude-bridge/claude-opus-5-5@medium"), "claude-bridge/claude-opus-5-5 · medium");
  assert.equal(modelText("gpt-5.5"), "gpt-5.5");
});

test("Profiles shows the provider and model pi's orchestrator runs on in each profile (T56)", async () => {
  const d = data("default");
  d.profiles.push({ name: "api", provider: "anthropic", delegation: "writer", agents: [row("jhon-orchestrator", "pi", "anthropic/claude-opus-4-5@medium", "profile tier pi.strong")] });
  const panel = new JhonPanel({ theme: plain, runtime: "pi", view: "profiles", load: async () => d, use: async () => undefined, loadChange: async () => ({}), height: () => 46, ...noGrants, onClose() {}, requestRender() {} });
  panel.open();
  await flush();
  assert.match(panel.render(80).join("\n"), /▸ Orchestrator +claude-bridge\/claude-opus-5-5 +high {2}⊙ inline/);
  panel.handleInput("\x1b[C");
  panel.handleInput("\x1b[C");
  assert.match(panel.render(80).join("\n"), /▸ Orchestrator +anthropic\/claude-opus-4-5 +medium {2}⇄ writer/);
  assert.ok(panel.render(80).every((l) => visibleWidth(l) <= 80), "every row fits the overlay");
});

test("Profiles shows the profiles as tabs with the default starred, the chosen one's models in this runtime, and d makes it the default", async () => {
  let active = "default";
  const used: string[] = [];
  const panel = new JhonPanel({ theme: plain, runtime: "pi", view: "profiles", load: async () => data(active), use: async (n) => { used.push(n); active = n; return undefined; }, loadChange: async () => ({}), height: () => 46, ...noGrants, onClose() {}, requestRender() {} });
  panel.open();
  await flush();
  let text = panel.render(80).join("\n");
  assert.match(text, /^╭─+╮\n│ {2}default ⊙★ {2}jhon ⊙ {2}\+ New +│\n│ ━+─+ │\n│ default {3}This session runs on it\. New sessions start on it\./, "near full screen, the profiles as tabs (L695)");
  assert.match(text, /│ {3}worker +claude-bridge\/claude-opus-5-5 +medium +│/, "the chosen profile's models in pi");
  assert.doesNotMatch(text, /claude-code|claude-opus-5-5@/);
  panel.handleInput("\x1b[C");
  text = panel.render(80).join("\n");
  assert.match(text, /worker +claude-bridge\/claude-fable-5-1 +high/);
  // pi's panel is pi's: no runtime switch, no r runtime hint (L134).
  assert.doesNotMatch(text, /r runtime/);
  panel.handleInput("d");
  await flush(); await flush();
  assert.deepEqual(used, ["jhon"]);
  text = panel.render(80).join("\n");
  assert.match(text, /default ⊙ {2}jhon ⊙★/);
  assert.match(text, /jhon is the default for new sessions\./);
  assert.ok(panel.render(80).every((l) => visibleWidth(l) <= 80), "every row fits the overlay");
});

test("a failed read or default change says why", async () => {
  const panel = new JhonPanel({ theme: plain, runtime: "pi", view: "profiles", load: async () => { throw new Error("no profile"); }, use: async () => undefined, loadChange: async () => ({}), height: () => 46, ...noGrants, onClose() {}, requestRender() {} });
  panel.open();
  await flush();
  assert.match(panel.render(80).join("\n"), /Could not read the profiles: no profile/);
  const failing = new JhonPanel({ theme: plain, runtime: "pi", view: "profiles", load: async () => data("default"), use: async () => { throw new Error("bad tier"); }, loadChange: async () => ({}), height: () => 46, ...noGrants, onClose() {}, requestRender() {} });
  failing.open();
  await flush();
  failing.handleInput("\x1b[C");
  failing.handleInput("d");
  await flush(); await flush();
  assert.match(failing.render(80).join("\n"), /Could not make jhon the default: bad tier/);
});

test("the panel's key is alt+j, JHON_SHELL_JHON_KEY sets another or turns it off", () => {
  assert.equal(jhonPanelKey(envSettings({})), "alt+j");
  assert.equal(jhonPanelKey(envSettings({ JHON_SHELL_JHON_KEY: "ctrl+o" })), "ctrl+o");
  assert.equal(jhonPanelKey(envSettings({ JHON_SHELL_JHON_KEY: "off" })), undefined);
});

const status = {
  change: "abc-process-exercises", next: "apply", strict_tdd: { value: true },
  route: { level: "verified", design: false, reasons: ["high risk: reaches the level that asks for verification (high)"] },
  tasks: { total: 8, completed: 5, slice: "Slice 2: deadline extension", slices: [
    { name: "Slice 1: launch", total: 4, completed: 4, delivers: "CA-09" },
    { name: "Slice 2: deadline extension", total: 3, completed: 1, delivers: "CA-13", tasks: [{ text: "2.1 a", done: true }, { text: "2.2 extend the deadline", done: false }, { text: "2.3 c", done: false }] },
    { name: "Slice 3: panel", total: 1, completed: 0 }] },
  artifacts: [{ artifact: "exploration.md", state: "done" }, { artifact: "design.md", state: "missing" }, { artifact: "tasks.md", state: "done" }, { artifact: "verify-report.md", state: "missing" }],
  tests: { timestamp: "2026-10-29T05:00:00Z", passed: true, failed: [], scope: "affected", complete: true, commit: "94ce20d8a7", clean: false },
  notes: ["scenarios named by a test: 49/50"],
  root: "/p/acme", directory: "openspec/changes/abc-process-exercises",
  risk: { tier: "high", reasons: [{ level: "medium", file: "backend/abc/views.py", why: "code" }, { level: "passive", file: "backend/abc/tests/test_views.py", why: "test" }, { level: "high", file: "backend/svc/policy.py", why: "credentials" }] },
};

test("Change says the whole change: route and why, every slice and what it delivers, the open slice's tasks, the record, the last run, the notes", () => {
  const text = changeCardLines(status, plain, 70, Date.parse("2026-10-29T05:26:00Z")).join("\n");
  assert.match(text, /◆ abc-process-exercises\nverified route · strict TDD\nrisk high · backend\/svc\/policy\.py \(credentials\) ·\n     backend\/abc\/views\.py \(code\)\n  · high risk: reaches the level that asks for verification \(high\)\nnext apply/);
  assert.match(text, /Slices  1\/3 done · tasks 5\/8/);
  assert.match(text, /✓ 1 launch  4\/4 · delivers CA-09/);
  assert.match(text, /◐ 2 deadline extension  1\/3 · delivers CA-13\n    ✓ 2\.1 a\n    ◐ 2\.2 extend the deadline\n    · 2\.3 c\n· 3 panel  0\/1/);
  assert.match(text, /Record\n✓ exploration   · design   ✓ tasks   \S+ verify report missing/, "only what the route asks for is missing");
  assert.match(text, /Last run\n✓ passed · affected · 26 min ago · 94ce20d\n  ran on uncommitted changes/);
  assert.match(text, /Notes\n· scenarios named by a test: 49\/50/, "the engine's own notes");
  const laned = changeCardLines({ ...status, lanes: [{ change: "fix-infra-hardening", worktree: "infra-hardening", done: 5, slices: 5, next: "verify" }] }, plain, 70).join("\n");
  assert.match(laned, /Also in progress\n◐ fix-infra-hardening  infra-hardening · 5\/5 slices · next verify/, "a lane in done/total");
  assert.ok(!/\bverified\b(?! route)/.test(laned), `no bare «verified» (T29):\n${laned}`);
  assert.deepEqual(changeCardLines({ change: null }, plain, 70)[0], "No active LDD change in this project.");
});

test("alt+j opens the change's detail as one overlay: the header names the change with esc, no tabs, the footer inside the frame; it scrolls and r reads it again (L113)", async () => {
  let loads = 0;
  const panel = new JhonPanel({ theme: plain, runtime: "pi", load: async () => data("default"), use: async () => undefined, loadChange: async () => { loads++; return status; }, height: () => 11, ...noGrants, onClose() {}, requestRender() {} });
  panel.open();
  await flush();
  let lines = panel.render(80);
  assert.match(lines[0]!, /^╭─+╮$/, "the border carries no title");
  assert.match(lines[1]!, /^│ {3}LDD +│$/, "the view's one tab, as G's profiles view (L174)");
  assert.match(lines[3]!, /^│ {2}LDD · abc-process-exercises +│$/, "named as LDD's (L174)");
  assert.doesNotMatch(lines.join("\n"), /Profiles|Grants|Audit|Memory/, "no other tabs");
  assert.equal(lines.length, 11, "the terminal's rows");
  assert.doesNotMatch(lines.join("\n"), /◆ abc-process-exercises/, "the header names the change once (L119)");
  assert.match(lines[5]!, /^│ verified route · strict TDD +│$/, "the body starts with the route");
  assert.match(hintRow(lines), /↑\/↓ scroll {2}t tests {2}r refresh {2}esc close/);
  assert.match(lines.at(-1)!, /^╰─+╯$/);
  panel.handleInput("j");
  assert.doesNotMatch(panel.render(80)[5]!, /verified route/, "it scrolled");
  assert.equal(loads, 1);
  panel.handleInput("r");
  await flush();
  assert.equal(loads, 2);
});

test("the LDD detail takes the terminal's rows as G's profiles view (L174): the long body scrolls inside it, a short body leaves blank rows below", async () => {
  const long = { ...status, notes: Array.from({ length: 40 }, (_, i) => `note ${i + 1}`) };
  const make = (rows: number, loadChange: () => Promise<unknown>) => {
    const panel = new JhonPanel({ theme: plain, runtime: "pi", view: "change", load: async () => data("default"), use: async () => undefined, loadChange, height: () => 20, rows: () => rows, ...noGrants, onClose() {}, requestRender() {} });
    panel.open();
    return panel;
  };
  const tall = make(60, async () => long);
  await flush();
  assert.equal(tall.render(80).length, 60, "every row of the terminal, past G's 24-row detail card");
  const small = make(14, async () => long);
  await flush();
  const first = small.render(80);
  assert.equal(first.length, 14);
  assert.match(hintRow(first), /↑\/↓ scroll/);
  small.handleInput("j");
  const next = small.render(80);
  assert.equal(next.length, 14, "scrolling keeps the height");
  assert.notEqual(next[5], first[5], "the body scrolled");
  const loading = make(30, () => new Promise(() => {})).render(80);
  assert.equal(loading.length, 30);
  assert.ok(loading.slice(10, -1).every((line) => /^│ +│$/.test(line)), "blank rows below, as G pads its body");
});

test("Grants lists every class with its state in this project, and gives or revokes one only after a confirming key", async () => {
  const now = Date.now();
  const root = "/p/acme";
  let grants = [
    { class: "push", root, granted_at: new Date(now - 6e5).toISOString(), expires_at: new Date(now + 24 * 6e4).toISOString(), live: true },
    { class: "deploy", root, granted_at: "2026-10-23T18:00:00.000Z", expires_at: "2026-10-23T18:40:00.000Z", live: false },
    { class: "push", root: "/p/other", granted_at: "", expires_at: new Date(now + 6e5).toISOString(), live: true },
  ];
  const ran: string[] = [];
  const panel = new JhonPanel({ theme: plain, runtime: "pi", view: "grants", load: async () => data("default"), use: async () => undefined, loadChange: async () => ({}), height: () => 46,
    loadGrants: async () => ({ root, classes: ["push", "gh-write", "deploy"], grants }),
    ...noTestsDeps(), grant: async (cls, d) => { ran.push(`grant ${cls} ${d}`); grants = [...grants, { class: cls, root, granted_at: "", expires_at: new Date(now + 3.6e6).toISOString(), live: true }]; },
    revoke: async (cls) => { ran.push(`revoke ${cls}`); grants = grants.filter((g) => !(g.class === cls && g.root === root)); },
    onClose() {}, requestRender() {} });
  panel.open();
  await flush();
  let text = panel.render(90).join("\n");
  assert.match(text, /acme · this project and its worktrees/);
  assert.match(text, /▸ ✓ push +live 2[34] min · until \d\d:\d\d/);
  assert.match(text, /git push: push or delete branches/);
  assert.match(text, /✗ deploy +expired \d\d:\d\d on 2026-10-23/);
  assert.match(text, /· gh-write +not granted/);
  assert.doesNotMatch(text, /bypass-session|never asks permission|no safety net/, "bypass-session is gone (S31)");
  // L175 (7): every live grant, its class, scope and expiry, before the classes.
  assert.match(text, /│ Active grants · 2 +│\n│ {3}push +acme · this project +until \d\d:\d\d · 2[34] min +│\n│ {3}push +other · \/p\/other +until \d\d:\d\d · (9|10) min +│/);
  assert.doesNotMatch(text, /deploy +[^\n]*until/, "an expired grant is not active");
  assert.doesNotMatch(text, /Other projects/, "the list says them");
  assert.match(text, /Duration +30 min/);
  assert.match(text, /^│ Grants +esc │$/m, "its own overlay on the pattern");
  assert.doesNotMatch(text, /Mode|normal|yolo|since/, "only the grants: the mode is Customize's (L214)");
  assert.match(panel.render(90).at(-2)!, /Enter grant · x revoke · d duration · r refresh · Esc close/);
  assert.match(text, /Grants · 1\/3 +acme · this project and its worktrees/, "the pane header with the cursor's row");

  panel.handleInput("j"); panel.handleInput("d"); panel.handleInput("d");
  panel.handleInput("\r");
  assert.match(panel.render(90).join("\n"), /Grant gh-write to acme for 2 h\? enter confirms/);
  assert.deepEqual(ran, [], "nothing runs before the confirming key");
  panel.handleInput("\r");
  await flush(); await flush();
  assert.deepEqual(ran, ["grant gh-write 2h"]);
  text = panel.render(90).join("\n");
  assert.match(text, /gh-write granted for 2 h\./);
  assert.match(text, /✓ gh-write +live/);

  panel.handleInput("\r");
  panel.handleInput("\u001b");
  assert.match(panel.render(90).join("\n"), /Cancelled: nothing changed\./, "esc cancels a waiting grant instead of closing");
  panel.handleInput("k");
  panel.handleInput("x");
  assert.match(panel.render(90).join("\n"), /Revoke push from acme\? x confirms/);
  panel.handleInput("x");
  await flush(); await flush();
  assert.deepEqual(ran, ["grant gh-write 2h", "revoke push"]);
  assert.match(panel.render(90).join("\n"), /push revoked\./);
});

test("/jhon:grants offers no bypass-session: the engine removed the class (S31)", async () => {
  const { CLASS_WORDS } = await import("../lib/ui/jhon-panel-grants.ts");
  assert.equal(CLASS_WORDS["bypass-session"], undefined);
  assert.deepEqual(Object.keys(CLASS_WORDS), ["push", "gh-write", "publish", "remote", "deploy", "outside-write", "destroy"]);
});

test("/jhon:grants with no live grant says so under Active grants (L175)", async () => {
  const panel = new JhonPanel({ theme: plain, runtime: "pi", view: "grants", load: async () => data("default"), use: async () => undefined, loadChange: async () => ({}), height: () => 46,
    loadGrants: async () => ({ root: "/p/acme", classes: ["push"], grants: [{ class: "push", root: "/p/acme", granted_at: "", expires_at: "2026-01-01T00:00:00.000Z", live: false }] }),
    grant: async () => {}, revoke: async () => {}, ...noTestsDeps(), onClose() {}, requestRender() {} });
  panel.open();
  await flush();
  const text = panel.render(90).join("\n");
  assert.match(text, /│ Active grants · 0 +│\n│ {3}No active grants: enter on a class below gives one\. +│/);
  assert.match(text, /Grants · 1\/1/, "the classes still follow");
});

test("outside a project Grants gives nothing and says why", async () => {
  const panel = new JhonPanel({ theme: plain, runtime: "pi", view: "grants", load: async () => data("default"), use: async () => undefined, loadChange: async () => ({}), height: () => 46,
    loadGrants: async () => ({ root: "", classes: ["push"], grants: [] }), ...noTestsDeps(), grant: async () => { throw new Error("must not run"); }, revoke: async () => {}, onClose() {}, requestRender() {} });
  panel.open();
  await flush();
  panel.handleInput("\r");
  const text = panel.render(90).join("\n");
  assert.match(text, /This session is not in a project: grants are per project\./);
  assert.doesNotMatch(text, /enter grant|Grant /);
});

test("t runs the change's tests in the background and the detail shows the run and its end, then reads the change again (L113)", async () => {
  let now = Date.parse("2026-10-29T06:00:00Z");
  const started: string[] = [];
  let finish: (code: number | null) => void = () => {};
  let loads = 0;
  const tests = { change: "abc", base: "main", projects: [{ project: "backend/core/apps/abc_service", tests: "own", modules: 45, last_seconds: 837, checks: [] }] };
  const panel = new JhonPanel({ theme: plain, runtime: "pi", load: async () => data("default"), use: async () => undefined, loadChange: async () => { loads++; return status; }, height: () => 60, ...noGrants,
    loadTests: async () => tests, startTests: (d, onEnd) => { finish = onEnd; started.push(`${d.change} ${d.base}`); return { id: "job-1", startedAt: now }; }, stopTests: () => finish(null),
    onClose() {}, requestRender() {} });
  (panel as any).tests.deps.now = () => now;
  // G's detail card keeps to 24 rows: the run is at the body's end, scrolled to.
  const end = () => { for (let i = 0; i < 40; i++) panel.handleInput("j"); };
  panel.open();
  await flush();
  assert.doesNotMatch(panel.render(100).join("\n"), /\nTests\n/, "no run, no Tests section");
  panel.handleInput("t");
  await flush(); await flush();
  assert.deepEqual(started, ["abc main"]);
  now += 75_000;
  end();
  let lines = panel.render(100);
  assert.match(lines.join("\n"), /Tests[^\n]*\n│ ◐ running 1m 15s of ~14 min · also in BACKGROUND/);
  assert.match(hintRow(lines), /s stop tests/);
  panel.handleInput("t");
  assert.equal(started.length, 1, "one run at a time");
  finish(0);
  await flush(); await flush();
  end();
  assert.match(panel.render(100).join("\n"), /Finished ✓ passed: the record is the one jhon deliver check reads\./);
  assert.equal(loads, 2, "the change is read again: its Last run is the new record");
  panel.handleInput("t");
  await flush(); await flush();
  panel.handleInput("s");
  await flush(); await flush();
  end();
  assert.match(panel.render(100).join("\n"), /Run stopped: its record is partial\./);
  panel.dispose();
});

test("t on a change without tests runs nothing and says why", async () => {
  const panel = new JhonPanel({ theme: plain, runtime: "pi", load: async () => data("default"), use: async () => undefined, loadChange: async () => status, height: () => 60, ...noGrants, onClose() {}, requestRender() {} });
  // G's detail card keeps to 24 rows: the run is at the body's end, scrolled to.
  const end = () => { for (let i = 0; i < 40; i++) panel.handleInput("j"); };
  panel.open();
  await flush();
  panel.handleInput("t");
  await flush(); await flush();
  end();
  assert.match(panel.render(100).join("\n"), /Nothing to run: jhon executions finds no tests for the change\./);
});

test("Profiles creates a profile, edits an agent's model and effort in a draft saved with s, and removes a profile only after a y", async () => {
  const efforts = { pi: ["off", "minimal", "low", "medium", "high", "xhigh", "max"], "claude-code": ["low", "medium", "high", "xhigh", "max"] };
  let current = { ...data("default"), efforts };
  const calls: string[] = [];
  const panel = new JhonPanel({ theme: plain, runtime: "pi", view: "profiles", load: async () => current, use: async () => undefined, loadChange: async () => ({}), height: () => 66, ...noGrants,
    createProfile: async (name, from) => {
      calls.push(`new ${name} ${from}`);
      current = { ...current, profiles: [...current.profiles, { name, delegation: "inline", agents: current.profiles.find((p) => p.name === from)!.agents }] };
    },
    setModel: async (profile, agent, runtime, value) => {
      calls.push(`set ${profile} ${agent} ${runtime} ${value || "-"}`);
      current = { ...current, profiles: current.profiles.map((p) => p.name !== profile ? p : { ...p, agents: p.agents.map((a) => a.agent === agent && a.runtime === runtime ? row(agent, runtime, value, `profile agent ${agent}.${runtime}`) : a) }) };
    },
    setMode: async (profile, mode) => { calls.push(`mode ${profile} ${mode}`); },
    removeProfile: async (name) => { calls.push(`rm ${name}`); current = { ...current, profiles: current.profiles.filter((p) => p.name !== name) }; },
    models: () => ["claude-bridge/claude-fable-5-1", "claude-bridge/claude-opus-5-5", "claude-bridge/claude-sonnet-5-5", "openai/gpt-5.5"],
    onClose() {}, requestRender() {} });
  const keys = async (...ks: string[]) => { for (const k of ks) { panel.handleInput(k); await flush(); await flush(); } };
  const text = () => panel.render(100).join("\n");
  panel.open();
  await flush();
  assert.match(text(), /←→ profile {2}↑↓ agent {2}enter model {2}e effort {2}w mode {2}s save {2}u use {2}esc close/);
  await keys("e");
  assert.match(text(), /default is agents\.yaml's models, read-only/, "default is agents.yaml itself: nothing to edit");

  await keys("n", "r", "a", "p", "\x7f", "p", "i", "d", "o");
  assert.match(text(), /new profile name: rapido▏ · copy of default/);
  await keys("\r");
  assert.deepEqual(calls, ["new rapido default"]);
  assert.match(text(), /│ rapido {3}Not in use\./, "the new profile is chosen");
  assert.match(text(), /rapido created as a copy of default\./);

  await keys("j", "\r");
  assert.match(text(), /model: type to filter▏ 4 available/);
  await keys("s", "o", "n");
  assert.match(text(), /model: son▏ 1 available\n?[^\n]*\n[^\n]*▸ claude-bridge\/claude-sonnet-5-5/);
  await keys("\r", "e");
  assert.match(text(), /effort:[^\n]*\n(?:[^\n]*\n){3}[^\n]*▸ medium/, "it starts on the agent's effort");
  await keys("k", "\r");
  assert.deepEqual(calls, ["new rapido default"], "edits change the draft only");
  assert.match(text(), /▸ worker +claude-bridge\/claude-sonnet-5-5 +low/);
  await keys("s");
  assert.deepEqual(calls.at(-1), "set rapido worker pi claude-bridge/claude-sonnet-5-5@low");
  assert.match(text(), /Saved +│/);
  assert.match(text(), /esc close/, "the view's keys again");
  assert.doesNotMatch(text(), /\btab \w/, "no tab hint: the arrows move between tabs (L134)");

  await keys("x", "q");
  assert.match(text(), /Cancelled: nothing changed\./);
  await keys("x");
  assert.match(text(), /Delete profile rapido\? y deletes/);
  await keys("y");
  assert.deepEqual(calls.at(-1), "rm rapido");
  assert.doesNotMatch(text().split("\n")[1]!, /rapido/);
  assert.ok(panel.render(100).every((l) => visibleWidth(l) <= 100));
});

test("Change says the worktree its change is in, and with several active and none chosen, says so", () => {
  const text = changeCardLines({ change: "test-suite-review", worktree: "test-suite-review", tasks: { slices: [] } }, plain, 70).join("\n");
  assert.match(text, /◆ test-suite-review\nin worktree test-suite-review/);
  // The other worktrees' changes in progress show beside the selected one, so the card shows every lane, not only the last one touched.
  const lanes = [{ change: "test-suite-tidyup-authz", worktree: "tst-authz", done: 1, slices: 1, next: "verify" }, { change: "fix-widget-creation", worktree: "fix-widgets", done: 0, slices: 0, next: "tasks" }];
  const withLanes = changeCardLines({ change: "architecture-compliance-audit", worktree: "arch-audit", next: "propose", lanes, tasks: { slices: [] } }, plain, 90).join("\n");
  assert.match(withLanes, /Also in progress\n◐ test-suite-tidyup-authz  tst-authz · 1\/1 slices · next verify\n◐ fix-widget-creation  fix-widgets · next tasks/);
  assert.match(changeCardLines({ change: null, activeChanges: ["a", "b"], lanes }, plain, 90).join("\n"), /Also in progress\n◐ test-suite-tidyup-authz/, "with no change selected, the lanes still show");
  assert.match(changeCardLines({ change: null, activeChanges: ["a", "b"] }, plain, 90)[0]!, /No change selected: 2 active, and neither the branch nor a worktree names one\./);
});

test("a message with newlines stays one row inside the frame", async () => {
  const panel = new JhonPanel({ theme: plain, runtime: "pi", view: "change", load: async () => data("default"), use: async () => undefined,
    loadChange: async () => { throw new Error('{\n  "ok": false,\n  "change": null\n}'); }, height: () => 16, ...noGrants, onClose() {}, requestRender() {} });
  panel.open();
  await flush();
  const lines = panel.render(80);
  assert.ok(lines.every((l) => !l.includes("\n") && visibleWidth(l) <= 80), JSON.stringify(lines));
  assert.match(lines[5]!, /^│ ✗ Could not read the change +│$/, "the error is a state block: what failed, bold bad");
  assert.match(lines[6]!, /^│   \{ "ok": false, "change": null \} +│$/, "the cause on its own row, hanging");
  assert.match(hintRow(lines), /^│  r retry {2}esc close +│$/, "only the keys that act");
});

test("the detail fits at 120, 80 and 60 columns: esc close is never clipped (P1, P2)", async () => {
  const panel = new JhonPanel({ theme: plain, runtime: "pi", view: "change", load: async () => data("default"), use: async () => undefined, loadChange: async () => status, height: () => 14, ...noGrants, onClose() {}, requestRender() {} });
  panel.open();
  await flush();
  for (const width of [120, 80, 60]) {
    const lines = panel.render(width);
    for (const line of lines) assert.equal(visibleWidth(line), width, `${width}: ${line}`);
    assert.match(lines[0]!, /╮$/);
    assert.match(hintRow(lines), /esc close +│$/);
  }
});

test("while the change loads the body says so once, and only esc acts (P7)", () => {
  const panel = new JhonPanel({ theme: plain, runtime: "pi", view: "change", load: async () => data("default"), use: async () => undefined, loadChange: () => new Promise(() => {}), height: () => 14, ...noGrants, onClose() {}, requestRender() {} });
  panel.open();
  const lines = panel.render(80);
  assert.match(lines[3]!, /^│ {2}LDD +│$/, "the header says nothing about loading");
  assert.match(lines[5]!, /◐ Reading the change…/);
  assert.match(hintRow(lines), /^│  esc close +│$/);
});

test("Change and Tests say a run in one vocabulary: ✓ passed, ✗ N failed; the risk row falls back to the route's tier (S29)", () => {
  const at = Date.parse("2026-10-29T05:26:00Z");
  const failed = changeCardLines({ ...status, risk: undefined, route: { level: "standard", tier: "medium", reasons: [] }, tests: { timestamp: "2026-10-29T05:00:00Z", passed: false, failed: ["backend", "web"], scope: "affected" } }, plain, 70, at).join("\n");
  assert.match(failed, /◆ abc-process-exercises\nstandard route · strict TDD\nrisk medium\nnext apply/, "without jhon risk's reasons, the route's tier");
  assert.match(failed, /Last run\n✗ 2 failed · affected · 26 min ago/);
  assert.doesNotMatch(failed, /\bgreen\b|\bred\b/);
  const unfinished = changeCardLines({ ...status, tests: { timestamp: "2026-10-29T05:00:00Z", passed: false, failed: [], scope: "affected", complete: false } }, plain, 70, at).join("\n");
  assert.match(unfinished, /Last run\n✗ failed · affected · 26 min ago\n  did not finish/);
  const many = changeCardLines({ ...status, risk: { tier: "medium", reasons: ["a.py", "b.py", "c.py", "d.py", "e.py"].map((file) => ({ level: "medium", file, why: "code" })) } }, plain, 200, at).join("\n");
  assert.match(many, /risk medium · a\.py \(code\) · b\.py \(code\) · c\.py \(code\) · \+2 more/, "the top three, and how many more");
});

test("the detail's keys act on its change: t runs its tests here, o opens tasks.md or says where it is; d does nothing (T174: the changes view is gone)", async () => {
  const started: string[] = [];
  const files: string[] = [];
  let closed = 0;
  let editor: string | undefined;
  const tests = { change: "abc-process-exercises", base: "main", projects: [{ project: "backend", tests: "own", modules: 2, checks: [] }] };
  const make = () => new JhonPanel({ theme: plain, runtime: "pi", view: "change", load: async () => data("default"), use: async () => undefined, loadChange: async () => status, height: () => 66, ...noGrants,
    loadTests: async () => tests, startTests: (d) => { started.push(`${d.change} ${d.base}`); return { id: "job-1", startedAt: 0 }; },
    openFile: (path) => { files.push(path); return editor; },
    onClose() { closed++; }, requestRender() {} });
  let panel = make();
  panel.open();
  await flush();
  assert.match(hintRow(panel.render(100)), /t tests {2}o tasks\.md {2}r refresh {2}esc close/);

  panel.handleInput("t");
  await flush(); await flush();
  assert.deepEqual(started, ["abc-process-exercises main"], "the owner's runner, once its plan is read");
  // G's detail card keeps to 24 rows: the run is at the body's end, scrolled to.
  for (let i = 0; i < 40; i++) panel.handleInput("j");
  const text = panel.render(100).join("\n");
  assert.match(text, /◐ running/, "the run shows in the detail");
  assert.match(text, /abc-process-exercises/, "under the change");
  panel.dispose();

  panel = make();
  panel.open();
  await flush();
  panel.handleInput("d");
  assert.equal(closed, 0, "d opens nothing: the panel stays");

  panel = make();
  panel.open();
  await flush();
  editor = "No editor: set $VISUAL or $EDITOR.";
  panel.handleInput("o");
  assert.deepEqual(files, ["/p/acme/openspec/changes/abc-process-exercises/tasks.md"]);
  for (let i = 0; i < 40; i++) panel.handleInput("j");
  assert.match(panel.render(120).join("\n"), /tasks\.md is \/p\/acme\/openspec\/changes\/abc-process-exercises\/tasks\.md: No editor: set \$VISUAL or \$EDITOR\./, "with no editor, its path");
  assert.equal(closed, 0, "with no editor the panel stays open");

  const none = new JhonPanel({ theme: plain, runtime: "pi", view: "change", load: async () => data("default"), use: async () => undefined, loadChange: async () => ({ change: null }), height: () => 66, ...noGrants,
    openFile: (path) => { files.push(path); return undefined; }, onClose() { closed++; }, requestRender() {} });
  none.open();
  await flush();
  for (const k of ["t", "d", "o"]) none.handleInput(k);
  assert.doesNotMatch(hintRow(none.render(100)), /t tests|d diff|o tasks/, "no change, no actions");
  assert.equal(files.length, 1, "nothing more ran");
  assert.equal(started.length, 1);
});

// T31.3 (B): the Record row says what this change needs (status `needs`): needed and missing marked, needed and done done, not needed absent.
test("Record shows what this change needs: a needed one missing is marked, a needed one written is done, one not needed is absent (T31 B)", () => {
  const needs = { tasks: true, spec: true, proposal: false, design: true, why: { tasks: ["risk high"], spec: ["a main spec changes"], proposal: [], design: ["the route asks"] } };
  const artifacts = [{ artifact: "exploration.md", state: "missing" }, { artifact: "proposal.md", state: "missing" }, { artifact: "specs/", state: "done" },
    { artifact: "design.md", state: "missing" }, { artifact: "tasks.md", state: "done" }, { artifact: "tdd-evidence.md", state: "missing" }, { artifact: "verify-report.md", state: "missing" }];
  const lines = changeCardLines({ ...status, route: { level: "standard", design: true, reasons: [] }, needs, artifacts, folder: true }, plain, 100);
  const record = lines[lines.indexOf("Record") + 1];
  assert.equal(record, "✓ specs   ◉ design missing   ✓ tasks", "specs and tasks needed and written, design needed and missing; proposal, exploration, TDD evidence and verify report not needed, so absent");
  const verified = changeCardLines({ ...status, needs, artifacts }, plain, 100);
  assert.match(verified[verified.indexOf("Record") + 1]!, /✓ specs   ◉ design missing   ✓ tasks   ◉ verify report missing$/, "the verified route needs its verify report");
});

test("a change with no folder says so under its name, and its Record is its RED and GREEN runs instead of artifacts (T31 B, L254)", () => {
  const missing = ["exploration.md", "research.md", "proposal.md", "specs/", "design.md", "tasks.md", "tdd-evidence.md", "verify-report.md"].map((artifact) => ({ artifact, state: "missing" }));
  const needs = { tasks: false, spec: false, proposal: false, design: false, why: { tasks: [], spec: [], proposal: [], design: [] } };
  const base = { change: "feat-small-fix", directory: null, folder: false, next: "apply", route: { level: "standard", reasons: [] }, tasks: { total: 0, completed: 0, slices: [], slice: null }, artifacts: missing, needs, notes: [] };
  const record = (s: any) => { const l = changeCardLines(s, plain, 100); return l[l.indexOf("Record") + 1]; };
  const lines = changeCardLines({ ...base, tests: null }, plain, 100);
  assert.equal(lines[1], "no folder · branch names it", "under its name");
  assert.equal(record({ ...base, tests: null }), "◉ RED test missing: no run on record");
  assert.equal(record({ ...base, tests: { timestamp: "2026-10-29T05:00:00Z", passed: false, failed: ["t"], red_before_green: false } }), "✓ RED   · GREEN next");
  assert.equal(record({ ...base, tests: { timestamp: "2026-10-29T05:00:00Z", passed: true, failed: [], red_before_green: false } }), "◉ GREEN without a RED before it");
  assert.equal(record({ ...base, next: "merge", tests: { timestamp: "2026-10-29T05:00:00Z", passed: true, failed: [], red_before_green: true } }), "✓ RED → GREEN");
  assert.equal(record({ ...base, needs: { ...needs, tasks: true }, tests: null }), "◉ RED test missing: no run on record   ◉ tasks missing", "a record it needs is still owed");
  assert.match(changeCardLines({ ...base, next: "merge", tests: null }, plain, 100).join("\n"), /\nnext merge\n/, "merge as the engine names it");
  assert.doesNotMatch(changeCardLines({ ...base, tests: null }, plain, 100).join("\n"), /exploration|verify report/);
});

test("a change with no folder lists [] artifacts, and its Record still owes what needs asks for (T30, T31b)", () => {
  const needs = { tasks: true, spec: true, proposal: false, design: false, why: { tasks: ["risk high"], spec: ["a main spec changes"], proposal: [], design: [] } };
  const base = { change: "feat-small-fix", directory: null, folder: false, next: "apply", route: { level: "standard", reasons: [] }, tasks: { total: 0, completed: 0, slices: [], slice: null }, artifacts: [], needs, notes: [], tests: null };
  const record = (s: any) => { const l = changeCardLines(s, plain, 100); return l[l.indexOf("Record") + 1]; };
  assert.equal(record(base), "◉ RED test missing: no run on record   ◉ specs missing   ◉ tasks missing");
  assert.equal(record({ ...base, needs: { ...needs, spec: false } }), "◉ RED test missing: no run on record   ◉ tasks missing");
  assert.equal(record({ ...base, needs: { ...needs, tasks: false, spec: false } }), "◉ RED test missing: no run on record", "nothing needed, nothing owed");
});

test("the selected change's sections stay together under its header, and Also in progress comes last, after Notes (L316)", () => {
  const lines = changeCardLines({ ...status, lanes: [{ change: "fix-svcb-encoding", worktree: "svcb", done: 1, slices: 1, next: "archive" }] }, plain, 100);
  const at = (s: string) => lines.findIndex((l) => l.startsWith(s));
  const order = ["◆ abc-process-exercises", "verified route", "risk high", "next apply", "Slices", "Record", "Last run", "Notes", "Also in progress"].map(at);
  assert.ok(order.every((i) => i >= 0), `every section shown: ${order}`);
  assert.deepEqual(order, [...order].sort((a, b) => a - b), `in this order:\n${lines.join("\n")}`);
  assert.match(lines.at(-1)!, /fix-svcb-encoding/, "the lane is the card's last row");
});

test("o opens tasks.md only when the change has one: a change with no tasks.md hides it (T31.3)", async () => {
  const files: string[] = [];
  const noTasks = { ...status, artifacts: status.artifacts.map((a) => a.artifact === "tasks.md" ? { ...a, state: "missing" } : a) };
  for (const s of [noTasks, { ...noTasks, directory: null, folder: false }]) {
    const panel = new JhonPanel({ theme: plain, runtime: "pi", view: "change", load: async () => data("default"), use: async () => undefined, loadChange: async () => s, height: () => 66, ...noGrants,
      openFile: (path) => { files.push(path); return undefined; }, onClose() {}, requestRender() {} });
    panel.open();
    await flush();
    assert.doesNotMatch(hintRow(panel.render(100)), /o tasks\.md/);
    assert.match(hintRow(panel.render(100)), /t tests {2}r refresh/);
    panel.handleInput("o");
    panel.dispose();
  }
  assert.deepEqual(files, [], "o opens nothing");
});
