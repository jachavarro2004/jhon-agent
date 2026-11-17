// Unit tests for dashboard/app.js's pure helpers: the formats, the day
// groups, the English words for checks and changes, and the verdict.
//
//   jhon test

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { attentionOf, dayOf, dur, esc, kb, pct, tok, verdictOf, whenOf } from "../app.js";

test("formats read the way the page shows them", () => {
  assert.equal(esc(`<a href="x">'&'</a>`), "&#60;a href=&#34;x&#34;&#62;&#39;&#38;&#39;&#60;/a&#62;");
  assert.equal(esc(undefined), "-");
  assert.deepEqual([tok(0), tok(950), tok(18_400_000), tok(2_500_000_000)], ["0", "950", "18.4M", "2.50B"]);
  assert.deepEqual([kb(900), kb(4_800_000)], ["900 B", "4.8 MB"]);
  assert.deepEqual([dur(0), dur(38 * 60000), dur(192 * 60000)], ["0m", "38m", "3h 12m"]);
  assert.equal(pct(0.414), "41%");
});

test("sessions group by day, and a row shows the hour, the weekday, or the date", () => {
  const now = new Date(2026, 9, 29, 10, 0);
  const at = (d, h) => new Date(2026, 9, d, h, 30).toISOString();
  assert.deepEqual([dayOf(at(29, 8), now), dayOf(at(28, 23), now), dayOf(at(25, 9), now), dayOf(at(10, 9), now)],
    ["TODAY · 今日", "YESTERDAY · 昨日", "THIS WEEK · 今週", "EARLIER · 以前"]);
  assert.equal(whenOf(at(29, 8), now), "08:30");
  assert.equal(whenOf(at(25, 9), now), "SUN");
  assert.equal(whenOf(at(10, 9), now), "10/10");
});

test("each reason a change needs a look is in English", () => {
  assert.deepEqual(attentionOf({ verify: { fail: 1 }, record: { passed: false, timeouts: 1 }, tests: { failed: 2, timeouts: 1 }, relaunches: 3 }),
    ["verify failed 1 time", "3 relaunch(es)", "the last test run failed", "2 test(s) timed out", "2 failed test run(s)"]);
  assert.deepEqual(attentionOf({ verify: { pass: 1 }, record: { passed: true } }), []);
});

test("the verdict reads a broken rule first, then what to look at, then clean", () => {
  const view = (violation, suspect, changes = [], status = "findings") => ({ status, summary: { findings: { violation, suspect } }, changes });
  const broken = verdictOf(view(1, 2));
  assert.equal(broken.seal, "破");
  assert.equal(broken.broken, true);
  assert.equal(broken.text, "The audit proved one broken rule and 1 more thing to look at.");
  assert.equal(broken.go, "hallazgos");
  assert.equal(verdictOf(view(0, 1)).seal, "疑");
  assert.equal(verdictOf(view(0, 0, [{ verify: { fail: 1 } }], "clean")).go, "cambios");
  assert.equal(verdictOf(view(0, 0, [], "clean")).go, undefined);
  assert.equal(verdictOf(view(0, 0, [{ verify: { fail: 1 } }], "clean")).seal, "疑");
  assert.equal(verdictOf(view(0, 0, [], "clean")).seal, "済");
  assert.equal(verdictOf(view(0, 0, [], "nothing-to-audit")).seal, "無");
  assert.equal(verdictOf({ status: "audit-error", summary: {} }).seal, "誤");
});

test("what to look at comes in order: a broken rule, then each change's troubles, then suspects, the cost outside changes, and the notes", async () => {
  const { lookItems } = await import("../app.js");
  const view = {
    summary: { findings: { violation: 1, suspect: 2 } },
    changes: [{ name: "c1", relaunches: 6, record: { passed: false }, tests: { failed: 2 }, verify: {} }],
    unattributed: { processed: 132e6, share: 0.76 },
    errors: [], notes: ["a", "b"],
  };
  assert.deepEqual(lookItems(view).map((i) => [i.seal, i.tab]), [["破", "hallazgos"], ["再", "cambios"], ["赤", "cambios"], ["疑", "hallazgos"], ["外", "uso"], ["注", "hallazgos"]]);
  assert.match(lookItems(view)[4].title, /132\.0M tokens outside every change \(76%\)/);
  assert.deepEqual(lookItems({ summary: { findings: {} }, changes: [] }), []);
});

test("the tokens split outside every change first, then each change, largest first", async () => {
  const { tokenSplit } = await import("../app.js");
  const split = tokenSplit({ summary: { processed: 100 }, unattributed: { processed: 60, share: 0.6 }, changes: [{ name: "a", processed: 10, share: 0.1 }, { name: "b", processed: 30, share: 0.3 }] });
  assert.equal(split.total, 100);
  assert.deepEqual(split.parts.map((p) => [p.name, p.share]), [["Outside every change", 0.6], ["b", 0.3], ["a", 0.1]]);
});

test("the timeline places each change by its start and end within the span they cover", async () => {
  const { timelineOf } = await import("../app.js");
  const tl = timelineOf({ changes: [
    { name: "a", start: "2026-10-29T10:00:00Z", end: "2026-10-29T11:00:00Z" },
    { name: "b", start: "2026-10-29T11:00:00Z", end: "2026-10-29T14:00:00Z", relaunches: 1 },
    { name: "c" },
  ] });
  assert.deepEqual(tl.rows.map((r) => [r.name, r.left, r.width, r.look]), [["a", 0, 0.25, false], ["b", 0.25, 0.75, true]]);
  assert.equal(timelineOf({ changes: [{ name: "c" }] }), null);
});

test("the agents filter by type, change, id, and long runs, and sort by any column", async () => {
  const { filterAgents, isLong, agentsCsv, LONG_TURNS } = await import("../app.js");
  const agents = [
    { id: "a1", type: "worker", change: "c1", turns: 104, processed: 30 },
    { id: "a2", type: "worker", change: "c1", turns: 12, processed: 50 },
    { id: "b1", type: "explore", change: null, turns: 36, processed: 10, long: "201K context in one turn" },
  ];
  assert.equal(LONG_TURNS, 100, "the audit's review mark: no executor has a turn cap");
  assert.deepEqual(agents.map(isLong), [true, false, true]);
  assert.deepEqual(filterAgents(agents).map((a) => a.id), ["a2", "a1", "b1"], "the heaviest first by default");
  assert.deepEqual(filterAgents(agents, { type: "worker", sort: ["turns", 1] }).map((a) => a.id), ["a2", "a1"]);
  assert.deepEqual(filterAgents(agents, { change: "" }).map((a) => a.id), ["b1"], "outside every change");
  assert.deepEqual(filterAgents(agents, { change: "c1", longOnly: true }).map((a) => a.id), ["a1"]);
  assert.deepEqual(filterAgents(agents, { q: "b" }).map((a) => a.id), ["b1"]);
  assert.equal(agentsCsv([{ id: "a1", type: "worker", change: 'c,"1"', turns: 2 }]).split("\n")[1], 'a1,worker,"c,""1""",,2,,,,,');
});

test("a listed session's state comes from its summary, and its queue reason names what went wrong", async () => {
  const { sessionState, queueReason } = await import("../app.js");
  assert.deepEqual([undefined, { violation: 1 }, { suspect: 1 }, { look: 1 }, { status: "clean" }, { status: "audit-error" }].map(sessionState), ["pending", "broken", "review", "review", "clean", "error"]);
  assert.equal(queueReason({ checks: [{ check: "delivered-untested", title: "Delivered untested", violation: 1 }] }), "delivered untested");
  assert.equal(queueReason({ relaunches: 6, record_failed: true, checks: [{ check: "refusal-bypass", title: "Refusal bypassed", suspect: 1 }] }), "6 relaunch(es), failed tests, 1 refusal bypassed");
  assert.equal(queueReason({ checks: [{ check: "new-check", violation: 1 }] }), "new-check", "a summary from before titles names the check");
});

test("Inicio counts the sessions in its range: the queue broken first, the heat by weekday and 2h slot", async () => {
  const { homeView } = await import("../app.js");
  const now = new Date(2026, 9, 29, 12, 0);
  const at = (d, h) => new Date(2026, 9, d, h, 30).toISOString();
  const sessions = [
    { id: "1", name: "acme", modified: at(29, 9), summary: { violation: 0, suspect: 1, processed: 100, outside: 60, active_ms: 3600000, elapsed_ms: 7200000, checks: [] } },
    { id: "2", name: "acme", modified: at(27, 21), summary: { violation: 1, processed: 50, outside: 0, active_ms: 0, elapsed_ms: 0, checks: [{ check: "delivered-untested", title: "Delivered untested", violation: 1 }] } },
    { id: "3", name: "jhon", modified: at(28, 0) },
    { id: "4", name: "jhon", modified: at(1, 10), summary: { violation: 3 } },
  ];
  const h = homeView(sessions, "7d", now);
  assert.deepEqual([h.sessions, h.projects, h.pending, h.processed, h.outside, h.broken], [3, 2, 1, 150, 60, 1]);
  assert.deepEqual(h.queue.map((x) => [x.s.id, x.state]), [["2", "broken"], ["1", "review"]]);
  assert.deepEqual(h.brokenChecks, [{ title: "Delivered untested", n: 1 }]);
  assert.equal(h.heat[3][4], 1, "Thursday 09:30 falls in the 8-10 slot");
  assert.equal(h.byProject, undefined, "nothing rendered it (T17b, H7)");
  assert.equal(homeView(sessions, "hoy", now).sessions, 1);
  assert.equal(homeView(sessions, "todo", now).broken, 4);
});

test("a change's slices: done, the first unfinished current, the rest pending; jhon status notes read in English", async () => {
  const { sliceStates, notesOf } = await import("../app.js");
  const l = { slices: [{ total: 4, completed: 4 }, { total: 1, completed: 0 }, { total: 2, completed: 1 }, { total: 0, completed: 0 }] };
  assert.deepEqual(sliceStates(l), { states: ["done", "current", "pending", "pending"], done: 1 });
  assert.deepEqual(sliceStates({}), { states: [], done: 0 });
  assert.deepEqual(notesOf({ notes: ["a"] }), ["a"], "the engine's notes as written");
  assert.deepEqual(notesOf({ notes: ["a"] }), ["a"]);
  assert.deepEqual(notesOf({}), []);
});

test("the final design's session: the verdict in one line", async () => {
  const { verdictLine } = await import("../app.js");
  const view = { status: "findings", summary: { findings: { violation: 0, suspect: 1 } }, changes: [{ name: "a", processed: 10, relaunches: 2 }, { name: "b", processed: 90 }], unattributed: null };
  assert.equal(verdictLine(view), "Nothing broken · 2 things to look at, below in order.");
  assert.equal(verdictLine({ summary: { findings: { violation: 1 } }, changes: [] }), "One broken rule · nothing else to look at.");
});

test("the session page shows what the session did: no change in progress, no next step, no state today (L138, answer 4)", async () => {
  const app = await import("../app.js");
  assert.equal(app.currentChange, undefined, "the Summary's change in progress is gone");
  const s = (n, t, c) => ({ name: `Slice ${n}: s${n}`, total: t, completed: c });
  const view = { status: "clean", summary: { findings: {}, processed: 100 }, changes: [{ name: "b", processed: 90, share: 0.9 }], unattributed: null, errors: [], notes: [],
    ldd: { b: { slices: [s(1, 1, 1), s(2, 1, 0)], next: "apply", slice: "Slice 2: s2", completed: 1, total: 2 } } };
  const summary = app.summaryPanel(view), changes = app.changesPanel(view);
  for (const [name, html] of [["Summary", summary], ["Changes tab", changes]]) {
    assert.doesNotMatch(html, /Change in progress|Next step|next <b>|today|LDD/, `${name} shows no live state`);
  }
  assert.match(changes, /<code>b<\/code>/, "the Changes tab keeps the per-change audit table");
});

test("today's change state lives on the Changes pages, labelled as today's", async () => {
  const { lddCard, CHANGE_HEAD } = await import("../app.js");
  const card = lddCard("b", { slices: [{ name: "Slice 1: a", total: 1, completed: 1 }, { name: "Slice 2: b", total: 2, completed: 1 }], next: "apply", slice: "Slice 2: b", completed: 2, total: 3, artifacts: [], notes: [] });
  assert.match(card, /Today/);
  assert.match(card, /Next step: <b>apply<\/b>/);
  assert.match(CHANGE_HEAD, /Slices today/);
  assert.match(CHANGE_HEAD, /Last run today/);
});

test("an outline current slice is pending, never all slices done (L842 A1)", async () => {
  const { lddCard } = await import("../app.js");
  const done = { name: "Slice 1: a", total: 1, completed: 1 };
  const outline = lddCard("b", { slices: [done, { name: "Slice 2: b", total: 0, completed: 0 }], slice: null, current: "Slice 2: b", slicesDone: 1, next: "tasks", completed: 1, total: 1, artifacts: [], notes: [] });
  assert.doesNotMatch(outline, /All slices done/);
  assert.match(outline, /<p>To task: <b>Slice 2: b<\/b><\/p>/);
  assert.match(outline, /<b>1 of 2 slices<\/b>/);
  const all = lddCard("b", { slices: [done], slice: null, current: null, slicesDone: 1, next: "archive", completed: 1, total: 1, artifacts: [], notes: [] });
  assert.match(all, /All slices done\./);
  const old = lddCard("b", { slices: [done, { name: "Slice 2: b", total: 2, completed: 1 }], slice: "Slice 2: b", next: "apply", completed: 2, total: 3, artifacts: [], notes: [] });
  assert.match(old, /<p>In progress: <b>Slice 2: b<\/b><\/p>/, "a payload without current reads as before");
});

test("a session with nothing to audit still says what to look at in its verdict line", async () => {
  const { verdictLine } = await import("../app.js");
  const view = { status: "nothing-to-audit", summary: { findings: {} }, changes: [{ name: "a", record: { passed: false } }] };
  assert.equal(verdictLine(view), "Nothing broken · 1 thing to look at, below in order.");
});

test("a run whose tests passed on uncommitted changes is not one that failed", async () => {
  const { lookItems, attentionOf } = await import("../app.js");
  const view = { summary: { findings: {} }, changes: [{ name: "a", record: { passed: false, tests_passed: true } }] };
  assert.deepEqual(lookItems(view).map((i) => [i.seal, i.color]), [["未", "var(--gold)"]]);
  assert.deepEqual(attentionOf(view.changes[0]), ["the last run passed on uncommitted changes"]);
  assert.equal(lookItems({ summary: { findings: {} }, changes: [{ name: "a", record: { passed: false } }] })[0].seal, "赤");
});

test("the owner's yeses to the guard read one line each: the classes, once or a 30-minute grant, when, and what was asked", async () => {
  const { authorizationLines } = await import("../app.js");
  const now = new Date("2026-10-29T23:00:00Z");
  const view = { authorizations: [
    { classes: ["push"], scope: "once", at: "2026-10-29T22:10:00Z", ask: "push: git push" },
    { classes: ["push", "gh-write"], scope: "grant", at: "2026-10-29T22:20:00Z", ask: "push: git push; gh-write: gh pr create" },
  ] };
  const lines = authorizationLines(view, now);
  assert.match(lines[0], /^push · this once only · \d\d:\d\d · push: git push$/);
  assert.match(lines[1], /^push and gh-write · allowed for 30 min · /);
  assert.deepEqual(authorizationLines({}), []);
});

test("what worked: each success with where it happened and its evidence", async () => {
  const { workedItems } = await import("../app.js");
  const view = { worked: [
    { kind: "delivery", change: "c1", what: "merge ready", evidence: "jhon deliver check at T on 0123abcd" },
    { kind: "guards", change: null, what: "safety-guard stopped 2 call(s) and the agent went on", evidence: "within 5s" },
    { kind: "new-kind", change: null, what: "x", evidence: "y" },
  ] };
  assert.deepEqual(workedItems(view), [
    { title: "Delivery · c1: merge ready", text: "jhon deliver check at T on 0123abcd" },
    { title: "Guards: safety-guard stopped 2 call(s) and the agent went on", text: "within 5s" },
    { title: "new-kind: x", text: "y" },
  ]);
  assert.deepEqual(workedItems({}), []);
});

test("how it ran: the main thread, sub-agent start-up and turns, the compactions", async () => {
  const { timingRows } = await import("../app.js");
  const thread = (o) => ({ agent: "a", agent_type: "worker", change: null, turns: 0, model_ms: 0, tool_ms: 0, first_response_ms: null, processed: 0, cache_read: 0, cache_hit: null, ...o });
  const view = { timing: {
    main: thread({ turns: 10, model_ms: 100000, tool_ms: 30000, cache_hit: 0.99 }),
    agents: [thread({ turns: 4, model_ms: 40000, tool_ms: 2000, first_response_ms: 20000 }), thread({ agent_type: "verify", turns: 6, model_ms: 60000, tool_ms: 1000, first_response_ms: 31000 }), thread({ first_response_ms: 25000 })],
    compactions: [{ at: "2026-10-30T10:00:00Z", trigger: "extension", tokens_before: 120400, duration_ms: 0 }],
  } };
  const rows = timingRows(view);
  assert.deepEqual(rows.map((r) => [r.label, r.value]), [
    ["Main thread", "10s per turn"], ["Sub-agent start-up", "25s to a first response"], ["Sub-agents", "10s per turn"], ["Compactions", "1"],
  ]);
  assert.equal(rows[0].note, "10 turn(s) · model 1.7m · tools 30s · cache 99%");
  assert.equal(rows[1].note, "median of 3 · slowest 31s (verify)");
  assert.equal(rows[2].note, "10 turn(s) · model 1.7m · tools 3s", "no usage recorded: no cache share");
  assert.match(rows[3].note, /^\d\d:\d\d 120k extension$/);
  assert.deepEqual(timingRows({ timing: { main: thread({}), agents: [], compactions: [] } }).map((r) => [r.label, r.value, r.note]),
    [["Main thread", "no turns", "0 turn(s) · model 0s · tools 0s"], ["Compactions", "none", "no compaction in this session"]]);
  assert.deepEqual(timingRows({}), []);
});

test("telemetry figures: refusals, green runs, ready checks, sub-agents at their cap, the slowest hook", async () => {
  const { telemetryFigures } = await import("../app.js");
  const f = telemetryFigures({
    hooks: [{ name: "safety-guard", count: 8, deny: 2, p95_ms: 30 }, { name: "route-guard", count: 2, deny: 0, p95_ms: 90 }, { name: "x", count: 0, deny: 0, p95_ms: null }],
    tests: { runs: 3, green: 2 }, deliveries: { checks: 1, ready: 1 },
    subagents: [{ agent: "worker", count: 3, turn_cap: 1 }, { agent: "verify", count: 1, turn_cap: 0 }],
  });
  assert.deepEqual(f, { decisions: 10, denies: 2, denyShare: 0.2, tests: { runs: 3, green: 2 }, deliveries: { checks: 1, ready: 1 },
    subagents: 4, capped: 1, cappedShare: 0.25, slowest: { name: "route-guard", p95_ms: 90 } });
  assert.deepEqual(telemetryFigures({}), { decisions: 0, denies: 0, denyShare: 0, tests: { runs: 0, green: 0 }, deliveries: { checks: 0, ready: 0 },
    subagents: 0, capped: 0, cappedShare: 0, slowest: null });
});

test("sessions: yours, under the hood, scratch; filtered by state, project, runtime, and text", async () => {
  const { originOf, isScratch, filterSessions, changesInFlight } = await import("../app.js");
  const at = (h) => `2026-10-30T${String(h).padStart(2, "0")}:00:00Z`;
  const s = (id, o) => ({ id, name: "acme", project: "-home-u-acme", runtime: "claude-code", entrypoint: "cli", modified: at(1), bytes: 1, summary: null, ...o });
  const all = [
    s("a", { modified: at(5), summary: { status: "clean", violation: 0, suspect: 0, look: 0, change: "c1" } }),
    s("b", { runtime: "pi", entrypoint: undefined, modified: at(4), summary: { status: "findings", violation: 0, suspect: 1, look: 0, change: "c1" } }),
    s("c", { entrypoint: "sdk-ts", modified: at(3) }),
    s("d", { entrypoint: "sdk-cli", modified: at(2) }),
    s("e", { project: "-home-u--claude-jobs-x-tmp-pitest", name: "claude-jobs-x-tmp-pitest", modified: at(6) }),
    s("f", { entrypoint: "", name: "designs", title: "Mira los designs", modified: at(0), summary: { status: "clean", change: "c2" } }),
  ];
  assert.deepEqual(all.map(originOf), ["yours", "yours", "bridge", "headless", "yours", "yours"]);
  assert.deepEqual(all.map(isScratch), [false, false, false, false, true, false]);
  const ids = (f) => filterSessions(all, f).map((x) => x.id).join("");
  assert.equal(ids({}), "abf", "yours, scratch hidden, newest first");
  assert.equal(ids({ show: "hood" }), "cd");
  assert.equal(ids({ show: "all", scratch: true }), "eabcdf");
  assert.equal(ids({ state: "review" }), "b");
  assert.equal(ids({ runtime: "pi" }), "b");
  assert.equal(ids({ project: "designs" }), "f");
  assert.equal(ids({ q: "C1" }), "ab", "a change's name finds its sessions");
  assert.equal(ids({ q: "mira" }), "f");
  assert.deepEqual(changesInFlight(all).map((c) => [c.change, c.latest.id, c.sessions]), [["c1", "a", 2], ["c2", "f", 1]]);
});

test("the verdict and its line read one count: tokens outside every change are something to look at in both (pi fixture 01a17221)", async () => {
  const { verdictOf, verdictLine } = await import("../app.js");
  const view = { status: "clean", summary: { findings: { violation: 0, suspect: 0 } }, changes: [], unattributed: { processed: 72e3, share: 1 }, errors: [], notes: [] };
  const v = verdictOf(view);
  assert.notEqual(v.seal, "済", "not clean while the line names something to look at");
  assert.equal(v.title, "Nothing broken, but something to look at");
  assert.equal(v.text, "72K tokens outside every change (100%).");
  assert.equal(v.go, "uso");
  assert.equal(verdictLine(view), "Nothing broken · 1 thing to look at, below in order.");
  const clean = { ...view, unattributed: { processed: 1e3, share: 0.1 } };
  assert.equal(verdictOf(clean).seal, "済");
  assert.equal(verdictLine(clean), "Nothing broken · nothing else to look at.");
  const notes = { ...clean, notes: ["a"], errors: ["b"] };
  assert.equal(verdictOf(notes).text, "2 things to look at, first: 1 audit error(s).");
});

test("Copy resume copies the command that resumes the session in its own runtime", async () => {
  const { resumeCommand } = await import("../app.js");
  assert.equal(resumeCommand({ id: "01a17221-ea5a", runtime: "pi" }), "jhon start pi --session 01a17221-ea5a");
  assert.equal(resumeCommand({ id: "0b712a44-f695", runtime: "claude-code" }), "jhon start --resume 0b712a44-f695");
  assert.equal(resumeCommand({ id: "0b712a44-f695" }), "jhon start --resume 0b712a44-f695");
});

test("a check reads in View's words, one wording for the page, the list and Home (T17b, V3)", async () => {
  const app = await import("../app.js");
  assert.equal(app.CHECKS, undefined, "the page keeps no titles of its own");
  assert.equal(app.checkOf, undefined);
  const view = { findings: [{ check: "isolation-bypass", title: "Isolation left", explain: "An agent confined to a worktree wrote outside it.", items: [{ severity: "violation", where: "w1", agent: "a", message: "m" }] }] };
  const html = app.findingsPanel("s1", view);
  assert.match(html, /<h3>Isolation left<\/h3>/);
  assert.match(html, /An agent confined to a worktree wrote outside it\./);
});

test("what T17b removed stays removed: Export JSON, the font size and density, the dead styles (X1c, K5, V2)", () => {
  const js = readFileSync(fileURLToPath(new URL("../app.js", import.meta.url)), "utf8");
  for (const gone of ["data-export", "Export JSON", "data-fs", "data-density", "applyPrefs", "byProject"]) assert.ok(!js.includes(gone), `app.js still holds ${gone}`);
  const css = readFileSync(fileURLToPath(new URL("../app.css", import.meta.url)), "utf8");
  for (const sel of ["list", "lh", "chg", "flag", "fold", "gl", "mono", "ph", "rt", "sorts", "tlink", "it", "items", "folded"]) {
    assert.doesNotMatch(css, new RegExp(`\\.${sel}(?![\\w-])`), `app.css still styles .${sel}, which no element has`);
  }
  assert.doesNotMatch(css, /^\.compact /m, "the compact density is gone");
  assert.doesNotMatch(css, /--fs\b/, "the font size preference is gone");
});

test("the session list reads the page's rule: a session with something to look at is not clean (pi fixture 01a17221, L139)", async () => {
  const { sessionState, verdictOf, STATES } = await import("../app.js");
  // 01a17221's summary: no finding, no change, 72K tokens all outside every change.
  const pi = { status: "clean", violation: 0, suspect: 0, look: 0, processed: 72e3, outside: 72e3, notes: 0, errors: 0 };
  assert.equal(sessionState(pi), "review");
  assert.equal(sessionState({ ...pi, outside: 1e3 }), "clean");
  assert.equal(sessionState({ ...pi, outside: 1e3, notes: 1 }), "review");
  assert.equal(sessionState({ ...pi, outside: 1e3, errors: 1 }), "review");
  // Each view and its summary, as the engine writes it (SummaryOf): the list's state and the page's verdict agree.
  const pairs = [
    [{ status: "clean", summary: { findings: {}, processed: 72e3 }, changes: [], unattributed: { processed: 72e3, share: 1 }, errors: [], notes: [] }, pi],
    [{ status: "clean", summary: { findings: {}, processed: 10 }, changes: [{ name: "a", processed: 10, attention: ["1 failed test run(s)"], tests: { failed: 1 } }], unattributed: null, errors: [], notes: [] },
      { status: "clean", violation: 0, suspect: 0, look: 1, processed: 10, outside: 0, notes: 0, errors: 0 }],
    [{ status: "clean", summary: { findings: {}, processed: 10 }, changes: [], unattributed: null, errors: [], notes: [] },
      { status: "clean", violation: 0, suspect: 0, look: 0, processed: 10, outside: 0, notes: 0, errors: 0 }],
    [{ status: "findings", summary: { findings: { violation: 1 }, processed: 10 }, changes: [], unattributed: null, errors: [], notes: [] },
      { status: "findings", violation: 1, suspect: 0, look: 0, processed: 10, outside: 0, notes: 0, errors: 0 }],
  ];
  for (const [view, summary] of pairs) assert.equal(verdictOf(view).glyph, STATES[sessionState(summary)].glyph, JSON.stringify(summary));
});
