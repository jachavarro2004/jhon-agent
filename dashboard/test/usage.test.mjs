// Unit tests for the Usage page and a session's Usage tab (T17c, L138
// answers 2 and 3): the range, the money, the figures, the keys.
//
//   timeout 120 node --test dashboard/test/usage.test.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { barResults, KEYS, keyAction, lookItems, money, sessionUsage, usageFigures, usageQuery } from "../app.js";

const src = readFileSync(fileURLToPath(new URL("../app.js", import.meta.url)), "utf8");

test("money is what pi recorded, — where nothing was, and a sum says what it covers", () => {
  assert.equal(money(null), "—");
  assert.equal(money(undefined, "none"), "—");
  assert.equal(money(1.2, "pi only"), "$1.20 · pi only");
  assert.equal(money(0, "all"), "$0.00");
  assert.equal(money(12.345), "$12.35");
});

test("the Usage page asks for Home's range and the Sessions page's project", () => {
  const now = new Date(2026, 10, 7, 10, 0);
  assert.equal(usageQuery("hoy", "", now), "/api/usage?from=2026-11-07&to=2026-11-07");
  assert.equal(usageQuery("7d", "jhon", now), "/api/usage?from=2026-11-01&to=2026-11-07&project=jhon");
  assert.equal(usageQuery("30d", "a b", now), "/api/usage?from=2026-10-09&to=2026-11-07&project=a%20b");
  assert.equal(usageQuery("todo", "", now), "/api/usage?to=2026-11-07");
});

test("the figures: tokens and their split, money, active days, streaks, most active day, longest session", () => {
  const stats = {
    sessions: 3, pending: 1, totals: { input: 10, output: 20, cache_read: 1_500_000, cache_write: 3000, total: 1_503_030, cost: 1.2, messages: 7 }, cost_covers: "pi only",
    active_days: 4, days_in_range: 7, longest_streak: 3, current_streak: 1,
    most_active_day: { day: "2026-11-03", total: 900_000 }, longest_session: { id: "c1", title: "Fix it", project: "jhon", active_ms: 3_600_000 },
    models: [{ model: "claude-opus-5-5" }],
  };
  assert.deepEqual(usageFigures(stats), [
    { label: "Tokens", value: "1.5M", note: "in 3 session(s) · 1 not audited yet" },
    { label: "Money", value: "$1.20 · pi only", note: "recorded by pi; Claude Code records none" },
    { label: "Active days", value: "4/7", note: "longest streak 3 days · current 1 day" },
    { label: "Most active day", value: "3 Nov", note: "900K tokens" },
    { label: "Longest session", value: "1h 0m", note: "Fix it · jhon", href: "#/s/c1" },
    { label: "Favourite model", value: "claude-opus-5-5", note: "10 in · 20 out · 1.5M cache read · 3K cache write" },
  ]);
  const empty = usageFigures({ sessions: 0, pending: 0, totals: { total: 0, cost: null }, cost_covers: "none", active_days: 0, days_in_range: 1, longest_streak: 0, current_streak: 0, most_active_day: null, longest_session: null, models: [] });
  assert.deepEqual(empty.map((f) => f.value), ["0", "—", "0/1", "—", "—", "—"]);
  assert.equal(empty[1].note, "Claude Code records no money; pi recorded none here");
});

test("a session's usage: its models, money, messages and token breakdown", () => {
  const u = sessionUsage({ rows: [
    { day: "2026-11-01", model: "opus", input: 1, output: 2, cache_read: 3, cache_write: 4, total: 10, cost: 0.5, messages: 2 },
    { day: "2026-11-02", model: "opus", input: 1, output: 1, cache_read: 1, cache_write: 1, total: 4, cost: 0.25, messages: 1 },
    { day: "2026-11-02", model: "haiku", input: 30, output: 0, cache_read: 0, cache_write: 0, total: 30, cost: null, messages: 1 },
  ] });
  assert.equal(u.model, "haiku");
  assert.deepEqual(u.totals, { input: 32, output: 3, cache_read: 4, cache_write: 5, total: 44, cost: 0.75, messages: 4 });
  assert.equal(u.covers, "pi only");
  assert.deepEqual(u.models.map((m) => [m.model, m.total, m.cost, m.messages]), [["haiku", 30, null, 1], ["opus", 14, 0.75, 3]]);
  const claude = sessionUsage({ rows: [{ day: "d", model: "m", input: 1, output: 1, cache_read: 0, cache_write: 0, total: 2, cost: null, messages: 1 }] });
  assert.equal(claude.totals.cost, null);
  assert.equal(claude.covers, "none");
  assert.equal(sessionUsage(undefined).model, null);
});

test("the session's Cost tab is Usage now; no Don Quixote, no lines added and removed", () => {
  const outside = lookItems({ summary: { findings: {} }, changes: [], unattributed: { processed: 10, share: 0.9 } }).find((i) => /outside every change/.test(i.title));
  assert.equal(outside.tab, "uso");
  assert.equal(outside.go, "See usage");
  assert.match(src, /\["uso", "量", "Usage"/);
  assert.ok(!src.includes('"Cost"'), "no tab or link says Cost");
  assert.doesNotMatch(src, /Quixote|lines \+/i);
});

test("g u goes to Usage; the keys help, the bar and the nav name it", () => {
  const k = (key) => ({ key, ctrlKey: false, metaKey: false });
  assert.deepEqual(keyAction(k("u"), "g"), { do: "go", to: "#/usage", pending: "" });
  assert.ok(KEYS.some(([key, what]) => key === "g u" && /usage/.test(what)));
  const labels = barResults("", [], []).actions.map((a) => a.label);
  assert.deepEqual(labels, ["Open", "Sessions", "Changes", "Usage", "Memory", "Telemetry"]);
  const html = readFileSync(fileURLToPath(new URL("../index.html", import.meta.url)), "utf8");
  assert.match(html, /href="#\/usage" data-page="usage">Usage/);
});

test("a range names what it covers: all history only when nothing is cut (T17d, L142)", async () => {
  const { coverageLabel, memoryPickerNote } = await import("../app.js");
  const whole = { listed: 12, total: 12, capped: false };
  const cut = { listed: 80, total: 214, capped: true };
  assert.equal(coverageLabel("todo", whole), "all history");
  assert.equal(coverageLabel("todo", cut), "the 80 most recent sessions");
  assert.equal(coverageLabel("7d", whole), "the last 7 days");
  assert.equal(coverageLabel("7d", cut), "the last 7 days (of the 80 most recent sessions)");
  assert.equal(coverageLabel("hoy", undefined), "today");
  assert.equal(memoryPickerNote(cut), "Projects of the 80 most recent sessions.");
  assert.equal(memoryPickerNote(whole), "");
  assert.doesNotMatch(src, /\btodo: "all history"/, "no page says all history by itself");
});
