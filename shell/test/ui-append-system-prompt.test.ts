import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { SESSION_SECTION, appendSessionOnce, appendSystemPromptOnce } from "../lib/ui/append-system-prompt.ts";

// The extensions write their blocks into systemPromptOptions; a provider
// bridge forwards only those parts, never a returned replacement prompt.

test("the first block lands without its leading newlines", () => {
	const options = { appendSystemPrompt: "" };
	appendSystemPromptOnce(options, "\n\n\nJhon rules");
	assert.equal(options.appendSystemPrompt, "Jhon rules");
});

test("a block follows what the owner appended, after one blank line", () => {
	const options = { appendSystemPrompt: "owner's APPEND_SYSTEM.md" };
	appendSystemPromptOnce(options, "\nJhon rules");
	assert.equal(options.appendSystemPrompt, "owner's APPEND_SYSTEM.md\n\nJhon rules");
});

test("a handler that runs twice adds its block once", () => {
	const options = { appendSystemPrompt: "" };
	for (let run = 0; run < 3; run++) appendSystemPromptOnce(options, "\n\nJhon rules");
	assert.equal(options.appendSystemPrompt, "Jhon rules");
});

test("blocks from two extensions both stay, in the order they came", () => {
	const options = { appendSystemPrompt: "" };
	appendSystemPromptOnce(options, "Jhon rules");
	appendSystemPromptOnce(options, "\n\nMemory");
	assert.equal(options.appendSystemPrompt, "Jhon rules\n\nMemory");
});

test("nothing, or only newlines, changes nothing; no options is not an error", () => {
	const options = { appendSystemPrompt: "as it was" };
	for (const text of ["", "\n", "\n\n\n"]) appendSystemPromptOnce(options, text);
	assert.equal(options.appendSystemPrompt, "as it was");
	for (const missing of [undefined, null]) {
		assert.doesNotThrow(() => appendSystemPromptOnce(missing, "Jhon rules"));
		assert.doesNotThrow(() => appendSessionOnce(missing, "Jhon rules"));
	}
});

// T45b: what varies per run goes to the session section, drawn after the appended prompt.
test("the session section takes varying text the same way, and leaves the appended prompt alone", () => {
	const options: { appendSystemPrompt: string; sections?: Record<string, string> } = { appendSystemPrompt: "fixed" };
	appendSessionOnce(options, "\n\nRun 1");
	appendSessionOnce(options, "Todo");
	appendSessionOnce(options, "\nTodo");
	assert.equal(options.appendSystemPrompt, "fixed");
	assert.deepEqual(options.sections, { [SESSION_SECTION]: "Run 1\n\nTodo" });
	assert.equal(SESSION_SECTION, "jhon_session");
});

test("other sections are kept, and only newlines open no section", () => {
	const options: { sections?: Record<string, string> } = {};
	appendSessionOnce(options, "\n\n");
	assert.equal(options.sections, undefined);
	options.sections = { other: "theirs" };
	appendSessionOnce(options, "mine");
	assert.deepEqual(options.sections, { other: "theirs", [SESSION_SECTION]: "mine" });
});
