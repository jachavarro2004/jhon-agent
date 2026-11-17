/**
 * /jhon:dashboard (T17b, L138 answer 5): the dashboard opened at this
 * session, started first when it does not answer; with no display, the URL
 * said instead of a browser.
 *
 *   node --experimental-strip-types --test test/dashboard-link.test.ts
 */
import "./isolated-env.ts";
import { test } from "node:test";
import assert from "node:assert/strict";
import { hasDisplay, openDashboard, type DashboardDeps } from "../extensions/jhon.ts";

const ID = "01a17221-ea5a-7541-b421-e245e0ca439f";

function deps(o: { answers?: boolean[]; starts?: boolean; display?: boolean } = {}) {
	const calls: string[] = [];
	const answers = [...(o.answers ?? [true])];
	const d: DashboardDeps = {
		probe: async (url) => { calls.push(`probe ${url}`); return answers.length > 1 ? answers.shift()! : answers[0]; },
		start: async () => { calls.push("start"); return o.starts ?? true; },
		open: (url) => { calls.push(`open ${url}`); },
		display: o.display ?? true,
		wait: async () => { calls.push("wait"); },
	};
	return { d, calls };
}

test("a dashboard that answers opens at this session, nothing started", async () => {
	const { d, calls } = deps();
	const r = await openDashboard(ID, d);
	assert.deepEqual(calls, ["probe http://127.0.0.1:7480/", `open http://127.0.0.1:7480/#/s/${ID}`]);
	assert.deepEqual(r, { level: "info", message: `Jhon dashboard: http://127.0.0.1:7480/#/s/${ID} (opened in your browser)` });
});

test("a dashboard that does not answer is started, then opened once it answers", async () => {
	const { d, calls } = deps({ answers: [false, false, true] });
	const r = await openDashboard(ID, d);
	assert.deepEqual(calls, ["probe http://127.0.0.1:7480/", "start", "wait", "probe http://127.0.0.1:7480/", "wait", "probe http://127.0.0.1:7480/", `open http://127.0.0.1:7480/#/s/${ID}`]);
	assert.equal(r.level, "info");
});

test("a dashboard that cannot start, or never answers, says so and opens nothing", async () => {
	const failed = deps({ answers: [false], starts: false });
	assert.deepEqual(await openDashboard(ID, failed.d), { level: "error", message: "jhon dashboard did not start: run `jhon dashboard` in a terminal to see why" });
	assert.ok(!failed.calls.some((c) => c.startsWith("open")));
	const silent = deps({ answers: [false] });
	assert.deepEqual(await openDashboard(ID, silent.d), { level: "error", message: "jhon dashboard did not answer at http://127.0.0.1:7480 after 2 s: run `jhon dashboard` in a terminal to see why" });
	assert.equal(silent.calls.filter((c) => c === "wait").length, 8);
	assert.ok(!silent.calls.some((c) => c.startsWith("open")));
});

test("with no display it prints the URL instead of opening a browser", async () => {
	const { d, calls } = deps({ display: false });
	const r = await openDashboard(ID, d);
	assert.ok(!calls.some((c) => c.startsWith("open")));
	assert.deepEqual(r, { level: "info", message: `Jhon dashboard: http://127.0.0.1:7480/#/s/${ID} (no display here to open a browser: open the address yourself)` });
});

test("a session not saved to disk opens the dashboard's home", async () => {
	const { d, calls } = deps();
	await openDashboard(undefined, d);
	assert.equal(calls.at(-1), "open http://127.0.0.1:7480/");
});

test("a display is X11 or Wayland on Linux, always on macOS", () => {
	assert.equal(hasDisplay({}, "linux"), false);
	assert.equal(hasDisplay({ DISPLAY: ":0" }, "linux"), true);
	assert.equal(hasDisplay({ WAYLAND_DISPLAY: "wayland-1" }, "linux"), true);
	assert.equal(hasDisplay({}, "darwin"), true);
});

test("the dashboard starts on JHON_DASHBOARD_URL's port, the default 7480 otherwise (T17d, L142)", async () => {
	const { dashboardStartArgs } = await import("../extensions/jhon.ts");
	assert.deepEqual(dashboardStartArgs("http://127.0.0.1:17999"), ["dashboard", "--port", "17999"]);
	assert.deepEqual(dashboardStartArgs("http://127.0.0.1:7480"), ["dashboard", "--port", "7480"]);
	assert.deepEqual(dashboardStartArgs("http://localhost"), ["dashboard", "--port", "7480"]);
});

test("the probe answers yes only for a Jhon dashboard, not any 200 (T17d, L142)", async () => {
	const { probeDashboard } = await import("../extensions/jhon.ts");
	const answer = (ok: boolean, header: string | null) => async () => ({ ok, headers: { get: (k: string) => (k.toLowerCase() === "x-jhon-dashboard" ? header : null) } }) as unknown as Response;
	assert.equal(await probeDashboard("http://127.0.0.1:1/", answer(true, "1")), true);
	assert.equal(await probeDashboard("http://127.0.0.1:1/", answer(true, null)), false);
	assert.equal(await probeDashboard("http://127.0.0.1:1/", answer(false, "1")), false);
	assert.equal(await probeDashboard("http://127.0.0.1:1/", async () => { throw new Error("refused"); }), false);
});

test("isChild keeps its doc comment, above it", async () => {
	const { readFileSync } = await import("node:fs");
	const src = readFileSync(new URL("../extensions/jhon.ts", import.meta.url), "utf8");
	assert.match(src, /\/\*\* True in a child pi process the `subagent` tool started\. \*\/\nexport const isChild/);
});

test("an older Jhon dashboard on the port is replaced, then opened (T15.16, L180)", async () => {
	const { d, calls } = deps();
	const staleness = [true, true, false];
	d.stale = async (base) => { calls.push(`stale ${base}`); return staleness.shift()!; };
	const r = await openDashboard(ID, d);
	assert.deepEqual(calls, ["probe http://127.0.0.1:7480/", "stale http://127.0.0.1:7480", "start", "wait", "stale http://127.0.0.1:7480", "wait", "stale http://127.0.0.1:7480", `open http://127.0.0.1:7480/#/s/${ID}`]);
	assert.deepEqual(r, { level: "info", message: `Jhon dashboard: http://127.0.0.1:7480/#/s/${ID} (opened in your browser)` });
});

test("a dashboard of this build is opened as is, nothing started", async () => {
	const { d, calls } = deps();
	d.stale = async () => { calls.push("stale"); return false; };
	await openDashboard(ID, d);
	assert.deepEqual(calls, ["probe http://127.0.0.1:7480/", "stale", `open http://127.0.0.1:7480/#/s/${ID}`]);
});

test("an older dashboard that is not replaced is still opened, and says how to replace it", async () => {
	const { d, calls } = deps({ display: false });
	d.stale = async () => true;
	d.base = "http://127.0.0.1:17999";
	const r = await openDashboard(undefined, d);
	assert.equal(calls.filter((c) => c === "start").length, 1);
	assert.equal(calls.filter((c) => c === "wait").length, 24);
	assert.deepEqual(r, { level: "info", message: "Jhon dashboard: http://127.0.0.1:17999/ (no display here to open a browser: open the address yourself); an older dashboard answers there and was not replaced: run `jhon dashboard --port 17999` in a terminal to see why" });
});

test("a dashboard is older when it has no /api/version or runs another binary than the shell's jhon", async () => {
	const { dashboardIsStale } = await import("../extensions/jhon.ts");
	const answer = (status: number, body: unknown) => async () => new Response(JSON.stringify(body), { status, headers: { "X-Jhon-Dashboard": "1" } });
	const mine = "/r/fe03226d/build/jhon";
	assert.equal(await dashboardIsStale("http://127.0.0.1:1", mine, answer(404, "not found")), true);
	assert.equal(await dashboardIsStale("http://127.0.0.1:1", mine, answer(200, { binary: "/r/09124008/build/jhon", pid: 1 })), true);
	assert.equal(await dashboardIsStale("http://127.0.0.1:1", mine, answer(200, { binary: mine, pid: 1 })), false);
	assert.equal(await dashboardIsStale("http://127.0.0.1:1", undefined, answer(200, { binary: "/x", pid: 1 })), false, "with no binary to compare, nothing is replaced");
	assert.equal(await dashboardIsStale("http://127.0.0.1:1", mine, async () => { throw new Error("refused"); }), false);
});
