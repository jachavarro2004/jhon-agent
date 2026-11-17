// Unit tests for the Memory page (T17c, L138 answer 1): the project picker,
// Engram's rows and its "does not answer" state, the search key, the
// addresses, and that the page writes nothing.
//
//   timeout 120 node --test dashboard/test/memory.test.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { barResults, engramDown, KEYS, keyAction, memoryProjects, memoryRoute, observationLine, searchTarget } from "../app.js";

const src = readFileSync(fileURLToPath(new URL("../app.js", import.meta.url)), "utf8");

test("the project picker lists your projects, the most recent session's first, which is the default", () => {
  const s = (id, project, name, modified, extra = {}) => ({ id, project, name, modified, runtime: "claude-code", entrypoint: "cli", ...extra });
  const list = memoryProjects([
    s("1", "-home-u-acme", "acme", "2026-11-01T10:00:00Z"),
    s("2", "-home-u-jhon", "jhon", "2026-11-03T10:00:00Z"),
    s("3", "-home-u-acme", "acme", "2026-11-02T10:00:00Z"),
    s("4", "-tmp-x", "scratch x", "2026-11-05T10:00:00Z"),
    s("5", "-home-u-bridge", "bridge", "2026-11-06T10:00:00Z", { entrypoint: "sdk-ts" }),
    s("6", "-home-u-pi", "pi", "2026-11-04T10:00:00Z", { runtime: "pi", entrypoint: undefined }),
  ]);
  assert.deepEqual(list, [{ dir: "-home-u-pi", name: "pi", sessions: 1 }, { dir: "-home-u-jhon", name: "jhon", sessions: 1 }, { dir: "-home-u-acme", name: "acme", sessions: 2 }]);
  assert.deepEqual(memoryProjects([]), []);
});

test("an observation reads its title, then its type in words, its id and its date", () => {
  assert.deepEqual(observationLine({ id: 7, type: "session_summary", title: "Closed T17b", created_at: "2026-11-01 10:00:00" }),
    { title: "Closed T17b", detail: "session summary · #7 · 2026-11-01 10:00" });
  assert.deepEqual(observationLine({ id: 8, type: "odd", title: "" }), { title: "Untitled", detail: "odd · #8" });
});

test("Engram that does not answer is its own state, not an error page", () => {
  assert.equal(engramDown("502: Engram does not answer: no answer within 3s"), "Engram does not answer: no answer within 3s");
  assert.equal(engramDown("502: Engram answered something that is not JSON"), "Engram answered something that is not JSON");
  assert.equal(engramDown("404: no session names this project"), null);
  assert.equal(engramDown(undefined), null);
});

test("/ searches Engram on the Memory page and the sessions elsewhere; g m goes to Memory", () => {
  assert.equal(searchTarget("#/memory"), "#/memory");
  assert.equal(searchTarget("#/memory/o/7"), "#/memory");
  assert.equal(searchTarget("#/usage"), "#/sessions");
  const k = (key) => ({ key, ctrlKey: false, metaKey: false });
  assert.deepEqual(keyAction(k("m"), "g"), { do: "go", to: "#/memory", pending: "" });
  assert.ok(KEYS.some(([key, what]) => key === "g m" && /memory/.test(what)));
  assert.ok(KEYS.some(([key, what]) => key === "/" && /Engram/.test(what)));
  assert.deepEqual(barResults("", [], []).actions.map((a) => a.label), ["Open", "Sessions", "Changes", "Usage", "Memory", "Telemetry"]);
  const html = readFileSync(fileURLToPath(new URL("../index.html", import.meta.url)), "utf8");
  assert.match(html, /href="#\/memory" data-page="memory">Memory/);
});

test("one memory opens at its own address: an observation by its id, a file by its bare name", () => {
  assert.deepEqual(memoryRoute("#/memory"), { kind: "list" });
  assert.deepEqual(memoryRoute("#/memory/o/7"), { kind: "engram", id: "7" });
  assert.deepEqual(memoryRoute("#/memory/f/plan%20v1.md"), { kind: "file", name: "plan v1.md" });
  assert.equal(memoryRoute("#/memory/o/x"), null);
  assert.equal(memoryRoute("#/memoryx"), null);
});

test("the Memory page reads only: it fetches the memory routes and writes nothing", () => {
  const body = src.slice(src.indexOf("// ── the memory page"), src.indexOf("// ── end of the memory page"));
  assert.ok(body.length > 100, "the memory page's section is marked");
  assert.match(body, /\/api\/memory\/engram/);
  assert.match(body, /\/api\/memory\/files/);
  assert.doesNotMatch(body, /method:|localStorage|POST|PUT|DELETE/);
  assert.doesNotMatch(src, /https?:\/\//, "the page names no address: jhon audit --html loads nothing external");
});
