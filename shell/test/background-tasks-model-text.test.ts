/**
 * T174-S11/S12 (L1493): the background tasks were rewritten in Jhon's own
 * code with no word changed that the model reads. The snapshot in
 * fixtures/background-tasks-model-text.json was taken from the code before
 * the rewrite (4e05e7e): the registered tools (names, labels, descriptions,
 * schemas), the commands and keys, and every tool result, error, notice and
 * notice card of one scripted session (background-tasks-capture.ts).
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { capture } from "./background-tasks-capture.ts";

const snapshot = JSON.parse(readFileSync(new URL("./fixtures/background-tasks-model-text.json", import.meta.url), "utf8"));

test("the tools, commands and keys are registered as before, byte for byte", async () => {
	const now = await capture() as typeof snapshot;
	assert.deepEqual(now.definitions, snapshot.definitions);
	// Key order too: the schemas reach the model as JSON.
	assert.equal(JSON.stringify(now.definitions), JSON.stringify(snapshot.definitions));
});

test("every tool result, error, notice and notice card reads as before, byte for byte", async () => {
	const now = await capture() as typeof snapshot;
	assert.equal(now.log.length, snapshot.log.length);
	for (const [i, entry] of now.log.entries()) assert.deepEqual(entry, snapshot.log[i], `entry ${i}`);
	assert.equal(JSON.stringify(now.log), JSON.stringify(snapshot.log));
});
