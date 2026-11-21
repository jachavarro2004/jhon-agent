import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { stallContinueText, stallNotice, stallThreshold } from "../lib/ui/stall-recovery.ts";

const seconds = { stall: 75, thinking: 300 };

test("a direct provider with no thinking keeps stall_seconds; the bridge or a thinking model gets thinking_stall_seconds (T38, L382)", () => {
	assert.equal(stallThreshold({ provider: "anthropic", reasoning: true }, "off", seconds), 75);
	assert.equal(stallThreshold({ provider: "anthropic", reasoning: true }, "minimal", seconds), 75);
	assert.equal(stallThreshold({ provider: "openai", reasoning: false }, "high", seconds), 75);
	for (const level of ["low", "medium", "high", "xhigh", "max"]) assert.equal(stallThreshold({ provider: "anthropic", reasoning: true }, level, seconds), 300, level);
	assert.equal(stallThreshold({ provider: "claude-bridge", reasoning: true }, "off", seconds), 300);
	assert.equal(stallThreshold({ provider: "claude-bridge", reasoning: false }, undefined, seconds), 300);
	// No model known: stall_seconds, as before T38.
	assert.equal(stallThreshold(undefined, undefined, seconds), 75);
	// thinking_stall_seconds 0: never in that case; stall_seconds 0: never at all.
	assert.equal(stallThreshold({ provider: "claude-bridge" }, "off", { stall: 75, thinking: 0 }), 0);
	assert.equal(stallThreshold({ provider: "anthropic", reasoning: true }, "off", { stall: 75, thinking: 0 }), 75);
	assert.equal(stallThreshold({ provider: "claude-bridge" }, "high", { stall: 0, thinking: 300 }), 0);
	assert.equal(stallThreshold({ provider: "anthropic" }, "off", { stall: 0, thinking: 300 }), 0);
});

test("the notice and the continue text name the real seconds; the continue, sent as user text, carries the shell's mark (T194)", () => {
	assert.equal(stallNotice(75), "Claude API did not answer in 75s; retried");
	assert.equal(stallContinueText(300), "[From the Jhon shell, not the owner; it does not set the reply language] The Claude API sent no answer for 300s; the turn was restarted. Continue where you stopped.");
});
