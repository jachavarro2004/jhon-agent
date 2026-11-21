/**
 * T174-S13 (L1493): the ask view and lib/ask.ts were rewritten in Jhon's own
 * code with no word changed that the model reads. The snapshot in
 * fixtures/ask-model-text.json was taken from the code before the rewrite
 * (0ce5178): the ask_user tool as registered (name, label, description,
 * schema, in the main session and in a child), every refusal, the tool's
 * own results, and the answer of every scripted round through the question
 * view and through pi's dialogs, `<text> (typed)` included (ask-capture.ts).
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { capture } from "./ask-capture.ts";

const snapshot = JSON.parse(readFileSync(new URL("./fixtures/ask-model-text.json", import.meta.url), "utf8"));

test("ask_user is registered as before, byte for byte", async () => {
	const now = await capture();
	assert.deepEqual(now.definitions, snapshot.definitions);
	// Key order too: the schema reaches the model as JSON.
	assert.equal(JSON.stringify(now.definitions), JSON.stringify(snapshot.definitions));
});

test("every refusal, tool result and answer reads as before, byte for byte, (typed) included", async () => {
	const now = await capture();
	assert.deepEqual(now.refused, snapshot.refused);
	assert.deepEqual(now.executions, snapshot.executions);
	assert.equal(now.rounds.length, snapshot.rounds.length);
	for (const [i, round] of now.rounds.entries()) assert.deepEqual(round, snapshot.rounds[i], `round ${i}`);
	assert.equal(JSON.stringify(now), JSON.stringify(snapshot));
	assert.ok(JSON.stringify(snapshot).includes("Valkey (typed)"), "the snapshot holds a typed answer");
});
