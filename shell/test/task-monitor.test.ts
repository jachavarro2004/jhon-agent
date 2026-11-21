/**
 * lib/ui/task-monitor.ts: a monitor's output lines reach the agent in
 * batches of 200 ms and at most 20 lines, blank lines are not events, and
 * the monitor stops at its timeout or past 120 lines a minute; a stop, a
 * cancel or the command's own end each leave exactly the notices they owe.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test, { after } from "node:test";
import { createTaskRegistry, type JobExecOperations, type TaskRecord } from "../lib/background-tasks.ts";
import { createMonitorController, MONITOR_BATCH_MS, MONITOR_FLOOD_LINES, MONITOR_FLOOD_WINDOW_MS, MONITOR_LINES_PER_NOTICE, MONITOR_MAX_TIMEOUT_SECONDS, type MonitorNotice } from "../lib/ui/task-monitor.ts";

const dirs: string[] = [];
after(() => { for (const d of dirs) rmSync(d, { recursive: true, force: true }); });

/** A controller over a real registry, a fake shell, and a clock with timers the test moves. */
function monitors() {
	const dir = mkdtempSync(join(tmpdir(), "jhon-monitor-"));
	dirs.push(dir);
	let clock = 0;
	const timers: Array<{ at: number; fn: () => void; live: boolean }> = [];
	const execs: Array<{ signal?: AbortSignal; print(text: string): void; exit(code: number): void }> = [];
	const operations: JobExecOperations = {
		exec: (_command, _cwd, options) => new Promise((resolve, reject) => {
			options.signal?.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
			execs.push({ signal: options.signal, print: (text) => options.onData(Buffer.from(text)), exit: (exitCode) => resolve({ exitCode }) });
		}),
	};
	const settled: TaskRecord[] = [];
	const registry = createTaskRegistry({ outputDir: () => dir, now: () => clock, shell: () => ({ operations }), runAgent: () => new Promise(() => {}), onSettled: (task) => settled.push(task) });
	const notices: MonitorNotice[] = [];
	const controller = createMonitorController({
		registry,
		now: () => clock,
		schedule: (fn, ms) => { const timer = { at: clock + ms, fn, live: true }; timers.push(timer); return () => { timer.live = false; }; },
		deliver: (notice) => notices.push(notice),
	});
	const wait = (ms: number) => {
		clock += ms;
		for (const timer of timers.filter((t) => t.live && t.at <= clock)) { timer.live = false; timer.fn(); }
	};
	const live = () => timers.filter((t) => t.live).length;
	const ended = async () => { for (const deadline = Date.now() + 3000; settled.length === 0; await new Promise((r) => setTimeout(r, 5))) if (Date.now() > deadline) throw new Error("no end"); };
	return { controller, registry, execs, notices, settled, wait, live, ended, print: (text: string) => execs.at(-1)!.print(text) };
}

test("the limits: 200 ms batches of 20 lines, 120 lines a minute, timeouts up to 30 minutes", () => {
	assert.deepEqual([MONITOR_BATCH_MS, MONITOR_LINES_PER_NOTICE, MONITOR_FLOOD_LINES, MONITOR_FLOOD_WINDOW_MS, MONITOR_MAX_TIMEOUT_SECONDS], [200, 20, 120, 60_000, 1800]);
});

test("lines within 200 ms of the first share one notice; the next line opens another; the monitor keeps running and counts its events", () => {
	const m = monitors();
	const job = m.controller.start({ command: "gh run watch", cwd: "/", ownerSessionId: "s", label: "CI", timeoutSeconds: 600 });
	assert.deepEqual([job.kind, job.events, job.timeoutSeconds, job.label], ["monitor", 0, 600, "CI"]);
	m.print("lint: pass\n");
	m.wait(150);
	m.print("unit: fail\n");
	m.wait(49);
	assert.deepEqual(m.notices, []);
	m.wait(1);
	assert.deepEqual(m.notices, [{ job, kind: "events", batch: { lines: ["lint: pass", "unit: fail"], omitted: 0 } }]);
	m.print("e2e: pass\n");
	m.wait(200);
	assert.deepEqual(m.notices[1], { job, kind: "events", batch: { lines: ["e2e: pass"], omitted: 0 } });
	assert.deepEqual([job.status, job.events], ["running", 3]);
	m.wait(10_000);
	assert.equal(m.notices.length, 2, "no line, no notice");
	m.controller.cancelAll();
});

test("a notice carries 20 lines and counts the rest; blank lines are no events", () => {
	const m = monitors();
	const job = m.controller.start({ command: "x", cwd: "/", ownerSessionId: "s", timeoutSeconds: 60 });
	m.print("  \n\n");
	m.wait(500);
	assert.deepEqual([m.notices, job.events], [[], 0]);
	m.print(Array.from({ length: 27 }, (_, i) => `line ${i}\n`).join(""));
	m.wait(200);
	assert.deepEqual(m.notices, [{ job, kind: "events", batch: { lines: Array.from({ length: 20 }, (_, i) => `line ${i}`), omitted: 7 } }]);
	assert.equal(job.events, 27);
	m.controller.cancelAll();
});

test("past 120 lines within a minute the monitor is stopped once, after the lines so far; a slower stream runs on", () => {
	const fast = monitors();
	const job = fast.controller.start({ command: "yes", cwd: "/", ownerSessionId: "s", timeoutSeconds: 600 });
	fast.print(Array.from({ length: 120 }, (_, i) => `spam ${i}\n`).join(""));
	assert.deepEqual(fast.notices, [], "120 is not yet a flood");
	fast.print("spam 120\n");
	assert.deepEqual(fast.notices.map((n) => n.kind), ["events", "stopped"]);
	assert.deepEqual(fast.notices[1], { job, kind: "stopped", reason: "flood" });
	assert.deepEqual([(fast.notices[0] as any).batch.omitted, job.status, job.events, fast.execs[0]!.signal?.aborted], [101, "stopped", 121, true]);
	fast.print("more\n");
	fast.wait(1_000_000);
	assert.equal(fast.notices.length, 2, "a stopped monitor says nothing more, nor times out");
	assert.equal(fast.live(), 0);

	const slow = monitors();
	const steady = slow.controller.start({ command: "tick", cwd: "/", ownerSessionId: "s", timeoutSeconds: 1800 });
	for (let i = 0; i < 400; i++) { slow.print("tick\n"); slow.wait(500); }
	assert.equal(steady.status, "running", "one line every 500 ms is 120 a minute, not more");
	assert.ok(slow.notices.every((n) => n.kind === "events"));
	slow.controller.cancelAll();
});

test("at its timeout a monitor delivers its open batch, then stops and says so", () => {
	const m = monitors();
	const job = m.controller.start({ command: "tail -f log", cwd: "/", ownerSessionId: "s", timeoutSeconds: 30 });
	m.wait(29_900);
	m.print("ERROR one\n");
	m.wait(100);
	assert.deepEqual(m.notices, [{ job, kind: "events", batch: { lines: ["ERROR one"], omitted: 0 } }, { job, kind: "stopped", reason: "timeout" }]);
	assert.deepEqual([job.status, m.execs[0]!.signal?.aborted, m.live()], ["stopped", true, 0]);
});

test("a timeout outside 1 to 1800 whole seconds is refused before anything runs", () => {
	const m = monitors();
	for (const timeoutSeconds of [0, -1, 1801, 1.5, Number.NaN, Infinity]) {
		assert.throws(() => m.controller.start({ command: "x", cwd: "/", ownerSessionId: "s", timeoutSeconds }), { message: "timeout_seconds must be a whole number from 1 to 1800." });
	}
	assert.deepEqual([m.execs.length, m.registry.list().length, m.live()], [0, 0, 0]);
	m.controller.start({ command: "x", cwd: "/", ownerSessionId: "s", timeoutSeconds: 1800 });
	m.controller.start({ command: "x", cwd: "/", ownerSessionId: "s", timeoutSeconds: 1 });
	assert.equal(m.execs.length, 2);
	m.controller.cancelAll();
	assert.equal(m.live(), 0);
});

test("when the command ends, finish delivers the open batch, with what was left unfinished, and the timeout goes", async () => {
	const m = monitors();
	const job = m.controller.start({ command: "watch", cwd: "/", ownerSessionId: "s", timeoutSeconds: 60 });
	m.print("check: pass\nlast");
	m.execs[0]!.exit(0);
	await m.ended();
	assert.deepEqual(m.settled, [job]);
	assert.deepEqual(m.notices, [], "the batch waits for finish");
	m.controller.finish(job);
	assert.deepEqual(m.notices, [{ job, kind: "events", batch: { lines: ["check: pass", "last"], omitted: 0 } }]);
	m.wait(120_000);
	assert.equal(m.notices.length, 1);
	m.controller.finish(job);
	assert.equal(m.notices.length, 1, "finish twice delivers once");
});

test("a timeout that comes after the command exited reports nothing: its end does", async () => {
	const m = monitors();
	const job = m.controller.start({ command: "watch", cwd: "/", ownerSessionId: "s", timeoutSeconds: 1 });
	m.wait(900);
	m.print("last line\n");
	m.execs[0]!.exit(0);
	await Promise.resolve();
	await Promise.resolve();
	assert.equal(job.status, "exited", "the exit is recorded before the log closes");
	m.wait(100);
	assert.deepEqual(m.notices, []);
	await m.ended();
	m.controller.finish(job);
	assert.deepEqual(m.notices, [{ job, kind: "events", batch: { lines: ["last line"], omitted: 0 } }]);
});

test("cancel drops a monitor's timers and open batch without a word; a cancelled monitor's later lines are ignored", () => {
	const m = monitors();
	const job = m.controller.start({ command: "watch", cwd: "/", ownerSessionId: "s", timeoutSeconds: 60 });
	m.print("pending\n");
	m.controller.cancel(job.id);
	m.controller.cancel(job.id);
	m.print("after\n");
	m.wait(120_000);
	assert.deepEqual([m.notices, m.live(), job.status, job.events], [[], 0, "running", 1]);
	m.registry.stop(job.id);
});
