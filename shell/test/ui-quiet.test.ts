// extensions/quiet.ts (T174-S6): what each tool card says and keeps, read
// through the public renderers. The exact rows of design/shell-tui 01 are in
// ui-tool-card-rows.test.ts; the default (full) bash card in
// ui-tool-cards.test.ts. Most cases here run under shell.appearance.density
// "minimal", where bash is quiet too.
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { initTheme, keyHint } from "@earendil-works/pi-coding-agent";
import { imageFallback, visibleWidth } from "@earendil-works/pi-tui";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import piPretty from "../extensions/pretty.ts";
import productionQuietTools, { createQuietToolRenderer } from "../extensions/quiet.ts";
import { installShellSettings, type ShellSettings } from "../lib/settings.ts";
import { envOverrides, memorySettings } from "./fake-jhon-settings.ts";

initTheme("dark");

const MINIMAL = { "shell.appearance.density": "minimal" } as const;
installShellSettings(memorySettings(MINIMAL));

const plain = { bold: (s: string) => s, fg: (_role: string, s: string) => s };
// The key's hint as pi words it; with no keybindings loaded the key itself is empty.
const EXPAND = stripAnsi(keyHint("app.tools.expand", "to expand")).trim();
const COLLAPSE = stripAnsi(keyHint("app.tools.expand", "to collapse")).trim();

function context(overrides: Record<string, unknown> = {}) {
	return {
		args: {}, toolCallId: "call-1", invalidate() {}, lastComponent: undefined, cwd: "/repo", state: {},
		executionStarted: true, argsComplete: true, isPartial: false, expanded: false, showImages: false, isError: false,
		...overrides,
	};
}

const result = (text: string, details?: unknown) => ({ content: [{ type: "text", text }], details });

interface Drawn { header: string; state: string; body: string[]; rows: string[] }

/**
 * One tool row as pi draws it (the call, then the result, one row state),
 * at width, with a plain theme: the header row, the state at its right
 * (after the last two-space gap), and the body rows without their lead.
 */
function draw(tool: any, args: Record<string, unknown>, value: unknown, extra: Record<string, unknown> = {}, width = 120, theme: object = plain): Drawn {
	const ctx = context({ args, ...extra });
	const call = tool.renderCall(args, theme, ctx);
	const body = value === undefined ? undefined : tool.renderResult(value, { expanded: ctx.expanded === true, isPartial: ctx.isPartial === true }, theme, ctx);
	const rows = [...call.render(width), ...(body?.render(width) ?? [])].map(stripAnsi);
	const header = rows[0]?.trimEnd() ?? "";
	// The state: the header's last part after a two-space gap, with the expand key's hint before it when it shows.
	const hint = [EXPAND, COLLAPSE].map((h) => header.lastIndexOf(` ${h}  `)).find((at) => at >= 0);
	const parts = header.trim().split(/ {2,}/);
	const state = hint !== undefined ? header.slice(hint + 1) : parts.length > 1 ? parts.at(-1)! : "";
	return { header, state, body: rows.slice(1).map((row) => row.replace(/^ {4}/, "").trimEnd()), rows };
}

const bodyOf = (d: Drawn) => d.body.join("\n");

function fakePi(options: { throwOnConflict?: boolean } = {}) {
	const tools = new Map<string, any>();
	const hooks = new Map<string, any[]>();
	const pi = {
		registerTool(tool: any) {
			if (options.throwOnConflict && tools.has(tool.name)) throw new Error(`Tool ${tool.name} already registered`);
			tools.set(tool.name, tool);
		},
		registerCommand() {},
		on(name: string, handler: any) { hooks.set(name, [...(hooks.get(name) ?? []), handler]); },
	};
	return { pi, tools, hooks };
}

/** The six cards and bash's card registered as a tool, for rendering only (no executor). */
function cards(): Map<string, any> {
	const { pi, tools } = fakePi();
	productionQuietTools(pi as any);
	pi.registerTool({ name: "bash", ...createQuietToolRenderer("bash") });
	return tools;
}

const sdkTool = (name: string) => ({ name, label: name, description: `${name} tool`, parameters: { type: "object", properties: {} }, execute: async () => result(`${name} result`) });

// pi-pretty registers each of its tools unless PRETTY_DISABLE_TOOLS names it; so does this stand-in.
const fakePretty = async (api: any) => {
	const off = new Set((process.env.PRETTY_DISABLE_TOOLS ?? "").split(",").filter(Boolean));
	for (const name of ["read", "bash", "ls", "find", "grep"]) if (!off.has(name)) api.registerTool(sdkTool(name));
};
const fakePrettyDeps = { sdk: Object.fromEntries(["Read", "Bash", "Ls", "Find", "Grep"].map((n) => [`create${n}Tool`, () => sdkTool(n.toLowerCase())])) };

/** Runs with these environment variables set and the settings `jhon settings` would give under them. */
async function withEnv<T>(updates: Record<string, string | undefined>, run: () => Promise<T> | T): Promise<T> {
	const before = Object.fromEntries(Object.keys(updates).map((key) => [key, process.env[key]]));
	let previous: ShellSettings | undefined;
	const restore = (values: Record<string, string | undefined>) => {
		for (const [key, value] of Object.entries(values)) {
			if (value === undefined) delete process.env[key];
			else process.env[key] = value;
		}
	};
	try {
		restore(updates);
		previous = installShellSettings(memorySettings({ ...MINIMAL }, undefined, envOverrides(process.env)));
		return await run();
	} finally {
		if (previous) installShellSettings(previous);
		restore(before);
	}
}

// --- registration -------------------------------------------------------------------------------

test("the cards register six of pi's tools, never bash or codemode, each delegating its execution", () => {
	const { pi, tools } = fakePi();
	productionQuietTools(pi as any);
	assert.equal(tools.has("bash"), false);
	assert.equal(tools.has("codemode"), false, "Jhon turns codemode off; MCP tools are exposed directly");
	for (const name of ["read", "grep", "find", "ls", "edit", "write"]) {
		const tool = tools.get(name);
		assert.ok(tool, name);
		assert.equal(tool.renderShell, "self", `${name} draws its own card`);
		assert.equal(typeof tool.execute, "function");
		assert.ok(tool.parameters, `${name} keeps pi's parameters`);
	}
});

test("the cards never replace a native bash tool already registered", () => {
	const { pi, tools } = fakePi({ throwOnConflict: true });
	const native = sdkTool("bash");
	pi.registerTool(native);
	productionQuietTools(pi as any);
	assert.strictEqual(tools.get("bash"), native);
});

test("bash's card draws the same whatever shellPath the context carries", () => {
	const bash = cards().get("bash");
	const header = (extra: Record<string, unknown>) => draw(bash, { command: "printf output" }, undefined, extra).header;
	assert.equal(header({ shellPath: "/configured/bash" }), header({}));
	assert.doesNotMatch(header({ shellPath: "/configured/bash" }), /configured/);
});

test("pi-pretty leaves the overlapping tools to the cards, and keeps a read's model-visible text byte for byte", async () => {
	await withEnv({ JHON_PI_CHILD: undefined, PRETTY_DISABLE_TOOLS: "multi_grep" }, async () => {
		const { pi, tools } = fakePi({ throwOnConflict: true });
		await piPretty(pi as any, fakePrettyDeps as any, fakePretty as any, process.env);
		productionQuietTools(pi as any);
		assert.equal(tools.has("bash"), false);
		for (const name of ["read", "grep", "find", "ls", "edit", "write"]) assert.ok(tools.has(name), name);
		assert.equal(process.env.PRETTY_DISABLE_TOOLS, "multi_grep,read,bash,ls,find,grep");
	});
	await withEnv({ JHON_PI_CHILD: undefined, PRETTY_DISABLE_TOOLS: undefined }, async () => {
		const { pi, hooks } = fakePi();
		await piPretty(pi as any, fakePrettyDeps as any, fakePretty as any, process.env);
		const original = "[alpha]\ntarget=old\n\n[beta]\ntarget=old\n";
		let event: any = { toolName: "read", content: [{ type: "text", text: original }] };
		for (const handler of hooks.get("tool_result") ?? []) {
			const replaced = await handler(event, {});
			if (replaced) event = { ...event, ...replaced };
		}
		assert.equal(event.content[0]?.text, original);
	});
});

// --- what each card says -----------------------------------------------------------------------------

test("each card says its call: the tool, its target, a read's range, a search's glob and limit, bash's timeout", () => {
	const t = cards();
	assert.match(draw(t.get("read"), { path: "/tmp/example.ts", offset: 2, limit: 3 }, undefined).header, /^ {2}▤ read {2}\/tmp\/example\.ts:2-4/);
	assert.match(draw(t.get("bash"), { command: "printf noisy", timeout: 5 }, undefined).header, /^ {2}\$ printf noisy \(timeout 5s\)/);
	assert.match(draw(t.get("grep"), { pattern: "needle", path: "src", glob: "*.ts", limit: 9 }, undefined).header, /^ {2}⌕ grep {2}\/needle\/ in src \(\*\.ts\) limit 9/);
	assert.match(draw(t.get("find"), { pattern: "*.md" }, undefined).header, /^ {2}⌕ find {2}\*\.md in \./);
	assert.match(draw(t.get("ls"), {}, undefined).header, /^ {2}· ls {2}\./);
	assert.match(draw(t.get("write"), { path: "a.ts", content: "x\ny" }, undefined).header, /^ {2}✎ write {2}a\.ts \(2 lines\)/);
	assert.equal(draw(t.get("read"), { path: "a.ts" }, undefined, { isPartial: true }).state, "◐ running", "no result yet");
});

test("each card says its result in a few words at the header's right", () => {
	const t = cards();
	const state = (name: string, args: Record<string, unknown>, value: unknown) => draw(t.get(name), args, value).state;
	assert.equal(state("grep", { pattern: "x" }, result("a.ts:1:x\nb.ts:2:x\nc.ts:3:x")), "↳ 3 matches");
	assert.equal(state("grep", { pattern: "x" }, result("a.ts:1:x")), "↳ 1 match");
	assert.equal(state("grep", { pattern: "x" }, result("No matches found")), "↳ no matches");
	assert.equal(state("find", { pattern: "*.ts" }, result("a.ts\nb.ts")), "↳ 2 files");
	assert.equal(state("find", { pattern: "*.ts" }, result("No files found matching pattern")), "↳ no files");
	assert.equal(state("ls", { path: "." }, result("a\nb")), "↳ 2 entries");
	assert.equal(state("ls", { path: "." }, result("(empty directory)")), "↳ empty");
	assert.equal(state("ls", { path: "." }, result("Directory is empty")), "↳ empty");
	assert.equal(state("read", { path: "a.ts" }, result("one\ntwo")), "↳ 2 lines");
	assert.equal(state("edit", { path: "a.ts" }, result("Applied", { diff: "-before\n+after\n+more" })), "+2 −1");
	assert.equal(state("edit", { path: "a.ts" }, result("Applied")), `${EXPAND}  ↳ applied`, "no diff: expanded, the result's words");
	assert.equal(state("write", { path: "a.ts", content: "body" }, result("Successfully wrote 4 bytes")), `${EXPAND}  ↳ wrote 4 bytes`, "expanded, the content");
	assert.equal(state("write", { path: "a.ts", content: "x" }, result("done")), `${EXPAND}  ↳ written`);
	assert.equal(state("bash", { command: "printf hi" }, result("hi\nthere")), "↳ 2 lines");
	assert.equal(state("bash", { command: "true" }, result("(no output)")), "↳ no output");
});

test("grep with context lines counts its match rows only; a marker beside real output is output", () => {
	const t = cards();
	const rows = [["one", 12], ["two", 20], ["three", 28], ["four", 36], ["five", 44]] as const;
	const text = rows.flatMap(([f, n]) => [`src/${f}.ts:${n}: match`, `src/${f}.ts-${n - 1}- before`, `src/${f}.ts-${n + 1}- after`]).join("\n");
	assert.equal(draw(t.get("grep"), { pattern: "m", context: 1 }, result(text)).state, `${EXPAND}  ↳ 5 matches`);
	const listed = draw(t.get("ls"), { path: "." }, result("(empty directory)\nreal-entry.txt"));
	assert.equal(listed.state, "↳ 2 entries");
	assert.match(bodyOf(listed), /real-entry\.txt/);
	const said = draw(t.get("bash"), { command: "printf x" }, result("(no output)\nreal output"));
	assert.equal(said.state, "↳ 2 lines");
	assert.equal(bodyOf(said), "(no output)\nreal output");
});

test("previews: a read and a search keep their first lines, bash its last; expanded, all of it", () => {
	const t = cards();
	const read = draw(t.get("read"), { path: "a.ts" }, result("read one\nread two\nread three\nread four"));
	assert.deepEqual(read.body, ["read one", "read two", "read three"]);
	assert.equal(read.state, `${EXPAND}  ↳ 4 lines`, "the expand key while lines are hidden");
	assert.deepEqual(draw(t.get("bash"), { command: "printf x" }, result("bash one\nbash two\nbash three\nbash four")).body, ["bash two", "bash three", "bash four"]);
	for (const [name, text] of [["grep", "src/a.ts:1:m\nsrc/b.ts:2:m\nsrc/c.ts:3:m\nsrc/d.ts:4:m"], ["find", "a\nb\nc\nd"], ["ls", "a\nb\nc\nd"]] as const) {
		assert.equal(draw(t.get(name), {}, result(text)).body.length, 3, `${name} collapsed`);
		const all = draw(t.get(name), {}, result(text), { expanded: true });
		assert.deepEqual(all.body, text.split("\n"), `${name} expanded`);
		assert.ok(all.state.startsWith(COLLAPSE), `${name}: the key to collapse`);
	}
	const short = draw(t.get("read"), { path: "a.ts" }, result("one"));
	assert.equal(short.state, "↳ 1 line", "nothing hidden: no key");
});

test("a git command keeps ten lines of its tail; a compact JSON line previews as its first fields", () => {
	const t = cards();
	const text = Array.from({ length: 12 }, (_, i) => `git line ${i + 1}`).join("\n");
	for (const command of ["git diff", "git status --short", "git -C repo status"]) {
		assert.deepEqual(draw(t.get("bash"), { command }, result(text)).body, text.split("\n").slice(2), command);
	}
	assert.deepEqual(draw(t.get("bash"), { command: "echo git diff" }, result(text)).body, text.split("\n").slice(-3), "not a git command");
	const json = JSON.stringify({ schema: "review", contract: "compact", nested: { longTail: "stays out of the preview", deeper: { value: true } } });
	const collapsed = draw(t.get("bash"), { command: "printf json" }, result(json));
	assert.deepEqual(collapsed.body, ['  "schema": "review",', '  "contract": "compact",', '  "nested": {']);
	assert.equal(collapsed.state, `${EXPAND}  ↳ 1 line`);
	assert.deepEqual(draw(t.get("bash"), { command: "printf json" }, result(json), { expanded: true }, 1000).body, [json]);
});

test("an edit's numbered diff: pi's numbers, − and + rows, a skipped run as `...`; expanded, every row", () => {
	const edit = cards().get("edit");
	const diff = [" 1 a", "-2 old", "+2 new", " 3 c", "   ...", "-9 gone"].join("\n");
	const many = Array.from({ length: 20 }, (_, i) => `+${String(i + 1).padStart(2, " ")} line ${i + 1}`).join("\n");
	installShellSettings(memorySettings({}));
	try {
		const drawn = draw(edit, { path: "a.ts" }, result("Applied", { diff }));
		assert.equal(drawn.state, "+1 −2");
		assert.deepEqual(drawn.body, [" 1   a", " 2 − old", " 2 + new", " 3   c", "     ...", " 9 − gone"]);
		const full = draw(edit, { path: "a.ts" }, result("Applied", { diff: many }));
		assert.equal(full.body.length, 12, "twelve rows collapsed");
		assert.equal(full.state, `${EXPAND}  +20 −0`);
		assert.equal(draw(edit, { path: "a.ts" }, result("Applied", { diff: many }), { expanded: true }).body.length, 20);
	} finally {
		installShellSettings(memorySettings(MINIMAL));
	}
	assert.equal(draw(edit, { path: "a.ts" }, result("Applied", { diff: many })).body.length, 3, "minimal: a preview's worth");
	assert.deepEqual(draw(edit, { path: "a.ts" }, result("Applied", { diff: "@@\n-old\n+new" })).body, ["@@", "  − old", "  + new"], "a diff without numbers");
});

test("a write's card says it wrote; expanded, it shows what the call wrote", () => {
	const write = cards().get("write");
	const content = Array.from({ length: 12 }, (_, i) => `written line ${i + 1}`).join("\n");
	const args = { path: "file.ts", content };
	const collapsed = draw(write, args, result("Successfully wrote 182 bytes to file.ts"));
	assert.deepEqual(collapsed.body, []);
	assert.equal(collapsed.state, `${EXPAND}  ↳ wrote 182 bytes`);
	assert.deepEqual(draw(write, args, result("Successfully wrote 182 bytes to file.ts"), { expanded: true }).body, content.split("\n"));
	for (const fallback of [{}, { content: 42 }, { content: "" }]) {
		assert.deepEqual(draw(write, fallback, result("Successfully wrote 0 bytes to file.ts"), { expanded: true }).body, ["Successfully wrote 0 bytes to file.ts"], "no content: the result's own words");
	}
	const failed = draw(write, args, result("EACCES: permission denied"), { isError: true, expanded: true });
	assert.equal(failed.body.join("\n"), "EACCES: permission denied");
	assert.doesNotMatch(bodyOf(failed), /written line/);
	const partial = draw(write, args, result("writing…"), { isPartial: true, expanded: true });
	assert.match(bodyOf(partial), /writing…/);
	assert.doesNotMatch(bodyOf(partial), /written line/);
});

test("a write after pi's real write tool: the file is written and the card shows the content", async () => {
	const cwd = await mkdtemp(join(tmpdir(), "jhon-write-card-"));
	try {
		const write = cards().get("write");
		const args = { path: "example.ts", content: "export const answer = 42;\n" };
		const done = await write.execute("write-1", args, undefined, undefined, { cwd });
		assert.equal(await readFile(join(cwd, args.path), "utf8"), args.content);
		assert.deepEqual(draw(write, args, done, { expanded: true }).body, ["export const answer = 42;"]);
	} finally {
		await rm(cwd, { recursive: true, force: true });
	}
});

// --- failures -------------------------------------------------------------------------------------------

test("a failure: `✗ failed` at the right, the cause once, then the tail without it; the error tone", () => {
	const t = cards();
	for (const name of ["read", "bash", "grep", "find", "ls", "edit", "write"]) {
		const drawn = draw(t.get(name), name === "bash" ? { command: "false" } : {}, result("error one\nerror two\nerror three\nerror four"), { isError: true });
		assert.deepEqual(drawn.body, ["error one", "error three", "error four"], name);
		assert.equal(drawn.state, `${EXPAND}  ✗ failed`, name);
	}
	const tinted = { ...plain, bg: (role: string, s: string) => `\x1b[48;5;${role === "toolErrorBg" ? 52 : 22}m${s}\x1b[49m` };
	const ctx = context({ args: { path: "gone.md" }, isError: true });
	const header = cards().get("read").renderCall({ path: "gone.md" }, tinted, ctx);
	cards().get("read").renderResult(result("ENOENT"), { expanded: false, isPartial: false }, tinted, ctx);
	assert.ok(header.render(60)[0]!.startsWith(" \x1b[48;5;52m"), "the header on errorContainer");
});

test("a failed bash says why: the exit code first, the first failure and its place, each once", () => {
	const bash = cards().get("bash");
	const output = "▶ ask view\n✖ the question view (2.1ms)\n  at test/ui-ask-view.test.ts:42:9\nℹ tests 5\nℹ fail 1\nCommand exited with code 1";
	const drawn = draw(bash, { command: "node --test" }, result(output), { isError: true });
	assert.equal(drawn.body[0], "exited 1 · 1 failed");
	assert.equal(drawn.body.filter((row) => row.includes("the question view")).length, 1, drawn.body.join("\n"));
	assert.equal(drawn.body.filter((row) => row.includes("ui-ask-view.test.ts:42")).length, 1, drawn.body.join("\n"));
	assert.ok(drawn.body.some((row) => row.includes("ℹ fail 1")), "the rest of the tail stays");
});

test("bash errors in both orders pi reports them: collapsed bounded, expanded every line once, without ANSI", () => {
	const bash = cards().get("bash");
	for (const text of [
		"    B3 stdout before failure\n    B3 stderr detail\n        \n    Command exited with code 7",
		"probe stdout before failure\n\nprobe stderr detail \x1b[31mwith colour\x1b[0m\n\nprobe exit status: 7",
	]) {
		const collapsed = draw(bash, { command: "false" }, result(text), { isError: true });
		assert.ok(collapsed.body.length <= 4, collapsed.body.join("\n"));
		assert.equal(collapsed.body.filter((row) => row.trim() === "").length, 0, "no blank preview rows");
		if (/exited with code 7/.test(text)) assert.equal(collapsed.body[0], "exited 7");
		const expanded = draw(bash, { command: "false" }, result(text), { isError: true, expanded: true }, 1000);
		for (const line of stripAnsi(text).split("\n").map((l) => l.trimEnd()).filter((l) => l.trim() && !/Command exited/.test(l))) {
			assert.equal(expanded.body.filter((row) => row.trim() === line.trim()).length, 1, `«${line}» once`);
		}
		assert.doesNotMatch(expanded.rows.join("\n"), /\x1b\[/);
	}
});

test("a refused call is the guard's band: the reason and the fix, every guard's wording recognised", () => {
	const bash = cards().get("bash");
	const refusal = (text: string) => draw(bash, { command: "git push" }, result(text), { isError: true });
	const cases: Array<[string, string, string]> = [
		["safety-guard: refused: git push --force: it rewrites history. This is destructive or irreversible.", "safety guard", "git push --force: it rewrites history."],
		["vcs-guard: a commit outside the session's worktree", "vcs guard", "a commit outside the session's worktree"],
		["jhon hook route-guard did not answer in 10s; the call is refused; try again or check `jhon doctor`", "route guard", "jhon hook route-guard did not answer in 10s; the call is refused; try again or check `jhon doctor`"],
		["jhon: writer mode: this edit changes 30 lines of a.ts more than the 20 the orchestrator writes inline per request. Hand the rest to a jhon-builder.", "writer guard", "this edit changes 30 lines of a.ts more than the 20 the orchestrator writes inline per request."],
		["jhon: sandbox.mode is on and the sandbox cannot run here: no bwrap; the command is refused, never run unsandboxed in silence. Fix: install bubblewrap", "sandbox", "sandbox.mode is on and the sandbox cannot run here: no bwrap; the command is refused, never run unsandboxed in silence."],
		["push: git push origin main: the owner did not authorize it", "permission", "push: git push origin main: the owner did not authorize it"],
	];
	for (const [text, guard, reason] of cases) {
		const drawn = refusal(text);
		assert.match(drawn.header, /^ {2}✗ refused {2}\$ git push/, text);
		assert.ok(drawn.state.endsWith(guard), `${text}: ${drawn.state}`);
		assert.equal(draw(bash, { command: "git push" }, result(text), { isError: true, expanded: true }, 1000).body[0], reason, text);
	}
	assert.equal(draw(bash, { command: "git push" }, result(cases[4]![0]), { isError: true, expanded: true }, 1000).body[1], "install bubblewrap", "the fix after `Fix:`");
	const plainFailure = refusal("fatal: not a git repository");
	assert.doesNotMatch(plainFailure.header, /refused/, "another failure is no refusal");
	const noted = draw(bash, { command: "git push" }, result("safety-guard: refused: x\nthe session is stale: start a new one"), { isError: true });
	assert.deepEqual(noted.body, ["x"], "collapsed: the reason");
	assert.deepEqual(draw(bash, { command: "git push" }, result("safety-guard: refused: x\nthe session is stale: start a new one"), { isError: true, expanded: true }).body, ["x", "the session is stale: start a new one"], "expanded: the note too");
});

// --- running, bounds, sanitizing ------------------------------------------------------------------------

test("a partial result: the running label and the newest lines; no expand key until it ends", () => {
	const bash = cards().get("bash");
	const args = { command: "printf output" };
	const collapsed = draw(bash, args, result("first\nsecond\nthird\nfourth"), { isPartial: true, toolCallId: "p-1" });
	assert.match(collapsed.body[0]!, /^… bash · 4 lines · running · \d+s · esc to cancel$/);
	assert.deepEqual(collapsed.body.slice(1), ["second", "third", "fourth"]);
	assert.equal(collapsed.state, "◐ running");
	assert.deepEqual(draw(bash, args, result("first\nsecond\nthird\nfourth"), { isPartial: true, expanded: true }).body.slice(1), ["first", "second", "third", "fourth"]);
	assert.match(draw(bash, args, result("first\n\nsecond\n\n"), { isPartial: true }).body[0]!, /^… bash · 2 lines/);
	assert.match(draw(cards().get("read"), { path: "a" }, result(""), { isPartial: true }).body[0]!, /^… read$/);
	const sanitized = draw(bash, args, result("safe\x1b]52;c;Y2xpcGJvYXJk\x07\x1b[2Jdone"), { isPartial: true, expanded: true });
	assert.match(bodyOf(sanitized), /safedone/);
});

test("every card stays inside its width, collapsed within its budget, at any width", () => {
	const t = cards();
	for (const [name, glyph] of [["read", "▤"], ["bash", "$"], ["grep", "⌕"], ["find", "⌕"], ["ls", "·"], ["edit", "✎"], ["write", "✎"]] as const) {
		const args = { command: "printf hello", path: "src/example.ts", pattern: "needle" };
		assert.ok(draw(t.get(name), args, result("x")).header.startsWith(`  ${glyph} `), `${name} carries its family icon`);
		for (const width of [0, 1, 2, 3, 4, 5, 6, 7, 8, 16, 24]) {
			for (const extra of [{}, { isError: true }, { isPartial: true }]) {
				const drawn = draw(t.get(name), { ...args, command: "printf x" }, result("界é🌹".repeat(80) + "\nnext\nmore\nlast"), extra, width);
				assert.ok(drawn.rows.every((row) => visibleWidth(row) === width), `${name} at ${width}: ${JSON.stringify(drawn.rows)}`);
				if (width === 0) assert.deepEqual(drawn.rows, []);
				else assert.ok(drawn.body.length <= 5, `${name} at ${width}: ${drawn.body.length} body rows`);
				const long = draw(t.get(name), { ...args, command: `printf ${"x".repeat(80)}`, path: "p".repeat(80) }, undefined, extra, width);
				assert.ok(long.rows.every((row) => visibleWidth(row) === width), `${name}'s long call at ${width}`);
			}
		}
	}
});

test("a long line is cut at the card's edge while collapsed and whole when expanded", () => {
	const bash = cards().get("bash");
	const long = `${"older text ".repeat(16)}NEWEST-END`;
	const collapsed = draw(bash, { command: "printf x" }, result(`first\n${long}`), {}, 40);
	assert.match(collapsed.rows.at(-1)!, /…/);
	assert.doesNotMatch(collapsed.rows.join("\n"), /NEWEST-END/);
	assert.ok(collapsed.state.includes(EXPAND), "a cut line is hidden text: the expand key shows");
	assert.match(draw(bash, { command: "printf x" }, result(`first\n${long}`), { expanded: true }, 40).rows.join("\n"), /NEWEST-END/);
	const read = draw(cards().get("read"), { path: "a" }, result(`READ-FIRST\n${long}\nmiddle\nREAD-NEW`), {}, 40);
	assert.match(read.rows.join("\n"), /READ-FIRST/);
	assert.doesNotMatch(read.rows.join("\n"), /READ-NEW/);
});

test("a long command wraps under the header with every word kept, the state and the key at the right", () => {
	const bash = cards().get("bash");
	const command = "printf alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november oscar papa quebec romeo sierra tango";
	for (const width of [80, 120]) {
		const drawn = draw(bash, { command }, result("a\nb\nc\nd"), {}, width);
		assert.ok(drawn.header.endsWith(`${EXPAND}  ↳ 4 lines`), drawn.header);
		const words = [drawn.header.replace(/^ {2}\$ /, "").replace(/ {2,}.*$/, ""), ...drawn.body.slice(0, drawn.body.length - 3)].join(" ").replace(/\s+/g, " ").trim();
		assert.equal(words, command, `width ${width}`);
	}
});

test("the cards sanitize what they draw and read only the fields they draw", () => {
	const t = cards();
	const collapsed = draw(t.get("bash"), { command: "echo \x1b[31mred\x1b[0m" }, result("safe\x1b[31mred\x1b[0m\nprefix\rSECRET\r\nnext"));
	assert.match(collapsed.header, /\$ echo red/);
	assert.match(bodyOf(collapsed), /safered/);
	assert.match(bodyOf(collapsed), /prefixSECRET\nnext/);
	const write = draw(t.get("write"), { path: "file.ts", content: "first\n\nsafe\x1b]52;c;Y2xpcGJvYXJk\x07\x1b[2Jdone\n界é😀" }, result("Successfully wrote to file.ts"), { expanded: true }, 16);
	assert.ok(write.rows.every((row) => visibleWidth(row) === 16));
	assert.doesNotMatch(write.rows.join("\n"), /\x1b/);
	const edit = draw(t.get("edit"), { path: "file\x1b[2J.ts" }, result("Applied", { diff: "-1 old\x1b]52;c;Y2xpcGJvYXJk\x07\n+1 new" }), { expanded: true });
	assert.deepEqual(edit.body, [" 1 − old", " 1 + new"]);
	assert.doesNotMatch(edit.rows.join("\n"), /\x1b/);
	let reads = 0;
	const watched = (record: Record<string, unknown>) => Object.defineProperties(record, Object.fromEntries(Array.from({ length: 200 }, (_, i) => [`unused${i}`, { enumerable: true, get() { reads++; return "x"; } }])));
	draw(t.get("bash"), watched({ command: "printf output" }), result("first\nsecond", watched({})));
	assert.equal(reads, 0);
});

test("an image read expanded is drawn by pi's own read renderer, sanitized", () => {
	const drawn = draw(cards().get("read"), { path: "image\x1b[2J.png" }, { content: [{ type: "image", data: "iVBORw0KGgo=", mimeType: "image/png" }], details: { path: "image\x1b[2J.png" } }, { expanded: true, cwd: "/repo\x1b[2J", showImages: false });
	assert.ok(drawn.rows.join("\n").includes(imageFallback("image/png")));
	assert.doesNotMatch(drawn.rows.join("\n"), /\x1b/);
	assert.equal(draw(cards().get("read"), { path: "a.png" }, { content: [{ type: "image", data: "iVBORw0KGgo=", mimeType: "image/png" }] }).state, "↳ image");
});

test("a theme change repaints: after an invalidate the cards draw in the new colours, the same text", () => {
	const bash = cards().get("bash");
	let paint = "\x1b[31m";
	const theme = { bold: (s: string) => s, fg: (_r: string, s: string) => `${paint}${s}\x1b[0m`, bg: (_r: string, s: string) => `\x1b[44m${s}\x1b[49m` };
	const args = { command: "printf failure" };
	const ctx = context({ args, isError: true });
	const parts = [bash.renderCall(args, theme, ctx), bash.renderResult(result("denied\nreason"), { expanded: false, isPartial: false }, theme, ctx)];
	const first = parts.flatMap((p) => p.render(80)).join("\n");
	paint = "\x1b[35m";
	for (const p of parts) p.invalidate();
	const changed = parts.flatMap((p) => p.render(80)).join("\n");
	assert.match(first, /\x1b\[31m/);
	assert.doesNotMatch(changed, /\x1b\[31m/);
	assert.match(changed, /\x1b\[35m/);
	assert.equal(stripAnsi(first), stripAnsi(changed));
});

// --- the resolver ---------------------------------------------------------------------------------------

function resolverOf(register: (pi: any) => void) {
	const { pi } = fakePi();
	const resolvers: Array<(name: string, next: () => unknown) => unknown> = [];
	(pi as any).registerToolRenderer = (r: (name: string, next: () => unknown) => unknown) => { resolvers.push(r); };
	register(pi);
	return (name: string, next: () => unknown) => resolvers.reduceRight<() => unknown>((n, r) => () => r(name, n), next)() as any;
}

test("bash is a Jhon card over pi's native tool: pi's fields stay, the drawing is Jhon's", () => {
	const bash = resolverOf((pi) => productionQuietTools(pi))("bash", () => ({ marker: "native" }));
	assert.equal(bash.marker, "native");
	assert.equal(bash.renderShell, "self");
	assert.equal(typeof bash.renderCall, "function");
	assert.equal(typeof bash.renderResult, "function");
});

test("every other tool's call gets its family icon (T33); a card's own icon is left alone", () => {
	const resolve = resolverOf((pi) => productionQuietTools(pi));
	const own = { render: () => ["web_search pi themes"], invalidate() {} };
	assert.equal(stripAnsi(resolve("web_search", () => ({ renderCall: () => own })).renderCall({}, plain, context()).render(80)[0]), "◍ web_search pi themes");
	assert.match(stripAnsi(resolve("mcp__engram__mem_save", () => ({ renderCall: () => own })).renderCall({ title: "a fact" }, plain, context()).render(80)[0]), /^▢ save “a fact”/);
	const read = cards().get("read");
	assert.equal(resolve("read", () => read).renderCall, read.renderCall);
	assert.equal(resolve("unknown_tool", () => undefined), undefined);
});
