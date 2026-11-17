import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import {
	applyTodo,
	emptyTodo,
	renderTodoCard,
	renderTodoSection,
	replayTodo,
	staleTurns,
	TODO_STATUS,
	todoPromptBlock,
	todoSummary,
	type TodoState,
} from "../lib/ui/shell-todo.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";

// The todo list: a task list the model rewrites as it works. The reducer is
// pure, the state replays from the session branch, and staleness is derived
// from how many turns passed since the last write while tasks stay open.

const plainTheme = {
	fg(_color: string, text: string) {
		return text;
	},
	strikethrough(text: string) {
		return `~${text}~`;
	},
};

/** The card's rows without the trailing blank row the rail trims. */
function rows(lines: readonly string[]): string[] {
	return lines.at(-1) === "" ? lines.slice(0, -1) : [...lines];
}
/** A rail card's body (T15.8): its rows between the sides, the borders gone, each trimmed. */
function inside(lines: readonly string[]): string[] {
	return lines.filter((line) => !/^[╭╰+]/.test(line) && line !== "").map((line) => line.replace(/^[│|] /, "").replace(/ [│|]$/, "").trim());
}

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

test("a folded card with long tasks is its heading and the current task, at the width, without losing task data (G's card)", () => {
	const title = "界🙂 long title ".repeat(100);
	const note = "long description ".repeat(100);
	for (const status of ["in_progress", "pending", "blocked"]) {
		const state = applyTodo(emptyTodo(), { action: "write", tasks: [{ title, note, status }] }, 1).state;
		const snapshot = structuredClone(state);
		for (const width of [16, 40, 70]) {
			for (const staleTurns of [0, 4]) {
				const lines = rows(renderTodoCard(state, plainTheme, width, { collapsed: true, staleTurns, scrollable: true }));
				assert.equal(lines.length, 3, "the top rule, the current task, the bottom rule");
				assert.match(stripAnsi(lines[0]!), /^╭─ 力 Todos/);
				for (const line of lines) assert.equal(visibleWidth(line), width);
			}
		}
		assert.deepEqual(state, snapshot);
	}
});

test("scrollable Todo: every task in its order, as G's body rows; a done task struck, never folded (T15.12e, L160)", () => {
	const theme = { ...plainTheme, strikethrough: (text: string) => `\x1b[9m${text}\x1b[29m` };
	const state = applyTodo(emptyTodo(), { action: "write", tasks: [
		{ title: "Done one", status: "done" },
		{ title: "ACTIVE", status: "in_progress", note: "writing" },
		{ title: "PENDING", status: "pending" },
		{ title: "BLOCKED", status: "blocked", note: "waiting" },
		{ title: "DROPPED", status: "dropped" },
	] }, 1).state;
	const lines = renderTodoCard(state, theme, 40, { collapsed: false, staleTurns: 0, scrollable: true });
	const body = lines.slice(1, -1).map(stripAnsi);
	const at = (word: string) => body.findIndex((line) => line.includes(word));
	assert.deepEqual(["Done one", "ACTIVE", "PENDING", "BLOCKED", "DROPPED"].map(at), [0, 1, 2, 3, 4], body.join("\n"));
	assert.match(body[1]!, /^│ ◐ ACTIVE · writing +│$/);
	assert.ok(lines.join("\n").includes("\x1b[9mDone one\x1b[29m"), "the done task is struck, as G's");
	for (const line of lines) assert.ok(visibleWidth(line) <= 40);
});

test("non-scrollable Todo caps physical wrapped rows, not task count", () => {
	const state = applyTodo(emptyTodo(), { action: "write", tasks: [{ title: "long title ".repeat(300), status: "in_progress", note: "long note ".repeat(300) }] }, 1).state;
	const rows = renderTodoCard(state, plainTheme, 40, { collapsed: false, staleTurns: 4 });
	assert.ok(rows.length <= 15, "the cap, and the bottom border");
	assert.match(stripAnsi(rows.at(-2)!), /…/);
});

test("renderTodoCard draws the section with status glyphs and keeps every line at width", () => {
	const lines = rows(renderTodoCard(seeded(), plainTheme, 60, { collapsed: false, staleTurns: 0 }));
	for (const line of lines) assert.equal(visibleWidth(line), 60, `"${stripAnsi(line)}" is not 60 wide`);
	const plain = lines.map(stripAnsi);
	assert.match(plain[0], /^╭─ 力 Todos ▾ Collapse · 1 of 3 ─+ alt\+t collapse ╮$/, "G's card: the glyph, Todos and its control, done of planned, the key and its action");
	assert.match(plain[1], /^│ ✓ ~Add quiet tool rendering~ +│$/, "G's body rows: a done task keeps its row, struck");
	assert.match(plain[2], /^│ ◐ Fix quiet tools conflict · fixing conflict +│$/);
	assert.match(plain[3], /^│ · Show git bash tails +│$/);
	assert.match(plain[4], /^╰─+╯$/);
	assert.equal(plain.length, 5);
});

test("renderTodoCard folds a long list: done tasks become one row and the open ones are capped", () => {
	const tasks = Array.from({ length: 40 }, (_, index) => ({ title: `Task ${index + 1}`, status: index < 25 ? "done" : index === 25 ? "in_progress" : "pending" }));
	const state = applyTodo(emptyTodo(), { action: "write", tasks }, 1).state;
	const plain = rows(renderTodoCard(state, plainTheme, 60, { collapsed: false, staleTurns: 0 })).map(stripAnsi);
	assert.equal(plain.length, 14, "the top border, twelve rows, the bottom border");
	assert.match(plain[0], /^╭─ 力 Todos ▾ Collapse · 25 of 40 ─+ alt\+t collapse ╮$/);
	assert.match(plain[1], /^│ ✓ 25 done +│$/);
	assert.match(plain[2], /^│ ◐ Task 26 +│$/);
	assert.match(plain[11], /^│ · Task 35 +│$/);
	assert.match(plain[12], /^│ … 5 more +│$/);
	const allDone = applyTodo(emptyTodo(), { action: "write", tasks: tasks.map((task) => ({ ...task, status: "done" })) }, 1).state;
	const folded = rows(renderTodoCard(allDone, plainTheme, 60, { collapsed: false, staleTurns: 0 })).map(stripAnsi);
	assert.equal(folded.length, 3);
	assert.match(folded[1], /^│ ✓ 40 done +│$/);
});

test("renderTodoCard folds to the current task, ▸ Expand in its title; stale state stays visible (G's card)", () => {
	const freshExpanded = rows(renderTodoCard(seeded(), plainTheme, 70, { collapsed: false, staleTurns: 0 })).map(stripAnsi);
	assert.match(freshExpanded[0], /^╭─ 力 Todos ▾ Collapse · 1 of 3 ─+ alt\+t collapse ╮$/);

	for (const staleTurns of [0, 2]) {
		const folded = rows(renderTodoCard(seeded(), plainTheme, 70, { collapsed: true, staleTurns })).map(stripAnsi);
		assert.equal(folded.length, 3, "the top rule, the current task, the bottom rule");
		assert.match(folded[0], /^╭─ 力 Todos ▸ Expand · 1 of 3 ─+ alt\+t expand ╮$/);
		assert.match(folded[1], staleTurns ? /^│ stale · 2 turns · ◐ Fix quiet tools conflict/ : /^│ ◐ Fix quiet tools conflict/);
	}

	const staleExpanded = rows(renderTodoCard(seeded(), plainTheme, 70, { collapsed: false, staleTurns: 2 })).map(stripAnsi);
	assert.match(staleExpanded[1], /^│ stale · 2 turns +│$/);

	assert.deepEqual(renderTodoCard(emptyTodo(), plainTheme, 70, { collapsed: false, staleTurns: 0 }), []);
	for (const collapsed of [false, true]) {
		const line = renderTodoCard(seeded(), plainTheme, 18, { collapsed, staleTurns: 0 })[0]!;
		assert.match(stripAnsi(line), new RegExp(`^╭─ 力 Todos ${collapsed ? "▸" : "▾"} ·`), "under 28 columns the control is its glyph alone");
		assert.equal(visibleWidth(line), 18);
	}
});

// G's body rows (T15.12e, L160): up to twelve tasks each keep their row in
// order, a done one struck; a longer list folds done and dropped into counts.
test("the card's body rows are G's: every task in order, a done task struck, until the list outgrows the card", () => {
	const theme = {
		fg: (color: string, text: string) => `<${color}>${text}</${color}>`,
		strikethrough: (text: string) => `\x1b[9m${text}\x1b[29m`,
	};
	const state = applyTodo(emptyTodo(), { action: "write", tasks: [
		{ title: "Pending first", status: "pending" },
		{ title: "Alpha beta", status: "done" },
		{ title: "Active", status: "in_progress" },
		{ title: "Second done", status: "done" },
	] }, 1).state;
	for (const options of [{ collapsed: false, staleTurns: 0 }, { collapsed: false, staleTurns: 0, scrollable: true }]) {
		const body = renderTodoCard(state, theme, 120, options).slice(1, -1);
		const untagged = (row: string) => stripAnsi(row.replace(/<\/?[a-zA-Z]+>/g, ""));
		assert.deepEqual(body.map((row) => untagged(row).replace(/ *│$/, "")), ["│ · Pending first", "│ ✓ Alpha beta", "│ ◐ Active", "│ ✓ Second done"]);
		assert.ok(body[1]!.includes("\x1b[9mAlpha beta\x1b[29m"), "struck");
	}
	const allDone = applyTodo(emptyTodo(), { action: "write", tasks: [{ title: "A", status: "done" }] }, 1).state;
	assert.match(stripAnsi(renderTodoCard(allDone, theme, 60, { collapsed: true, staleTurns: 0 })[1]!.replace(/<\/?[a-zA-Z]+>/g, "")), /^│ · 0 open/, "folded with nothing open: G's `· N open`");
});

test("renderTodoCard titles the card as the Gentle Shell's: the title in the accent, the subtitle muted, the frame in the border role", () => {
	const taggedTheme = {
		fg(color: string, text: string) {
			return `<${color}>${text}</${color}>`;
		},
		strikethrough(text: string) {
			return text;
		},
	};
	for (const options of [{ scrollable: true, collapsed: false, staleTurns: 0 }, { collapsed: false, staleTurns: 0 }]) {
		const lines = renderTodoCard(seeded(), taggedTheme, 160, options);
		assert.match(lines[0]!, /^<border>╭<\/border><border>─ <\/border><accent>力 Todos <accent>▾ Collapse<\/accent><\/accent> <muted>·<\/muted> <muted>1 of 3<\/muted>/);
	}
});

// H1 (odd/tasks/usage-click-and-changes-attribution.md): the same shared
// hover role every other clickable surface uses.
test("renderTodoCard paints its clickable title in the shared hover role when hovered", () => {
	const taggedTheme = {
		fg(color: string, text: string) {
			return `<${color}>${text}</${color}>`;
		},
		strikethrough(text: string) {
			return text;
		},
	};
	const idle = renderTodoCard(seeded(), taggedTheme, 160, { collapsed: false, staleTurns: 0 });
	assert.match(idle[0], /<accent>▾ Collapse<\/accent>/, "idle: the control in the accent");
	const hovered = renderTodoCard(seeded(), taggedTheme, 160, { collapsed: false, staleTurns: 0, hovered: true });
	assert.match(hovered[0], /<warning>▾ Collapse<\/warning>/, "hovering paints the shared hover role");
});

test("renderTodoCard keeps stale indicators in scrollable lists", () => {
	const tasks = Array.from({ length: 40 }, (_, index) => ({ title: `Task ${index + 1}`, status: index < 25 ? "done" : "pending" }));
	const state = applyTodo(emptyTodo(), { action: "write", tasks }, 1).state;
	const lines = rows(renderTodoCard(state, plainTheme, 70, { scrollable: true, collapsed: false, staleTurns: 2 })).map(stripAnsi);
	assert.equal(lines.length, 43, "the top border, the stale row, every task (G's scrollable rows), the bottom border");
	assert.match(lines[0], /^╭─ 力 Todos ▾ Collapse · 25 of 40 ─+ alt\+t collapse ╮$/);
	assert.match(lines[1], /^│ stale · 2 turns +│$/);
	assert.match(lines[2], /^│ ✓ ~Task 1~ +│$/);
	assert.match(lines[27], /^│ · Task 26 +│$/);
	assert.match(lines[41], /^│ · Task 40 +│$/);
	for (const line of lines) assert.equal(visibleWidth(line), 70);
});

test("renderTodoCard remains width-safe at every width", () => {
	for (const width of [0, 1, 2, 3, 4, 5, 8, 16]) {
		const lines = rows(renderTodoCard(seeded(), plainTheme, width, { collapsed: false, staleTurns: 2 }));
		for (const line of lines) assert.ok(visibleWidth(line) <= width, `"${stripAnsi(line)}" is wider than ${width}`);
	}
});

// L75: in the float style the Todos card is a filled panel like Gentle
// Shell's rail cards, with its clickable header on row 1 (panelHeaderRow).
test("a long done todo wraps before it is struck: every physical row closes its own SGR 9, as G's", () => {
	const state = applyTodo(emptyTodo(), { action: "write", tasks: [{ title: "word ".repeat(20).trim(), status: "done" }] }, 1).state;
	const sgr = { ...plainTheme, strikethrough: (text: string) => `\x1b[9m${text}\x1b[29m` };
	const lines = rows(renderTodoCard(state, sgr, 40, { collapsed: false, staleTurns: 0 }));
	assert.ok(lines.length > 3, "the title wraps over several rows");
	assert.match(stripAnsi(lines[1]!), /^│ ✓ word word/);
	for (const row of lines.slice(1, -1)) {
		assert.equal((row.match(/\x1b\[9m/g) ?? []).length, (row.match(/\x1b\[29m/g) ?? []).length, `struck and closed on its own row: ${JSON.stringify(row)}`);
		assert.ok(row.includes("\x1b[9m"));
	}
	for (const row of lines) assert.equal(visibleWidth(row), 40);
});

// #1814 dropped: terminal, never counts as open, reads apart from done.
// #1820 blocked: open but not actionable, always carries its reason.

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

test("renderTodoCard draws blocked with its reason and dropped dim without strikethrough, excluding dropped from the count", () => {
	const state = applyTodo(
		emptyTodo(),
		{ action: "write", tasks: [{ title: "Ship", status: "done" }, { title: "Old plan", status: "dropped" }, { title: "Deploy", status: "blocked", note: "waiting for an admin" }, { title: "Docs" }] },
		1,
	).state;
	const plain = renderTodoCard(state, plainTheme, 60, { collapsed: false, staleTurns: 0 }).map(stripAnsi);
	assert.match(plain[0], /^╭─ 力 Todos ▾ Collapse · 1 of 3 ─+ alt\+t collapse ╮$/);
	assert.match(plain[2], /^│ ✕ Old plan +│$/);
	assert.match(plain[3], /^│ ◉ Deploy · waiting for an admin +│$/);
	const roles: string[] = [];
	const spy = { ...plainTheme, fg(color: string, text: string) { roles.push(`${color}:${text}`); return text; } };
	renderTodoCard(state, spy, 60, { collapsed: false, staleTurns: 0 });
	assert.ok(roles.includes("muted:✕") && roles.includes("dim:Old plan"), "dropped: muted glyph, dim title");
	assert.ok(roles.includes("error:◉"), "blocked: needs-you glyph");
});


test("renderTodoCard folds dropped tasks next to done ones in a long list", () => {
	const tasks = Array.from({ length: 20 }, (_, index) => ({ title: `Task ${index + 1}`, status: index < 5 ? "done" : index < 8 ? "dropped" : "pending" }));
	const state = applyTodo(emptyTodo(), { action: "write", tasks }, 1).state;
	const plain = rows(renderTodoCard(state, plainTheme, 60, { collapsed: false, staleTurns: 0 })).map(stripAnsi);
	assert.equal(plain.length, 14);
	assert.match(plain[0], /^╭─ 力 Todos ▾ Collapse · 5 of 17 ─+ alt\+t collapse ╮$/);
	assert.match(plain[1], /^│ ✓ 5 done +│$/);
	assert.match(plain[2], /^│ ✕ 3 dropped +│$/);
	assert.match(plain[3], /^│ · Task 9 +│$/);
	assert.match(plain[12], /^│ … 3 more +│$/);
});

// T15.12d (L158): the rail's TODOS is the Gentle Shell's panel section: the
// count in G's `done/planned` form, its fold key dim before it, a stale list
// saying so in the count (warning) with no row of its own.
test("renderTodoSection: `▾ TODOS … alt+t  d/p`, done and dropped folded into one row each, the task in progress in full with its note", () => {
	const state = applyTodo(emptyTodo(), { action: "write", tasks: [
		{ title: "Ship", status: "done" },
		{ title: "Old plan", status: "dropped" },
		{ title: "Fix quiet tools conflict and every caller that reads it", status: "in_progress", note: "fixing conflict" },
		{ title: "Show git bash tails", status: "pending" },
	] }, 1).state;
	const section = renderTodoSection(state, plainTheme, 40, { collapsed: false, staleTurns: 0, collapseKey: "alt+t" });
	assert.match(section.header, /^▾ TODOS +alt\+t  1\/3$/);
	assert.deepEqual(section.body.map((row) => row.trimEnd()), [
		"  ✓ 1 done",
		"  ✕ 1 dropped",
		"  ◐ Fix quiet tools conflict and every",
		"    caller that reads it",
		"    └ fixing conflict",
		"  · Show git bash tails",
	]);
	for (const row of [section.header, ...section.body]) assert.equal(visibleWidth(row), 40, row);
	const tagged = { ...plainTheme, fg: (c: string, t: string) => `<${c}>${t}</${c}>`, bold: (t: string) => `<b>${t}</b>` };
	const stale = renderTodoSection(state, tagged, 120, { collapsed: false, staleTurns: 3, collapseKey: "alt+t" });
	assert.match(stale.header, /<muted>▾<\/muted> <text><b>TODOS<\/b><\/text> +<warning><dim>alt\+t<\/dim>  stale · 3 turns · 1\/3<\/warning>$/, "stale says so in the count, in the warning role");
	assert.ok(!stale.body.some((row) => row.includes("stale")), "no stale row of its own");
	const folded = renderTodoSection(state, plainTheme, 40, { collapsed: true, staleTurns: 0, collapseKey: "alt+t" });
	assert.match(folded.header, /^▸ TODOS +alt\+t  1\/3$/);
	assert.deepEqual(folded.body.map((row) => row.trimEnd()), ["  ◐ Fix quiet tools conflict and every …"], "folded: the current task, clipped");
	// L175: an empty list is TODOS idle, as AGENTS idle reads.
	const idle = renderTodoSection(emptyTodo(), plainTheme, 40, { collapsed: false, staleTurns: 0 });
	assert.match(idle.header, /^▾ TODOS +idle$/);
	assert.deepEqual(idle.body.map((row) => row.trim()), ["no tasks"]);
});
