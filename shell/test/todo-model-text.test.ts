/**
 * T174-S14 (L1493): lib/todo.ts, extensions/todo.ts and
 * lib/ui/append-system-prompt.ts were rewritten in Jhon's own code with no
 * word changed that the model reads. fixtures/todo-model-text.json was taken
 * from the code before the rewrite (4182f3f) by todo-capture.ts: the tool as
 * registered in a main session and a child, every result and refusal, the
 * list block and start rule of each run, the nudges, the pure list functions
 * and the appended-prompt helpers.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { capture } from "./todo-capture.ts";

const snapshot = JSON.parse(readFileSync(new URL("./fixtures/todo-model-text.json", import.meta.url), "utf8"));

test("the todo tool is registered as before, byte for byte, schema key order included", async () => {
	const now = await capture();
	assert.deepEqual(now.definitions, snapshot.definitions);
	assert.equal(JSON.stringify(now.definitions), JSON.stringify(snapshot.definitions));
});

test("every result, refusal, run block and nudge reads as before, byte for byte", async () => {
	const now = await capture();
	assert.equal(now.scripts.length, snapshot.scripts.length);
	for (const [i, script] of now.scripts.entries()) assert.deepEqual(script, snapshot.scripts[i], `script ${i}`);
	assert.deepEqual(now.pure, snapshot.pure);
	assert.deepEqual(now.appended, snapshot.appended);
	assert.equal(JSON.stringify(now), JSON.stringify(snapshot));
	const text = JSON.stringify(snapshot);
	for (const needle of ["jhon-todo-reminder", "jhon-todo-start", "Error: a blocked task needs a note", "stale: "]) assert.ok(text.includes(needle), `the snapshot holds ${needle}`);
});
