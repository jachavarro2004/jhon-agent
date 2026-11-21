/**
 * T174-S14 (L1493): the adapted parts of extensions/ui.ts were rewritten in
 * Jhon's own code with no word changed that the model reads.
 * fixtures/ui-model-text.json was taken from the code before the rewrite
 * (4182f3f) by ui-tool-capture.ts: the session_worktree_register tool as
 * registered, its result and its refusals.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { capture } from "./ui-tool-capture.ts";

const snapshot = JSON.parse(readFileSync(new URL("./fixtures/ui-model-text.json", import.meta.url), "utf8"));

test("session_worktree_register reads as before, byte for byte: definition, result and refusals", async () => {
	const now = await capture();
	assert.deepEqual(now, snapshot);
	assert.equal(JSON.stringify(now), JSON.stringify(snapshot));
	assert.ok(JSON.stringify(snapshot).includes("is now one of this session's worktrees."));
});
