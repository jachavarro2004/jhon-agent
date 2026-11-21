// T91 (L650): every tool call is a visible card: bash shows its full command,
// its output tail and how long it took; todo and other tools show their result
// line; read shows its path. shell.appearance.density "minimal" keeps the
// collapsed (quiet) cards. T174-S6: the look is design/shell-tui 01's
// (ui-tool-card-rows.test.ts pins the rows).
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { initTheme, keyHint } from "@earendil-works/pi-coding-agent";
import { Text } from "@earendil-works/pi-tui";
import productionQuietTools from "../extensions/quiet.ts";
import { installShellSettings } from "../lib/settings.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { memorySettings } from "./fake-jhon-settings.ts";

initTheme("dark");

const theme = { bold: (v: string) => v, fg: (_c: string, v: string) => v };

function resolver() {
	const tools = new Map<string, any>();
	const resolvers: Array<(name: string, next: () => unknown) => unknown> = [];
	const pi = {
		registerTool(tool: any) { tools.set(tool.name, tool); },
		registerToolRenderer(r: (name: string, next: () => unknown) => unknown) { resolvers.push(r); },
		registerCommand() {}, on() {},
	};
	productionQuietTools(pi as any);
	const resolve = (name: string, native: unknown) => resolvers.reduceRight<() => unknown>((next, r) => () => r(name, next), () => native)() as any;
	return { tools, resolve };
}

function row(tool: any, args: Record<string, unknown>, text: string, extra: Record<string, unknown> = {}, width = 100): string {
	const context = { args, toolCallId: "c1", invalidate() {}, cwd: "/repo", executionStarted: true, argsComplete: true, isPartial: false, expanded: false, showImages: false, isError: false, state: {}, ...extra };
	const call = tool.renderCall(args, theme, context);
	const body = tool.renderResult({ content: [{ type: "text", text }] }, { expanded: context.expanded === true, isPartial: false }, theme, context);
	return [...call.render(width), ...body.render(width)].map((l: string) => stripAnsi(l)).join("\n");
}

function withDensity(t: any, density: string) {
	const previous = installShellSettings(memorySettings({ "shell.appearance.density": density }));
	t.after(() => { installShellSettings(previous); });
}

test("full (default): a bash card shows the full multi-line command, the output tail under the expand hint, and its duration at the right", () => {
	const bash = resolver().resolve("bash", { marker: "native" });
	assert.equal(typeof bash.renderCall, "function", "Jhon draws bash as a card");
	const command = "cat <<'EOF' > notes.txt\nfirst\nEOF\nwc -l notes.txt";
	const output = Array.from({ length: 8 }, (_, i) => `out ${i + 1}`).join("\n");
	const card = row(bash, { command }, output, { durationMs: 400 });
	for (const line of ["cat <<'EOF' > notes.txt", "first", "EOF", "wc -l notes.txt"]) assert.match(card, new RegExp(line.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
	assert.match(card.split("\n")[0]!, /^  \$ cat <<'EOF' > notes\.txt +✓ 0\.4s  $/, "the duration at the header's right (T174-S6: was a `Took 0.4s` row)");
	assert.match(card, /out 4 *\n +out 5/);
	assert.match(card, /out 8/);
	assert.doesNotMatch(card, /out 3\b/, "the tail keeps the last five lines");
	assert.match(card, new RegExp(`\\.\\.\\. \\(3 earlier lines, ${stripAnsi(keyHint("app.tools.expand", "to expand"))}\\)`));
	assert.doesNotMatch(card, /↳ \d+ lines/, "not collapsed to a count");
	const expanded = row(bash, { command }, output, { durationMs: 400, expanded: true });
	assert.match(expanded, /out 1\b/);
	assert.doesNotMatch(expanded, /earlier lines/);
});

test("full: a failed bash card keeps Jhon's failure headline above the output, its duration at the right", () => {
	const bash = resolver().resolve("bash", undefined);
	const text = "FAILED test_a.py::test_x - assert 1 == 2\n1 failed, 3 passed\n\nCommand exited with code 1";
	const card = row(bash, { command: "pytest" }, text, { isError: true, durationMs: 1200 });
	assert.match(card.split("\n")[0]!, /✗ 1\.2s  $/);
	assert.match(card, /exited 1 · 1 failed · 3 passed/);
	assert.match(card, /✗ test_a\.py::test_x — assert 1 == 2/);
});

test("full: a read card shows its path and a short result", () => {
	const read = resolver().tools.get("read");
	const card = row(read, { path: "src/app.ts" }, "line one\nline two");
	assert.match(card, /read  src\/app\.ts/);
	assert.match(card.split("\n")[0]!, /↳ 2 lines  $/);
	assert.match(card, /line one/);
});

test("full: a todo card keeps `todo · add` and its result line", () => {
	const todo = {
		renderShell: "self",
		renderCall: (args: any) => new Text(`todo · ${args.action}`, 0, 0),
		renderResult: (result: any) => new Text(result.content[0].text.split("\n")[0], 0, 0),
	};
	const resolved = resolver().resolve("todo", todo);
	const card = row(resolved, { action: "add" }, "Added #3736 write the tests\n(1 open)");
	assert.match(card, /todo · add/);
	assert.match(card, /Added #3736 write the tests/);
});

test("quiet (density minimal): bash says `↳ N lines` and a short preview, without its duration", (t) => {
	withDensity(t, "minimal");
	const bash = resolver().resolve("bash", undefined);
	const output = Array.from({ length: 8 }, (_, i) => `out ${i + 1}`).join("\n");
	const card = row(bash, { command: "ls" }, output, { durationMs: 400 });
	assert.match(card.split("\n")[0]!, /↳ 8 lines  $/);
	assert.doesNotMatch(card, /out 5|0\.4s|earlier lines/);
	const expanded = row(bash, { command: "ls" }, output, { expanded: true });
	assert.match(expanded, /out 1\b[\s\S]*out 8/, "the expand key still shows everything");
});
