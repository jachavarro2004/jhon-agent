import assert from "node:assert/strict";
import test from "node:test";
import { MainNotificationRun, isNotificationSourceEvent, subagentNotificationEvent, NOTIFICATION_SOURCE_EVENT } from "../lib/ui/notification-events.ts";
import { NOTIFICATION_PRIORITY } from "../lib/ui/notification-policy.ts";

test("main lifecycle correlates settlement, resets outcomes and ignores stale/duplicate boundaries", () => {
	const main = new MainNotificationRun(() => 42);
	const first = main.begin("s");
	assert.equal(first.event, "agent.started");
	main.beforeSettle("s", first.runId, "completed");
	assert.equal(main.settled("s", first.runId)?.event, "agent.completed");
	assert.equal(main.settled("s", first.runId), undefined);
	const second = main.begin("s");
	assert.notEqual(first.runId, second.runId);
	main.beforeSettle("s", first.runId, "completed");
	assert.equal(main.settled("s", first.runId), undefined);
	assert.equal(main.settled("s", second.runId), undefined, "no stale success evidence");
	for (const [outcome, event] of [["error", "agent.failed"], ["aborted", "agent.cancelled"]]) {
		const run = main.begin("s");
		main.turnEnd("other", run.runId, "completed");
		main.turnEnd("s", run.runId, "completed");
		main.beforeSettle("s", run.runId, outcome);
		assert.equal(main.settled("s", run.runId)?.event, event);
	}
});

test("fallback needs run correlation; unknown authoritative evidence is omitted; reset cancels old runs", () => {
	const main = new MainNotificationRun(() => 0);
	let run = main.begin("s");
	main.turnEnd("s", "wrong", "completed");
	assert.equal(main.settled("s", run.runId), undefined);
	run = main.begin("s");
	main.turnEnd("s", run.runId, "completed");
	assert.equal(main.settled("s", run.runId)?.event, "agent.completed");
	run = main.begin("s");
	main.turnEnd("s", run.runId, "completed");
	main.beforeSettle("s", run.runId, "unknown");
	assert.equal(main.settled("s", run.runId), undefined);
	run = main.begin("s");
	main.beforeSettle("s", run.runId, "error");
	main.reset();
	assert.equal(main.settled("s", run.runId), undefined);
});

test("attention uses only correlated active edges, not generic dialog events", () => {
	const main = new MainNotificationRun(() => 10);
	const run = main.begin("s");
	assert.equal(main.blocked("s", run.runId, { active: true })?.event, "agent.attention");
	assert.equal(main.blocked("s", run.runId, { active: true }), undefined);
	assert.equal(main.blocked("s", run.runId, { active: false }), undefined);
	assert.equal(main.blocked("other", run.runId, { active: true }), undefined);
	assert.equal(main.blocked("s", run.runId, { type: "ui_prompt_start" }), undefined);
	const next = main.blocked("s", run.runId, { active: true })!;
	assert.equal(next.sequence, 3);
	assert.equal(NOTIFICATION_PRIORITY[next.event], 2);
	main.settled("s", run.runId);
	assert.equal(main.blocked("s", run.runId, { active: true }), undefined);
});

test("wire validation checks source, identity, status, clock and parent isolation", () => {
	const event = subagentNotificationEvent({ producerId: "producer", sessionId: "parent", parentSessionId: "parent",
		taskId: "task", runId: "run", status: "completed", sequence: 2 }, 100);
	assert.equal(event.event, "subagent.completed");
	assert.equal(NOTIFICATION_PRIORITY[event.event], 1);
	// T15.6: a sub-agent's other states are not events; the bus refuses them.
	for (const status of ["queued", "running", "waiting", "cancelled", "timed_out"])
		assert.equal(isNotificationSourceEvent({ ...event, status, event: `subagent.${status}` }), false, status);
	assert.ok(isNotificationSourceEvent(event));
	for (const patch of [{ sequence: 0 }, { occurredAt: NaN }, { parentSessionId: "other" }, { status: "bad" },
		{ event: "agent.attention" }, { runId: "" }, { producerId: "" }, { source: "unknown" }]) {
		assert.equal(isNotificationSourceEvent({ ...event, ...patch }), false);
	}
	const main = new MainNotificationRun(() => 2).begin("s");
	assert.ok(isNotificationSourceEvent(main));
	assert.equal(isNotificationSourceEvent({ ...main, event: "subagent.running" }), false);
	assert.equal(isNotificationSourceEvent(null), false);
	assert.notEqual(event.runId, subagentNotificationEvent({ ...event, producerId: "reload", runId: "run" }, 100).runId);
});


