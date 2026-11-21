import "./isolated-env.ts";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test, { after } from "node:test";
import { initTheme, type ExtensionAPI, type ExtensionContext } from "@earendil-works/pi-coding-agent";
import { visibleWidth, VStack, type TUI } from "@earendil-works/pi-tui";
import { resolveSessionWorktree } from "../lib/ui/session-worktree-registry.ts";
import { SESSION_PROFILE_CHANGED, SESSION_PROFILE_ENTRY } from "../lib/session-profile.ts";
import installShellUi, { createActiveProfileReader, JhonPromptEditor } from "../extensions/ui.ts";
import { barModel, renderTopRow } from "../lib/ui/bars.ts";
import { SANDBOX_EVENT } from "../lib/events.ts";
import { paneState, type PanePart } from "../lib/ui/pane.ts";
import type { ThemeLike as ShellBarTheme } from "../lib/ui/primitives/index.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { installShellSettings, type SettingValue } from "../lib/settings.ts";
import { envOverrides, memorySettings } from "./fake-jhon-settings.ts";

// extensions/ui.ts against a stand-in pi: the status bar and the top row,
// the pane's parts, the prompt, the stall recovery, the LDD reads and
// Customize. The prompt's Esc handling, its editor slot, paste and
// Customize's appearance rows have files of their own (ui-prompt-esc,
// ui-prompt-claim, ui-customize-settings), as do the worktree tool
// (ui-session-worktree-tool) and the external editor (ui-external-editor).

initTheme("dark");

/** Every path under /repo (and ".") is the one clone's /repo; anything else is a root of its own in the same clone. */
const resolveWorktree = (path: string) => ({ root: path.startsWith("/repo") || path === "." ? "/repo" : path, commonDir: "/clone/git" });

// Settings: each home (JHON_SHELL_CONFIG_HOME) names in-memory settings of
// its own, made on first use and installed for the process when a shell is
// made, with the JHON_SHELL_* variables of the shell's env applied as the
// engine applies them. No test sees the owner's settings or another test's.
const homes = new Map<string, ReturnType<typeof memorySettings>>();
function homeSettings(home: string, file: Record<string, SettingValue> = {}): ReturnType<typeof memorySettings> {
	let settings = homes.get(home);
	if (!settings) { settings = memorySettings(file); homes.set(home, settings); }
	return settings;
}
let homeCount = 0;
/** The env a shell is made with: its own home unless the test names one, and that home's settings installed. */
function withSettings(env: NodeJS.ProcessEnv = process.env): NodeJS.ProcessEnv {
	const home = env.JHON_SHELL_CONFIG_HOME ?? `ui-shell-home-${++homeCount}`;
	const settings = homeSettings(home);
	settings.jhon.env = envOverrides(env);
	settings.refresh();
	installShellSettings(settings);
	return { ...env, JHON_SHELL_CONFIG_HOME: home };
}
// The profile reader reads a profiles.json of the suite's own, which does
// not exist (so `default`); a test that shows a profile passes its own reader.
const profilesDir = mkdtempSync(join(tmpdir(), "jhon-ui-profiles-"));
after(() => rmSync(profilesDir, { recursive: true, force: true }));
process.env.JHON_PROFILES = join(profilesDir, "profiles.json");
const shellUi: typeof installShellUi = (pi, env, deps) => installShellUi(pi, withSettings(env), { resolveWorktree, rootExists: () => true, ...deps });

/** A theme that leaves text as it is. */
const plainTheme = {
	fg: (_role: string, text: string) => text,
	bold: (text: string) => text,
	bg: (_role: string, text: string) => text,
	getBgAnsi: () => "",
} satisfies ShellBarTheme & { bg(color: string, value: string): string; getBgAnsi(): string };

/** What the shell set on a session's UI. */
interface FakeUi {
	footerFactory: unknown;
	editorFactory: unknown;
	widgets: Map<string, unknown>;
	workingVisible: boolean | undefined;
	notices: string[];
	/** The last overlay's factory and the view it built. */
	overlay: unknown;
	overlayView: { render(width: number): string[]; handleInput(data: string): void; handleMouse?(event: unknown): unknown } | undefined;
}

type Handler = (event: unknown, ctx: ExtensionContext) => unknown;
interface Registered { description?: string; handler: (...args: never[]) => unknown }

/** A stand-in pi: what the shell registers, and what it sends. */
function fakePi() {
	const handlers = new Map<string, Handler[]>();
	const commands = new Map<string, Registered & { handler(args: string, ctx: ExtensionContext): Promise<void> }>();
	const shortcuts = new Map<string, Registered & { handler(ctx: ExtensionContext): Promise<void> }>();
	const tools = new Map<string, { renderShell?: string; execute(id: string, params: unknown, signal: undefined, update: undefined, ctx: ExtensionContext): Promise<unknown> }>();
	const sentMessages: Array<{ content: string; options?: { deliverAs?: "steer" | "followUp" } }> = [];
	const listeners = new Map<string, (data: unknown) => void>();
	let entries: unknown[] = [];
	const pi = {
		// Each handler sees the entries of the context it ran with, which appendEntry adds to.
		on(name: string, handler: Handler) {
			handlers.set(name, [...(handlers.get(name) ?? []), (event, ctx) => { entries = ctx.sessionManager.getEntries() as unknown[]; return handler(event, ctx); }]);
		},
		appendEntry(customType: string, data: unknown) { entries.push({ type: "custom", customType, data }); },
		events: {
			on(name: string, fn: (data: unknown) => void) { listeners.set(name, fn); return () => listeners.delete(name); },
			emit(name: string, data: unknown) { listeners.get(name)?.(data); },
		},
		registerTool(tool: { name: string }) { tools.set(tool.name, tool as never); },
		registerCommand(name: string, command: never) { commands.set(name, command); },
		registerShortcut(key: string, shortcut: never) { shortcuts.set(key, shortcut); },
		registerMessageRenderer() {},
		getThinkingLevel: () => "medium",
		getCommands: () => [],
		sendUserMessage(content: string, options?: { deliverAs?: "steer" | "followUp" }) { sentMessages.push({ content, options }); },
		async exec() { return { stdout: "", stderr: "", code: 0, killed: false }; },
	} as unknown as ExtensionAPI;
	return { pi, handlers, commands, shortcuts, tools, sentMessages };
}

async function fire(handlers: Map<string, Handler[]>, event: string, ctx: ExtensionContext): Promise<void> {
	for (const handler of handlers.get(event) ?? []) await handler({}, ctx);
}

/** A session in a full-screen TUI on /repo, with Opus 5.5 at 45% of its context; what the shell sets lands in `ui`. */
function fakeContext(options: { theme?: typeof plainTheme & { getBgAnsi?(): string }; hasUI?: boolean; entries?: unknown[]; pending?: boolean; idle?: boolean; editorFactory?: unknown; select?: (title: string, options: string[]) => Promise<string | undefined> } = {}): { ctx: ExtensionContext; ui: FakeUi } {
	const ui: FakeUi = { footerFactory: undefined, editorFactory: options.editorFactory, widgets: new Map(), workingVisible: undefined, notices: [], overlay: undefined, overlayView: undefined };
	const entries = options.entries ?? [];
	const ctx = {
		hasUI: options.hasUI ?? true,
		mode: "tui",
		hasPendingMessages: () => options.pending ?? false,
		isIdle: () => options.idle ?? true,
		cwd: "/repo",
		model: { id: "claude-opus-5-5", provider: "anthropic", reasoning: true, contextWindow: 272_000 },
		sessionManager: { getCwd: () => "/repo", getSessionName: () => "Release notes", getEntries: () => entries, getSessionId: () => "shell-session" },
		modelRegistry: { isUsingOAuth: () => true, getApiKeyForProvider: async () => undefined },
		getContextUsage: () => ({ tokens: 122_400, contextWindow: 272_000, percent: 45 }),
		ui: {
			theme: options.theme ?? plainTheme,
			getAllThemes: () => [{ name: "dark", path: undefined }, { name: "light", path: undefined }],
			getTheme: (name: string) => (name === "dark" || name === "light" ? { name } : undefined),
			setTheme: (name: string) => ({ success: name === "dark" || name === "light" }),
			// Only when asked for: without it, a question falls back to having no menu.
			...(options.select ? { select: options.select } : {}),
			setFooter: (factory: unknown) => { ui.footerFactory = factory; },
			setEditorComponent: (factory: unknown) => { ui.editorFactory = factory; },
			getEditorComponent: () => ui.editorFactory,
			setWidget: (key: string, content: unknown) => { if (content === undefined) ui.widgets.delete(key); else ui.widgets.set(key, content); },
			notify: (message: string) => { ui.notices.push(message); },
			setWorkingVisible: (visible: boolean) => { ui.workingVisible = visible; },
			// The overlay resolves with what its `done` gets; the view is built at once. No bg, so a focused row shows as ▸.
			custom<T>(factory: (tui: unknown, theme: unknown, keybindings: unknown, done: (value: T) => void) => { render(width: number): string[]; handleInput(data: string): void }) {
				ui.overlay = factory;
				return new Promise<T | null>((resolve) => { ui.overlayView = factory(fakeTui, { ...plainTheme, bg: undefined }, fakeKeybindings, (value: T) => resolve(value)); });
			},
		},
	} as unknown as ExtensionContext;
	return { ctx, ui };
}

/** An assistant message's usage entry. */
function assistantEntry(usage: { input: number; output: number; cost: number }) {
	return { type: "message", message: { role: "assistant", usage: { input: usage.input, output: usage.output, cacheRead: 0, cacheWrite: 0, cost: { total: usage.cost } } } };
}

test("barModel reads the session, the model and the footer data (T174-S3)", () => {
	const { pi } = fakePi();
	const { ctx } = fakeContext({
		entries: [assistantEntry({ input: 1000, output: 200, cost: 0.5 }), assistantEntry({ input: 500, output: 100, cost: 0.25 }), { type: "message", message: { role: "user" } }],
	});
	const footerData = {
		getGitBranch: () => "main",
		getExtensionStatuses: () => new Map([["mcp", "MCP: 3 servers enabled"]]),
		getAvailableProviderCount: () => 1,
		onBranchChange: () => () => {},
	};
	const built = barModel(pi, ctx, footerData);
	assert.equal(built.cwd, "/repo");
	assert.equal(built.branch, "main");
	assert.equal("sessionName" in built, false, "the session name is on no bar");
	assert.equal(built.modelId, "claude-opus-5-5");
	assert.equal(built.effort, "medium");
	assert.equal(built.contextPercent, 45);
	assert.equal(built.costTotal, 0.75);
	assert.equal(built.subscription, true);
	assert.deepEqual(built.statuses, ["MCP: 3 servers enabled"], "T174-S5b: other extensions' statuses are back on the bar");
});

test("barModel keeps the folder whole for the top row, which shows the home as ~; no effort for a non-reasoning model", () => {
	const { pi } = fakePi();
	const { ctx } = fakeContext();
	(ctx as unknown as { model: { reasoning: boolean } }).model.reasoning = false;
	(ctx.sessionManager as unknown as { getCwd: () => string }).getCwd = () => "/home/alan/work/jhon-demo";
	const footerData = {
		getGitBranch: () => null,
		getExtensionStatuses: () => new Map(),
		getAvailableProviderCount: () => 1,
		onBranchChange: () => () => {},
	};
	const built = barModel(pi, ctx, footerData);
	assert.equal(built.cwd, "/home/alan/work/jhon-demo");
	assert.match(stripAnsi(renderTopRow(built, plainTheme, 60, { home: "/home/alan" })), /^ 力 jhon-demo {2}~\/work\/jhon-demo +$/);
	assert.equal(built.effort, undefined);
	assert.equal(built.branch, null);
});

test("with a UI, the session start sets the footer: in a regular terminal one row, the status bar", () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	for (const handler of handlers.get("session_start") ?? []) handler({}, ctx);
	const footer = ui.footerFactory as (tui: unknown, theme: ShellBarTheme, data: unknown) => { render(width: number): string[] };
	assert.equal(typeof footer, "function");
	const data = { getGitBranch: () => "main", getExtensionStatuses: () => new Map(), getAvailableProviderCount: () => 1, onBranchChange: () => () => {} };
	const drawn = footer({ requestRender() {} }, plainTheme, data).render(120);
	assert.equal(drawn.length, 1);
	assert.match(drawn[0]!, /^ ⎇ main +claude-opus-5-5 · medium │ ctx 45% │ \$0\.000 sub $/, "under 140 columns the bar's narrow form");
});

test("the fullscreen LDD rail and the bars carry live digests: a profile switch refreshes the status bar, never the top row; an extension status shows nowhere (T174)", async () => {
	const { pi, handlers } = fakePi();
	let profile: string | undefined = "team";
	shellUi(pi, { JHON_SHELL_CHANGES_WATCH_MS: "off" }, { activeProfile: () => profile });
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);

	const statuses = new Map<string, string>();
	const liveFooterData = { getGitBranch: () => "main", getExtensionStatuses: () => statuses, getAvailableProviderCount: () => 1, onBranchChange: () => () => {} };
	const tui = { terminal: { rows: 40, columns: 160 }, requestRender() {} };
	const factory = ui.footerFactory as (tui: unknown, theme: ShellBarTheme, footerData: unknown) => { render(width: number): string[]; dispose(): void };
	const component = factory(tui, plainTheme, liveFooterData);
	try {
		const rail = paneState(tui as unknown as TUI).parts.get("ldd") as PanePart;
		assert.equal(typeof rail.digest, "function", "the LDD card paints live state and must declare a digest");
		assert.doesNotMatch(rail.render(46).join("\n"), /claude-opus-5-5/, "the model is the status bar's, not the Status card's");

		// The profile is the status bar's (beside the model): STATUS and the top row never show it.
		const header = paneState(tui as unknown as TUI).parts.get("header") as PanePart;
		const bar = paneState(tui as unknown as TUI).parts.get("statusbar") as PanePart;
		assert.doesNotMatch(rail.render(46).join("\n"), /team/);
		assert.doesNotMatch(header.render(160).join("\n"), /team|claude/, "the top row is the project and its path alone");
		assert.match(bar.render(160).join("\n"), /claude-opus-5-5 · medium · team │/);
		const beforeProfile = bar.digest?.();
		profile = "other";
		assert.notEqual(bar.digest?.(), beforeProfile, "a profile switch refreshes the status bar");
		assert.match(bar.render(160).join("\n"), /· other │/);

		statuses.set("mcp", "MCP: 3 servers enabled");
		assert.doesNotMatch(rail.render(46).join("\n"), /MCP/, "no INTEGRATIONS: an extension's status is not drawn");
		assert.equal(rail.digest?.(), rail.digest?.(), "an unchanged digest still reuses the prepared rail");
	} finally {
		component.dispose();
	}
});

test("the fullscreen status line carries a live digest so model, context, and cost changes refresh it", async () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CHANGES_WATCH_MS: "off" });
	const entries: unknown[] = [];
	const { ctx, ui } = fakeContext({ entries });
	await fire(handlers, "session_start", ctx);

	const liveFooterData = { getGitBranch: () => "main", getExtensionStatuses: () => new Map([["mcp", "MCP: 3 servers enabled"]]), getAvailableProviderCount: () => 1, onBranchChange: () => () => {} };
	const tui = { terminal: { rows: 40, columns: 160 }, requestRender() {} };
	const factory = ui.footerFactory as (tui: unknown, theme: ShellBarTheme, footerData: unknown) => { render(width: number): string[]; dispose(): void };
	const component = factory(tui, plainTheme, liveFooterData);
	try {
		const header = paneState(tui as unknown as TUI).parts.get("statusbar") as PanePart;
		assert.equal(typeof header.digest, "function", "the status line paints live state every frame and must declare a digest");
		const live = () => header.digest?.();
		const text = () => header.render(160).join("\n");
		assert.match(text(), /claude-opus-5-5/);
		assert.match(text(), /MCP: 3 servers/, "other extensions' statuses reach the status line (T174-S5b)");
		assert.doesNotMatch(text(), /working/i, "the working state never reaches the status line");

		const beforeModel = live();
		(ctx.model as { id: string }).id = "claude-opus-5-6";
		assert.notEqual(live(), beforeModel, "/model must change the status line's digest");
		assert.match(text(), /claude-opus-5-6/);

		const beforeUsage = live();
		(ctx as unknown as { getContextUsage: () => unknown }).getContextUsage = () => ({ tokens: 200_000, contextWindow: 272_000, percent: 74 });
		assert.notEqual(live(), beforeUsage, "context usage must change the status line's digest");
		assert.match(text(), /74%/);

		const beforeCost = live();
		entries.push(assistantEntry({ input: 100, output: 20, cost: 0.42 }));
		assert.notEqual(live(), beforeCost, "session cost must change the status line's digest");
		assert.match(text(), /\$0\.420/);
		assert.equal(live(), live(), "an unchanged digest still reuses the prepared status line");
	} finally {
		component.dispose();
	}
});

test("with JHON_SHELL=0 the shell registers nothing but the change capture; without a UI it sets no footer", () => {
	const off = fakePi();
	shellUi(off.pi, { JHON_SHELL: "0" });
	assert.equal(off.commands.size, 0);
	assert.equal(off.tools.size, 0);
	assert.ok(off.handlers.has("tool_call"), "a headless child still records its writes");
	const on = fakePi();
	shellUi(on.pi, {});
	const { ctx, ui } = fakeContext({ hasUI: false });
	for (const handler of on.handlers.get("session_start") ?? []) handler({}, ctx);
	assert.equal(ui.footerFactory, undefined);
	assert.equal(ui.editorFactory, undefined);
});

const fakeTui = { terminal: { rows: 40, columns: 120 }, requestRender() {} };
// The shell runs full-screen; its prompt's rule carries no [ Panel ] (T169).
const promptTui = Object.create(fakeTui, { mode: { value: "fullscreen" } }) as typeof fakeTui & { mode: string };
const editorTheme = { borderColor: (text: string) => text, selectList: {} };
const fakeKeybindings = { matches: () => false };

/** Starts the session and builds the shell's prompt as pi would. */
function installedPrompt(ctx: ExtensionContext, ui: FakeUi, handlers: Map<string, Handler[]>, keybindings: unknown = fakeKeybindings): JhonPromptEditor {
	for (const handler of handlers.get("session_start") ?? []) handler({}, ctx);
	return (ui.editorFactory as (tui: unknown, theme: unknown, keybindings: unknown) => JhonPromptEditor)(promptTui, editorTheme, keybindings);
}

test("the installed prompt says the Esc hints at the working line's right end and queued on it; it ends on its rule (T174-S8)", () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_DOUBLE_ESC_CANCEL: "on" });
	const { ctx, ui } = fakeContext({ pending: true });
	const editor = installedPrompt(ctx, ui, handlers, { matches: (data: string, action: string) => action === "app.interrupt" && data === "\x1b" });
	try {
		editor.focused = true;
		assert.doesNotMatch(editor.render(80).join("\n"), /\x1b\[4\dm/, "the prompt paints no background");
		editor.setText("draft");
		editor.handleInput("\x1b");
		assert.match(stripAnsi(editor.render(80)[0]!), /^ 力 ready for you +esc again clears it $/);
		assert.equal(stripAnsi(editor.render(80).at(-1)!), "─".repeat(80), "the prompt ends on its rule");
		for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
		assert.match(stripAnsi(editor.render(80)[0]!), /^ 力 Working…  thinking · \d+s · queued +esc esc interrupts $/);
		editor.handleInput("\x1b");
		assert.match(stripAnsi(editor.render(80)[0]!), /^ 力 Working…  thinking · \d+s · queued +esc again stops it $/);
	} finally { editor.dispose(); }
});

test("the prompt never names the LDD phase: the Status card says it, once", async () => {
	const status = { change: "x", slice: "Slice 2: launch", slices: ["done", "current"] as Array<"done" | "current" | "pending">, next: "apply" };
	const { pi, handlers } = fakePi();
	shellUi(pi, {}, { lddStatus: async () => status });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers);
	await new Promise((resolve) => setTimeout(resolve, 0));
	assert.doesNotMatch(editor.render(80).map(stripAnsi).join("\n"), /APPLY|launch/);
});

test("shellUi takes over the prompt: pi's Working row hidden, the working line over the rule, a hint while empty", () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers);
	assert.equal(ui.workingVisible, false, "pi's own Working row must be hidden");
	editor.focused = true;
	const lines = editor.render(60).map(stripAnsi);
	assert.match(lines[0], /^ 力 ready for you +$/);
	assert.match(lines[1], /^─+$/);
	assert.doesNotMatch(editor.render(60).join("\n"), /\x1b\[44m/, "prompt must not paint passive backgrounds");
	assert.match(lines[2], /^ ❯ .*write, or \/ for a command/);
	editor.setText("hola");
	assert.doesNotMatch(editor.render(60).map(stripAnsi)[2], /write, or/);
	editor.dispose();
});

test("sections, the default: the prompt under a rule line, one status line at the bottom, and no key line under the prompt (L97, L105)", async (t) => {
	const home = mkdtempSync(join(tmpdir(), "jhon-sections-shell-"));
	t.after(() => rmSync(home, { recursive: true, force: true }));
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home, JHON_SHELL_CHANGES_WATCH_MS: "off" });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers);
	try {
		editor.focused = true;
		const lines = editor.render(60).map(stripAnsi);
		assert.match(lines[0], /^ 力 ready for you +$/);
		assert.match(lines[2], /^ ❯ .*write, or \/ for a command/);
		assert.ok(lines.every((line) => visibleWidth(line) === 60), lines.join("\n"));
		editor.setText("hola");
		assert.match(editor.render(60).map(stripAnsi)[2], /^ ❯ hola +$/);
		const footer = renderFooter(ui);
		assert.match(footer, /^ ⎇ main +claude-opus-5-5 · medium │ ctx ▰▰▰▰▱▱▱▱ 45% │ \$0\.000 sub $/, "the cost closes the bar");
		assert.equal(handlers.has("after_provider_response"), true, "T174-S5b: the subscription windows are read from the replies again");
		// The README teaches the keys; the prompt keeps none (L105).
		assert.equal(ui.widgets.has("jhon-shell-key-hints"), false);
	} finally { editor.dispose(); }
});

test("the status line wears the owner's mode from STATUS's own status read, with no read of its own; normal shows nothing (S31)", async (t) => {
	const home = mkdtempSync(join(tmpdir(), "jhon-mode-shell-"));
	t.after(() => rmSync(home, { recursive: true, force: true }));
	let mode: string | undefined = "super-yolo";
	let clock = 1_000_000;
	const asked: string[] = [];
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home, JHON_SHELL_CHANGES_WATCH_MS: "off" }, { now: () => clock, lddStatus: async () => undefined, permissionMode: (cwd: string) => { asked.push(cwd); return mode; } });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers);
	try {
		await new Promise((resolve) => setTimeout(resolve, 0));
		assert.match(renderFooter(ui), /^ ⎇ main │ SUPER YOLO +claude-opus-5-5 · medium │ .* │ \$0\.000 sub $/, "the badge after the branch, on the left");
		assert.ok(asked.every((cwd) => cwd === "/repo"), "the session's folder, the one STATUS reads");
		mode = "normal";
		clock += 60_000;
		for (const handler of handlers.get("turn_end") ?? []) handler({}, ctx);
		await new Promise((resolve) => setTimeout(resolve, 0));
		assert.doesNotMatch(renderFooter(ui), /YOLO/, "the next status read takes it off");
	} finally { editor.dispose(); }
});

test("without the rail the status bar is exactly one row: other extensions' statuses on it, no jobs, never a second line (T15.12d, T174, T174-S5b)", async (t) => {
	const home = mkdtempSync(join(tmpdir(), "jhon-sections-statuses-"));
	t.after(() => rmSync(home, { recursive: true, force: true }));
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home, JHON_SHELL_CHANGES_WATCH_MS: "off" });
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	const factory = ui.footerFactory as (tui: unknown, theme: ShellBarTheme, data: unknown) => { render(width: number): string[] };
	const narrowTui = { mode: "fullscreen", layoutRoot: new VStack([]), terminal: { rows: 40, columns: 120 }, requestRender() {} };
	// Hosted (lib/ui/pane.ts), the narrow status row is the statusbar part's, a
	// row of the host's own under pi's layout; the footer itself paints nothing.
	const footer = factory(narrowTui, plainTheme, { ...footerData, getExtensionStatuses: () => new Map([["mcp", "MCP: 2 servers"]]) });
	const statusRow = () => paneState(narrowTui as unknown as TUI).parts.get("statusbar")!.render(120);
	assert.deepEqual(footer.render(120), [], "the footer steps aside for the statusbar row");
	const rows = statusRow();
	assert.equal(rows.length, 1, rows.join("\n"));
	assert.match(rows.join("\n"), /│ MCP: 2 servers/, "another extension's status, on the bar (T174-S5b)");
	pi.events.emit("jhon-shell:tasks", { sessionId: ctx.sessionManager.getSessionId(), tasks: [{ id: "job-1", label: "build", startedAt: 1 }] });
	const busy = statusRow();
	assert.equal(busy.length, 1);
	assert.equal(busy[0], rows[0], "a running job is the right pane's, not the bar's (T174-S3)");
	assert.match(rows[0]!, /^ ⎇ main │ MCP: 2 servers +claude-opus-5-5 · medium │ ctx 45% │ \$0\.000 sub $/, "120 columns: the narrow form");
});

test("a regular (not fullscreen) terminal's footer is the status bar alone: branch and sandbox, other extensions' statuses, model, ctx and cost; no session name or jobs (T174-S3, S5b)", async (t) => {
	const home = mkdtempSync(join(tmpdir(), "jhon-regular-bar-"));
	t.after(() => rmSync(home, { recursive: true, force: true }));
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home, JHON_SHELL_CHANGES_WATCH_MS: "off" });
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	const factory = ui.footerFactory as (tui: unknown, theme: ShellBarTheme, data: unknown) => { render(width: number): string[] };
	const rows = factory(fakeTui, plainTheme, { ...footerData, getExtensionStatuses: () => new Map([["mcp", "MCP: 2 servers"]]) }).render(160);
	assert.deepEqual(rows, [" ⎇ main │ MCP: 2 servers" + " ".repeat(79) + "claude-opus-5-5 · medium │ ctx ▰▰▰▰▱▱▱▱ 45% │ $0.000 sub "]);
	pi.events.emit("jhon-shell:tasks", { sessionId: ctx.sessionManager.getSessionId(), tasks: [{ id: "job-1", label: "build", startedAt: 1 }] });
	assert.doesNotMatch(factory(fakeTui, plainTheme, footerData).render(160)[0]!, /⧗|job|Release notes/);
	pi.events.emit(SANDBOX_EVENT, { mode: "on" });
	assert.match(factory(fakeTui, plainTheme, footerData).render(160)[0]!, /^ ⎇ main │ sandbox on +claude/, "the session's sandbox pin");
	pi.events.emit(SANDBOX_EVENT, { mode: "off" });
	assert.match(factory(fakeTui, plainTheme, footerData).render(160)[0]!, /^ ⎇ main │ sandbox off +claude/);
	pi.events.emit(SANDBOX_EVENT, { mode: undefined });
	assert.match(factory(fakeTui, plainTheme, footerData).render(160)[0]!, /^ ⎇ main +claude/, "unread: no segment");
});

// L1489 (T174-S4): the bar says what lib/sandbox.ts's pin says, never `on` for a sandbox that cannot run.
test("the status bar's sandbox is the pin's real state: on, off, or not running and why", async (t) => {
	const home = mkdtempSync(join(tmpdir(), "jhon-sandbox-state-"));
	t.after(() => rmSync(home, { recursive: true, force: true }));
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home, JHON_SHELL_CHANGES_WATCH_MS: "off" });
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	const bar = () => renderFooter(ui);
	pi.events.emit(SANDBOX_EVENT, { mode: "on", state: "on" });
	assert.match(bar(), /^ ⎇ main │ sandbox on +claude/);
	pi.events.emit(SANDBOX_EVENT, { mode: "off", state: "off" });
	assert.match(bar(), /^ ⎇ main │ sandbox off +claude/);
	pi.events.emit(SANDBOX_EVENT, { mode: "on", state: "not running", why: "bubblewrap is not on PATH" });
	assert.match(bar(), /^ ⎇ main │ sandbox not running · bubblewrap is not on PATH +claude/, "mode on, but it cannot run: not `on`");
	pi.events.emit(SANDBOX_EVENT, { mode: undefined, state: "not running", why: "no status answer" });
	assert.match(bar(), /^ ⎇ main │ sandbox not running · no status answer +claude/);
});

// L1489 (T174-S4): a regular terminal shows the narrow form through pi's public
// slots: the top row and the strip in the chrome widget above the editor, the
// status bar in the footer under it.
test("a regular terminal shows the top row and the strip above the editor, the status bar under it (04's narrow form)", async (t) => {
	const home = mkdtempSync(join(tmpdir(), "jhon-regular-chrome-"));
	t.after(() => rmSync(home, { recursive: true, force: true }));
	const status = { change: "add-invoice-export", slice: "Slice 3: Date-range filter", slices: ["done", "done", "current", "pending"] as Array<"done" | "current" | "pending">, detail: [{ name: "a", total: 4, completed: 4 }, { name: "b", total: 3, completed: 3 }, { name: "c", total: 4, completed: 2 }, { name: "d", total: 3, completed: 0 }], next: "apply slice 3" };
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home, JHON_SHELL_CHANGES_WATCH_MS: "off" }, { lddStatus: async () => status });
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	await new Promise((resolve) => setTimeout(resolve, 0));
	const footer = (ui.footerFactory as (tui: unknown, theme: unknown, data: unknown) => { render(width: number): string[]; dispose(): void })(fakeTui, plainTheme, footerData);
	try {
		const chrome = (ui.widgets.get("jhon-chrome") as ((tui: unknown, theme: unknown) => { render(width: number): string[] }) | undefined)?.(fakeTui, plainTheme);
		assert.ok(chrome, "the chrome widget is registered");
		assert.deepEqual(chrome.render(100).map(stripAnsi), [
			" 力 repo  /repo".padEnd(99),
			` ◆ add-invoice-export  ◐ slice 3/4 · tasks 2/4 · next apply slice 3${" ".repeat(25)}alt+j ▾ `,
		]);
		assert.match(stripAnsi(footer.render(100)[0]!), /^ ⎇ main +claude/, "the footer keeps the status bar");
		pi.events.emit("jhon-shell:tasks", { sessionId: ctx.sessionManager.getSessionId(), tasks: [{ id: "job-1", label: "build", startedAt: 1 }] });
		assert.equal(chrome.render(100).length, 2, "two rows, whatever runs");
	} finally { footer.dispose(); await fire(handlers, "session_shutdown", ctx); }
});

// Design 01 and 04 (T174-S3): the top row and the status bar are one row
// each at every width and density; under 140 columns the status bar takes
// its narrow form and the footer steps aside for it.
test("the chrome: the top row and the status bar one row each; under 140 columns the narrow form", async () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {}, { activeProfile: () => "jhon" });
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	const tui = { mode: "fullscreen", layoutRoot: new VStack([]), terminal: { rows: 40, columns: 160 }, requestRender() {} };
	const footer = (ui.footerFactory as (tui: unknown, theme: unknown, data: unknown) => { render(width: number): string[]; dispose(): void })(tui, plainTheme, footerData);
	try {
		const parts = paneState(tui as unknown as TUI).parts;
		assert.deepEqual(parts.get("header")!.render(160).map(stripAnsi), [" 力 repo  /repo".padEnd(159)], "the project and its path, nothing else");
		assert.deepEqual(parts.get("statusbar")!.render(160).map(stripAnsi), [" ⎇ main" + " ".repeat(89) + "claude-opus-5-5 · medium · jhon │ ctx ▰▰▰▰▱▱▱▱ 45% │ $0.000 sub "]);
		tui.terminal.columns = 100;
		assert.deepEqual(parts.get("header")!.render(100).map(stripAnsi), [" 力 repo  /repo".padEnd(99)]);
		assert.deepEqual(footer.render(100), [], "the footer steps aside for the statusbar row");
		assert.deepEqual(parts.get("statusbar")!.render(100).map(stripAnsi), [" ⎇ main" + " ".repeat(38) + "claude-opus-5-5 · medium · jhon │ ctx 45% │ $0.000 sub "], "the narrow form: no meter");
	} finally { footer.dispose(); await fire(handlers, "session_shutdown", ctx); }
});

// T15.12c slice 4, T174-S5: a fullscreen terminal without the right pane
// (under 140 columns, or Status hidden) draws the narrow editor with the
// Running block above its rule while agents or jobs run (design 04; the todo
// list is the right pane's alone). T169 (L1438): the panel sheet is gone, and
// the rule's [ Panel ] button with it.
function narrowShell(columns = 100) {
	const { pi, handlers, shortcuts } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	const tui = { mode: "fullscreen", terminal: { rows: 40, columns }, requestRender() {} };
	for (const handler of handlers.get("session_start") ?? []) handler({}, ctx);
	const footer = (ui.footerFactory as (tui: unknown, theme: unknown, data: unknown) => { render(width: number): string[]; dispose(): void })(tui, plainTheme, footerData);
	const editor = (ui.editorFactory as (tui: unknown, theme: unknown, keybindings: unknown) => JhonPromptEditor)(tui, editorTheme, fakeKeybindings);
	return { pi, handlers, shortcuts, ctx, ui, tui, footer, editor, state: paneState(tui as unknown as TUI) };
}

test("without the rail the editor rule carries no [ Panel ], and a click at its right end opens nothing (T169)", async () => {
	const { ctx, ui, footer, editor, handlers } = narrowShell();
	try {
		const lines = editor.render(100).map(stripAnsi);
		assert.match(lines[0]!, /^ 力 ready for you +$/);
		editor.handleMouse({ type: "click", button: "left", x: 93, y: 0, width: 100, height: lines.length } as never);
		assert.equal(ui.overlayView, undefined, "no sheet");
	} finally { editor.dispose(); footer.dispose(); await fire(handlers, "session_shutdown", ctx); }
});

test("without the pane the Running block sits above the editor rule while agents or jobs run; a click on a row opens its view (design 04, T174-S5)", async () => {
	const { pi, ctx, ui, footer, editor, state, handlers } = narrowShell();
	const opened: Array<{ view?: string }> = [];
	pi.events.on("jhon-shell:open-tasks", (payload: unknown) => opened.push(payload as { view?: string }));
	try {
		assert.match(stripAnsi(editor.render(100)[0]!), /^ 力 ready for you +$/, "nothing runs: no block, the working line first");
		const now = Date.now();
		pi.events.emit("jhon-shell:tasks", { sessionId: ctx.sessionManager.getSessionId(), tasks: [{ id: "agent-1", kind: "agent", label: "jhon-builder: slice 3", startedAt: now - 5_000 }, { id: "job-1", kind: "command", label: "npm test", startedAt: now - 3_000 }] });
		const lines = editor.render(100).map(stripAnsi);
		assert.match(lines[0]!, /^── Running {2}1 agent · 1 job ─+$/);
		assert.match(lines[1]!, /^ {2}◐ jhon-builder {2}slice 3 +\d+s +$/);
		assert.match(lines[2]!, /^ {2}⧗ job-1 npm test +\d+s +$/);
		assert.equal(lines[3]!.trim(), "", "a blank row before the prompt");
		assert.match(lines[4]!, /^ 力 ready for you +$/, "the working line under it, no [ Panel ] (T169)");
		assert.match(lines[5]!, /^─+$/, "then the rule");
		for (const line of lines.slice(0, 4)) assert.equal(visibleWidth(line), 100);
		assert.equal(state.wide?.() ?? false, false, "no pane: the block carries the work");
		const click = (y: number) => editor.handleMouse({ type: "click", button: "left", x: 3, y, width: 100, height: lines.length } as never);
		click(1);
		click(2);
		click(3);
		assert.deepEqual(opened.map((payload) => payload.view), ["agents", "jobs"], "an agent's row opens the agents view, a job's the jobs view, the blank row nothing");
		assert.equal(ui.overlayView, undefined, "not the sheet");
		state.wide = () => true;
		assert.doesNotMatch(editor.render(100).map(stripAnsi).join("\n"), /Running/, "beside the pane the work is the pane's");
		state.wide = undefined;
		pi.events.emit("jhon-shell:tasks", { sessionId: ctx.sessionManager.getSessionId(), tasks: [] });
		assert.match(stripAnsi(editor.render(100)[0]!), /^ 力 ready for you +$/, "the work ended: no block");
	} finally { editor.dispose(); footer.dispose(); await fire(handlers, "session_shutdown", ctx); }
});

test("a regular (not fullscreen) terminal draws the same prompt as the full screen (design 01), and no work row nor [ Panel ] (T174-S8)", () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	for (const handler of handlers.get("session_start") ?? []) handler({}, ctx);
	const factory = ui.editorFactory as (tui: unknown, theme: unknown, keybindings: unknown) => JhonPromptEditor;
	const editor = factory(fakeTui, editorTheme, fakeKeybindings);
	try {
		const rows = editor.render(100).map(stripAnsi);
		assert.match(rows[0]!, /^ 力 ready for you +$/);
		assert.match(rows[1]!, /^─+$/);
		assert.match(rows[2]!, /^ ❯ .*write, or \/ for a command/);
		assert.match(rows.at(-1)!, /^─+$/);
		assert.doesNotMatch(rows.join("\n"), /Panel/);
	} finally { editor.dispose(); }
});

test("an unknown or unreported phase falls back to the generic working label", () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers);
	try {
		for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
		assert.match(editor.render(60).map(stripAnsi)[0], /^ 力 Working…  thinking · \d+s +esc interrupts $/);
	} finally {
		editor.dispose();
	}
});

test("after 60 s with no stream event and no tool running, the working line says it waits for the Claude API, until the next event or the turn's end", () => {
	let now = 1_000_000;
	const { pi, handlers } = fakePi();
	shellUi(pi, {}, { now: () => now });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers);
	const on = (event: string) => { for (const handler of handlers.get(event) ?? []) handler({}, ctx); };
	const top = () => stripAnsi(editor.render(80)[0]);
	try {
		on("agent_start");
		on("turn_start");
		now += 59_999;
		assert.match(top(), /^ 力 Working…  (?!waiting)\S+ · \d+(m\d+)?s +esc interrupts $/);
		now += 1;
		assert.match(top(), /^ 力 Working…  waiting for Claude API · 60s +esc interrupts $/);
		now += 12_400;
		assert.match(top(), /^ 力 Working…  waiting for Claude API · 72s +esc interrupts $/);
		// The next stream event clears it, and the wait counts again from there.
		on("message_start");
		assert.match(top(), /^ 力 Working…  (?!waiting)\S+ · \d+(m\d+)?s +esc interrupts $/);
		now += 60_000;
		assert.match(top(), /waiting for Claude API · 60s/);
		on("message_update");
		assert.match(top(), /^ 力 Working…  (?!waiting)\S+ · \d+(m\d+)?s +esc interrupts $/);
		// A running tool, however long, is not a wait on the API.
		on("tool_execution_start");
		now += 300_000;
		assert.match(top(), /^ 力 Working…  (?!waiting)\S+ · \d+(m\d+)?s +esc interrupts $/);
		on("tool_execution_end");
		now += 59_999;
		assert.match(top(), /Working…  (?!waiting)/);
		now += 1;
		assert.match(top(), /waiting for Claude API · 60s/);
		// The turn's end clears it, and an idle prompt never shows it.
		on("agent_end");
		assert.doesNotMatch(top(), /waiting/);
		on("agent_settled");
		now += 600_000;
		assert.match(top(), /^ 力 ready for you +$/);
		// The next turn starts its own count.
		on("agent_start");
		assert.match(top(), /^ 力 Working…  (?!waiting)\S+ · \d+(m\d+)?s +esc interrupts $/);
	} finally {
		editor.dispose();
	}
});

// Stall recovery (T47, L164): a fake clock and fake one-shot timers drive the
// shell's single setTimeout; the fake context counts aborts.
const STALL_CONTINUE = "The Claude API sent no answer for 75s; the turn was restarted. Continue where you stopped.";
// The default is a real stall's setting: a direct API provider with thinking
// off (T38: a thinking model or the claude-bridge provider never recovers by
// silence alone).
function stallHarness(env: NodeJS.ProcessEnv = {}, model: { provider?: string; reasoning?: boolean; thinking?: string } = {}) {
	const clock = { now: 1_000_000 };
	const timers: Array<{ at: number; fn: () => void; live: boolean }> = [];
	const schedule = (fn: () => void, ms: number) => {
		const timer = { at: clock.now + ms, fn, live: true };
		timers.push(timer);
		return () => { timer.live = false; };
	};
	const advance = (ms: number) => {
		const target = clock.now + ms;
		for (;;) {
			const due = timers.filter((timer) => timer.live && timer.at <= target).sort((a, b) => a.at - b.at)[0];
			if (!due) break;
			clock.now = due.at;
			due.live = false;
			due.fn();
		}
		clock.now = target;
	};
	const { pi, handlers, sentMessages } = fakePi();
	(pi as unknown as { getThinkingLevel(): string }).getThinkingLevel = () => model.thinking ?? "off";
	shellUi(pi, env, { now: () => clock.now, schedule });
	const { ctx, ui } = fakeContext();
	(ctx as unknown as { model: unknown }).model = { id: "claude-opus-5-5", provider: model.provider ?? "anthropic", reasoning: model.reasoning ?? true, contextWindow: 272_000 };
	const aborts = { count: 0 };
	(ctx as unknown as { abort(): void }).abort = () => { aborts.count += 1; };
	const on = (event: string, payload: unknown = {}) => { for (const handler of handlers.get(event) ?? []) handler(payload, ctx); };
	// A child session installs no prompt.
	const editor = env.JHON_PI_CHILD === "1" ? (on("session_start"), undefined) : installedPrompt(ctx, ui, handlers);
	const ownerMessage = (prompt: string) => {
		on("before_agent_start", { type: "before_agent_start", messageType: "user", prompt, systemPrompt: "", systemPromptOptions: {} });
		on("agent_start");
		on("turn_start");
	};
	const settle = (aborted: boolean) => { on("agent_end"); on("agent_settled", { type: "agent_settled", aborted }); };
	const live = () => timers.filter((timer) => timer.live).length;
	const top = () => stripAnsi(editor!.render(100)[0]);
	return { advance, on, ownerMessage, settle, live, top, aborts, ui, sentMessages, editor };
}

test("a 75 s stall with no stream event and no tool aborts the turn once and sends one continue after it settles", () => {
	const h = stallHarness();
	try {
		h.ownerMessage("Review the change.");
		h.advance(74_999);
		assert.equal(h.aborts.count, 0);
		h.advance(1);
		assert.equal(h.aborts.count, 1);
		assert.deepEqual(h.ui.notices.filter((notice) => /Claude API/.test(notice)), ["Claude API did not answer in 75s; retried"]);
		// The continue goes out once the aborted run settles, as an Esc's queued text does.
		assert.deepEqual(h.sentMessages, []);
		h.settle(true);
		assert.deepEqual(h.sentMessages, [{ content: STALL_CONTINUE, options: undefined }]);
		// Nothing stays armed once the turn ended.
		assert.equal(h.live(), 0);
		h.advance(1_000_000);
		assert.equal(h.aborts.count, 1);
		assert.equal(h.sentMessages.length, 1);
	} finally {
		h.editor?.dispose();
	}
});

test("a second stall in the same owner message only shows the waiting label, and the next owner message re-arms recovery", () => {
	const h = stallHarness();
	try {
		h.ownerMessage("Review the change.");
		h.advance(75_000);
		h.settle(true);
		h.ownerMessage(STALL_CONTINUE);
		h.advance(600_000);
		assert.equal(h.aborts.count, 1);
		assert.equal(h.sentMessages.length, 1);
		assert.match(h.top(), /waiting for Claude API · 600s/);
		assert.equal(h.ui.notices.filter((notice) => /Claude API did not answer/.test(notice)).length, 1);
		h.settle(false);
		h.ownerMessage("Sigue.");
		h.advance(75_000);
		assert.equal(h.aborts.count, 2);
		h.settle(true);
		assert.deepEqual(h.sentMessages.map((message) => message.content), [STALL_CONTINUE, STALL_CONTINUE]);
	} finally {
		h.editor?.dispose();
	}
});

test("a stream event before 75 s restarts the count, and a running tool holds recovery however long it runs", () => {
	const h = stallHarness();
	try {
		h.ownerMessage("Review the change.");
		h.advance(70_000);
		h.on("message_update");
		h.advance(74_999);
		assert.equal(h.aborts.count, 0);
		h.on("tool_execution_start");
		assert.equal(h.live(), 0, "no timer while a tool runs");
		h.advance(600_000);
		assert.equal(h.aborts.count, 0);
		h.on("tool_execution_end");
		h.advance(74_999);
		assert.equal(h.aborts.count, 0);
		h.advance(1);
		assert.equal(h.aborts.count, 1);
	} finally {
		h.editor?.dispose();
	}
});

const STALL_CONTINUE_300 = "The Claude API sent no answer for 300s; the turn was restarted. Continue where you stopped.";
// Recovers once at `at` ms (not a millisecond before), with the notice and continue naming `secs`.
function assertRecoversAt(h: ReturnType<typeof stallHarness>, at: number, secs: number, label: string) {
	h.ownerMessage("Review the change.");
	h.advance(at - 1);
	assert.equal(h.aborts.count, 0, label);
	h.advance(1);
	assert.equal(h.aborts.count, 1, label);
	assert.deepEqual(h.ui.notices.filter((notice) => /Claude API did not answer/.test(notice)), [`Claude API did not answer in ${secs}s; retried`], label);
	h.settle(true);
	assert.deepEqual(h.sentMessages.map((message) => message.content), [`The Claude API sent no answer for ${secs}s; the turn was restarted. Continue where you stopped.`], label);
}

test("a thinking model waits thinking_stall_seconds (300 s), not 75 s; with thinking off or minimal a real stall recovers at 75 s (T38, L382)", () => {
	for (const thinking of ["low", "medium", "high", "xhigh"]) {
		const h = stallHarness({}, { thinking });
		try {
			assertRecoversAt(h, 300_000, 300, thinking);
		} finally {
			h.editor?.dispose();
		}
	}
	for (const thinking of ["off", "minimal"]) {
		const h = stallHarness({}, { thinking });
		try {
			assertRecoversAt(h, 75_000, 75, thinking);
		} finally {
			h.editor?.dispose();
		}
	}
	// A model without reasoning does not think, whatever the level says.
	const h = stallHarness({}, { reasoning: false, thinking: "high" });
	try {
		assertRecoversAt(h, 75_000, 75, "no reasoning");
	} finally {
		h.editor?.dispose();
	}
});

test("on the claude-bridge provider the threshold is thinking_stall_seconds, even with thinking off; the continue it sends is not an owner message (T38, L382)", () => {
	const h = stallHarness({}, { provider: "claude-bridge", thinking: "off" });
	try {
		assertRecoversAt(h, 300_000, 300, "bridge");
		h.ownerMessage(STALL_CONTINUE_300);
		h.advance(1_000_000);
		assert.equal(h.aborts.count, 1);
	} finally {
		h.editor?.dispose();
	}
});

test("JHON_SHELL_THINKING_STALL_SECONDS sets the long threshold, 0 never recovers there and leaves a direct provider at 75 s; stall_seconds 0 turns both off", () => {
	const set = stallHarness({ JHON_SHELL_THINKING_STALL_SECONDS: "200" }, { provider: "claude-bridge" });
	try {
		assertRecoversAt(set, 200_000, 200, "200");
	} finally {
		set.editor?.dispose();
	}
	for (const [env, model] of [
		[{ JHON_SHELL_THINKING_STALL_SECONDS: "0" }, { provider: "claude-bridge" }],
		[{ JHON_SHELL_THINKING_STALL_SECONDS: "0" }, { thinking: "high" }],
		[{ JHON_SHELL_STALL_SECONDS: "0" }, { provider: "claude-bridge" }],
		[{ JHON_SHELL_STALL_SECONDS: "0" }, { thinking: "high" }],
		[{ JHON_PI_CHILD: "1" }, { provider: "claude-bridge" }],
	] as const) {
		const h = stallHarness(env, model);
		try {
			h.ownerMessage("Review the change.");
			h.advance(1_000_000);
			assert.equal(h.aborts.count, 0, JSON.stringify(env));
			assert.equal(h.live(), 0, JSON.stringify(env));
		} finally {
			h.editor?.dispose();
		}
	}
	const direct = stallHarness({ JHON_SHELL_THINKING_STALL_SECONDS: "0" });
	try {
		assertRecoversAt(direct, 75_000, 75, "direct");
	} finally {
		direct.editor?.dispose();
	}
});

test("an open owner prompt holds stall recovery until it closes", () => {
	const h = stallHarness();
	try {
		h.ownerMessage("Review the change.");
		h.on("ui_prompt_start", { type: "ui_prompt_start", reason: "ui_prompt", kind: "confirm" });
		h.advance(600_000);
		assert.equal(h.aborts.count, 0);
		h.on("ui_prompt_end", { type: "ui_prompt_end", reason: "ui_prompt", kind: "confirm" });
		h.on("message_end");
		h.advance(75_000);
		assert.equal(h.aborts.count, 1);
	} finally {
		h.editor?.dispose();
	}
});

test("a child session and JHON_SHELL_STALL_SECONDS=0 never recover; another value sets the threshold", () => {
	for (const env of [{ JHON_PI_CHILD: "1" }, { JHON_SHELL_STALL_SECONDS: "0" }]) {
		const h = stallHarness(env);
		try {
			h.ownerMessage("Review the change.");
			h.advance(600_000);
			assert.equal(h.aborts.count, 0, JSON.stringify(env));
			assert.equal(h.live(), 0, JSON.stringify(env));
			assert.deepEqual(h.ui.notices.filter((notice) => /Claude API did not answer/.test(notice)), [], JSON.stringify(env));
		} finally {
			h.editor?.dispose();
		}
	}
	const h = stallHarness({ JHON_SHELL_STALL_SECONDS: "120" });
	try {
		h.ownerMessage("Review the change.");
		h.advance(119_999);
		assert.equal(h.aborts.count, 0);
		h.advance(1);
		assert.equal(h.aborts.count, 1);
		assert.ok(h.ui.notices.includes("Claude API did not answer in 120s; retried"));
		h.settle(true);
		assert.deepEqual(h.sentMessages.map((message) => message.content), ["The Claude API sent no answer for 120s; the turn was restarted. Continue where you stopped."]);
	} finally {
		h.editor?.dispose();
	}
});

/** A settings home of the test's own, removed after it. */
function configHome(t: { after(callback: () => void): void }): string {
	const home = mkdtempSync(join(tmpdir(), "jhon-shell-home-"));
	t.after(() => rmSync(home, { recursive: true, force: true }));
	return home;
}

const footerData = { getGitBranch: () => "main", getExtensionStatuses: () => new Map(), getAvailableProviderCount: () => 1, onBranchChange: () => () => {} };

function renderFooter(ui: FakeUi): string {
	const factory = ui.footerFactory as (tui: unknown, theme: ShellBarTheme, footerData: unknown) => { render(width: number): string[] };
	return factory(fakeTui, plainTheme, footerData).render(160)[0];
}

async function settle(): Promise<void> {
	await new Promise((resolve) => setTimeout(resolve, 0));
}

test("the profile reader shows Jhon's active profile, refreshed on demand, and none for default", () => {
	let active = "team";
	const reader = createActiveProfileReader(() => active);
	assert.equal(reader(), undefined, "nothing is read before the first refresh");
	assert.equal(reader.bind("/w", resolveSessionWorktree), true);
	assert.equal(reader(), "team");
	active = "default";
	assert.equal(reader(), "team", "a frame never reads the store");
	assert.equal(reader.refresh(), true);
	assert.equal(reader(), undefined, "the default profile shows as none");
	assert.equal(reader.refresh(), false, "an unchanged profile reports no change");
	reader.reset();
	assert.equal(reader(), undefined);
});

test("T120: the profile reader shows the session's own profile, marked while it differs from the default", () => {
	let active = "team";
	let session: string | undefined;
	const reader = createActiveProfileReader(() => active, () => session);
	reader.refresh();
	assert.equal(reader(), "team", "a session with no profile of its own shows the default, as before");
	session = "fast";
	assert.equal(reader.refresh(), true);
	assert.equal(reader(), "fast (session)");
	active = "fast";
	assert.equal(reader.refresh(), true);
	assert.equal(reader(), "fast", "no mark once it is the default too");
	[session, active] = ["default", "team"];
	reader.refresh();
	assert.equal(reader(), "default (session)", "the default profile is named when it is the session's own");
	[session, active] = [undefined, "default"];
	reader.refresh();
	assert.equal(reader(), undefined);
});

test("T120: the status bar shows the session's profile from its branch at the start, and follows the session-profile event of this session only", async (t) => {
	const profiles = join(mkdtempSync(join(tmpdir(), "jhon-shell-profiles-")), "profiles.json");
	t.after(() => rmSync(dirname(profiles), { recursive: true, force: true }));
	writeFileSync(profiles, JSON.stringify({ active: "team", profiles: { team: {}, fast: {}, slow: {} } }));
	const saved = process.env.JHON_PROFILES;
	process.env.JHON_PROFILES = profiles;
	t.after(() => { if (saved === undefined) delete process.env.JHON_PROFILES; else process.env.JHON_PROFILES = saved; });
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CHANGES_WATCH_MS: "off" });
	const { ctx, ui } = fakeContext();
	Object.assign(ctx.sessionManager, { getBranch: () => [{ type: "custom", customType: SESSION_PROFILE_ENTRY, data: { profile: "fast" } }] });
	await fire(handlers, "session_start", ctx);
	const tui = { mode: "fullscreen", terminal: { rows: 40, columns: 160 }, requestRender() {} };
	const footer = (ui.footerFactory as (tui: unknown, theme: unknown, data: unknown) => { render(width: number): string[]; dispose(): void })(tui, plainTheme, { getGitBranch: () => "main", getExtensionStatuses: () => new Map(), getAvailableProviderCount: () => 1, onBranchChange: () => () => {} });
	const bar = () => paneState(tui as unknown as TUI).parts.get("statusbar")!.render(160).map(stripAnsi)[0]!;
	try {
		assert.match(bar(), /claude-opus-5-5 · medium · fast \(session\) │ ctx/);
		pi.events.emit(SESSION_PROFILE_CHANGED, { sessionId: "another-session", profile: "slow" });
		assert.match(bar(), /· fast \(session\) │/, "another session's change is not this one's");
		pi.events.emit(SESSION_PROFILE_CHANGED, { sessionId: "shell-session", profile: "slow" });
		assert.match(bar(), /claude-opus-5-5 · medium · slow \(session\) │ ctx/);
		pi.events.emit(SESSION_PROFILE_CHANGED, { sessionId: "shell-session" });
		assert.match(bar(), /claude-opus-5-5 · medium · team │ ctx/, "back on the default: no mark");
	} finally { footer.dispose(); await fire(handlers, "session_shutdown", ctx); }
});

test("the bar never counts the background tasks lib/background-tasks-tools.ts reports: the right pane holds them (T174-S3)", async () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	const tui = { terminal: { rows: 40, columns: 180 }, requestRender() {} };
	const footer = (ui.footerFactory as (tui: unknown, theme: ShellBarTheme, data: unknown) => { render(width: number): string[]; dispose(): void })(tui, plainTheme, {
		getGitBranch: () => "main", getExtensionStatuses: () => new Map(), getAvailableProviderCount: () => 1, onBranchChange: () => () => {},
	});
	try {
		const idle = footer.render(180);
		const sessionId = ctx.sessionManager.getSessionId();
		pi.events.emit("jhon-shell:tasks", { sessionId, tasks: [{ id: "job-1", label: "build", startedAt: 1 }, { id: "agent-1", label: "explore: callers", startedAt: 2 }] });
		assert.deepEqual(footer.render(180), idle);
		assert.doesNotMatch(footer.render(180).join("\n"), /⧗|job|agent/);
	} finally { footer.dispose(); }
});

test("STATUS reads the change again when the branch switches or a change is archived, with no turn of the agent's", async (t) => {
	const { execFileSync } = await import("node:child_process");
	const repo = realpathSync(mkdtempSync(join(tmpdir(), "estado-")));
	execFileSync("git", ["init", "-q", repo]);
	execFileSync("git", ["-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"]);
	mkdirSync(join(repo, "openspec", "changes", "a"), { recursive: true });
	let reads = 0;
	const { pi, handlers } = fakePi();
	shellUi(pi, {}, { lddStatus: async () => { reads++; return undefined; } });
	const { ctx, ui } = fakeContext();
	(ctx as unknown as { cwd: string }).cwd = repo;
	installedPrompt(ctx, ui, handlers);
	t.after(async () => { await fire(handlers, "session_shutdown", ctx); rmSync(repo, { recursive: true, force: true }); });
	await new Promise((r) => setTimeout(r, 100));
	const settle = () => new Promise((r) => setTimeout(r, 700));
	let before = reads;
	execFileSync("git", ["-C", repo, "checkout", "-q", "-b", "feat/x"]);
	await settle();
	assert.ok(reads > before, "a branch switch reads STATUS again");
	before = reads;
	renameSync(join(repo, "openspec", "changes", "a"), join(repo, "openspec", "changes", "b"));
	await settle();
	assert.ok(reads > before, "a change archived or created reads it again");
});

test("with no change in pi's folder, STATUS shows the one a worktree the session worked in selects, the latest first, and says where", async () => {
	const asked: string[] = [];
	const status = { change: "test-suite-review", slice: "Slice 3: x", slices: ["done", "current"] as Array<"done" | "current" | "pending">, detail: [] };
	const { pi, handlers } = fakePi();
	shellUi(pi, {}, { rootExists: () => true, lddStatus: async (cwd: string) => { asked.push(cwd); return cwd.endsWith("test-suite-review") ? status : undefined; } });
	const entry = (root: string) => ({ type: "custom", customType: "jhon-shell.session-worktree/v1", data: { sessionId: "shell-session", root, evidence: "explicit" } });
	const { ctx, ui } = fakeContext({ entries: [entry("/w/acme-worktrees/test-suite-review"), entry("/w/acme-worktrees/other")] });
	installedPrompt(ctx, ui, handlers);
	await new Promise((r) => setTimeout(r, 30));
	assert.deepEqual(asked.slice(0, 3), ["/repo", "/w/acme-worktrees/other", "/w/acme-worktrees/test-suite-review"], "pi's folder, then the latest worktree first");
	const { estadoLines } = await import("../lib/ui/ldd-status.ts");
	const plainPaint = { fg: (_r: string, t: string) => t, value: (t: string) => t, label: (t: string) => t };
	assert.deepEqual(estadoLines({ ...status, worktree: "test-suite-review" }, plainPaint, 60).slice(0, 2), ["◆ test-suite-review" + " ".repeat(22) + "⎇ test-suite-review", "  1/2 slices · on 2: x"]);
});

test("STATUS and the Change panel show one selection: when the newest root changes, both follow it together (T29, L93)", async (t) => {
	const statuses: Record<string, { change: string; slices: Array<"done" | "current" | "pending"> }> = { "/w/infra-hardening": { change: "fix-infra-hardening", slices: ["done"] }, "/w/layering": { change: "refactor-layering-by-service", slices: ["done", "current"] } };
	const { pi, handlers } = fakePi();
	shellUi(pi, {}, { rootExists: () => true, lddStatus: async (cwd: string) => statuses[cwd] });
	const entry = (root: string) => ({ type: "custom", customType: "jhon-shell.session-worktree/v1", data: { sessionId: "shell-session", root, evidence: "explicit" } });
	const entries: unknown[] = [entry("/w/layering")];
	const { ctx, ui } = fakeContext({ entries });
	installedPrompt(ctx, ui, handlers);
	t.after(async () => { await fire(handlers, "session_shutdown", ctx); });
	await new Promise((r) => setTimeout(r, 30));
	const { selectedChange } = await import("../lib/ui/ldd-status.ts");
	const { sessionChange } = await import("../extensions/jhon.ts");
	// No binary: the panel takes STATUS's selection, it never reads its own.
	const bin = "/nonexistent/jhon";
	assert.equal(selectedChange("shell-session")?.change, "refactor-layering-by-service", "STATUS selected the newest root's change");
	let where = await sessionChange(ctx, ctx.cwd, bin);
	assert.deepEqual([where.change, where.root, where.worktree], ["refactor-layering-by-service", "/w/layering", "layering"], "the panel shows STATUS's change");
	entries.push(entry("/w/infra-hardening"));
	where = await sessionChange(ctx, ctx.cwd, bin);
	assert.deepEqual([where.change, where.root], ["fix-infra-hardening", "/w/infra-hardening"], "opening the panel refreshes the selection");
	assert.equal(selectedChange("shell-session")?.change, "fix-infra-hardening", "and STATUS follows it in the same refresh");
	assert.equal(selectedChange("another-session"), undefined, "another session's selection is not this one's");
});

test("STATUS follows the worktree the session works in and the lanes beside it at once: their branch, their changes, and their tasks", async (t) => {
	const base = realpathSync(mkdtempSync(join(tmpdir(), "estado-wt-")));
	const repo = join(base, "app");
	execFileSync("git", ["init", "-q", repo]);
	execFileSync("git", ["-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"]);
	const wt = (name: string) => { const p = join(base, "wts", name); execFileSync("git", ["-C", repo, "worktree", "add", "-q", "-b", name, p]); return p; };
	const [caches, audit] = [wt("tenant-caches"), wt("arch-audit")];
	for (const [w, c] of [[caches, "fix-tenant-caches"], [audit, "architecture-audit"]]) mkdirSync(join(w, "openspec", "changes", c), { recursive: true });
	let reads = 0;
	const statuses: Record<string, any> = { [caches]: { change: "fix-tenant-caches", slices: ["current"] }, [audit]: { change: "architecture-audit", slices: ["current"] } };
	const { pi, handlers } = fakePi();
	shellUi(pi, {}, { lddStatus: async (cwd: string) => { reads++; return statuses[cwd]; } });
	const entry = { type: "custom", customType: "jhon-shell.session-worktree/v1", data: { sessionId: "shell-session", root: caches, evidence: "explicit" } };
	const { ctx, ui } = fakeContext({ entries: [entry] });
	(ctx as unknown as { cwd: string }).cwd = repo;
	installedPrompt(ctx, ui, handlers);
	t.after(async () => { await fire(handlers, "session_shutdown", ctx); rmSync(base, { recursive: true, force: true }); });
	await new Promise((r) => setTimeout(r, 400));
	const settle = () => new Promise((r) => setTimeout(r, 700));
	const step = async (what: string, act: () => void) => { const before = reads; act(); await settle(); assert.ok(reads > before, what); };
	await step("a task checked off in the session's worktree", () => writeFileSync(join(caches, "openspec", "changes", "fix-tenant-caches", "tasks.md"), "- [x] a\n"));
	await step("a change created in the session's worktree", () => mkdirSync(join(caches, "openspec", "changes", "next-one")));
	await step("its branch switched", () => execFileSync("git", ["-C", caches, "checkout", "-q", "-b", "fix/other"]));
	await step("a task checked off in a lane beside it", () => writeFileSync(join(audit, "openspec", "changes", "architecture-audit", "tasks.md"), "- [x] a\n"));
});

test("a watch firing in one lane re-reads the session's change and that lane only, not the lanes that did not move", async (t) => {
	const base = realpathSync(mkdtempSync(join(tmpdir(), "estado-lanes-")));
	const repo = join(base, "app");
	execFileSync("git", ["init", "-q", repo]);
	execFileSync("git", ["-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"]);
	const wt = (name: string) => { const p = join(base, "wts", name); execFileSync("git", ["-C", repo, "worktree", "add", "-q", "-b", name, p]); return p; };
	const [own, laneA, laneB] = [wt("own"), wt("lane-a"), wt("lane-b")];
	for (const [w, c] of [[own, "own-change"], [laneA, "change-a"], [laneB, "change-b"]]) mkdirSync(join(w, "openspec", "changes", c), { recursive: true });
	const reads: Record<string, number> = {};
	const statuses: Record<string, any> = { [own]: { change: "own-change", slices: ["current"] }, [laneA]: { change: "change-a", slices: ["current"] }, [laneB]: { change: "change-b", slices: ["current"] } };
	const { pi, handlers } = fakePi();
	shellUi(pi, {}, { lddStatus: async (cwd: string) => { reads[cwd] = (reads[cwd] ?? 0) + 1; return statuses[cwd]; } });
	const entry = { type: "custom", customType: "jhon-shell.session-worktree/v1", data: { sessionId: "shell-session", root: own, evidence: "explicit" } };
	const { ctx, ui } = fakeContext({ entries: [entry] });
	(ctx as unknown as { cwd: string }).cwd = repo;
	installedPrompt(ctx, ui, handlers);
	t.after(async () => { await fire(handlers, "session_shutdown", ctx); rmSync(base, { recursive: true, force: true }); });
	await new Promise((r) => setTimeout(r, 400));
	assert.ok(reads[laneA]! > 0 && reads[laneB]! > 0, "a lane never read is read");
	const before = { ...reads };
	statuses[laneA] = { change: "change-a", slices: ["done"] };
	writeFileSync(join(laneA, "openspec", "changes", "change-a", "tasks.md"), "- [x] a\n");
	await new Promise((r) => setTimeout(r, 700));
	assert.ok(reads[own]! > before[own]!, "the session's own change is read again");
	assert.ok(reads[laneA]! > before[laneA]!, "the lane whose watch fired is read again");
	assert.equal(reads[laneB], before[laneB], "a lane whose watches did not fire is not read again");
});

async function laneSession(t: { after(fn: () => Promise<void>): void }, watchDir: (dir: string) => unknown) {
	const base = realpathSync(mkdtempSync(join(tmpdir(), "estado-dead-")));
	const repo = join(base, "app");
	execFileSync("git", ["init", "-q", repo]);
	execFileSync("git", ["-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"]);
	const wt = (name: string) => { const p = join(base, "wts", name); execFileSync("git", ["-C", repo, "worktree", "add", "-q", "-b", name, p]); return p; };
	const [own, lane] = [wt("own"), wt("lane")];
	for (const [w, c] of [[own, "own-change"], [lane, "change-a"]]) mkdirSync(join(w, "openspec", "changes", c), { recursive: true });
	const reads: Record<string, number> = {};
	const statuses: Record<string, any> = { [own]: { change: "own-change", slices: ["current"] }, [lane]: { change: "change-a", slices: ["current"] } };
	let now = 1_000_000;
	const { pi, handlers } = fakePi();
	shellUi(pi, {}, { now: () => now, watch: ((dir: string) => watchDir(dir)) as never, lddStatus: async (cwd: string) => { reads[cwd] = (reads[cwd] ?? 0) + 1; return statuses[cwd]; } });
	const entry = { type: "custom", customType: "jhon-shell.session-worktree/v1", data: { sessionId: "shell-session", root: own, evidence: "explicit" } };
	const { ctx, ui } = fakeContext({ entries: [entry] });
	(ctx as unknown as { cwd: string }).cwd = repo;
	installedPrompt(ctx, ui, handlers);
	t.after(async () => { await fire(handlers, "session_shutdown", ctx); rmSync(base, { recursive: true, force: true }); });
	await new Promise((r) => setTimeout(r, 400));
	const nextRefresh = async () => { now += 31_000; await fire(handlers, "turn_end", ctx); await new Promise((r) => setTimeout(r, 400)); };
	return { lane, reads, nextRefresh };
}

test("a watcher that errors is dropped and made again, and its lane is read at the next refresh", async (t) => {
	const { EventEmitter } = await import("node:events");
	const made: Array<{ dir: string; w: InstanceType<typeof EventEmitter> }> = [];
	const { lane, reads, nextRefresh } = await laneSession(t, (dir) => { const w = Object.assign(new EventEmitter(), { close() {} }); made.push({ dir, w }); return w; });
	const laneChanges = join(lane, "openspec", "changes");
	const dead = made.find((m) => m.dir === laneChanges);
	assert.ok(dead, `the lane's changes are watched: ${made.map((m) => m.dir).join(", ")}`);
	const before = reads[lane]!;
	dead!.w.emit("error", new Error("EACCES"));
	await nextRefresh();
	assert.ok(reads[lane]! > before, "the lane whose watcher died is read again");
	assert.ok(made.filter((m) => m.dir === laneChanges).length > 1, "its watch is made again");
});

test("a watch that cannot be made leaves its lane to be read at the next refresh, and is tried again", async (t) => {
	let refuse = true;
	const tried: string[] = [];
	const { EventEmitter } = await import("node:events");
	const { lane, reads, nextRefresh } = await laneSession(t, (dir) => {
		tried.push(dir);
		if (refuse && dir.startsWith(lane)) throw Object.assign(new Error("ENOSPC"), { code: "ENOSPC" });
		return Object.assign(new EventEmitter(), { close() {} });
	});
	const laneChanges = join(lane, "openspec", "changes");
	const before = { reads: reads[lane]!, tries: tried.filter((d) => d === laneChanges).length };
	assert.ok(before.tries > 0, "the lane's watch was tried");
	refuse = false;
	await nextRefresh();
	assert.ok(reads[lane]! > before.reads, "the lane no watch follows is read again");
	assert.ok(tried.filter((d) => d === laneChanges).length > before.tries, "its watch is tried again");
});

// T15.3 (L76): an owner whose settings.toml still sets a retired key is told
// once, at the first session start, what went and why; the shell drops them
// with `jhon settings migrate` (backed up), so the next start says nothing.
test("retired settings are dropped once with `jhon settings migrate`, and the owner is told what went and why", async (t) => {
	const home = configHome(t);
	const jhon = homeSettings(home).jhon;
	jhon.retired = [
		{ key: "shell.appearance.card_style", was: "float", why: "the card style is shell.appearance.cards now (float or neon)", now: "shell.appearance.cards = \"float\"" },
		{ key: "shell.notifications.audio.backend", was: "auto", why: "it only ever had one value", silent: true },
		{ key: "shell.appearance.animations", was: "potato", why: "one animation level now: full or off", now: "shell.appearance.animations = \"off\"" },
	];
	homeSettings(home).refresh();
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home });
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	assert.equal(jhon.migrations, 1, "migrate ran once");
	const notice = ui.notices.find((n) => n.includes("Settings changed"));
	assert.ok(notice, ui.notices.join("\n"));
	assert.match(notice!, /shell\.appearance\.card_style = "float" → shell\.appearance\.cards = "float"/);
	assert.match(notice!, /shell\.appearance\.animations = "potato" → shell\.appearance\.animations = "off"/);
	assert.doesNotMatch(notice!, /backend/, "a silent retirement says nothing");
	assert.match(notice!, /backup: \/fake\/settings-backup\/settings\.toml/);
	await fire(handlers, "session_start", ctx);
	assert.equal(jhon.migrations, 1, "nothing left to drop: no second run");
	assert.equal(ui.notices.filter((n) => n.includes("Settings changed")).length, 1, "told once");
});

// T15.7: /jhon:customize <section> opens on that section, the focus on its settings.
test("/jhon:customize keys opens Customize on Keys", async (t) => {
	const home = configHome(t);
	const { pi, commands } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home });
	const { ctx, ui } = fakeContext();
	const pending = commands.get("jhon:customize")!.handler("keys", ctx);
	const lines = ui.overlayView!.render(120).map(stripAnsi);
	ui.overlayView!.handleInput("\x1b");
	await pending;
	const text = lines.join("\n");
	assert.match(text, /│ {2}▸ Keys +│/);
	assert.match(text, /▸ LDD status +alt\+j/);
	assert.doesNotMatch(text, /Animations|Runtimes|Integrations|Profile/);
	assert.match(lines.at(-2)!, /Click · ←\/→ or Tab panes · ↑\/↓ moves · Enter applies · Esc closes/, "the footer");
});

// T174-S5: the pane's LDD section is its first part; with no change it says
// so in one dim line, and it folds no more (design 01 draws no fold glyph).
test("the LDD section leads the pane: one line without a change, and a click on it folds nothing", async () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	renderFooter(ui);
	type Part = PanePart & { handleMouse?(e: unknown): unknown };
	const ldd = paneState(fakeTui as unknown as TUI).parts.get("ldd")! as Part;
	assert.deepEqual(ldd.render(42).map(stripAnsi), ["No active LDD change"]);
	assert.equal(ldd.handleMouse?.({ type: "click", button: "left", x: 3, y: 0, width: 42, height: 1 }), undefined);
	assert.deepEqual(ldd.render(42).map(stripAnsi), ["No active LDD change"]);
	assert.equal(paneState(fakeTui as unknown as TUI).parts.has("footer"), false, "no CHANGES, no INTEGRATIONS: the footer has no part in the pane (T174-S4)");
	await fire(handlers, "session_shutdown", ctx);
});

// T174-S5: Agents and Jobs are sections of their own, each only while
// something runs; a click on one of their rows opens its view.
test("Agents and Jobs: each its own part, drawn while something runs; a click on a row opens the agents or the jobs view, and no key is on a header", async () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	const tui = { terminal: { rows: 40, columns: 180 }, requestRender() {} };
	const footer = (ui.footerFactory as (tui: unknown, theme: unknown, data: unknown) => { render(width: number): string[]; dispose(): void })(tui, plainTheme, footerData);
	const opened: Array<{ view?: string }> = [];
	pi.events.on("jhon-shell:open-tasks", (payload: unknown) => opened.push(payload as { view?: string }));
	try {
		const parts = paneState(tui as unknown as TUI).parts;
		type Part = PanePart & { handleMouse(e: unknown): unknown };
		const jobs = parts.get("jobs") as Part;
		const agents = parts.get("agents") as Part;
		const draw = (part: Part) => part.render(42).map(stripAnsi);
		assert.deepEqual([draw(jobs), draw(agents)], [[], []], "nothing runs: neither shows");
		const now = Date.now();
		pi.events.emit("jhon-shell:tasks", { sessionId: ctx.sessionManager.getSessionId(), tasks: [{ id: "job-1", label: "build", startedAt: now - 1_000 }, { id: "agent-1", kind: "agent", label: "jhon-checker: callers", startedAt: now - 2_000 }] });
		assert.match(draw(jobs)[0]!, /^Jobs {2}1 running$/);
		assert.match(draw(jobs)[1]!, /^⧗ job-1 build +\ds$/);
		assert.match(draw(agents)[0]!, /^Agents {2}1 running$/);
		assert.match(draw(agents)[1]!, /^◐ jhon-checker {2}callers +\ds$/, "the whole name, never jhon-chec… (L1475)");
		assert.ok(!draw(jobs).some((line) => /callers/.test(line)) && !draw(agents).some((line) => /build/.test(line)), "each holds its own");
		assert.doesNotMatch([...draw(jobs), ...draw(agents)].join("\n"), /alt\+|\/jhon/, "no key on a header (C6)");
		assert.deepEqual(agents.handleMouse({ type: "click", button: "left", x: 3, y: 1, width: 42, height: 2 }), { handled: true });
		assert.deepEqual(jobs.handleMouse({ type: "click", button: "left", x: 3, y: 1, width: 42, height: 2 }), { handled: true });
		assert.deepEqual(jobs.handleMouse({ type: "move", button: "none", x: 3, y: 1, width: 42, height: 2 }), { handled: true, render: true }, "a move paints the row, and opens nothing");
		assert.deepEqual(opened.map((payload) => payload.view), ["agents", "jobs"]);
		// T174-S5b: a click on the title folds the section to its title and first row.
		assert.deepEqual(jobs.handleMouse({ type: "click", button: "left", x: 3, y: 0, width: 42, height: 2 }), { handled: true, render: true });
		assert.match(draw(jobs)[0]!, /^Jobs {2}1 running +▸$/);
		assert.deepEqual(opened.map((payload) => payload.view), ["agents", "jobs"], "the title opens nothing");
	} finally { footer.dispose(); await fire(handlers, "session_shutdown", ctx); }
});

test("the rail draws no key line and no / commands: those live only in the prompt's placeholder (L143, C6)", async () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	renderFooter(ui);
	const parts = paneState(fakeTui as unknown as TUI).parts;
	assert.equal(parts.has("keys"), false);
	for (const [key, part] of parts) if (key !== "header" && key !== "statusbar") assert.doesNotMatch(stripAnsi(part.render(50).join("\n")), /\/ commands/, key);
	await fire(handlers, "session_shutdown", ctx);
});

// L175 (6), the parent's security decision: the permission mode is a Customize
// row whose ←/→ only move a pending choice; enter applies it through changeMode
// (super yolo only after the owner's Yes); esc or leaving the row drops it.
async function customizeMode(t: { after(callback: () => void): void }, answers: string[] = []) {
	const dir = mkdtempSync(join(tmpdir(), "jhon-customize-mode-"));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const bin = join(dir, "jhon");
	writeFileSync(join(dir, "state"), "normal");
	writeFileSync(bin, `#!/bin/sh
echo "$*" >> "${dir}/log"
case "$1 $2" in
  "mode --json") printf '{"mode":"%s","root":"${dir}","since":null}' "$(cat "${dir}/state")" ;;
  "mode "*) printf '%s' "$2" > "${dir}/state" ;;
esac
`);
	chmodSync(bin, 0o755);
	const { pi, commands } = fakePi();
	shellUi(pi, { JHON_BIN: bin });
	const asked: string[] = [];
	const { ctx, ui } = fakeContext({ select: async (title) => { asked.push(title); const answer = answers.shift(); if (answer === "THROW") throw new Error("ui gone"); return answer; } });
	(ctx as { cwd: string }).cwd = dir;
	const log = () => existsSync(join(dir, "log")) ? readFileSync(join(dir, "log"), "utf8").trim().split("\n") : [];
	const sets = () => log().filter((line) => line !== "mode --json");
	const state = () => readFileSync(join(dir, "state"), "utf8");
	const until = async (ok: () => boolean) => { for (let i = 0; i < 200 && !ok(); i++) await new Promise((r) => setTimeout(r, 10)); };
	const settle = () => new Promise((r) => setTimeout(r, 150));
	const pending = commands.get("jhon:customize")!.handler("permissions", ctx);
	const view = ui.overlayView!;
	const text = () => view.render(120).map(stripAnsi).join("\n");
	const row = () => view.render(120).map(stripAnsi).find((line) => line.includes("Permission mode")) ?? "";
	await until(() => log().includes("mode --json") && /● normal/.test(row()));
	const close = async () => { view.handleInput("\x1b"); if (ui.overlayView) view.handleInput("\x1b"); await pending; };
	return { dir, view, text, row, asked, sets, state, until, settle, close, ui };
}

test("Customize's Permission mode: ←/→ run no jhon mode, they move a pending choice the row shows; esc drops it", async (t) => {
	const m = await customizeMode(t);
	assert.match(m.row(), /▸ Permission mode +● normal +○ yolo +○ super yolo/);
	assert.match(m.text(), /What the guards ask in this project\. The grants: \/jhon:grants\./);
	m.view.handleInput("\x1b[C");
	m.view.handleInput("\x1b[C");
	await m.settle();
	assert.deepEqual(m.sets(), [], "moving the choice runs nothing");
	assert.equal(m.state(), "normal");
	assert.match(m.row(), /● super yolo · enter applies/, "the pending choice, marked");
	assert.match(m.text(), /Permission mode — super yolo: enter applies, esc drops it/);
	m.view.handleInput("\x1b");
	assert.ok(m.ui.overlayView, "esc drops the pending choice first, Customize stays open");
	assert.match(m.row(), /● normal +○ yolo +○ super yolo +│$/);
	m.view.handleInput("\x1b[C");
	m.view.handleInput("\t");
	m.view.handleInput("\x1b[C");
	assert.doesNotMatch(m.row(), /enter applies/, "leaving the row (Tab to the categories) drops it");
	await m.settle();
	assert.deepEqual(m.sets(), []);
	await m.close();
});

test("Customize's Permission mode: a declined super yolo leaves the previous mode and runs no command", async (t) => {
	const m = await customizeMode(t, ["No"]);
	m.view.handleInput("\x1b[C");
	m.view.handleInput("\x1b[C");
	m.view.handleInput("\r");
	await m.until(() => m.asked.length === 1);
	await m.settle();
	assert.deepEqual(m.asked, ["Turn on SUPER YOLO: destroy will not ask"]);
	assert.deepEqual(m.sets(), [], "no jhon mode ran");
	assert.equal(m.state(), "normal");
	assert.match(m.row(), /● normal +○ yolo +○ super yolo +│$/, "the previous mode, nothing pending");
	assert.match(m.ui.notices.join("\n"), /Cancelled: nothing changed\./);
	await m.close();
});

test("Customize's Permission mode: enter on yolo runs jhon mode yolo once", async (t) => {
	const m = await customizeMode(t);
	m.view.handleInput("\x1b[C");
	m.view.handleInput("\r");
	await m.until(() => /● yolo +○ super yolo +│$/.test(m.row()));
	await m.settle();
	assert.deepEqual(m.sets(), [`mode yolo --root ${m.dir}`]);
	assert.deepEqual(m.asked, [], "yolo asks nothing");
	assert.equal(m.state(), "yolo");
	await m.close();
});

// L195: /jhon:settings is gone with no notice; Customize is /jhon:customize, and
// its argument completes to its categories, each with what it holds.
test("/jhon:settings is not registered; /jhon:customize completes its categories, each with a one-line description", async () => {
	const { pi, commands } = fakePi();
	shellUi(pi, {});
	assert.equal(commands.has("jhon:settings"), false, "no /jhon:settings, not even a notice");
	for (const [name, command] of commands) {
		if (!name.startsWith("jhon")) continue;
		assert.ok((command.description ?? "").length <= 80, `${name}: ${command.description}`);
		assert.doesNotMatch(command.description ?? "", /\/jhon[ \]]/, `${name} names no old /jhon <word> form`);
	}
	const customize = commands.get("jhon:customize")!;
	assert.deepEqual(await customize.getArgumentCompletions!(""), [
		{ value: "appearance", label: "appearance", description: "Theme, animations, glyphs, tool icons, cards, surface, density, status line, reset" },
		{ value: "behaviour", label: "behaviour", description: "Compaction, the fresh-session notice, stall recovery, double Esc" },
		{ value: "keys", label: "keys", description: "The shell's keys: LDD status, agents, jobs, usage, todos; reload" },
		{ value: "permissions", label: "permissions", description: "The project's permission mode" },
	]);
	assert.deepEqual((await customize.getArgumentCompletions!("pe"))!.map((item) => item.value), ["permissions"]);
	assert.equal(await customize.getArgumentCompletions!("no"), null, "no notifications group (T169)");
	assert.equal(await customize.getArgumentCompletions!("zz"), null, "no match: nothing to offer");
});

// T15.9 (L103, B2): /jhon:customize <group> never falls back to Appearance. A
// group that moved says where it lives now; any other word lists the groups.
test("/jhon:customize with a group that moved points where it lives, and an unknown word lists the groups; neither opens Customize", async () => {
	const { pi, commands } = fakePi();
	shellUi(pi, {});
	for (const [word, says] of [
		["runtimes", /runtimes.*jhon setup/i], ["integrations", /integrations.*jhon setup/i], ["palette", /palette.*jhon setup/i],
		["profile", /^The model profile is chosen in \/jhon:profile\.$/], ["colours", /No Customize group "colours".*appearance, behaviour, keys, permissions/],
	] as const) {
		const { ctx, ui } = fakeContext();
		await commands.get("jhon:customize")!.handler(word, ctx);
		assert.equal(ui.overlayView, undefined, `${word}: Settings stays closed`);
		assert.equal(ui.notices.length, 1, word);
		assert.match(ui.notices[0]!, says);
		assert.equal(ui.notices[0]!.split("\n").length, 1, `${word}: one line`);
	}
	const { ctx, ui } = fakeContext();
	const pending = commands.get("jhon:customize")!.handler("Keys", ctx);
	assert.ok(ui.overlayView, "a valid group, any case, opens Settings");
	ui.overlayView!.handleInput("\x1b");
	await pending;
});

// T169 (L1437, L1438): the command palette and the panel sheet are gone:
// no /jhon:commands, /jhon:panel, alt+k or alt+r, whatever the variables
// that used to set those keys say; the other commands and keys stay.
test("the shell registers no command palette and no panel sheet: no /jhon:commands, /jhon:panel, alt+k or alt+r (T169)", () => {
	for (const env of [{}, { JHON_SHELL_RAIL_KEY: "ctrl+alt+r", JHON_SHELL_COMMANDS_KEY: "ctrl+p" }]) {
		const { pi, commands, shortcuts } = fakePi();
		shellUi(pi, env);
		for (const name of ["jhon:commands", "jhon:panel"]) assert.equal(commands.has(name), false, `${name} with ${JSON.stringify(env)}`);
		for (const key of ["alt+k", "alt+r", "ctrl+alt+r", "ctrl+p"]) assert.equal(shortcuts.has(key), false, `${key} with ${JSON.stringify(env)}`);
		assert.ok(commands.has("jhon:customize"));
		// T174: the changes view is gone too; T174-S5b brought the usage view back.
		assert.equal(commands.has("jhon:changes"), false, "jhon:changes");
		assert.equal(shortcuts.has("alt+g"), false, "alt+g");
		assert.ok(commands.has("jhon:usage") && shortcuts.has("alt+u"), "/jhon:usage and alt+u are back");
	}
});

// T169: the rail has no STATS card; the dashboard keeps the session's usage.
test("the rail has no STATS part, whatever the session and its sub-agents spent (T169)", async () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const entries = [{ type: "message", timestamp: "2026-11-14T10:00:00.000Z", message: { role: "assistant", model: "claude-opus-5-5", usage: { input: 100, output: 50, cacheRead: 0, cacheWrite: 0, cost: { total: 0.5 } } } }];
	const { ctx, ui } = fakeContext({ entries });
	await fire(handlers, "session_start", ctx);
	const tui = { terminal: { rows: 40, columns: 180 }, requestRender() {} };
	const footer = (ui.footerFactory as (tui: unknown, theme: ShellBarTheme, data: unknown) => { render(width: number): string[]; dispose(): void })(tui, plainTheme, {
		getGitBranch: () => "main", getExtensionStatuses: () => new Map(), getAvailableProviderCount: () => 1, onBranchChange: () => () => {},
	});
	try {
		pi.events.emit("jhon-shell:tasks", { sessionId: ctx.sessionManager.getSessionId(), tasks: [{ id: "agent-1", kind: "agent", label: "explore: callers", startedAt: 1 }], spend: { runs: 2, tokens: 3000, cost: 0.1 } });
		const parts = paneState(tui as unknown as TUI).parts;
		assert.equal(parts.has("stats"), false, "no STATS part");
		for (const kept of ["ldd", "jobs", "agents"]) assert.ok(parts.has(kept), `${kept} stays: ${[...parts.keys()].join(", ")}`);
		for (const part of parts.values()) assert.doesNotMatch(part.render(48).map(stripAnsi).join("\n"), /STATS/);
	} finally { footer.dispose(); await fire(handlers, "session_shutdown", ctx); }
});

// The verifier's probes of the Permission mode row (L188): a confirm row runs
// `jhon mode` once per Enter, and only for a choice that is still pending.
test("the mode row: super yolo after Yes runs once, and a second Enter with nothing pending runs nothing", async (t) => {
	const m = await customizeMode(t, ["Yes"]);
	m.view.handleInput("\x1b[C");
	m.view.handleInput("\x1b[C");
	await m.settle();
	assert.deepEqual(m.sets(), []);
	m.view.handleInput("\r");
	await m.until(() => m.state() === "super-yolo");
	await m.settle();
	assert.deepEqual(m.asked, ["Turn on SUPER YOLO: destroy will not ask"]);
	assert.deepEqual(m.sets(), [`mode super-yolo --root ${m.dir}`]);
	m.view.handleInput("\r");
	await m.settle();
	assert.deepEqual(m.sets(), [`mode super-yolo --root ${m.dir}`]);
	await m.close();
});

test("the mode row: escape on the super yolo question runs nothing", async (t) => {
	const m = await customizeMode(t, []);
	m.view.handleInput("\x1b[C");
	m.view.handleInput("\x1b[C");
	m.view.handleInput("\r");
	await m.until(() => m.asked.length === 1);
	await m.settle();
	assert.deepEqual(m.sets(), []);
	assert.equal(m.state(), "normal");
	assert.match(m.row(), /● normal +○ yolo +○ super yolo/);
	await m.close();
});

test("the mode row: a super yolo question that throws runs nothing and says so", async (t) => {
	const m = await customizeMode(t, ["THROW"]);
	m.view.handleInput("\x1b[C");
	m.view.handleInput("\x1b[C");
	m.view.handleInput("\r");
	await m.until(() => m.asked.length === 1);
	await m.settle();
	assert.deepEqual(m.sets(), []);
	assert.equal(m.state(), "normal");
	assert.ok(m.ui.notices.includes("Cancelled: nothing changed."), JSON.stringify(m.ui.notices));
	await m.close();
});

test("the mode row: ← from yolo stays pending until Enter runs `mode normal`", async (t) => {
	const m = await customizeMode(t);
	m.view.handleInput("\x1b[C");
	m.view.handleInput("\r");
	await m.until(() => m.state() === "yolo");
	await m.settle();
	m.view.handleInput("\x1b[D");
	await m.settle();
	assert.deepEqual(m.sets(), [`mode yolo --root ${m.dir}`]);
	m.view.handleInput("\r");
	await m.until(() => m.state() === "normal");
	await m.settle();
	assert.deepEqual(m.sets(), [`mode yolo --root ${m.dir}`, `mode normal --root ${m.dir}`]);
	await m.close();
});

test("a mode set in Customize shows on the status line in the same session, no turn needed (L214)", async (t) => {
	const dir = mkdtempSync(join(tmpdir(), "jhon-badge-"));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const bin = join(dir, "jhon");
	writeFileSync(join(dir, "state"), "normal");
	writeFileSync(bin, `#!/bin/sh
case "$1 $2" in
  "mode --json") printf '{"mode":"%s","root":"${dir}","since":null}' "$(cat "${dir}/state")" ;;
  "mode "*) printf '%s' "$2" > "${dir}/state" ;;
esac
`);
	chmodSync(bin, 0o755);
	const { pi, commands, handlers } = fakePi();
	shellUi(pi, { JHON_BIN: bin, JHON_SHELL_CONFIG_HOME: dir, JHON_SHELL_CHANGES_WATCH_MS: "off" }, { lddStatus: async () => undefined, permissionMode: () => readFileSync(join(dir, "state"), "utf8") });
	const { ctx, ui } = fakeContext();
	(ctx as { cwd: string }).cwd = dir;
	const editor = installedPrompt(ctx, ui, handlers);
	try {
		await new Promise((r) => setTimeout(r, 20));
		assert.doesNotMatch(renderFooter(ui), /YOLO/);
		const pending = commands.get("jhon:customize")!.handler("permissions", ctx);
		const view = ui.overlayView!;
		for (let i = 0; i < 200 && !/● normal/.test(view.render(120).join("")); i++) await new Promise((r) => setTimeout(r, 10));
		view.handleInput("\x1b[C");
		view.handleInput("\r");
		for (let i = 0; i < 200 && !/YOLO/.test(renderFooter(ui)); i++) await new Promise((r) => setTimeout(r, 10));
		assert.match(renderFooter(ui), /│ YOLO/, "the badge follows the Customize change");
		view.handleInput("\x1b"); if (ui.overlayView) view.handleInput("\x1b"); await pending;
	} finally { editor.dispose(); }
});
