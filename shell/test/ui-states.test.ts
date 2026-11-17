import "./isolated-env.ts";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";
import { STATES } from "../lib/ui/primitives/states.ts";
import { THEME_NAME } from "../lib/ui/primitives/paint.ts";

const sources = (dir: string): string[] => readdirSync(dir, { withFileTypes: true }).flatMap((entry) =>
	entry.isDirectory() ? sources(join(dir, entry.name)) : entry.name.endsWith(".ts") ? [join(dir, entry.name)] : []);

test("one glyph per state, and each a distinct glyph", () => {
	assert.deepEqual(Object.fromEntries(Object.entries(STATES).map(([k, v]) => [k, v.glyph])),
		{ done: "✓", pending: "·", blocked: "◉", new: "●", working: "◐", failed: "✗", unknown: "?" });
	const glyphs = Object.values(STATES).map((s) => s.glyph);
	assert.equal(new Set(glyphs).size, glyphs.length);
});

test("pending and unknown read in the table's dim role (T15.2)", () => {
	assert.equal(THEME_NAME[STATES.pending.role], "dim");
	assert.equal(THEME_NAME[STATES.unknown.role], "dim");
});

test("the shell draws no braille spinner: it renders tiny in the owner's font", () => {
	const root = new URL("..", import.meta.url).pathname;
	const found = [...sources(join(root, "lib")), ...sources(join(root, "extensions"))].flatMap((file) =>
		readFileSync(file, "utf8").split("\n").map((line, i) => ({ file, line: i + 1, text: line }))
			.filter(({ text }) => /[⠁-⣿]/.test(text) && !/^\s*(\/\/|\*)/.test(text)));
	assert.deepEqual(found.map((f) => `${f.file}:${f.line}`), []);
});
