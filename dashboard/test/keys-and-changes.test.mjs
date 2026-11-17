// Unit tests for the dashboard's keys, command bar, changes page, one
// vocabulary (S29), loading and errors, and its colors (T23, S30).
//
//   timeout 120 node --test dashboard/test/keys-and-changes.test.mjs

import { test, mock } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { barResults, changeFacts, debounce, KEYS, keyAction, loadJSON, lookItems, runWords, skeleton } from "../app.js";

test("a test run reads in the one vocabulary: ✓ passed, ✗ N failed, nothing without a run", () => {
  assert.deepEqual(runWords({ passed: true, failed: [] }), { mark: "✓", text: "passed", color: "var(--ok)", ok: true });
  assert.deepEqual(runWords({ passed: false, failed: ["api", "web"] }), { mark: "✗", text: "2 failed", color: "var(--broken)", ok: false });
  assert.deepEqual(runWords({ passed: false, failed: [] }), { mark: "✗", text: "failed", color: "var(--broken)", ok: false });
  assert.deepEqual(runWords({ passed: true, count: 3 }), { mark: "✓", text: "passed 3", color: "var(--ok)", ok: true });
  assert.equal(runWords(null), null);
  const failed = lookItems({ summary: { findings: {} }, changes: [{ name: "a", record: { passed: false }, tests: { failed: 2 } }] })[0];
  assert.doesNotMatch(failed.title + failed.text, /\bred\b/i, "no red tests anywhere");
  assert.match(failed.text, /failed tests/);
});

test("the page reads no green or red for a run, nowhere in its words", () => {
  const src = readFileSync(fileURLToPath(new URL("../app.js", import.meta.url)), "utf8");
  for (const old of ["✓ green", "◉ ${(t.failed", " red</span>", "red tests", ">GREEN<", ">RED<", "GREEN, UNCOMMITTED", "all green"]) assert.ok(!src.includes(old), `app.js still says ${old}`);
});

test("keys: j/k move, Enter opens, Esc goes back, g then h/s/c/t goes to a page, ctrl+k opens the bar; / 1-5 r ? keep their jobs", () => {
  const k = (key, o = {}) => ({ key, ctrlKey: false, metaKey: false, ...o });
  assert.deepEqual(keyAction(k("j")), { do: "move", by: 1, pending: "" });
  assert.deepEqual(keyAction(k("k")), { do: "move", by: -1, pending: "" });
  assert.deepEqual(keyAction(k("ArrowDown")), { do: "move", by: 1, pending: "" });
  assert.deepEqual(keyAction(k("Enter")), { do: "open", pending: "" });
  assert.deepEqual(keyAction(k("Escape")), { do: "back", pending: "" });
  assert.deepEqual(keyAction(k("g")), { do: null, pending: "g" });
  assert.deepEqual(["h", "s", "c", "t"].map((x) => keyAction(k(x), "g")), ["#/", "#/sessions", "#/changes", "#/telemetry"].map((to) => ({ do: "go", to, pending: "" })));
  assert.deepEqual(keyAction(k("x"), "g"), { do: null, pending: "" }, "an unknown second key drops the g");
  assert.deepEqual(keyAction(k("k", { ctrlKey: true })), { do: "bar", pending: "" });
  assert.deepEqual(keyAction(k("k", { metaKey: true })), { do: "bar", pending: "" });
  assert.deepEqual(keyAction(k("?")), { do: "help", pending: "" });
  assert.deepEqual(keyAction(k("/")), { do: "search", pending: "" });
  assert.deepEqual(keyAction(k("3")), { do: "tab", n: "3", pending: "" });
  assert.deepEqual(keyAction(k("r")), { do: "reaudit", pending: "" });
  assert.deepEqual(keyAction(k("j"), "", true), { do: null, pending: "" }, "typing in a field keeps its letters");
  assert.deepEqual(keyAction(k("Escape"), "", true), { do: "back", pending: "" }, "Esc leaves a field");
  assert.deepEqual(keyAction(k("k", { ctrlKey: true }), "", true), { do: "bar", pending: "" }, "ctrl+k works anywhere");
  const listed = KEYS.map(([key]) => key);
  for (const key of ["j / k", "enter", "esc", "g h", "g s", "g c", "g t", "ctrl k", "/", "1 … 5", "r", "?"]) assert.ok(listed.includes(key), `? lists ${key}`);
});

test("the command bar filters sessions and changes by text and offers Open, Sessions, Changes, Usage, Memory, Telemetry", () => {
  const sessions = [
    { id: "aaaaaaaa-1", name: "acme", title: "Fix the soft delete", modified: "2026-11-03T10:00:00Z", summary: { change: "soft-delete" } },
    { id: "bbbbbbbb-2", name: "jhon", title: "Rose ink", modified: "2026-11-02T10:00:00Z" },
  ];
  const changes = [{ name: "soft-delete", project: "acme", sessions: [{ id: "aaaaaaaa-1" }] }, { name: "rose-ink", project: "jhon", sessions: [{ id: "bbbbbbbb-2" }] }];
  const all = barResults("", sessions, changes);
  assert.deepEqual(all.actions.map((a) => a.label), ["Open", "Sessions", "Changes", "Usage", "Memory", "Telemetry"]);
  assert.deepEqual(all.actions.slice(1).map((a) => a.href), ["#/sessions", "#/changes", "#/usage", "#/memory", "#/telemetry"]);
  const soft = barResults("SOFT", sessions, changes);
  assert.deepEqual(soft.items.map((i) => [i.kind, i.label]), [["change", "soft-delete"], ["session", "Fix the soft delete"]]);
  assert.equal(soft.items[0].href, "#/c/acme/soft-delete");
  assert.equal(soft.items[1].href, "#/s/aaaaaaaa-1");
  assert.deepEqual(barResults("ink", sessions, changes).items.map((i) => i.label), ["rose-ink", "Rose ink"]);
  assert.deepEqual(barResults("nothing", sessions, changes).items, []);
});

test("a change's row: its slices, its last run in the vocabulary, and its route", () => {
  const ldd = { slices: [{ name: "Slice 1: a", total: 2, completed: 2 }, { name: "Slice 2: b", total: 1, completed: 0 }, { name: "Slice 3: c", total: 1, completed: 0 }], tests: { passed: false, failed: ["api"] }, route: "verified", next: "apply" };
  assert.deepEqual(changeFacts({ name: "c", ldd }), { slices: "slice 2/3", run: { mark: "✗", text: "1 failed", color: "var(--broken)", ok: false }, route: "verified", next: "apply" });
  assert.deepEqual(changeFacts({ name: "c", ldd: { slices: [{ total: 1, completed: 1 }], tests: { passed: true }, route: "standard" } }).slices, "1/1 done");
  assert.deepEqual(changeFacts({ name: "c", ldd: null }), { slices: "", run: null, route: "", next: "" }, "a change its project no longer holds");
});

test("the session search waits ~150 ms after the last key, and calls once", () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    const seen = [];
    const typed = debounce((q) => seen.push(q), 150);
    typed("a"); typed("ab"); typed("abc");
    mock.timers.tick(149);
    assert.deepEqual(seen, []);
    mock.timers.tick(1);
    assert.deepEqual(seen, ["abc"]);
  } finally {
    mock.timers.reset();
  }
});

test("a failed fetch is an error to show with a retry, never an empty list", async () => {
  const ok = await loadJSON("/api/sessions", async () => ({ ok: true, json: async () => [{ id: "a" }] }));
  assert.deepEqual(ok, { data: [{ id: "a" }] });
  const down = await loadJSON("/api/sessions", async () => { throw new TypeError("Failed to fetch"); });
  assert.deepEqual(down, { error: "Failed to fetch" });
  const bad = await loadJSON("/api/telemetry", async () => ({ ok: false, status: 500, text: async () => "boom\n" }));
  assert.deepEqual(bad, { error: "500: boom" });
});

test("loading is a skeleton the reader is told about", () => {
  const s = skeleton(3);
  assert.match(s, /aria-busy="true"/);
  assert.equal((s.match(/class="skel"/g) || []).length, 3);
});

test("app.css takes every color from the palette: no hex outside the fallback block, no #000, no hue outside rose ink's roles", () => {
  const css = readFileSync(fileURLToPath(new URL("../app.css", import.meta.url)), "utf8");
  const root = css.match(/:root\{[^}]*\}/)[0];
  const rest = css.replace(root, "");
  assert.deepEqual(rest.match(/#[0-9a-f]{3,8}\b/gi) || [], []);
  for (const role of ["--accent-soft", "--frame-soft"]) assert.match(root, new RegExp(`${role}:`), `${role} has a fallback`);
  const js = readFileSync(fileURLToPath(new URL("../app.js", import.meta.url)), "utf8");
  for (const hue of ["--aqua", "--violet", "--orange", "--blue"]) {
    assert.ok(!css.includes(hue) && !js.includes(hue), `${hue} is not a rose ink role (design/system.md)`);
  }
  assert.match(css, /\.ch h2\{[^}]*color:var\(--accent-soft\)/, "section headers in accentSoft");
  for (const frame of [".cbox", ".help", ".drawer"]) assert.match(css, new RegExp(`\\${frame}\\{[^}]*border:1px solid var\\(--frame-soft\\)`), `${frame} is framed in frameSoft`);
});

test("every state reads in the one table: a glyph and a word, the kanji seal beside them (design/system.md)", async () => {
  const { STATES, verdictOf, sessionRow, findingsPanel, lddCard, changesPanel } = await import("../app.js");
  assert.deepEqual(Object.fromEntries(Object.entries(STATES).map(([k, v]) => [k, [v.glyph, v.word, v.seal]])), {
    broken: ["✗", "broke a rule", "破"], review: ["◉", "needs a look", "疑"], clean: ["✓", "clean", "済"], error: ["?", "unreadable", "誤"], pending: ["·", "not audited", "—"],
  });
  const v = verdictOf({ status: "clean", summary: { findings: {} }, changes: [] });
  assert.deepEqual([v.glyph, v.word, v.seal], ["✓", "clean", "済"]);
  assert.equal(v.status, undefined, "no upper-case status");
  const row = sessionRow({ id: "a1", name: "demo", modified: "2026-11-07T10:00:00Z", summary: { violation: 1 } });
  assert.match(row, />✗ broke a rule</, "the row says the state in words on screen");
  assert.match(row, /破/, "the seal stays beside it");
  const f = findingsPanel("s", { findings: [{ check: "c", title: "T", explain: "E", items: [{ severity: "violation", where: "w" }, { severity: "suspect", where: "x" }] }] });
  assert.match(f, /✗ broken/);
  assert.match(f, /◉ to review/);
  assert.doesNotMatch(f, /BROKEN|REVIEW/);
  const card = lddCard("b", { slices: [{ name: "a", total: 1, completed: 1 }, { name: "b", total: 2, completed: 1 }], artifacts: [], notes: [], tests: { passed: true, at: "2026-11-07T10:00:00Z" }, completed: 2, total: 3 });
  assert.match(card, /<span class="k">◐<\/span>/, "the slice in progress is ◐");
  assert.doesNotMatch(card, /▶|PASSED/);
  const tab = changesPanel({ changes: [{ name: "a", verify: { pass: 1, fail: 2 }, record: { passed: false, tests_passed: true } }, { name: "b", record: { passed: true } }], summary: {} });
  assert.match(tab, /✗ 2 failed/);
  assert.match(tab, /✓ passed 1/);
  assert.match(tab, /✓ passed, uncommitted/);
  assert.doesNotMatch(tab, /PASSED|FAILED|UNCOMMITTED/, "no upper-case pills");
});

test("a change's route reads in its own word, not upper case", () => {
  const src = readFileSync(fileURLToPath(new URL("../app.js", import.meta.url)), "utf8");
  assert.ok(!src.includes(".toUpperCase()"), "no word is upper-cased for a pill");
});

test("◉ keeps the bad role everywhere, as in design/system.md's table: no glyph takes a second colour (L139)", async () => {
  const { STATES, verdictOf, findingsPanel } = await import("../app.js");
  assert.equal(STATES.review.color, "var(--broken)");
  const colours = new Map();
  for (const s of Object.values(STATES)) { if (colours.has(s.glyph)) assert.equal(colours.get(s.glyph), s.color, s.glyph); colours.set(s.glyph, s.color); }
  assert.equal(verdictOf({ status: "findings", summary: { findings: { suspect: 1 } }, changes: [] }).color, "var(--broken)");
  const f = findingsPanel("s", { findings: [{ check: "c", title: "T", explain: "E", items: [{ severity: "suspect", where: "x" }] }] });
  assert.match(f, /--c:var\(--broken\)/, "a suspect finding's ◉ is the bad role");
  assert.doesNotMatch(f, /--gold/);
  const src = readFileSync(fileURLToPath(new URL("../app.js", import.meta.url)), "utf8");
  assert.ok(!/orth a look/.test(src), "one word for ◉: needs a look");
});
