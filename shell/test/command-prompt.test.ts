/**
 * T8f: a headless command's wait for its run (lib/command-prompt.ts) ends on
 * every path: a ctx.waitForIdle that throws or rejects at agent_start ends it
 * as a settle does, never leaving `pi -p` held open by a promise nobody settles.
 */
import "./isolated-env.ts";
import { test } from "node:test";
import assert from "node:assert/strict";
import { commandPrompts } from "../lib/command-prompt.ts";

/** A pi whose sendUserMessage starts the run at once (agent_start), and records what it was sent. */
function fakePi() {
	const handlers = new Map<string, Array<() => unknown>>();
	const sent: Array<{ text: string; options: unknown }> = [];
	const pi = {
		on: (name: string, handler: () => unknown) => { handlers.set(name, [...(handlers.get(name) ?? []), handler]); },
		sendUserMessage: (text: string, options: unknown) => {
			sent.push({ text, options });
			for (const handler of handlers.get("agent_start") ?? []) { try { handler(); } catch { /* pi reports a handler's throw as an extension error */ } }
		},
	};
	return { pi, sent };
}

const LATE = Symbol("late");
/** What `promise` settled with within 500 ms, or LATE; the idle bound is an hour here, so only the run's own end can settle it. */
const within = (promise: Promise<unknown>) => Promise.race([promise.then(() => "resolved", (error: Error) => `rejected: ${error.message}`), new Promise((resolve) => setTimeout(() => resolve(LATE), 500))]);

test("a headless command whose ctx.waitForIdle throws or rejects at agent_start still ends, resolved, having sent its prompt once", async () => {
	for (const [how, waitForIdle] of [
		["throws", () => { throw new Error("This extension ctx is stale"); }],
		["rejects", () => Promise.reject(new Error("This extension ctx is stale"))],
		["resolves", () => Promise.resolve()],
	] as const) {
		const { pi, sent } = fakePi();
		const send = commandPrompts(pi, { noRunMs: 3_600_000 });
		const ctx = { mode: "print", isIdle: () => true, waitForIdle };
		assert.equal(await within(send(ctx, "do it")), "resolved", how);
		assert.deepEqual(sent, [{ text: "do it", options: undefined }], how);
	}
});
