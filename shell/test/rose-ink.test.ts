import { test } from "node:test";
import assert from "node:assert/strict";
import { followPalette, switchToRoseInk } from "../lib/rose-ink.ts";

test("the shell asks jhon palette for the theme now and on each palette change, and stops watching", () => {
  const calls: string[][] = [];
  let onChange = () => {};
  let stopped = false;
  const stop = followPalette({
    jhon: "/bin/jhon", agentDir: "/home/u/.pi/jhon", colors: "/home/u/.cache/ryoku/colors.json",
    run: (bin, args, done) => { calls.push([bin, ...args]); done(null); },
    watch: (path, cb) => { assert.equal(path, "/home/u/.cache/ryoku/colors.json"); onChange = cb; return () => { stopped = true; }; },
  });
  assert.deepEqual(calls, [["/bin/jhon", "palette", "--write-pi", "/home/u/.pi/jhon"]]);
  onChange();
  assert.equal(calls.length, 2);
  stop();
  assert.equal(stopped, true);
});

test("a palette change says jhon:palette-changed with the palette's name on pi's bus and runs no other program", () => {
  const calls: string[][] = [];
  const said: [string, unknown][] = [];
  let onChange = () => {};
  followPalette({
    jhon: "/bin/jhon", agentDir: "/a", colors: "/c", palette: "ryoku",
    emit: (name, data) => { said.push([name, data]); },
    run: (bin, args, done) => { calls.push([bin, ...args]); done(null); },
    watch: (_p, cb) => { onChange = cb; return () => {}; },
  });
  assert.deepEqual(said, [], "nothing said at the start: nothing changed yet");
  onChange();
  assert.deepEqual(said, [["jhon:palette-changed", { palette: "ryoku" }]]);
  assert.deepEqual(calls, [["/bin/jhon", "palette", "--write-pi", "/a"], ["/bin/jhon", "palette", "--write-pi", "/a"]], "jhon alone runs");
});

// T15.18: another theme chosen in Customize is never pulled back; rose ink
// is rewritten only while it is the active theme.
test("rose ink is rewritten only while it is pi's active theme; the change is still said", () => {
  const calls: string[][] = [];
  const said: unknown[] = [];
  let onChange = () => {};
  let active: string | undefined = "kanagawa";
  followPalette({
    jhon: "/bin/jhon", agentDir: "/a", colors: "/c", palette: "ryoku", emit: (name, data) => { said.push([name, data]); }, active: () => active,
    run: (bin, args, done) => { calls.push([bin, ...args]); done(null); },
    watch: (_p, cb) => { onChange = cb; return () => {}; },
  });
  assert.deepEqual(calls, [], "no write at the start while kanagawa is active");
  onChange();
  assert.deepEqual(calls, []);
  assert.deepEqual(said, [["jhon:palette-changed", { palette: "ryoku" }]]);
  active = "rose-ink";
  onChange();
  assert.deepEqual(calls.at(-1), ["/bin/jhon", "palette", "--write-pi", "/a"]);
  active = undefined;
  onChange();
  assert.deepEqual(calls, [["/bin/jhon", "palette", "--write-pi", "/a"], ["/bin/jhon", "palette", "--write-pi", "/a"]], "no theme known yet: rose ink is the default");
});

test("a failed write is reported, not thrown", () => {
  const errors: string[] = [];
  followPalette({
    jhon: "/bin/jhon", agentDir: "/a", colors: "/c",
    run: (_b, _a, done) => done(Object.assign(new Error("boom"), { code: 1 })),
    watch: () => () => {},
    onError: (e) => errors.push(e.message),
  });
  assert.deepEqual(errors, ["boom"]);
});

// T15.18 (L208): switching back to rose ink refreshes its file first, through
// the same `jhon palette --write-pi` followPalette runs, then applies it.
test("switching to rose ink writes it with jhon palette first, then applies it", () => {
  const steps: string[] = [];
  const notices: string[] = [];
  let finish: (error: Error | null) => void = () => {};
  switchToRoseInk({
    jhon: "/bin/jhon", agentDir: "/a", colors: "/c",
    run: (bin, args, done, env) => { steps.push([bin, ...args].join(" ")); assert.equal(env?.JHON_DESKTOP_COLORS, "/c"); finish = done as typeof finish; },
    apply: () => { steps.push("apply"); return undefined; },
    notify: (m) => notices.push(m),
  });
  assert.deepEqual(steps, ["/bin/jhon palette --write-pi /a"], "nothing applied before the file is written");
  finish(null);
  assert.deepEqual(steps, ["/bin/jhon palette --write-pi /a", "apply"]);
  assert.deepEqual(notices, []);
});

test("a failed refresh still applies the existing rose ink and says so once", () => {
  const steps: string[] = [];
  const notices: string[] = [];
  switchToRoseInk({
    jhon: "/bin/jhon", agentDir: "/a", colors: "/c",
    run: (_b, _a, done) => done(Object.assign(new Error("boom"), { code: 1 })),
    apply: () => { steps.push("apply"); return undefined; },
    notify: (m) => notices.push(m),
  });
  assert.deepEqual(steps, ["apply"]);
  assert.deepEqual(notices, ["Rose ink could not be refreshed (boom); the existing file is applied."]);
});

test("a theme pi refuses is said", () => {
  const notices: string[] = [];
  switchToRoseInk({ jhon: "/bin/jhon", agentDir: "/a", colors: "/c", run: (_b, _a, done) => done(null), apply: () => "no such theme", notify: (m) => notices.push(m) });
  assert.deepEqual(notices, ["no such theme"]);
});
