import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import {
	applyTodo,
	emptyTodo,
	replayTodo,
	staleTurns,
	todoDetails,
	TODO_STATUS,
	todoPromptBlock,
	todoStartBlock,
	todoSummary,
	type TodoParams,
	type TodoState,
} from "../lib/todo.ts";

// lib/todo.ts on its own: every change to the list is a pure function of the
// list before it, the list comes back from the branch, and staleness is a
// count of turns.

const after = (state: TodoState, params: TodoParams, turn = 1) => applyTodo(state, params, turn);
const three = () => after(emptyTodo(), { action: "write", tasks: [{ title: "Read the spec", status: "done" }, { title: "Write the parser", status: "in_progress", note: "lexer first" }, { title: "Ship" }] }, 3).state;

test("write replaces the list: ids in order, pending by default, the turn kept", () => {
	const { state, text, error } = after(emptyTodo(), { action: "write", tasks: [{ title: "One" }, { title: "Two", status: "in_progress", note: "now" }] }, 2);
	assert.equal(error, undefined);
	assert.deepEqual(state, { tasks: [{ id: 1, title: "One", status: "pending" }, { id: 2, title: "Two", status: "in_progress", note: "now" }], nextId: 3, updatedTurn: 2 });
	assert.equal(text, "Todo list written: 2 tasks · 0 done · 1 in progress");
	assert.equal(after(emptyTodo(), { action: "write", tasks: [{ title: "x", status: "blocked", note: "n" }, { title: "y", status: "dropped" }] }).text, "Todo list written: 2 tasks · 0 done · 0 in progress · 1 blocked · 1 dropped");
	assert.equal(after(emptyTodo(), { action: "write", tasks: [{ title: "x" }] }).text, "Todo list written: 1 task · 0 done · 0 in progress");
	assert.deepEqual(after(three(), { action: "write" }).state.tasks, [], "no tasks given: an empty list");
});

test("write keeps the ids passed back, once each, and numbers the rest after the highest", () => {
	const two = after(emptyTodo(), { action: "write", tasks: [{ title: "A" }, { title: "B" }] }).state;
	const next = after(two, { action: "write", tasks: [{ id: 2, title: "B", status: "done" }, { title: "C" }] }, 2).state;
	assert.deepEqual(next.tasks.map((task) => `${task.id}:${task.title}:${task.status}`), ["2:B:done", "3:C:pending"]);
	const odd = after(two, { action: "write", tasks: [{ id: 9, title: "far" }, { id: 9, title: "again" }, { id: 0, title: "zero" }, { id: 1.5, title: "half" }] }).state;
	assert.deepEqual(odd.tasks.map((task) => task.id), [9, 10, 11, 12]);
	assert.equal(odd.nextId, 13);
});

test("add, update and clear change one thing at a time; a missing id or an empty update is refused", () => {
	const added = after(emptyTodo(), { action: "add", title: "Write tests" });
	assert.equal(added.text, "Added #1 Write tests (pending)");
	const updated = after(added.state, { action: "update", id: 1, status: "in_progress", note: "writing tests" }, 2);
	assert.equal(updated.error, undefined);
	assert.deepEqual(updated.state.tasks[0], { id: 1, title: "Write tests", status: "in_progress", note: "writing tests" });
	assert.equal(updated.text, "#1 Write tests → in_progress");
	assert.equal(after(updated.state, { action: "update", id: 1, title: "Write more tests" }).text, "#1 Write more tests updated");
	const missing = after(updated.state, { action: "update", id: 9, status: "done" });
	assert.deepEqual([missing.error, missing.text], ["no task #9", "Error: no task #9"]);
	assert.strictEqual(missing.state, updated.state);
	assert.equal(after(updated.state, { action: "update", id: 1 }).error, "nothing to update: pass status, title, or note");
	const cleared = after(updated.state, { action: "clear" }, 3);
	assert.deepEqual(cleared.state, { tasks: [], nextId: 1, updatedTurn: 3 });
	assert.equal(cleared.text, "Cleared 1 task");
	assert.equal(after(three(), { action: "clear" }).text, "Cleared 3 tasks");
	assert.deepEqual(after(emptyTodo(), { action: "rename" }), { state: emptyTodo(), text: 'Error: unknown action "rename" (use write, add, update, clear, or list)', error: 'unknown action "rename" (use write, add, update, clear, or list)' });
});

test("statuses: other words for them count, case, spaces and dashes do not matter, an unknown one is refused; titles are one clean line", () => {
	const statuses = (words: unknown[]) => after(emptyTodo(), { action: "write", tasks: words.map((status) => ({ title: "x", status, note: "n" })) as never }).state.tasks.map((task) => task.status);
	assert.deepEqual(statuses(["completed", "Complete", "doing", "TODO", " In Progress ", "in-progress", null, ""]), ["done", "done", "in_progress", "pending", "in_progress", "in_progress", "pending", "pending"]);
	const unknown = after(emptyTodo(), { action: "write", tasks: [{ title: "X", status: "later" }] });
	assert.equal(unknown.error, 'unknown status "later" (use pending, in_progress, blocked, done, or dropped)');
	assert.equal(after(emptyTodo(), { action: "add", title: "X", status: 3 as never }).error, 'unknown status "3" (use pending, in_progress, blocked, done, or dropped)');
	assert.equal(after(emptyTodo(), { action: "add", title: "Evil\x1b[31m  title\n" }).state.tasks[0]!.title, "Evil title");
	assert.equal(after(emptyTodo(), { action: "add", title: "   " }).error, "title is required");
	assert.equal(after(emptyTodo(), { action: "add", title: 7 as never }).error, "title is required");
	assert.deepEqual(after(emptyTodo(), { action: "add", title: "x", note: "  " }).state.tasks[0], { id: 1, title: "x", status: "pending" }, "a blank note is no note");
});

test("list reports the tasks and leaves the list as it was", () => {
	const state = three();
	const listed = after(state, { action: "list" }, 5);
	assert.strictEqual(listed.state, state);
	assert.equal(listed.text, "[done] #1 Read the spec\n[in_progress] #2 Write the parser — lexer first\n[pending] #3 Ship");
	assert.equal(after(emptyTodo(), { action: "list" }).text, "No tasks.");
});

test("the summary counts done and open; a list goes stale with turns while work it can do is open", () => {
	const state = three();
	// three() was written at turn 3: stale by the turns since, never below zero.
	const seen = { summary: todoSummary(state), stale: [1, 3, 5].map((turn) => staleTurns(state, turn)) };
	assert.deepEqual(seen, { summary: { done: 1, total: 3, open: 2 }, stale: [0, 0, 2] });
	const finished = after(state, { action: "write", tasks: state.tasks.map((task) => ({ ...task, status: "done" })) }, 4).state;
	const quiet = [finished, emptyTodo(), { ...state, updatedTurn: null }].map((list) => staleTurns(list, 9));
	assert.deepEqual(quiet, [0, 0, 0], "finished, empty, or never written: never stale");
});

test("the list comes back from the branch's last good todo result: Jhon's, before-T59 gentleTodo, and rpiv-todo's", () => {
	const result = (details: unknown, more: object = {}) => ({ type: "message", message: { role: "toolResult", toolName: "todo", isError: false, details, ...more } });
	const ours = result({ jhonTodo: { tasks: [{ id: 4, title: "Ours", status: "pending" }], nextId: 5, updatedTurn: 7 } });
	const saved = result({ gentleTodo: { tasks: [{ id: 6, title: "Saved", status: "done" }], nextId: 7, updatedTurn: 2 } });
	const rpiv = result({ action: "update", params: {}, tasks: [{ id: 1, subject: "Theirs", status: "completed", activeForm: "done" }, { id: 2, subject: "Gone", status: "deleted" }, { id: 3, subject: "Now", status: "in_progress", activeForm: "doing it" }, { subject: "no id" }], nextId: 4 });
	const noNext = result({ tasks: [{ id: 5, subject: "x" }, { id: 2, subject: "y" }] });
	const bash = { type: "message", message: { role: "toolResult", toolName: "bash", details: { tasks: [] } } };
	const failed = result({ jhonTodo: { tasks: [{ id: 1, title: "bad", status: "pending" }], nextId: 2 } }, { isError: true });
	assert.deepEqual(replayTodo([bash, rpiv]), { tasks: [{ id: 1, title: "Theirs", status: "done" }, { id: 3, title: "Now", status: "in_progress", note: "doing it" }], nextId: 4, updatedTurn: null });
	assert.equal(replayTodo([noNext]).nextId, 6);
	assert.deepEqual(replayTodo([rpiv, ours]), { tasks: [{ id: 4, title: "Ours", status: "pending" }], nextId: 5, updatedTurn: 7 });
	assert.deepEqual(replayTodo([rpiv, saved]).tasks, [{ id: 6, title: "Saved", status: "done" }]);
	assert.equal(replayTodo([saved, ours]).tasks[0]!.title, "Ours");
	assert.equal(replayTodo([ours, failed, result(undefined), null]).tasks[0]!.title, "Ours", "a failed result, no details or no entry change nothing");
	assert.equal(replayTodo([result({ jhonTodo: { tasks: [], nextId: 3 } })]).updatedTurn, null);
	assert.deepEqual(replayTodo([]), { tasks: [], nextId: 1, updatedTurn: null }, "no result on the branch: the empty list");
	assert.deepEqual(todoDetails({ jhonTodo: undefined, gentleTodo: { x: 1 } }), { state: undefined }, "Jhon's key first, even when empty");
	assert.equal(todoDetails({}), undefined);
	assert.equal(todoDetails(undefined), undefined);
});

test("the run's block: the open list with its rules, stale from two turns, nothing when nothing is open", () => {
	const block = todoPromptBlock(three(), 0)!;
	assert.equal(block, [
		"## Todo list",
		"Keep it current with the `todo` tool: mark a task in_progress before starting it, done right after finishing it, and rewrite the whole list with `write` whenever the plan changes. Update it before you end the turn.",
		"A blocked task waits on something outside the list: do not work on it until its condition holds. When the plan changes, mark the tasks it made obsolete dropped instead of leaving them pending or marking them done.",
		"1. [done] Read the spec",
		"2. [in_progress] Write the parser — lexer first",
		"3. [pending] Ship",
	].join("\n"));
	assert.equal(todoPromptBlock(three(), 1), block);
	assert.equal(todoPromptBlock(three(), 2), `${block}\n(stale: 2 turns without an update — bring the list up to date now)`);
	assert.equal(todoPromptBlock(emptyTodo()), undefined, "an empty list adds no block");
	assert.equal(todoPromptBlock(after(three(), { action: "write", tasks: [{ title: "A", status: "done" }] }).state, 0), undefined);
	assert.equal(todoStartBlock(three()), undefined, "the start rule only with nothing open");
	assert.match(todoStartBlock(emptyTodo())!, /^## Todo list\nInside an LDD change or not/);
});

test("dropped and blocked are statuses; cancel words mean dropped; a blocked task needs a note", () => {
	const { state, error } = after(emptyTodo(), { action: "write", tasks: [{ title: "Old plan", status: "dropped" }, { title: "Deploy", status: "blocked", note: "an admin" }, { title: "A", status: "cancelled" }, { title: "B", status: "canceled" }, { title: "C", status: "abandoned" }] });
	assert.equal(error, undefined);
	const { DROPPED, BLOCKED } = TODO_STATUS;
	assert.deepEqual(state.tasks.map(({ status }) => status), [DROPPED, BLOCKED, DROPPED, DROPPED, DROPPED]);
	const before = three();
	for (const params of [{ action: "add", title: "Deploy", status: "blocked" }, { action: "update", id: 3, status: "blocked" }, { action: "write", tasks: [{ title: "Deploy", status: "blocked", note: "   " }] }]) {
		const refused = after(before, params, 9);
		assert.deepEqual([refused.error, refused.text], ["blocked needs a note naming what it waits for", "Error: blocked needs a note naming what it waits for"]);
		assert.strictEqual(refused.state, before, params.action);
	}
	const blocked = after(before, { action: "update", id: 3, status: "blocked", note: "the API contract" }, 9);
	assert.equal(blocked.text, "#3 Ship → blocked");
	assert.equal(after(blocked.state, { action: "update", id: 3, status: "pending" }, 10).state.tasks[2]!.status, "pending");
	assert.equal(after(blocked.state, { action: "update", id: 3, status: "pending" }, 10).state.tasks[2]!.note, "the API contract", "the note stays unless replaced");
});

test("done and dropped together finish a list: not open, no block, never stale", () => {
	const state = after(emptyTodo(), { action: "write", tasks: [{ title: "A", status: "done" }, { title: "B", status: "dropped" }, { title: "C", status: "dropped" }] }).state;
	// Summary, the run's block five turns on, and the staleness at turn 9.
	assert.deepEqual([todoSummary(state), todoPromptBlock(state, 5), staleTurns(state, 9)], [{ done: 1, total: 3, open: 0 }, undefined, 0]);
});

test("waiting work is open but alone never stale; work the agent can do makes the list stale again", () => {
	const waiting = after(emptyTodo(), { action: "write", tasks: [{ title: "A", status: "done" }, { title: "Deploy", status: "blocked", note: "an admin" }] }).state;
	assert.deepEqual(todoSummary(waiting), { done: 1, total: 2, open: 1 });
	assert.equal(staleTurns(waiting, 9), 0);
	assert.match(todoPromptBlock(waiting, 0)!, /2\. \[blocked\] Deploy — an admin/);
	assert.equal(staleTurns(after(waiting, { action: "add", title: "Write docs" }).state, 3), 2);
});
