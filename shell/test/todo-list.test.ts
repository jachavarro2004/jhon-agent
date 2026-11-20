import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import {
	applyTodo,
	emptyTodo,
	replayTodo,
	staleTurns,
	TODO_STATUS,
	todoPromptBlock,
	todoSummary,
	type TodoState,
} from "../lib/todo.ts";

// The agent's todo list (lib/todo.ts; T174 removed its card, the tool
// stays): a task list the model rewrites as it works. The reducer is pure,
// the state replays from the session branch, and staleness is derived from
// how many turns passed since the last write while tasks stay open.

function seeded(): TodoState {
	return applyTodo(
		emptyTodo(),
		{ action: "write", tasks: [{ title: "Add quiet tool rendering", status: "done" }, { title: "Fix quiet tools conflict", status: "in_progress", note: "fixing conflict" }, { title: "Show git bash tails" }] },
		3,
	).state;
}

test("applyTodo write replaces the list, assigns ids, defaults to pending, and records the turn", () => {
	const { state, text, error } = applyTodo(emptyTodo(), { action: "write", tasks: [{ title: "First" }, { title: "Second", status: "in_progress", note: "working" }] }, 2);
	assert.equal(error, undefined);
	assert.deepEqual(state.tasks, [
		{ id: 1, title: "First", status: TODO_STATUS.PENDING },
		{ id: 2, title: "Second", status: TODO_STATUS.IN_PROGRESS, note: "working" },
	]);
	assert.equal(state.nextId, 3);
	assert.equal(state.updatedTurn, 2);
	assert.match(text, /2 tasks · 0 done · 1 in progress/);
});

test("applyTodo write keeps ids the model passes back and drops the rest", () => {
	const first = applyTodo(emptyTodo(), { action: "write", tasks: [{ title: "A" }, { title: "B" }] }, 1).state;
	const second = applyTodo(first, { action: "write", tasks: [{ id: 2, title: "B", status: "done" }, { title: "C" }] }, 2).state;
	assert.deepEqual(second.tasks.map((task) => `${task.id}:${task.title}:${task.status}`), ["2:B:done", "3:C:pending"]);
});

test("applyTodo add, update, and clear mutate one task at a time and validate ids", () => {
	let state = applyTodo(emptyTodo(), { action: "add", title: "Write tests" }, 1).state;
	assert.equal(state.tasks[0].id, 1);
	const updated = applyTodo(state, { action: "update", id: 1, status: "in_progress", note: "writing tests" }, 2);
	assert.equal(updated.error, undefined);
	assert.equal(updated.state.tasks[0].status, TODO_STATUS.IN_PROGRESS);
	assert.equal(updated.state.tasks[0].note, "writing tests");
	assert.match(updated.text, /#1 Write tests → in_progress/);
	const missing = applyTodo(updated.state, { action: "update", id: 9, status: "done" }, 2);
	assert.match(missing.error ?? "", /no task #9/);
	assert.strictEqual(missing.state, updated.state);
	const noField = applyTodo(updated.state, { action: "update", id: 1 }, 2);
	assert.match(noField.error ?? "", /nothing to update/);
	state = applyTodo(updated.state, { action: "clear" }, 3).state;
	assert.deepEqual(state.tasks, []);
	assert.equal(state.nextId, 1);
});

test("applyTodo accepts completed as an alias of done, rejects unknown statuses, and sanitizes text", () => {
	const alias = applyTodo(emptyTodo(), { action: "write", tasks: [{ title: "X", status: "completed" }] }, 1);
	assert.equal(alias.state.tasks[0].status, TODO_STATUS.DONE);
	const bad = applyTodo(emptyTodo(), { action: "write", tasks: [{ title: "X", status: "later" }] }, 1);
	assert.match(bad.error ?? "", /unknown status "later"/);
	const dirty = applyTodo(emptyTodo(), { action: "add", title: "Evil\x1b[31m title\n" }, 1).state;
	assert.equal(dirty.tasks[0].title, "Evil title");
	const empty = applyTodo(emptyTodo(), { action: "add", title: "   " }, 1);
	assert.match(empty.error ?? "", /title is required/);
});

test("applyTodo list reports the state without changing it", () => {
	const state = seeded();
	const listed = applyTodo(state, { action: "list" }, 5);
	assert.strictEqual(listed.state, state);
	assert.match(listed.text, /\[done\] #1 Add quiet tool rendering/);
	assert.match(listed.text, /\[in_progress\] #2 Fix quiet tools conflict — fixing conflict/);
	assert.match(listed.text, /\[pending\] #3 Show git bash tails/);
});

test("todoSummary and staleTurns describe progress and how long the list went untouched", () => {
	const state = seeded();
	assert.deepEqual(todoSummary(state), { done: 1, total: 3, open: 2 });
	assert.equal(staleTurns(state, 3), 0);
	assert.equal(staleTurns(state, 5), 2);
	const allDone = applyTodo(state, { action: "write", tasks: state.tasks.map((task) => ({ ...task, status: "done" })) }, 4).state;
	assert.equal(staleTurns(allDone, 9), 0, "a finished list never goes stale");
	assert.equal(staleTurns(emptyTodo(), 9), 0);
});

test("replayTodo rebuilds the latest state from jhon, legacy gentleTodo and rpiv tool results alike", () => {
	const ours = { type: "message", message: { role: "toolResult", toolName: "todo", isError: false, details: { jhonTodo: { tasks: [{ id: 4, title: "Ours", status: "pending" }], nextId: 5, updatedTurn: 7 } } } };
	// Sessions saved before T59 carry the snapshot under gentleTodo; they still restore.
	const legacy = { type: "message", message: { role: "toolResult", toolName: "todo", isError: false, details: { gentleTodo: { tasks: [{ id: 6, title: "Saved", status: "done" }], nextId: 7, updatedTurn: 2 } } } };
	const rpiv = { type: "message", message: { role: "toolResult", toolName: "todo", isError: false, details: { action: "update", params: {}, tasks: [{ id: 1, subject: "Theirs", status: "completed", activeForm: "done" }, { id: 2, subject: "Gone", status: "deleted" }, { id: 3, subject: "Now", status: "in_progress", activeForm: "doing it" }], nextId: 4 } } };
	const other = { type: "message", message: { role: "toolResult", toolName: "bash", details: { tasks: [] } } };
	assert.deepEqual(replayTodo([other, rpiv]).tasks, [
		{ id: 1, title: "Theirs", status: TODO_STATUS.DONE },
		{ id: 3, title: "Now", status: TODO_STATUS.IN_PROGRESS, note: "doing it" },
	]);
	assert.equal(replayTodo([other, rpiv]).nextId, 4);
	assert.equal(replayTodo([rpiv, ours]).tasks[0].title, "Ours", "the last write wins");
	assert.equal(replayTodo([rpiv, ours]).updatedTurn, 7);
	assert.deepEqual(replayTodo([rpiv, legacy]).tasks, [{ id: 6, title: "Saved", status: "done" }], "an old session restores");
	assert.equal(replayTodo([legacy, ours]).tasks[0].title, "Ours");
	assert.deepEqual(replayTodo([]), emptyTodo());
});

test("todoPromptBlock lists open work with the rules, and stays silent when there is nothing open", () => {
	const block = todoPromptBlock(seeded(), 0);
	assert.ok(block);
	assert.match(block, /^## Todo list/m);
	assert.match(block, /mark a task in_progress before starting/);
	assert.match(block, /1\. \[done\] Add quiet tool rendering/);
	assert.match(block, /2\. \[in_progress\] Fix quiet tools conflict — fixing conflict/);
	assert.match(block, /3\. \[pending\] Show git bash tails/);
	assert.doesNotMatch(block, /stale/);
	assert.match(todoPromptBlock(seeded(), 2) ?? "", /stale: 2 turns without an update/);
	assert.equal(todoPromptBlock(emptyTodo(), 0), undefined);
	const allDone = applyTodo(seeded(), { action: "write", tasks: [{ title: "A", status: "done" }] }, 1).state;
	assert.equal(todoPromptBlock(allDone, 0), undefined);
});

test("applyTodo accepts dropped and blocked, maps cancel vocabulary to dropped, and names every status on an unknown one", () => {
	const { state, error } = applyTodo(
		emptyTodo(),
		{ action: "write", tasks: [{ title: "Old plan", status: "dropped" }, { title: "Deploy", status: "blocked", note: "waiting for an admin" }, { title: "A", status: "cancelled" }, { title: "B", status: "canceled" }, { title: "C", status: "abandoned" }] },
		1,
	);
	assert.equal(error, undefined);
	assert.deepEqual(state.tasks.map((task) => task.status), [TODO_STATUS.DROPPED, TODO_STATUS.BLOCKED, TODO_STATUS.DROPPED, TODO_STATUS.DROPPED, TODO_STATUS.DROPPED]);
	assert.equal(state.tasks[1].note, "waiting for an admin");
	const bad = applyTodo(emptyTodo(), { action: "write", tasks: [{ title: "X", status: "later" }] }, 1);
	assert.equal(bad.error, 'unknown status "later" (use pending, in_progress, blocked, done, or dropped)');
});

test("applyTodo rejects blocked without a note and leaves the list unchanged", () => {
	const before = seeded();
	for (const params of [
		{ action: "add", title: "Deploy", status: "blocked" },
		{ action: "update", id: 3, status: "blocked" },
		{ action: "write", tasks: [{ title: "Deploy", status: "blocked", note: "   " }] },
	]) {
		const result = applyTodo(before, params, 9);
		assert.equal(result.error, "blocked needs a note naming what it waits for");
		assert.equal(result.text, "Error: blocked needs a note naming what it waits for");
		assert.strictEqual(result.state, before, `${params.action} must not change the list`);
	}
	const blocked = applyTodo(before, { action: "update", id: 3, status: "blocked", note: "waiting for the API contract" }, 9);
	assert.equal(blocked.error, undefined);
	assert.equal(blocked.text, "#3 Show git bash tails → blocked");
	const unblocked = applyTodo(blocked.state, { action: "update", id: 3, status: "pending" }, 10);
	assert.equal(unblocked.state.tasks[2].status, TODO_STATUS.PENDING);
});

test("dropped tasks never count as open: a list of done and dropped is finished, silent, and never stale", () => {
	const state = applyTodo(emptyTodo(), { action: "write", tasks: [{ title: "A", status: "done" }, { title: "B", status: "dropped" }, { title: "C", status: "dropped" }] }, 1).state;
	assert.deepEqual(todoSummary(state), { done: 1, total: 3, open: 0 });
	assert.equal(todoPromptBlock(state, 5), undefined);
	assert.equal(staleTurns(state, 9), 0);
});

test("blocked tasks stay open but alone never make the list stale; an actionable task still does", () => {
	const blockedOnly = applyTodo(emptyTodo(), { action: "write", tasks: [{ title: "A", status: "done" }, { title: "Deploy", status: "blocked", note: "waiting for an admin" }] }, 1).state;
	assert.deepEqual(todoSummary(blockedOnly), { done: 1, total: 2, open: 1 });
	assert.equal(staleTurns(blockedOnly, 9), 0);
	const block = todoPromptBlock(blockedOnly, 0) ?? "";
	assert.match(block, /2\. \[blocked\] Deploy — waiting for an admin/);
	assert.match(block, /A blocked task waits on something outside the list: do not work on it until its condition holds/);
	assert.match(block, /mark the tasks it made obsolete dropped instead of leaving them pending or marking them done/);
	const mixed = applyTodo(blockedOnly, { action: "add", title: "Write docs" }, 1).state;
	assert.equal(staleTurns(mixed, 3), 2);
});
