import assert from "node:assert/strict";
import test from "node:test";
import { sanitizeTerminalText, stripAnsi } from "../lib/ui/terminal-theme.ts";

test("sanitizeTerminalText removes user-controlled ANSI and control characters", () => {
	assert.equal(
		sanitizeTerminalText("safe\x1b[31mred\x1b[0m\x1b]52;c;Zm9v\u0007text"),
		"saferedtext",
	);
});

test("sanitizeTerminalText removes unterminated OSC payloads", () => {
	assert.equal(sanitizeTerminalText("safe\x1b]52;c;Zm9vtext"), "safe");
	assert.equal(sanitizeTerminalText("safe\x9d52;c;Zm9vtext"), "safe");
});

test("stripAnsi removes OSC payloads and ANSI styling", () => {
	assert.equal(
		stripAnsi("safe \x1b]52;c;Zm9v\u0007\x1b[31mred\x1b[0m"),
		"safe red",
	);
});

test("stripAnsi removes APC payloads such as pi-tui's cursor markers", () => {
	assert.equal(stripAnsi("a\x1b_pi:c\x07\x1b_pi:fc\x07b\x1b_pi:/fc\x07c"), "abc");
	assert.equal(sanitizeTerminalText("safe\x1b_unterminated"), "safe");
});
