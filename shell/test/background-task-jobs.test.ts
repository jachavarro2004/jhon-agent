/**
 * lib/background-tasks.ts, its commands: a job runs in the shell it is handed,
 * writes its bytes to `<id>.log`, keeps its last 20 lines of at most 2,000
 * characters each, hands a monitor each line, and reports its own end once;
 * a stop reports nothing. Commands and sub-agents share ids, the 25-task
 * limit and the per-session list.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test, { after } from "node:test";
import { createLocalBashOperations } from "@earendil-works/pi-coding-agent";
import { createOutputTail, createTaskRegistry, MAX_RUNNING_TASKS, MAX_TAIL_LINE_CHARS, TAIL_LINES, type JobExecOperations, type JobRecord, type TaskRecord } from "../lib/background-tasks.ts";
import type { AgentDef, ChildResult } from "../lib/agents.ts";

const dirs: string[] = [];
after(() => { for (const d of dirs) rmSync(d, { recursive: true, force: true }); });
const worker: AgentDef = { name: "worker", description: "w", model: "m", tools: [], bounded: true, body: "" };

interface Exec { command: string; cwd: string; signal?: AbortSignal; data(bytes: Buffer | string): void; exit(code: number | null): void; fail(error: Error): void }

/** A registry over a fake shell and fake sub-agents, with a clock the test moves. */
function jobs(commandPrefix?: string) {
	const dir = mkdtempSync(join(tmpdir(), "jhon-job-test-"));
	dirs.push(dir);
	const execs: Exec[] = [];
	const operations: JobExecOperations = {
		exec: (command, cwd, options) => new Promise((resolve, reject) => {
			options.signal?.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
			execs.push({ command, cwd, signal: options.signal, data: (bytes) => options.onData(Buffer.isBuffer(bytes) ? bytes : Buffer.from(bytes)), exit: (exitCode) => resolve({ exitCode }), fail: reject });
		}),
	};
	const answers: Array<(r: ChildResult) => void> = [];
	const settled: TaskRecord[] = [];
	let clock = 50_000;
	const registry = createTaskRegistry({
		outputDir() { return dir; },
		now: () => clock,
		shell: () => ({ operations, commandPrefix }),
		runAgent: (_a, _t, _c, { signal }) => new Promise((resolve) => { answers.push(resolve); signal.addEventListener("abort", () => resolve({ ok: false, text: "cancelled" }), { once: true }); }),
		onSettled: (task) => settled.push(task),
	});
	/** Until every job has closed its log and reported its end. */
	const closed = async () => {
		for (const deadline = Date.now() + 3000; registry.pending() > 0; await new Promise((r) => setTimeout(r, 5))) if (Date.now() > deadline) throw new Error("a job's log never closed");
	};
	return { dir, execs, answers, settled, registry, closed, move: (ms: number) => { clock += ms; } };
}

test("the tail: the newest 20 lines, chunks joined into lines, carriage returns dropped, the line being written last", () => {
	const tail = createOutputTail();
	assert.deepEqual(tail.push("a\r\nb\nc"), ["a", "b"], "push returns the lines it completed");
	assert.deepEqual(tail.lines(), ["a", "b", "c"]);
	assert.deepEqual(tail.push("d\n"), ["cd"]);
	assert.equal(tail.pending(), undefined);
	for (let i = 0; i < 30; i++) tail.push(`n${i}\r\n`);
	assert.equal(TAIL_LINES, 20);
	assert.deepEqual(tail.lines(), Array.from({ length: 20 }, (_, i) => `n${i + 10}`));
	tail.push("open\r");
	assert.deepEqual(tail.lines().at(-1), "open");
	assert.equal(tail.lines().length, 20);
});

test("the tail: a line that never ends keeps its newest 2,000 characters after …, and so does every overlong line", () => {
	assert.equal(MAX_TAIL_LINE_CHARS, 2000);
	const tail = createOutputTail();
	for (let i = 0; i < 10; i++) tail.push("x".repeat(1500));
	tail.push("END");
	assert.equal(tail.pending(), `…${"x".repeat(1997)}END`);
	assert.deepEqual(tail.push("!\nshort\n"), [`…${"x".repeat(1996)}END!`, "short"]);
	assert.deepEqual(tail.push(`${"y".repeat(2001)}\n${"z".repeat(2000)}\n`), [`…${"y".repeat(2000)}`, "z".repeat(2000)]);
	tail.push("w".repeat(2000));
	assert.equal(tail.pending(), "w".repeat(2000), "exactly 2,000 is not cut");
});

test("a job runs at once, and its exit reports it once: code, duration, last lines, and every byte in <id>.log", async () => {
	const j = jobs();
	const job = j.registry.start({ command: "make ci", cwd: "/repo", label: "ci", ownerSessionId: "s1" });
	assert.deepEqual([job.id, job.kind, job.status, job.label, job.command, job.cwd, job.outputPath, job.tail], ["job-1", "command", "running", "ci", "make ci", "/repo", join(j.dir, "job-1.log"), []]);
	assert.deepEqual([j.execs[0]!.command, j.execs[0]!.cwd], ["make ci", "/repo"]);
	j.execs[0]!.data("building\n");
	assert.deepEqual(job.tail, ["building"], "the tail is current while it runs");
	j.execs[0]!.data("ok\n");
	j.move(4_200);
	j.execs[0]!.exit(2);
	await j.closed();
	assert.deepEqual(j.settled, [job]);
	assert.deepEqual([job.status, job.exitCode, job.endedAt! - job.startedAt, job.tail], ["exited", 2, 4_200, ["building", "ok"]]);
	assert.equal(readFileSync(job.outputPath, "utf8"), "building\nok\n");
});

test("a character split across chunks reaches the tail whole, and the log keeps the bytes", async () => {
	const j = jobs();
	const job = j.registry.start({ command: "echo", cwd: "/", ownerSessionId: "s" });
	const bytes = Buffer.from("año ✓\n");
	for (const piece of [bytes.subarray(0, 2), bytes.subarray(2, 6), bytes.subarray(6)]) j.execs[0]!.data(piece);
	j.execs[0]!.exit(0);
	await j.closed();
	assert.deepEqual(job.tail, ["año ✓"]);
	assert.deepEqual(readFileSync(job.outputPath), bytes);
});

test("onLine gets each complete line as it comes and the unfinished one at the end; a monitor job says so", async () => {
	const j = jobs();
	const lines: string[] = [];
	const job = j.registry.start({ command: "watch", cwd: "/", ownerSessionId: "s", kind: "monitor", onLine: (line) => lines.push(line) });
	assert.equal(job.kind, "monitor");
	j.execs[0]!.data("lint: pa");
	assert.deepEqual(lines, []);
	j.execs[0]!.data("ss\r\ntest: fail\nlast");
	assert.deepEqual(lines, ["lint: pass", "test: fail"]);
	j.execs[0]!.exit(0);
	await j.closed();
	assert.deepEqual(lines, ["lint: pass", "test: fail", "last"]);
});

test("onLine gets an endless line as its newest 2,000 characters after …", () => {
	const j = jobs();
	const lines: string[] = [];
	j.registry.start({ command: "watch", cwd: "/", ownerSessionId: "s", kind: "monitor", onLine: (line) => lines.push(line) });
	j.execs[0]!.data(`${"x".repeat(9000)}END\nshort\n`);
	j.execs[0]!.data("y".repeat(9000));
	j.execs[0]!.data("TAIL\n");
	assert.deepEqual(lines, [`…${"x".repeat(1997)}END`, "short", `…${"y".repeat(1996)}TAIL`]);
});

test("the shell's prefix runs before the command; the job keeps the command as given", () => {
	const j = jobs("shopt -s expand_aliases");
	const job = j.registry.start({ command: "ll", cwd: "/", ownerSessionId: "s" });
	assert.equal(j.execs[0]!.command, "shopt -s expand_aliases\nll");
	assert.deepEqual([job.command, job.label], ["ll", "ll"]);
});

test("a stop aborts the command and reports nothing; stopping it again changes nothing; an unknown id is undefined", async () => {
	const j = jobs();
	const job = j.registry.start({ command: "sleep 100", cwd: "/", ownerSessionId: "s" });
	j.move(10);
	assert.equal(j.registry.stop(job.id), job);
	assert.deepEqual([job.status, j.execs[0]!.signal?.aborted, job.endedAt! - job.startedAt], ["stopped", true, 10]);
	await j.closed();
	j.move(10);
	assert.equal(j.registry.stop(job.id)?.endedAt, job.startedAt + 10);
	assert.deepEqual(j.settled, []);
	assert.equal(j.registry.stop("job-9"), undefined);
});

test("a command that cannot run reports failed, with why", async () => {
	const j = jobs();
	const job = j.registry.start({ command: "x", cwd: "/missing", ownerSessionId: "s" });
	j.execs[0]!.fail(new Error("Working directory does not exist: /missing"));
	await j.closed();
	assert.deepEqual(j.settled, [job]);
	assert.deepEqual([job.status, job.error], ["failed", "Working directory does not exist: /missing"]);
});

test("25 tasks run at once at most, sub-agents counted; one that ends frees its place", async () => {
	const j = jobs();
	assert.equal(MAX_RUNNING_TASKS, 25);
	j.registry.startAgent(worker, "review", "/", "s");
	for (let i = 1; i < 25; i++) j.registry.start({ command: `c${i}`, cwd: "/", ownerSessionId: i % 2 ? "s" : "t" });
	assert.equal(j.registry.running(), 25);
	assert.throws(() => j.registry.start({ command: "more", cwd: "/", ownerSessionId: "s" }), { message: "25 background tasks are already running; stop one with task_stop first." });
	assert.throws(() => j.registry.startAgent(worker, "more", "/", "s"), { message: "25 background tasks are already running; stop one with task_stop first." });
	assert.equal(j.execs.length, 24, "a refused job never runs");
	j.execs[0]!.exit(0);
	for (const deadline = Date.now() + 3000; j.settled.length === 0; await new Promise((r) => setTimeout(r, 5))) if (Date.now() > deadline) throw new Error("no end");
	assert.equal(j.registry.start({ command: "more", cwd: "/", ownerSessionId: "s" }).id, "job-25", "a refused job takes no id");
});

test("commands and sub-agents: one list per session in start order, ids of their own; stopAll stops them all, reports none, and waits for the logs", async () => {
	const j = jobs();
	const a = j.registry.start({ command: "a", cwd: "/", ownerSessionId: "s1" });
	const run = j.registry.startAgent(worker, "fix", "/", "s1");
	const b = j.registry.start({ command: "b", cwd: "/", ownerSessionId: "s2" });
	const other = j.registry.startAgent(worker, "elsewhere", "/", "s2");
	assert.deepEqual(j.registry.list("s1").map((t) => t.id), ["job-1", "agent-1"]);
	assert.deepEqual(j.registry.list().map((t) => t.id), ["job-1", "agent-1", "job-2", "agent-2"]);
	assert.equal(j.registry.get("agent-2"), other);
	j.answers[0]!({ ok: true, text: "fixed" });
	await new Promise((r) => setImmediate(r));
	assert.deepEqual(j.settled.map((t) => [t.id, t.status]), [["agent-1", "completed"]]);
	await j.registry.stopAll();
	assert.equal(j.registry.pending(), 0, "every log is closed");
	assert.deepEqual([a, run, b, other].map((t) => t.status), ["stopped", "completed", "stopped", "stopped"]);
	assert.equal(j.settled.length, 1);
});

test("a real command runs through pi's Bash operations and reports its exit code and both streams", { skip: process.platform === "win32" }, async () => {
	const dir = mkdtempSync(join(tmpdir(), "jhon-job-real-"));
	dirs.push(dir);
	const job = await new Promise<JobRecord>((resolve) => {
		createTaskRegistry({ outputDir: () => dir, now: Date.now, shell: () => ({ operations: createLocalBashOperations() }), runAgent: () => new Promise(() => {}), onSettled: (task) => resolve(task as JobRecord) })
			.start({ command: "echo one; echo two >&2; exit 3", cwd: dir, ownerSessionId: "s" });
	});
	assert.deepEqual([job.status, job.exitCode], ["exited", 3]);
	assert.deepEqual([...job.tail].sort(), ["one", "two"]);
	assert.deepEqual(readFileSync(job.outputPath, "utf8").split("\n").filter(Boolean).sort(), ["one", "two"]);
});
