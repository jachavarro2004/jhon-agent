import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { sanitizeTerminalText, stripAnsi } from "../lib/ui/terminal-theme.ts";

// Outside text is drawn only after its escapes (and, for sanitizeTerminalText,
// its control characters) are gone: a tool's output can neither recolour the
// screen nor reach the clipboard (OSC 52).

test("colours and a clipboard write vanish; the words around them stay", () => {
	assert.equal(stripAnsi("a \x1b]52;c;aGk=\x07\x1b[1;32mgreen\x1b[0m"), "a green");
	assert.equal(sanitizeTerminalText("x\x1b[4mund\x1b[24m\x1b]52;c;aGk=\x07y"), "xundy");
});

test("a string command with no terminator takes the rest of the text with it", () => {
	assert.equal(sanitizeTerminalText("kept\x1b]0;title never closed"), "kept");
	assert.equal(sanitizeTerminalText("kept\x9d0;8-bit title"), "kept");
	assert.equal(sanitizeTerminalText("kept\x1b_never closed"), "kept");
});

test("pi-tui's APC cursor marks go, every terminator form", () => {
	assert.equal(stripAnsi("x\x1b_pi:c\x07y\x1b_pi:fc\x1b\\z\x9fpi\x9c!"), "xyz!");
});

test("two-byte escapes and 8-bit CSI go; an incomplete CSI and a lone ESC [ stay", () => {
	assert.equal(stripAnsi("a\x1bMb\x1b\\c\x9b2Jd"), "abcd");
	assert.equal(stripAnsi("a\x1b[12"), "a\x1b[12");
	assert.equal(sanitizeTerminalText("a\x1b[12"), "a[12", "the ESC itself is a control character");
});

test("controls go, a tab and a newline stay, DEL and C1 go", () => {
	assert.equal(sanitizeTerminalText("a\tb\nc\r\x00\x08\x0b\x7f\x85d😀"), "a\tb\ncd😀");
	assert.equal(stripAnsi("a\r\x00b"), "a\r\x00b", "stripAnsi leaves controls alone");
});

test("a sequence split by a string command is removed once the command is gone", () => {
	assert.equal(stripAnsi("a\x1b\x1b]x\x07[31mb"), "ab");
});
