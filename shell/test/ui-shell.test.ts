import "./isolated-env.ts";
import assert from "node:assert/strict";
import { execFileSync, execFile, spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test, { after } from "node:test";
import { initTheme, type ExtensionAPI, type ExtensionContext, type SlashCommandInfo, type SourceInfo } from "@earendil-works/pi-coding-agent";
import { CURSOR_MARKER, matchesKey, visibleWidth, type TUI, type TuiMouseEvent } from "@earendil-works/pi-tui";
import { registerSubagentWorktree, resolveSessionWorktree } from "../lib/ui/session-worktree-registry.ts";
import { SESSION_PROFILE_CHANGED, SESSION_PROFILE_ENTRY } from "../lib/session-profile.ts";
import installShellUi, { createActiveProfileReader, extractQueuedText, JhonPromptEditor } from "../extensions/ui.ts";
import { barModel, renderTopRow } from "../lib/ui/bars.ts";
import { SANDBOX_EVENT } from "../lib/events.ts";
import { openInExternalEditor } from "../lib/ui/jhon-panel-editor.ts";
import { setSidebarHover, sidebarPart, sidebarState, type SidebarRail } from "../lib/ui/shell-sidebar.ts";
import type { ShellBarTheme } from "../lib/ui/shell-bar.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { CARD_STYLE, cardStyle, setCardStyle } from "../lib/ui/primitives/index.ts";
import { chromeSurface, setChromeSurface } from "../lib/ui/shell-panel.ts";
import { resolveVisualSettings, writeVisualSettings } from "../lib/ui/visual-customization-policy.ts";
import { resolveAnimationPolicy } from "../lib/ui/animation-policy.ts";
import { installShellSettings, type SettingValue } from "../lib/settings.ts";
import { envOverrides, memorySettings } from "./fake-jhon-settings.ts";

// The jhon-shell UI extension wires the pure bar renderer into pi's footer
// slot. These tests drive it with a fake ExtensionAPI and context.

initTheme("dark");

const resolveWorktree = (path: string) => ({ root: path.startsWith("/repo") || path === "." ? "/repo" : path, commonDir: "/clone/git" });
// The extension reads the owner's settings through `jhon settings`
// (lib/settings.ts). Each test's JHON_SHELL_CONFIG_HOME names its own
// in-memory settings (homeSettings), installed for the process when the
// shell is made, with the JHON_SHELL_* variables of its env resolved as the
// engine resolves them; a developer's persisted preferences (for example
// /jhon:animations off) never leak into tests.
const settingsByHome = new Map<string, ReturnType<typeof memorySettings>>();
// The settings `jhon settings` would give the shell under an environment (S38).
const envSettings = (env: NodeJS.ProcessEnv) => memorySettings({}, undefined, envOverrides(env));
function homeSettings(home: string, file: Record<string, SettingValue> = {}): ReturnType<typeof memorySettings> {
	let settings = settingsByHome.get(home);
	if (!settings) settingsByHome.set(home, settings = memorySettings(file));
	return settings;
}
/** What a home's settings.toml holds for one of the old preference files: its policy as that file named it, or undefined when unset. */
const SAVED = {
	animations: ["shell.appearance.animations", (v: SettingValue) => ({ full: "quality", off: "potato" })[v as string]],
	"double-esc-cancel": ["shell.behaviour.double_esc_cancel", (v: SettingValue) => v ? "on" : "off"],
} as const;
function savedPolicy(home: string, kind: keyof typeof SAVED): SettingValue | undefined {
	const [key, read] = SAVED[kind];
	const file = homeSettings(home).jhon.file;
	return key in file ? read(file[key]) : undefined;
}
let freshHomes = 0;
// Each test gets a fresh home unless it names its own.
function isolatedEnv(env: NodeJS.ProcessEnv = process.env): NodeJS.ProcessEnv {
	if (env.JHON_SHELL_CONFIG_HOME === undefined) {
		const home = `fresh-settings-${++freshHomes}`;
		homeSettings(home);
		env = { ...env, JHON_SHELL_CONFIG_HOME: home };
	}
	const settings = homeSettings(env.JHON_SHELL_CONFIG_HOME!);
	settings.jhon.env = envOverrides(env);
	settings.refresh();
	installShellSettings(settings);
	return env;
}
// The default profile reader reads a profiles.json of the suite's own (absent:
// `default`), never the machine's; a test that shows a profile injects it.
const profilesDir = mkdtempSync(join(tmpdir(), "jhon-ui-profiles-"));
after(() => rmSync(profilesDir, { recursive: true, force: true }));
process.env.JHON_PROFILES = join(profilesDir, "profiles.json");
const shellUi: typeof installShellUi = (pi, env, deps) => installShellUi(pi, isolatedEnv(env), { resolveWorktree, gitRunner: (cwd) => async (args) => pi.exec("git", ["-C", cwd, ...args], { timeout: 5000 }), rootExists: () => true, ...deps });

const plainTheme = {
	fg(_color: string, value: string) {
		return value;
	},
	bold(value: string) {
		return value;
	},
	bg(_color: string, value: string) {
		return value;
	},
	getBgAnsi() {
		return "";
	},
} satisfies ShellBarTheme & { bg(color: string, value: string): string; getBgAnsi(): string };

interface FakeUi {
	footerFactory: unknown;
	editorFactory: unknown;
	widgets: Map<string, unknown>;
	widgetSets: number;
	workingVisible: boolean | undefined;
	notices: string[];
	overlay: unknown;
	overlayView: { render(width: number): string[]; handleInput(data: string): void; handleMouse?(event: unknown): unknown } | undefined;
	closeOverlay: (() => void) | undefined;
	/** The options the last overlay opened with. */
	overlayOptions?: unknown;
}

interface GitScript {
	numstat: string;
	porcelain: string;
}

interface CommandRegistration {
	description?: string;
	handler: (args: string, ctx: ExtensionContext) => Promise<void>;
}

interface ShortcutRegistration {
	handler: (ctx: ExtensionContext) => Promise<void>;
}

type MessageRenderer = (message: { customType: string; content: unknown }, options: { expanded: boolean }, theme: unknown) => { render(width: number): string[] };
const renderers = new Map<string, MessageRenderer>();

const FAKE_SOURCE_INFO: SourceInfo = { path: "extensions/ui.ts", source: "jhon-shell", scope: "project", origin: "top-level" };

const DEFAULT_COMMANDS: SlashCommandInfo[] = [
	{ name: "jhon:profile", description: "Configure models", source: "extension", sourceInfo: FAKE_SOURCE_INFO },
	{ name: "jhon:changes", description: "View changes", source: "extension", sourceInfo: FAKE_SOURCE_INFO },
	{ name: "jhon:status", description: "The LDD detail", source: "extension", sourceInfo: FAKE_SOURCE_INFO },
	{ name: "jhon:not-in-catalog", description: "Not a curated command", source: "extension", sourceInfo: FAKE_SOURCE_INFO },
	{ name: "skill:foo", description: "A skill", source: "skill", sourceInfo: FAKE_SOURCE_INFO },
];

function fakePi(script: GitScript[] = [{ numstat: "", porcelain: "" }], commandsList: SlashCommandInfo[] = DEFAULT_COMMANDS) {
	const handlers = new Map<string, Array<(event: unknown, ctx: ExtensionContext) => unknown>>();
	const commands = new Map<string, CommandRegistration>();
	const shortcuts = new Map<string, ShortcutRegistration>();
	const git: string[][] = [];
	let entries: unknown[] = [];
	const sentMessages: Array<{ content: string; options?: { deliverAs?: "steer" | "followUp"; expandPromptTemplates?: boolean } }> = [];
	const tools = new Map<string, { renderShell?: string; execute(id: string, params: unknown, signal: undefined, update: undefined, ctx: ExtensionContext): Promise<unknown> }>();
	const listeners = new Map<string, (data: unknown) => void>();
	let round = 0;
	const pi = {
		on(event: string, handler: (event: unknown, ctx: ExtensionContext) => unknown) {
			handlers.set(event, [...(handlers.get(event) ?? []), (payload, ctx) => {
				entries = ctx.sessionManager.getEntries();
				return handler(payload, ctx);
			}]);
		},
		appendEntry(customType: string, data: unknown) { entries.push({ type: "custom", customType, data }); },
		events: { on(name: string, fn: (data: unknown) => void) { listeners.set(name, fn); return () => listeners.delete(name); }, emit(name: string, data: unknown) { listeners.get(name)?.(data); } },
		registerTool(tool: { name: string }) { tools.set(tool.name, tool as never); },
		registerCommand(name: string, registration: CommandRegistration) {
			commands.set(name, registration);
		},
		registerShortcut(key: string, registration: ShortcutRegistration) {
			shortcuts.set(key, registration);
		},
		registerMessageRenderer(type: string, renderer: MessageRenderer) {
			renderers.set(type, renderer);
		},
		getThinkingLevel() {
			return "medium";
		},
		getCommands() {
			return commandsList;
		},
		sendUserMessage(content: string, options?: { deliverAs?: "steer" | "followUp"; expandPromptTemplates?: boolean }) {
			sentMessages.push({ content, options });
		},
		async exec(_command: string, args: string[]) {
			git.push(args);
			if (args.includes("worktree")) return { stdout: "worktree /repo\0branch refs/heads/main\0\0", stderr: "", code: 0, killed: false };
			// Roots outside the worktree list read as a detached HEAD.
			if (args.includes("symbolic-ref")) return { stdout: "", stderr: "", code: 1, killed: false };
			const isNumstat = args.includes("diff");
			const step = script[Math.min(isNumstat ? round : round++, script.length - 1)];
			return { stdout: isNumstat ? step.numstat : step.porcelain, stderr: "", code: 0, killed: false };
		},
	} as unknown as ExtensionAPI;
	return { pi, handlers, git, commands, shortcuts, tools, sentMessages };
}

async function fire(handlers: Map<string, Array<(event: unknown, ctx: ExtensionContext) => unknown>>, event: string, ctx: ExtensionContext): Promise<void> {
	for (const handler of handlers.get(event) ?? []) await handler({}, ctx);
}

function fakeContext(options: { theme?: typeof plainTheme & { getBgAnsi?(): string }; hasUI?: boolean; entries?: unknown[]; oauth?: boolean; pending?: boolean; idle?: boolean; editorFactory?: unknown; token?: string; select?: (title: string, options: string[]) => Promise<string | undefined> } = {}): { ctx: ExtensionContext; ui: FakeUi; overlayReady: Promise<void> } {
	const ui: FakeUi = { footerFactory: undefined, editorFactory: options.editorFactory, widgets: new Map(), widgetSets: 0, workingVisible: undefined, notices: [], overlay: undefined, overlayView: undefined, closeOverlay: undefined };
	let resolveOverlay: () => void;
	const overlayReady = new Promise<void>((resolve) => { resolveOverlay = resolve; });
	const entries = options.entries ?? [];
	const ctx = {
		hasUI: options.hasUI ?? true,
		mode: "tui",
		hasPendingMessages: () => options.pending ?? false,
		isIdle: () => options.idle ?? true,
		cwd: "/repo",
		model: { id: "claude-opus-5-5", provider: "anthropic", reasoning: true, contextWindow: 272_000 },
		sessionManager: {
			getCwd: () => "/repo",
			getSessionName: () => "Release notes",
			getEntries: () => entries,
			getSessionId: () => "shell-session",
		},
		modelRegistry: { isUsingOAuth: () => options.oauth ?? true, getApiKeyForProvider: async () => options.token },
		getContextUsage: () => ({ tokens: 122_400, contextWindow: 272_000, percent: 45 }),
		ui: {
			theme: options.theme ?? plainTheme,
			getAllThemes: () => [{ name: "dark", path: undefined }, { name: "light", path: undefined }],
			getTheme: (name: string) => name === "dark" || name === "light" ? { name } : undefined,
			setTheme: (name: string) => ({ success: name === "dark" || name === "light" }),
			// Added only when requested: an absent select keeps the no-menu fallback
			// that every pre-existing test relies on.
			...(options.select ? { select: options.select } : {}),
			setFooter(factory: unknown) {
				ui.footerFactory = factory;
			},
			setEditorComponent(factory: unknown) {
				ui.editorFactory = factory;
			},
			getEditorComponent() {
				return ui.editorFactory;
			},
			setWidget(key: string, content: unknown) {
				ui.widgetSets += 1;
				if (content === undefined) ui.widgets.delete(key);
				else ui.widgets.set(key, content);
			},
			notify(message: string) {
				ui.notices.push(message);
			},
			setWorkingVisible(visible: boolean) {
				ui.workingVisible = visible;
			},
			// Generic over the result type: real callers resolve `custom` with
			// whatever their `done` callback is given (see ExtensionUIContext.custom
			// in pi-coding-agent), not always null. `closeOverlay` stays a
			// null-resolving escape hatch for tests that only need to end the wait.
			custom<T>(factory: (tui: unknown, theme: unknown, keybindings: unknown, done: (value: T) => void) => { render(width: number): string[]; handleInput(data: string): void }, options?: unknown) {
				ui.overlay = factory;
				ui.overlayOptions = options;
				return new Promise<T | null>((resolve) => {
					ui.closeOverlay = () => resolve(null);
					// An identity bg mock hides focused rows; omit it so overlay navigation
					// remains inspectable through Commands' ▸ fallback.
					ui.overlayView = factory(fakeTui, { ...plainTheme, bg: undefined }, fakeKeybindings, (value: T) => resolve(value));
					resolveOverlay();
				});
			},
		},
	} as unknown as ExtensionContext;
	return { ctx, ui, overlayReady };
}

function assistantEntry(usage: { input: number; output: number; cost: number }) {
	return {
		type: "message",
		message: {
			role: "assistant",
			usage: { input: usage.input, output: usage.output, cacheRead: 0, cacheWrite: 0, cost: { total: usage.cost } },
		},
	};
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
	assert.equal("statuses" in built, false, "T174: the extension statuses (the integrations) are gone");
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

test("shellUi installs the footer on session_start when a UI exists", () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	for (const handler of handlers.get("session_start") ?? []) handler({}, ctx);
	assert.equal(typeof ui.footerFactory, "function");

	const factory = ui.footerFactory as (tui: unknown, theme: ShellBarTheme, footerData: unknown) => { render(width: number): string[] };
	const component = factory(
		{ requestRender() {} },
		plainTheme,
		{ getGitBranch: () => "main", getExtensionStatuses: () => new Map(), getAvailableProviderCount: () => 1, onBranchChange: () => () => {} },
	);
	const lines = component.render(120);
	assert.equal(lines.length, 1);
	assert.match(lines[0], /^ ⎇ main +claude-opus-5-5 · medium │ ctx 45% │ \$0\.000 sub $/, "a regular terminal: the status bar alone, its narrow form under 140 columns");
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
		const rail = sidebarState(tui as unknown as TUI).parts.get("ldd") as SidebarRail;
		assert.equal(typeof rail.digest, "function", "the LDD card paints live state and must declare a digest");
		assert.doesNotMatch(rail.render(46).join("\n"), /claude-opus-5-5/, "the model is the status bar's, not the Status card's");

		// The profile is the status bar's (beside the model): STATUS and the top row never show it.
		const header = sidebarState(tui as unknown as TUI).parts.get("header") as SidebarRail;
		const bar = sidebarState(tui as unknown as TUI).parts.get("statusbar") as SidebarRail;
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
		const header = sidebarState(tui as unknown as TUI).parts.get("statusbar") as SidebarRail;
		assert.equal(typeof header.digest, "function", "the status line paints live state every frame and must declare a digest");
		const live = () => header.digest?.();
		const text = () => header.render(160).join("\n");
		assert.match(text(), /claude-opus-5-5/);
		assert.doesNotMatch(text(), /MCP: 3 servers/, "extension statuses never reach the status line");
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

test("shellUi stays out of the way without a UI or when disabled", () => {
	const disabled = fakePi();
	shellUi(disabled.pi, { JHON_SHELL: "0" });
	assert.equal(disabled.commands.size, 0);
	assert.ok(disabled.handlers.has("tool_call"), "capture remains available to headless children");

	const headless = fakePi();
	shellUi(headless.pi, {});
	const { ctx, ui } = fakeContext({ hasUI: false });
	for (const handler of headless.handlers.get("session_start") ?? []) handler({}, ctx);
	assert.equal(ui.footerFactory, undefined);
});

const fakeTui = { terminal: { rows: 40, columns: 120 }, requestRender() {} };
// The shell runs fullscreen: the installed prompt is G's panel editor (T169: its rule carries no [ Panel ]).
const promptTui = Object.create(fakeTui, { mode: { value: "fullscreen" } }) as typeof fakeTui & { mode: string };
const editorTheme = { borderColor: (text: string) => text, selectList: {} };
const fakeKeybindings = { matches: () => false };

function installedPrompt(
	ctx: ExtensionContext,
	ui: FakeUi,
	handlers: Map<string, Array<(event: unknown, ctx: ExtensionContext) => unknown>>,
	keybindings: unknown = fakeKeybindings,
): JhonPromptEditor {
	for (const handler of handlers.get("session_start") ?? []) handler({}, ctx);
	const factory = ui.editorFactory as (tui: unknown, theme: unknown, keybindings: unknown) => JhonPromptEditor;
	return factory(promptTui, editorTheme, keybindings);
}

interface MutablePromptThemeHost {
	theme: ExtensionContext["ui"]["theme"];
}

test("the installed prompt says the Esc hints at its rule's right end and queued in its rule; its last row stays blank (T15.12d)", () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_DOUBLE_ESC_CANCEL: "on" });
	const { ctx, ui } = fakeContext({ pending: true });
	const editor = installedPrompt(ctx, ui, handlers, { matches: (data: string, action: string) => action === "app.interrupt" && data === "\x1b" });
	try {
		editor.focused = true;
		assert.doesNotMatch(editor.render(80).join("\n"), /\x1b\[4\dm/, "the prompt paints no background");
		editor.setText("draft");
		editor.handleInput("\x1b");
		assert.match(stripAnsi(editor.render(80)[0]!), /^─ 力 ready for you ─+ esc again clears it ─$/);
		assert.equal(editor.render(80).at(-1), " ".repeat(80), "no hint row: G's blank last row");
		for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
		assert.match(stripAnsi(editor.render(80)[0]!), /^─ 力 queued ─+$/);
		editor.handleInput("\x1b");
		assert.match(stripAnsi(editor.render(80)[0]!), /^─ 力 queued ─+ esc again stops it ─$/);
	} finally { editor.dispose(); }
});

// The prompt's petal frame says «working…»; pi's own loader row would say it twice,
// whatever the environment names (no multiplexer keeps it, T24b).
test("shellUi hides Pi's native Working row whatever multiplexer it runs in", () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, { PI_CODING_AGENT_DIR: "/nonexistent-agent-dir" });
	const { ctx, ui } = fakeContext();
	installedPrompt(ctx, ui, handlers);
	assert.equal(ui.workingVisible, false);
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

test("shellUi takes over the prompt: pi's Working row hidden, the petal in its rule, a hint while empty", () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers);
	assert.equal(ui.workingVisible, false, "pi's own Working row must be hidden");
	editor.focused = true;
	const lines = editor.render(60).map(stripAnsi);
	assert.match(lines[0], /^─ 力 ready for you ─+$/);
	assert.doesNotMatch(editor.render(60).join("\n"), /\x1b\[44m/, "prompt must not paint passive backgrounds");
	assert.match(lines[1], /^› .*write, or \/ for a command/);
	editor.setText("hola");
	assert.doesNotMatch(editor.render(60).map(stripAnsi)[1], /write, or/);
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
		assert.match(lines[0], /^─ 力 ready for you ─+$/);
		assert.match(lines[1], /^› .*write, or \/ for a command/);
		assert.ok(lines.every((line) => visibleWidth(line) === 60), lines.join("\n"));
		editor.setText("hola");
		assert.match(editor.render(60).map(stripAnsi)[1], /^› hola +$/);
		const footer = renderFooter(ui);
		assert.match(footer, /^ ⎇ main +claude-opus-5-5 · medium │ ctx ▰▰▰▰▱▱▱▱ 45% │ \$0\.000 sub $/, "the cost closes the bar");
		assert.equal(handlers.has("after_provider_response"), false, "T174: no usage read from the replies");
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

test("without the rail the status bar is exactly one row: no extension status, no jobs, never a second line (T15.12d, T174)", async (t) => {
	const home = mkdtempSync(join(tmpdir(), "jhon-sections-statuses-"));
	t.after(() => rmSync(home, { recursive: true, force: true }));
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home, JHON_SHELL_CHANGES_WATCH_MS: "off" });
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	const factory = ui.footerFactory as (tui: unknown, theme: ShellBarTheme, data: unknown) => { render(width: number): string[] };
	const narrowTui = { mode: "fullscreen", terminal: { rows: 40, columns: 120 }, requestRender() {} };
	// As G's, the narrow status row is the statusbar part's, a sibling row under
	// the native layout; the footer itself paints nothing (T15.12g).
	const footer = factory(narrowTui, plainTheme, { ...footerData, getExtensionStatuses: () => new Map([["mcp", "MCP: 2 servers"]]) });
	const statusRow = () => sidebarState(narrowTui as unknown as TUI).parts.get("statusbar")!.render(120);
	assert.deepEqual(footer.render(120), [], "the footer steps aside for the statusbar row");
	const rows = statusRow();
	assert.equal(rows.length, 1, rows.join("\n"));
	assert.doesNotMatch(rows.join("\n"), /MCP/, "the integrations are gone (T174)");
	pi.events.emit("jhon-shell:tasks", { sessionId: ctx.sessionManager.getSessionId(), tasks: [{ id: "job-1", label: "build", startedAt: 1 }] });
	const busy = statusRow();
	assert.equal(busy.length, 1);
	assert.equal(busy[0], rows[0], "a running job is the right pane's, not the bar's (T174-S3)");
	assert.match(rows[0]!, /^ ⎇ main +claude-opus-5-5 · medium │ ctx 45% │ \$0\.000 sub $/, "120 columns: the narrow form");
});

test("a regular (not fullscreen) terminal's footer is the status bar alone: branch and sandbox, model, ctx and cost; no session name, jobs or integrations (T174-S3)", async (t) => {
	const home = mkdtempSync(join(tmpdir(), "jhon-regular-bar-"));
	t.after(() => rmSync(home, { recursive: true, force: true }));
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home, JHON_SHELL_CHANGES_WATCH_MS: "off" });
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	const factory = ui.footerFactory as (tui: unknown, theme: ShellBarTheme, data: unknown) => { render(width: number): string[] };
	const rows = factory(fakeTui, plainTheme, { ...footerData, getExtensionStatuses: () => new Map([["mcp", "MCP: 2 servers"]]) }).render(160);
	assert.deepEqual(rows, [" ⎇ main" + " ".repeat(96) + "claude-opus-5-5 · medium │ ctx ▰▰▰▰▱▱▱▱ 45% │ $0.000 sub "]);
	pi.events.emit("jhon-shell:tasks", { sessionId: ctx.sessionManager.getSessionId(), tasks: [{ id: "job-1", label: "build", startedAt: 1 }] });
	assert.doesNotMatch(factory(fakeTui, plainTheme, footerData).render(160)[0]!, /⧗|job|Release notes/);
	pi.events.emit(SANDBOX_EVENT, { mode: "on" });
	assert.match(factory(fakeTui, plainTheme, footerData).render(160)[0]!, /^ ⎇ main │ sandbox on +claude/, "the session's sandbox pin");
	pi.events.emit(SANDBOX_EVENT, { mode: "off" });
	assert.match(factory(fakeTui, plainTheme, footerData).render(160)[0]!, /^ ⎇ main │ sandbox off +claude/);
	pi.events.emit(SANDBOX_EVENT, { mode: undefined });
	assert.match(factory(fakeTui, plainTheme, footerData).render(160)[0]!, /^ ⎇ main +claude/, "unread: no segment");
});

// Design 01 and 04 (T174-S3): the top row and the status bar are one row
// each at every width and density; under 140 columns the status bar takes
// its narrow form and the footer steps aside for it.
test("the chrome: the top row and the status bar one row each; under 140 columns the narrow form", async () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {}, { activeProfile: () => "jhon" });
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	const tui = { mode: "fullscreen", terminal: { rows: 40, columns: 160 }, requestRender() {} };
	const footer = (ui.footerFactory as (tui: unknown, theme: unknown, data: unknown) => { render(width: number): string[]; dispose(): void })(tui, plainTheme, footerData);
	try {
		const parts = sidebarState(tui as unknown as TUI).parts;
		assert.deepEqual(parts.get("header")!.render(160).map(stripAnsi), [" 力 repo  /repo".padEnd(159)], "the project and its path, nothing else");
		assert.deepEqual(parts.get("statusbar")!.render(160).map(stripAnsi), [" ⎇ main" + " ".repeat(89) + "claude-opus-5-5 · medium · jhon │ ctx ▰▰▰▰▱▱▱▱ 45% │ $0.000 sub "]);
		tui.terminal.columns = 100;
		assert.deepEqual(parts.get("header")!.render(100).map(stripAnsi), [" 力 repo  /repo".padEnd(99)]);
		assert.deepEqual(footer.render(100), [], "the footer steps aside for the statusbar row");
		assert.deepEqual(parts.get("statusbar")!.render(100).map(stripAnsi), [" ⎇ main" + " ".repeat(38) + "claude-opus-5-5 · medium · jhon │ ctx 45% │ $0.000 sub "], "the narrow form: no meter");
	} finally { footer.dispose(); await fire(handlers, "session_shutdown", ctx); }
});

// T15.12c slice 4: a fullscreen terminal without the rail (under 140
// columns, or Status hidden) draws the Gentle Shell's narrow editor, with the
// work row above its rule (` ▸ <lead> … <count>`) in place of the AGENTS
// card (T174: the todo list is the right pane's alone). T169 (L1438): the panel sheet is gone, and the rule's
// [ Panel ] button with it.
function narrowShell(columns = 100) {
	const { pi, handlers, shortcuts } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	const tui = { mode: "fullscreen", terminal: { rows: 40, columns }, requestRender() {} };
	for (const handler of handlers.get("session_start") ?? []) handler({}, ctx);
	const footer = (ui.footerFactory as (tui: unknown, theme: unknown, data: unknown) => { render(width: number): string[]; dispose(): void })(tui, plainTheme, footerData);
	const editor = (ui.editorFactory as (tui: unknown, theme: unknown, keybindings: unknown) => JhonPromptEditor)(tui, editorTheme, fakeKeybindings);
	return { pi, handlers, shortcuts, ctx, ui, tui, footer, editor, state: sidebarState(tui as unknown as TUI) };
}

test("without the rail the editor rule carries no [ Panel ], and a click at its right end opens nothing (T169)", async () => {
	const { ctx, ui, footer, editor, handlers } = narrowShell();
	try {
		const lines = editor.render(100).map(stripAnsi);
		assert.match(lines[0]!, /^─ 力 ready for you ─+$/);
		editor.handleMouse({ type: "click", button: "left", x: 93, y: 0, width: 100, height: lines.length } as never);
		assert.equal(ui.overlayView, undefined, "no sheet");
	} finally { editor.dispose(); footer.dispose(); await fire(handlers, "session_shutdown", ctx); }
});

test("without the rail running sub-agents fold into the work row above the editor rule, the todo list never (it is the right pane's); a tap opens /jhon:agents (T15.12c, T15.12d, T169, T174)", async () => {
	const { pi, ctx, ui, footer, editor, state, handlers } = narrowShell();
	const opened: unknown[] = [];
	pi.events.on("jhon-shell:open-tasks", (payload: unknown) => opened.push(payload));
	try {
		assert.equal(editor.render(100).map(stripAnsi).filter((line) => /▸/.test(line)).length, 0, "no live work, no work row");
		pi.events.emit("jhon-shell:tasks", { sessionId: ctx.sessionManager.getSessionId(), tasks: [{ id: "agent-1", kind: "agent", label: "explore: callers", startedAt: 1 }] });
		state.parts.set("todo", { render: () => [], invalidate() {}, summary: () => ({ left: "◐ write the sheet", right: "1/3", rightLabel: "todos" }) } as never);
		const lines = editor.render(100).map(stripAnsi);
		assert.match(lines[0]!, /^ ▸ ◐ 1 agent +$/, "agents alone: no todo count");
		assert.match(lines[1]!, /^─ 力 ready for you ─+$/, "the rule under it, no [ Panel ] (T169)");
		assert.equal(visibleWidth(lines[0]!), 100);
		assert.equal(state.compactWork?.(), true, "the AGENTS card steps aside");
		assert.deepEqual(editor.handleMouse({ type: "click", button: "left", x: 3, y: 0, width: 100, height: lines.length } as never), { handled: true, render: true });
		assert.equal(opened.length, 1, "agents lead: the tap opens /jhon:agents, as G's opens its agents view");
		assert.equal(ui.overlayView, undefined, "not the sheet");
		pi.events.emit("jhon-shell:tasks", { sessionId: ctx.sessionManager.getSessionId(), tasks: [] });
		assert.equal(editor.render(100).map(stripAnsi).filter((line) => /▸/.test(line)).length, 0, "no agents: no work row, the todo list never leads it");
		assert.equal(opened.length, 1);
		state.active = true;
		state.ownsHost = () => true;
		assert.doesNotMatch(editor.render(100).map(stripAnsi).join("\n"), /▸ ◐/, "beside the rail the work is the rail's");
		assert.equal(state.compactWork?.(), false);
	} finally { editor.dispose(); footer.dispose(); await fire(handlers, "session_shutdown", ctx); }
});

test("a regular (not fullscreen) terminal draws G's card prompt, neon without a theme background, and no work row nor [ Panel ] (T15.12e)", () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	for (const handler of handlers.get("session_start") ?? []) handler({}, ctx);
	const factory = ui.editorFactory as (tui: unknown, theme: unknown, keybindings: unknown) => JhonPromptEditor;
	const editor = factory(fakeTui, editorTheme, fakeKeybindings);
	try {
		const rows = editor.render(100).map(stripAnsi);
		assert.match(rows[0]!, /^╭─ 力 ─+╮$/);
		assert.match(rows[1]!, /^│.*write, or \/ for a command.*│$/);
		assert.match(rows.at(-1)!, /^╰─+╯$/);
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
		assert.match(editor.render(60).map(stripAnsi)[0], /^─ .+ working… ─+$/);
	} finally {
		editor.dispose();
	}
});

// in ordinary editing another extension's alt+a shortcut
// must win over prompt select-all, for empty and non-empty drafts alike.
test("JhonPromptEditor gives extension shortcuts precedence over selection chords", () => {
	const { pi, handlers } = fakePi(); shellUi(pi, {});
	const { ctx, ui } = fakeContext(); const editor = installedPrompt(ctx, ui, handlers);
	try {
		const seen: string[] = [];
		editor.onExtensionShortcut = (data) => { seen.push(data); return matchesKey(data, "alt+a"); };
		for (const altA of ["\x1ba", "\x1b[97;3u", "\x1b[97;3:1u"]) {
			seen.length = 0;
			editor.setText("");
			editor.handleInput(altA);
			assert.deepEqual(seen, [altA], "empty draft dispatches the shortcut");
			seen.length = 0;
			editor.setText("abc\ndef");
			editor.handleInput(altA);
			assert.deepEqual(seen, [altA], "non-empty draft dispatches the shortcut");
			assert.doesNotMatch(editor.render(40).join("\n"), /\x1b\[7mabc/);
			editor.handleInput("\x7f");
			assert.equal(editor.getText(), "abc\nde", "the draft was not selected");
		}
		// alt+e is the dedicated select-all chord while alt+a belongs to Agents.
		editor.setText("abc\ndef");
		editor.handleInput("\x1be");
		assert.match(editor.render(40).join("\n"), /\x1b\[7mabc/);
		editor.handleInput("\x7f");
		assert.equal(editor.getText(), "");
	} finally { editor.dispose(); }
});

test("JhonPromptEditor keeps alt+a select-all when no extension claims it", () => {
	const { pi, handlers } = fakePi(); shellUi(pi, {});
	const { ctx, ui } = fakeContext(); const editor = installedPrompt(ctx, ui, handlers);
	try {
		editor.onExtensionShortcut = () => false;
		editor.setText("abc\ndef");
		editor.handleInput("\x1ba");
		assert.match(editor.render(40).join("\n"), /\x1b\[7mabc/);
		editor.handleInput("\x7f");
		assert.equal(editor.getText(), "");
	} finally { editor.dispose(); }
});

test("an esc again hint leaves the screen when its window closes, with no key pressed", () => {
	let now = 0, renders = 0;
	const timers: Array<{ fn: () => void; ms: number }> = [];
	const editor = new JhonPromptEditor(fakeTui as never, editorTheme as never, { matches: (data: string, action: string) => action === "app.interrupt" && data === "\x1b" } as never, {
		fg: (_color, text) => text, bold: (text) => text, requestRender() { renders++; },
		pending: () => false, now: () => now, doubleEscCancelEnabled: () => false,
		dispatchQueuedText() {}, schedule: (fn, ms) => { timers.push({ fn, ms }); return () => {}; },
	});
	try {
		editor.focused = true;
		editor.setText("draft");
		editor.handleInput("\x1b");
		assert.match(stripAnsi(editor.render(80).join("\n")), /esc again clears it/);
		assert.equal(timers.length, 1);
		now = timers[0]!.ms;
		const before = renders;
		timers[0]!.fn();
		assert.equal(renders, before + 1, "the window's end repaints");
		assert.doesNotMatch(stripAnsi(editor.render(80).join("\n")), /esc again/);
		assert.equal(editor.getText(), "draft");
	} finally { editor.dispose(); }
});

test("JhonPromptEditor autocomplete respects narrow frame widths", () => {
 const { pi, handlers } = fakePi();
 shellUi(pi, {});
 const { ctx, ui } = fakeContext();
 const editor = installedPrompt(ctx, ui, handlers);
 const internal = editor as unknown as { autocompleteState: string; autocompleteList: { render(width: number): string[] } };
 internal.autocompleteState = "force";
 internal.autocompleteList = { render: () => ["completion"] };
 try {
  for (const width of [0, 1, 2, 30]) {
   const rows = editor.render(width).map(stripAnsi);
   assert.ok(rows.every((row) => [...row].length <= width), `width ${width}: ${JSON.stringify(rows)}`);
   if (width === 30) assert.ok(rows.some((row) => row.includes("completion")));
  }
 } finally { editor.dispose(); }
});

test("registered prompt stays transparent while idle, working, and queued", () => {
	const { pi, handlers, tools } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext({ theme: { ...plainTheme, getBgAnsi: () => "\x1b[44m" } });
	const editor = installedPrompt(ctx, ui, handlers);
	try {
		for (const state of ["idle", "working", "queued"]) {
			if (state === "working") for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
			(ctx as unknown as { hasPendingMessages(): boolean }).hasPendingMessages = () => state === "queued";
			for (const width of [8, 40, 80]) assert.doesNotMatch(editor.render(width).join("\n"), /\x1b\[44m/, state);
		}
	} finally {
		editor.dispose();
	}
	assert.equal(tools.get("session_worktree_register")?.renderShell, "self");
});

test("shellUi shows working while the agent runs and queued when messages wait", () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const pending = { value: false };
	const { ctx, ui } = fakeContext();
	(ctx as unknown as { hasPendingMessages: () => boolean }).hasPendingMessages = () => pending.value;
	const editor = installedPrompt(ctx, ui, handlers);

	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	assert.match(stripAnsi(editor.render(60)[0]), /^─ 力 working… ─+$/);
	pending.value = true;
	assert.match(stripAnsi(editor.render(60)[0]), /^─ 力 queued ─+$/);
	for (const handler of handlers.get("agent_end") ?? []) handler({}, ctx);
	assert.match(stripAnsi(editor.render(60)[0]), /queued/, "low-level run end is not settled");
	pending.value = false;
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.match(stripAnsi(editor.render(60)[0]), /^─ 力 ready for you ─+$/);
	editor.dispose();
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
		assert.match(top(), /^─ 力 working… ─+$/);
		now += 1;
		assert.match(top(), /^─ 力 waiting for Claude API · 60s ─+$/);
		now += 12_400;
		assert.match(top(), /^─ 力 waiting for Claude API · 72s ─+$/);
		// The next stream event clears it, and the wait counts again from there.
		on("message_start");
		assert.match(top(), /^─ 力 working… ─+$/);
		now += 60_000;
		assert.match(top(), /waiting for Claude API · 60s/);
		on("message_update");
		assert.match(top(), /^─ 力 working… ─+$/);
		// A running tool, however long, is not a wait on the API.
		on("tool_execution_start");
		now += 300_000;
		assert.match(top(), /^─ 力 working… ─+$/);
		on("tool_execution_end");
		now += 59_999;
		assert.match(top(), /working…/);
		now += 1;
		assert.match(top(), /waiting for Claude API · 60s/);
		// The turn's end clears it, and an idle prompt never shows it.
		on("agent_end");
		assert.doesNotMatch(top(), /waiting/);
		on("agent_settled");
		now += 600_000;
		assert.match(top(), /^─ 力 ready for you ─+$/);
		// The next turn starts its own count.
		on("agent_start");
		assert.match(top(), /^─ 力 working… ─+$/);
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

test("visual customization registers once and remains discoverable", async (t) => {
	const home = scopedDoubleEscCancelConfigHome(t);
	const { pi, commands } = fakePi();
	const registrations: string[] = [];
	const register = pi.registerCommand.bind(pi);
	pi.registerCommand = ((name, registration) => {
		registrations.push(name);
		register(name, registration);
	}) as typeof pi.registerCommand;
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home });
	assert.equal(registrations.filter((registered) => registered === "jhon:customize").length, 1, "jhon:customize must register once");
	for (const merged of ["jhon:vim", "jhon:animations", "jhon:double-esc-cancel"]) assert.equal(commands.has(merged), false, `${merged} merged into /jhon:customize`);
	// T15.13: Customize is G's Customization view; typing does not filter it. L174: Settings is Customize.
	assert.equal(commands.get("jhon:customize")!.description, "Customize the shell: appearance, behaviour, keys, permissions");
	const { ctx, ui } = fakeContext();
	const shown = await setViaSettings(commands, ctx, ui, "Animations", "off");
	assert.ok(shown.some((row) => /Animations.*off/.test(row)), shown.join("\n"));
	assert.equal(savedPolicy(home, "animations"), "potato");
});

test("incomplete bracketed paste buffered up to exactly the 16 MiB bound still completes with one-step undo", () => {
	const BOUND = 16 * 1024 * 1024;
	const deps = {
		fg: (_color: string, text: string) => text, bold: (text: string) => text, requestRender() {}, pending: () => false,
		now: () => 0, doubleEscCancelEnabled: () => false, dispatchQueuedText() {},
	};
	const editor = new JhonPromptEditor(fakeTui as never, editorTheme as never, fakeKeybindings as never, deps);
	try {
		editor.setText("hello");
		editor.handleInput("\x1b[1;2H");
		editor.handleInput("\x1b[200~");
		const payload = "z".repeat(BOUND - "\x1b[200~".length);
		editor.handleInput(payload);
		editor.handleInput("\x1b[201~");
		assert.equal(editor.getExpandedText(), payload, "a frame buffered up to exactly the bound still completes");
		(editor as unknown as { undo(): void }).undo();
		assert.equal(editor.getText(), "hello", "one undo restores the selection's original text");
	} finally { editor.dispose(); }
});

test("a complete bracketed paste larger than the 16 MiB incomplete-frame bound, delivered as one chunk, is never capped", () => {
	const BOUND = 16 * 1024 * 1024;
	const deps = {
		fg: (_color: string, text: string) => text, bold: (text: string) => text, requestRender() {}, pending: () => false,
		now: () => 0, doubleEscCancelEnabled: () => false, dispatchQueuedText() {},
	};
	const editor = new JhonPromptEditor(fakeTui as never, editorTheme as never, fakeKeybindings as never, deps);
	try {
		editor.setText("hello");
		editor.handleInput("\x1b[1;2H");
		const payload = "z".repeat(BOUND + 1024);
		editor.handleInput(`\x1b[200~${payload}\x1b[201~`);
		assert.equal(editor.getExpandedText(), payload, "a complete frame larger than the incomplete-frame bound is never capped");
		(editor as unknown as { undo(): void }).undo();
		assert.equal(editor.getText(), "hello");
	} finally { editor.dispose(); }
});

test("an incomplete bracketed paste that crosses the 16 MiB bound without a terminator is abandoned, never forwarded to native", () => {
	const BOUND = 16 * 1024 * 1024;
	const deps = {
		fg: (_color: string, text: string) => text, bold: (text: string) => text, requestRender() {}, pending: () => false,
		now: () => 0, doubleEscCancelEnabled: () => false, dispatchQueuedText() {},
	};
	const editor = new JhonPromptEditor(fakeTui as never, editorTheme as never, fakeKeybindings as never, deps);
	try {
		editor.setText("hello");
		editor.handleInput("\x1b[1;2H");
		editor.handleInput("\x1b[200~");
		const payload = "z".repeat(BOUND - "\x1b[200~".length + 1);
		editor.handleInput(payload);
		assert.equal(editor.getText(), "hello", "no partial paste bytes are ever forwarded to the native editor");
		editor.handleInput("x");
		assert.equal(editor.getText(), "x", "input after abandonment behaves like a fresh keystroke over the still-active selection");
	} finally { editor.dispose(); }
});

// T29 S3: the prompt has no vim mode. An owner file that still sets
// shell.editor.vim = true changes nothing: Esc then i types the i, and the
// rule names no mode.
test("the prompt has no vim mode: Esc then i types i even with shell.editor.vim = true", async (t) => {
	const home = mkdtempSync(join(tmpdir(), "jhon-no-vim-"));
	t.after(() => rmSync(home, { recursive: true, force: true }));
	homeSettings(home, { "shell.editor.vim": true } as Record<string, SettingValue>);
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers);
	try {
		editor.setText("draft");
		editor.handleInput("\x1b");
		editor.handleInput("i");
		assert.equal(editor.getText(), "drafti");
		assert.doesNotMatch(editor.render(60).join("\n"), /NORMAL|INSERT|VISUAL/);
	} finally { editor.dispose(); }
});

test("the Animations setting switches the live pulse", async (t) => {
	const configHome = scopedDoubleEscCancelConfigHome(t);
	const delays: number[] = [];
	let active = 0;
	t.mock.method(globalThis, "setInterval", (_callback: () => void, delay: number) => {
		delays.push(delay); active++; return { unref() {} };
	});
	t.mock.method(globalThis, "clearInterval", () => { active--; });
	const { pi, handlers, commands } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: configHome });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers);
	assert.equal(commands.has("jhon:animations"), false, "merged into /jhon:customize");
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	assert.deepEqual(delays, [2000, 60_000, 80], "the profile poll, STATUS's minute re-read, and the live pulse");
	assert.equal(active, 3);
	await setViaSettings(commands, ctx, ui, "Animations", "off");
	assert.equal(active, 2, "the pulse stops; the polls stay");
	assert.match(stripAnsi(editor.render(60)[0]), /working/);
	assert.equal(savedPolicy(configHome, "animations"), "potato");
	await setViaSettings(commands, ctx, ui, "Animations", "full");
	assert.deepEqual(delays, [2000, 60_000, 80, 80]);
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.equal(active, 2);
	editor.dispose();
	await fire(handlers, "session_shutdown", ctx);
	assert.equal(active, 0);
});

test("potato repaints start/settle and shows queued state on the host's next render without intervals", async (t) => {
	const configHome = scopedDoubleEscCancelConfigHome(t);
	homeSettings(configHome, { "shell.appearance.animations": "off" });
	const intervals = t.mock.method(globalThis, "setInterval", (_callback: () => void, delay: number) => {
		assert.ok(delay === 2000 || delay === 60_000, "potato must not animate: only the profile poll and STATUS's minute re-read tick");
		return { unref() {} };
	});
	const renders = t.mock.method(fakeTui, "requestRender", () => {});
	const { pi, handlers, commands } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: configHome });
	const { ctx, ui } = fakeContext();
	const pending = { value: false };
	(ctx as unknown as { hasPendingMessages(): boolean }).hasPendingMessages = () => pending.value;
	const editor = installedPrompt(ctx, ui, handlers);
	let before = renders.mock.callCount();
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	assert.ok(renders.mock.callCount() > before);
	assert.match(stripAnsi(editor.render(60)[0]), /力 working/);
	before = renders.mock.callCount();
	pending.value = true;
	assert.equal(renders.mock.callCount(), before, "changing the queue flag is not a shell repaint event");
	// Pi owns enqueue and its repaint scheduling; this fake context exposes only
	// hasPendingMessages, not Pi's enqueue path. Simulate the host's render request:
	// this proves next-render visibility, not real Pi enqueue-to-paint latency.
	fakeTui.requestRender();
	assert.equal(renders.mock.callCount(), before + 1);
	assert.match(stripAnsi(editor.render(60)[0]), /力 queued/);
	before = renders.mock.callCount();
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.ok(renders.mock.callCount() > before);
	assert.doesNotMatch(stripAnsi(editor.render(60)[0]), /working|queued/);
	for (const handler of handlers.get("session_shutdown") ?? []) handler({}, ctx);
	assert.equal(intervals.mock.callCount(), 2, "the profile poll and STATUS's minute re-read; no animation");
});

test("the Animations setting reports a failed write and changes nothing", async (t) => {
	const configHome = scopedDoubleEscCancelConfigHome(t);
	const jhon = homeSettings(configHome).jhon;
	const { pi, commands } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: configHome });
	const { ctx, ui } = fakeContext();
	jhon.failSet = "EPERM: settings.toml is not writable";
	const shown = await setViaSettings(commands, ctx, ui, "Animations", "off");
	assert.match(ui.notices.join("\n"), /^Customize: .*EPERM/m, "a refused value is a notice, as G's");
	assert.doesNotMatch(shown.join("\n"), /EPERM/, "not the Preview's headline");
	assert.deepEqual(jhon.file, {});
});

// ---------------------------------------------------------------------------
// double-esc-cancel: opt-in, off by default. While the prompt
// is working and autocomplete is hidden, the first Esc is swallowed and the
// frame shows a hint; a second Esc within the window falls through to
// CustomEditor's own handleInput so Pi's onEscape performs the abort exactly
// as it always has. Idle double-Esc (tree/fork), bash-mode Esc, and
// autocomplete cancel live entirely in Pi's own onEscape/CustomEditor and are
// untouched by this gate.
// ---------------------------------------------------------------------------

const escapeKeybindings = { matches: (_data: string, keybinding: string) => keybinding === "app.interrupt" };

test("double-esc-cancel default off: a single Esc while working still aborts immediately, exactly as before", (t) => {
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	let aborted = 0;
	editor.onEscape = () => { aborted++; };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	assert.equal(aborted, 1);
	assert.doesNotMatch(stripAnsi(editor.render(60).join("\n")), /esc again stops it/);
	editor.dispose();
});

function scopedDoubleEscCancelConfigHome(t: { after(callback: () => void): void }): string {
	const configHome = mkdtempSync(join(tmpdir(), "gp-esc-cfg-"));
	t.after(() => rmSync(configHome, { recursive: true, force: true }));
	return configHome;
}

/** Open /jhon:customize, open the category that holds a row's label, move to it and step it with ←/→ (G's keys) until it shows `value`; then close. */
async function setViaSettings(commands: Map<string, { handler(args: string, ctx: ExtensionContext): Promise<void> | void }>, ctx: ExtensionContext, ui: FakeUi, label: string, value: string): Promise<string[]> {
	const pending = commands.get("jhon:customize")!.handler("", ctx);
	const view = ui.overlayView!;
	const lines = () => view.render(120).map(stripAnsi);
	for (let i = 0; i < 6 && !lines().some((line) => line.includes(`  ${label}`)); i++) view.handleInput("\x1b[B");
	view.handleInput("\x1b[C");
	for (let i = 0; i < 12 && !lines().some((line) => line.includes(`▸ ${label}`)); i++) view.handleInput("\x1b[B");
	const row = () => lines().find((line) => line.includes(`▸ ${label}`)) ?? "";
	// The current value is the radio `● value`, G's.
	for (const key of ["\x1b[C", "\x1b[C", "\x1b[C", "\x1b[D", "\x1b[D", "\x1b[D"]) { if (new RegExp(`● ${value}(\\s|$)`).test(row())) break; view.handleInput(key); }
	const shown = view.render(100).map(stripAnsi);
	view.handleInput("\x1b");
	await pending;
	return shown;
}

test("settings writes animations through `jhon settings set` (S38)", async (t) => {
	const home = scopedDoubleEscCancelConfigHome(t);
	const { pi, handlers, commands } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers);
	try {
		await setViaSettings(commands, ctx, ui, "Animations", "off");
	} finally { editor.dispose(); }
	assert.deepEqual(homeSettings(home).jhon.sets, [
		["shell.appearance.animations", "off"],
	]);
	assert.deepEqual(homeSettings(home).jhon.file, {
		"shell.appearance.animations": "off",
	});
});

// T15.12d (L153 answers 1-2): Cards and Surface are Appearance rows; each
// change is saved with `jhon settings set` and applies at once, and a
// session starts with the saved ones.
test("settings writes the card style and the surface through `jhon settings set` and applies them live", async (t) => {
	const home = scopedDoubleEscCancelConfigHome(t);
	t.after(() => { setCardStyle(CARD_STYLE.FLOAT); setChromeSurface("transparent"); });
	const { pi, handlers, commands } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers);
	try {
		await setViaSettings(commands, ctx, ui, "Cards", "neon");
		assert.equal(cardStyle(), CARD_STYLE.NEON);
		await setViaSettings(commands, ctx, ui, "Surface", "tinted");
		assert.equal(chromeSurface(), "tinted");
	} finally { editor.dispose(); }
	assert.deepEqual(homeSettings(home).jhon.sets, [["shell.appearance.cards", "neon"], ["shell.appearance.surface", "tinted"]]);
	setCardStyle(CARD_STYLE.FLOAT);
	setChromeSurface("transparent");
	await fire(handlers, "session_start", ctx);
	assert.equal(cardStyle(), CARD_STYLE.NEON, "a session starts with the saved style");
	assert.equal(chromeSurface(), "tinted", "and the saved surface");
});

test("settings refuses noninteractive UI and points at `jhon settings`", async (t) => {
	const home = scopedDoubleEscCancelConfigHome(t);
	const { pi, commands } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home });
	const { ctx: rpc, ui: rpcUi } = fakeContext();
	(rpc as unknown as { mode: string }).mode = "rpc";
	await commands.get("jhon:customize")!.handler("", rpc);
	assert.equal(rpcUi.overlayView, undefined);
	assert.match(rpcUi.notices.at(-1)!, /interactive terminal.*jhon settings/);
});

test("double-esc-cancel enabled: the first Esc while working is swallowed and shows the hint instead of aborting", (t) => {
	const configHome = scopedDoubleEscCancelConfigHome(t);
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_DOUBLE_ESC_CANCEL: "on", JHON_SHELL_CONFIG_HOME: configHome });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	let aborted = 0;
	editor.onEscape = () => { aborted++; };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	assert.equal(aborted, 0, "the first Esc must be swallowed, not aborted");
	assert.match(stripAnsi(editor.render(60).join("\n")), /esc again stops it/);
	editor.dispose();
});

test("double-esc-cancel enabled: a second Esc within the window falls through and aborts, clearing the hint", (t) => {
	let now = 1_000_000;
	const configHome = scopedDoubleEscCancelConfigHome(t);
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_DOUBLE_ESC_CANCEL: "on", JHON_SHELL_CONFIG_HOME: configHome }, { now: () => now });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	let aborted = 0;
	editor.onEscape = () => { aborted++; };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	assert.equal(aborted, 0);
	now += 500;
	editor.handleInput("\x1b");
	assert.equal(aborted, 1, "the second Esc within the window must fall through to abort");
	assert.doesNotMatch(stripAnsi(editor.render(60).join("\n")), /esc again stops it/);
	editor.dispose();
});

test("double-esc-cancel enabled: an Esc after the window expires is a fresh first press, not an abort", (t) => {
	let now = 1_000_000;
	const configHome = scopedDoubleEscCancelConfigHome(t);
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_DOUBLE_ESC_CANCEL: "on", JHON_SHELL_CONFIG_HOME: configHome }, { now: () => now });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	let aborted = 0;
	editor.onEscape = () => { aborted++; };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	assert.equal(aborted, 0);
	now += 1001;
	editor.handleInput("\x1b");
	assert.equal(aborted, 0, "the window expired, so this must be treated as a new first press");
	assert.match(stripAnsi(editor.render(60).join("\n")), /esc again stops it/);
	editor.dispose();
});

test("double-esc-cancel enabled: Esc while idle passes straight through, untouched by this gate", (t) => {
	const configHome = scopedDoubleEscCancelConfigHome(t);
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_DOUBLE_ESC_CANCEL: "on", JHON_SHELL_CONFIG_HOME: configHome });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	let aborted = 0;
	editor.onEscape = () => { aborted++; };
	// agent_start never fired: the prompt stays idle.
	editor.handleInput("\x1b");
	assert.equal(aborted, 1, "idle Esc must be unaffected by the policy");
	editor.dispose();
});

test("double-esc-cancel enabled: Esc while autocomplete is visible bypasses this gate entirely", (t) => {
	const configHome = scopedDoubleEscCancelConfigHome(t);
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_DOUBLE_ESC_CANCEL: "on", JHON_SHELL_CONFIG_HOME: configHome });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	(editor as unknown as { isShowingAutocomplete(): boolean }).isShowingAutocomplete = () => true;
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	assert.doesNotMatch(stripAnsi(editor.render(60).join("\n")), /esc again stops it/, "autocomplete must bypass the gate, matching CustomEditor's own guard");
	editor.dispose();
});

// ---------------------------------------------------------------------------
// Working-cancel keeps the queue moving. Pi's own onEscape
// restores `[queuedText, currentText].filter(t => t.trim()).join("\n\n")`
// into the editor and aborts. These tests fake that join in `onEscape` (the
// same seam the double-esc-cancel tests above use), then check that the
// user's own draft is all that is left in the editor and that the queued
// text is sent exactly once, after the aborted run settles.
// ---------------------------------------------------------------------------

test("extractQueuedText reverses Pi's queued+draft join", () => {
	assert.equal(extractQueuedText("follow up\n\ndraft reply", "draft reply"), "follow up");
	assert.equal(extractQueuedText("draft reply", "draft reply"), "", "no queue: the join degenerates to the draft alone");
	assert.equal(extractQueuedText("only queued", ""), "only queued", "empty draft: the join degenerates to the queue alone");
	assert.equal(extractQueuedText("", ""), "", "both empty");
	assert.equal(extractQueuedText("unrelated text", "draft reply"), undefined, "a shape that does not match the join is unrecognized, not an empty queue: Pi's own text must win, not be discarded");
});

test("extractQueuedText treats a whitespace-only draft as empty, matching Pi's own trim filter", () => {
	assert.equal(extractQueuedText("only queued", "   "), "only queued", "a whitespace-only draft never survives Pi's filter, so the whole join is the queue");
	assert.equal(extractQueuedText("", "   "), "", "whitespace-only draft with no queue is still no queue, not unrecognized");
});

test("working cancel: an aborted turn with a queued message sends it once settled and keeps the draft", (t) => {
	const { pi, handlers, sentMessages } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	editor.onEscape = () => { editor.setText(`follow up\n\n${editor.getText()}`); };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	assert.equal(editor.getText(), "draft reply", "the user's own draft stays in the editor");
	assert.equal(sentMessages.length, 0, "nothing is sent until the aborted run settles");
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.deepEqual(sentMessages.map((m) => m.content), ["follow up"]);
	editor.dispose();
});

test("working cancel: an aborted turn with no queued messages behaves exactly as before, with no redundant setText", (t) => {
	const { pi, handlers, sentMessages } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	// Pi's own onEscape restore is the one real setText call on this path
	// (it always writes the combined text, even when that equals the
	// draft); the spy below counts every call, so it must see only that one
	// and none added by abortAndDispatchQueued itself.
	editor.onEscape = () => { editor.setText(editor.getText()); };
	let setTextCalls = 0;
	const originalSetText = editor.setText.bind(editor);
	(editor as unknown as { setText(text: string): void }).setText = (text: string) => { setTextCalls++; originalSetText(text); };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	assert.equal(setTextCalls, 1, "the no-queue path must not add its own write on top of Pi's own restore");
	assert.equal(editor.getText(), "draft reply");
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.equal(sentMessages.length, 0, "an empty queue must never trigger a send");
	editor.dispose();
});

test("working cancel: an unrecognized restore shape leaves Pi's own text untouched and dispatches nothing", (t) => {
	const { pi, handlers, sentMessages } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	// Simulates a future Pi restore shape this code does not recognize.
	editor.onEscape = () => { editor.setText("an unrecognized shape"); };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	assert.equal(editor.getText(), "an unrecognized shape", "Pi's own restore wins on a shape mismatch; nothing here overwrites it");
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.equal(sentMessages.length, 0, "an unrecognized shape must never be dispatched as if it were queued text");
	editor.dispose();
});

test("working cancel: an empty draft with a queued message sends the whole queue and leaves the editor empty", (t) => {
	const { pi, handlers, sentMessages } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.onEscape = () => { editor.setText("only queued"); };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	assert.equal(editor.getText(), "", "an empty draft stays empty");
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.deepEqual(sentMessages.map((m) => m.content), ["only queued"]);
	editor.dispose();
});

test("working cancel: the same dispatch applies to the confirming second Esc when double-esc-cancel is enabled", (t) => {
	let now = 1_000_000;
	const configHome = scopedDoubleEscCancelConfigHome(t);
	const { pi, handlers, sentMessages } = fakePi();
	shellUi(pi, { JHON_SHELL_DOUBLE_ESC_CANCEL: "on", JHON_SHELL_CONFIG_HOME: configHome }, { now: () => now });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	editor.onEscape = () => { editor.setText(`follow up\n\n${editor.getText()}`); };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	now += 500;
	editor.handleInput("\x1b");
	assert.equal(editor.getText(), "draft reply");
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.deepEqual(sentMessages.map((m) => m.content), ["follow up"]);
	editor.dispose();
});

test("working cancel: queued text is never sent twice even if agent_settled fires again", (t) => {
	const { pi, handlers, sentMessages } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	editor.onEscape = () => { editor.setText(`follow up\n\n${editor.getText()}`); };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.deepEqual(sentMessages.map((m) => m.content), ["follow up"]);
	editor.dispose();
});

test("working cancel: a new agent_start before the aborted run settles keeps the pending text and sends it once that turn settles", (t) => {
	const { pi, handlers, sentMessages } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	editor.onEscape = () => { editor.setText(`follow up\n\n${editor.getText()}`); };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	// The user sent the draft (or another turn started) before the aborted
	// run's own agent_settled fired. Nothing is sent from inside agent_start:
	// Pi is mid-turn there, so the text waits for that turn to settle.
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	assert.equal(sentMessages.length, 0, "never send from inside agent_start");
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.deepEqual(sentMessages.map((m) => ({ content: m.content, deliverAs: m.options?.deliverAs })), [{ content: "follow up", deliverAs: undefined }]);
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.equal(sentMessages.length, 1, "the pending text must not be delivered a second time");
	editor.dispose();
});

test("working cancel: a whitespace-only recognized queue is treated as no queue and never dispatched", (t) => {
	const { pi, handlers, sentMessages } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	editor.onEscape = () => { editor.setText(`   \n\n${editor.getText()}`); };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.equal(sentMessages.length, 0, "whitespace is not a message");
	assert.equal(editor.getText(), "draft reply", "the recognized whitespace prefix is stripped and the draft comes back alone");
	editor.dispose();
});

test("working cancel: a failing sendUserMessage on settle is reported, not thrown, and the prompt still leaves the working state", (t) => {
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	editor.onEscape = () => { editor.setText(`follow up\n\n${editor.getText()}`); };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	(pi as unknown as { sendUserMessage: (content: string) => void }).sendUserMessage = () => { throw new Error("session is switching"); };
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.ok(ui.notices.some((notice) => /session is switching/.test(notice)), "the failure surfaces as a notice");
	assert.equal(editor.getText(), "follow up\n\ndraft reply", "the queued text is put back in front of the draft, exactly as Pi's own restore would have left it");
	assert.doesNotMatch(stripAnsi(editor.render(60).join("\n")), /esc again stops it/, "the prompt is idle again");
	editor.dispose();
});

test("working cancel: a failing send reported through a context without UI still restores the queued text into the editor", (t) => {
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const headless = fakeContext({ hasUI: false });
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	editor.onEscape = () => { editor.setText(`follow up\n\n${editor.getText()}`); };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	(pi as unknown as { sendUserMessage: (content: string) => void }).sendUserMessage = () => { throw new Error("session is switching"); };
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, headless.ctx);
	assert.equal(headless.ui.notices.length, 0, "no UI, no notice");
	assert.equal(editor.getText(), "follow up\n\ndraft reply", "with or without a UI to tell the user, the text is never dropped");
	editor.dispose();
});

test("working cancel: agent_settled never sends while another turn is still in flight; the text waits for an idle settle", (t) => {
	const { pi, handlers, sentMessages } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const busy = fakeContext({ idle: false }).ctx;
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	editor.onEscape = () => { editor.setText(`follow up\n\n${editor.getText()}`); };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, busy);
	assert.equal(sentMessages.length, 0, "a settle reported while not idle must not inject the text mid-turn");
	let aborted = 0;
	editor.onEscape = () => { aborted++; };
	editor.handleInput("\x1b");
	assert.equal(aborted, 1, "a run is still in flight, so the prompt stays working and Esc takes the cancel path");
	assert.doesNotMatch(stripAnsi(editor.render(60).join("\n")), /esc again clears it/, "not the idle clear");
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.deepEqual(sentMessages.map((m) => m.content), ["follow up"]);
	editor.dispose();
});

test("working cancel: two aborts before an idle settle deliver both queued texts, in order, as one message", (t) => {
	const { pi, handlers, sentMessages } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const busy = fakeContext({ idle: false }).ctx;
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	let aborts = 0;
	editor.onEscape = () => { aborts++; editor.setText(aborts === 1 ? "first" : "second"); };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, busy);
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.deepEqual(sentMessages.map((m) => m.content), ["first\n\nsecond"], "nothing from the first abort is lost, and order is preserved");
	editor.dispose();
});

test("working cancel: pending queued text never crosses a session boundary", (t) => {
	const { pi, handlers, sentMessages } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	editor.onEscape = () => { editor.setText(`follow up\n\n${editor.getText()}`); };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	for (const handler of handlers.get("session_shutdown") ?? []) handler({}, ctx);
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.equal(sentMessages.length, 0, "the old session's queued text must not be sent into the next session");
});

test("working cancel: agent_settled firing first delivers the pending text without a deliverAs override", (t) => {
	const { pi, handlers, sentMessages } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	editor.onEscape = () => { editor.setText(`follow up\n\n${editor.getText()}`); };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	for (const handler of handlers.get("agent_settled") ?? []) handler({}, ctx);
	assert.deepEqual(sentMessages.map((m) => ({ content: m.content, deliverAs: m.options?.deliverAs })), [{ content: "follow up", deliverAs: undefined }]);
	// The next turn's own agent_start must not re-deliver the already-sent text.
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	assert.equal(sentMessages.length, 1);
	editor.dispose();
});

// ---------------------------------------------------------------------------
// Idle double-Esc clears the draft. With the prompt idle,
// autocomplete hidden, and a non-empty draft, a first Esc shows a hint
// instead of doing nothing; a second Esc within 500ms adds the draft to
// history and clears it. An empty editor, autocomplete, or the working state
// are all untouched: Pi's own idle double-Esc (tree/fork) and the
// working-cancel gate above keep deciding those cases.
// ---------------------------------------------------------------------------

test("idle draft: the first Esc shows the clear hint and keeps the text", (t) => {
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	let aborted = 0;
	editor.onEscape = () => { aborted++; };
	editor.handleInput("\x1b");
	assert.equal(aborted, 0, "the first Esc must be swallowed, not passed to Pi's own idle handler");
	assert.equal(editor.getText(), "draft reply");
	assert.match(stripAnsi(editor.render(60).join("\n")), /esc again clears it/);
	editor.dispose();
});

test("idle draft: a second Esc within the window adds the draft to history and clears it", (t) => {
	let now = 1_000_000;
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) }, { now: () => now });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	const history: string[] = [];
	(editor as unknown as { addToHistory(text: string): void }).addToHistory = (text: string) => history.push(text);
	editor.handleInput("\x1b");
	now += 400;
	editor.handleInput("\x1b");
	assert.deepEqual(history, ["draft reply"]);
	assert.equal(editor.getText(), "");
	assert.doesNotMatch(stripAnsi(editor.render(60).join("\n")), /esc again clears it/);
	editor.dispose();
});

test("idle draft: an Esc after the window expires is a fresh first press, not a clear", (t) => {
	let now = 1_000_000;
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) }, { now: () => now });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	editor.handleInput("\x1b");
	now += 501;
	editor.handleInput("\x1b");
	assert.equal(editor.getText(), "draft reply", "the window expired, so this is a new first press, not a clear");
	assert.match(stripAnsi(editor.render(60).join("\n")), /esc again clears it/);
	editor.dispose();
});

test("idle empty editor: Esc passes straight through to Pi's own tree/fork double-Esc", (t) => {
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	let aborted = 0;
	editor.onEscape = () => { aborted++; };
	editor.handleInput("\x1b");
	assert.equal(aborted, 1, "an empty editor must be untouched by the idle-clear gate");
	editor.dispose();
});

test("idle draft: Esc while autocomplete is visible bypasses the idle-clear gate", (t) => {
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	(editor as unknown as { isShowingAutocomplete(): boolean }).isShowingAutocomplete = () => true;
	editor.handleInput("\x1b");
	assert.equal(editor.getText(), "draft reply", "autocomplete cancel must never clear the draft");
	assert.doesNotMatch(stripAnsi(editor.render(60).join("\n")), /esc again clears it/, "autocomplete must bypass the idle-clear gate, matching CustomEditor's own guard");
	editor.dispose();
});

test("working state: a non-empty draft's Esc is decided by the working-cancel gate, never the idle-clear hint", (t) => {
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.onEscape = () => { editor.setText(editor.getText()); };
	editor.handleInput("\x1b");
	assert.doesNotMatch(stripAnsi(editor.render(60).join("\n")), /esc again clears it/, "working must never show the idle-clear hint");
	editor.dispose();
});

test("idle draft: editing the text between two Esc presses starts a fresh clear window instead of clearing the edit away", (t) => {
	let now = 1_000_000;
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) }, { now: () => now });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("draft reply");
	editor.handleInput("\x1b");
	editor.setText("draft reply, edited");
	assert.doesNotMatch(stripAnsi(editor.render(60).join("\n")), /esc again clears it/, "the edit invalidates the pending clear, so the hint goes away with it");
	now += 400;
	editor.handleInput("\x1b");
	assert.equal(editor.getText(), "draft reply, edited", "an edit invalidates the earlier snapshot, so this must not clear");
	assert.match(stripAnsi(editor.render(60).join("\n")), /esc again clears it/, "the edit starts a fresh first press, with its own hint");
	now += 400;
	editor.handleInput("\x1b");
	assert.equal(editor.getText(), "", "the fresh window's own second Esc, on the unchanged edited text, does clear");
	editor.dispose();
});

test("idle draft: typing and deleting between two Esc presses still invalidates the pending clear", (t) => {
	let now = 1_000_000;
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) }, { now: () => now });
	const { ctx, ui } = fakeContext();
	// Esc-only matching: ordinary keystrokes must reach the editor as text.
	const preciseEscape = { matches: (data: string, keybinding: string) => keybinding === "app.interrupt" && data === "\x1b" };
	const editor = installedPrompt(ctx, ui, handlers, preciseEscape);
	editor.setText("draft reply");
	editor.handleInput("\x1b");
	editor.handleInput("x");
	editor.handleInput("\x7f");
	assert.equal(editor.getText(), "draft reply", "type then backspace lands on the same text");
	now += 400;
	editor.handleInput("\x1b");
	assert.equal(editor.getText(), "draft reply", "an intervening keystroke invalidates the confirmation even when the text ends up identical");
	editor.dispose();
});

test("idle draft: selection-only input and same-character replacement cancel pending clear", (t) => {
	let now = 1_000_000;
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) }, { now: () => now });
	const { ctx, ui } = fakeContext();
	const preciseEscape = { matches: (data: string, keybinding: string) => keybinding === "app.interrupt" && data === "\x1b" };
	const editor = installedPrompt(ctx, ui, handlers, preciseEscape);
	try {
		for (const inputs of [["\x1b[1;2H"], ["\x1b[1;2H", "x"]]) {
			editor.setText("x");
			editor.handleInput("\x1b");
			for (const input of inputs) editor.handleInput(input);
			assert.equal(editor.getText(), "x", "selection or same-character replacement leaves the draft unchanged");
			assert.doesNotMatch(stripAnsi(editor.render(60).join("\n")), /esc again clears it/);
			now += 400;
			editor.handleInput("\x1b");
			assert.equal(editor.getText(), "x", "intervening input must make this a fresh first Esc");
			assert.match(stripAnsi(editor.render(60).join("\n")), /esc again clears it/);
			now += 501;
		}
	} finally { editor.dispose(); }
});

test("idle draft: a bash-mode draft's Esc bypasses the idle-clear gate entirely", (t) => {
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("!ls");
	let aborted = 0;
	editor.onEscape = () => { aborted++; };
	editor.handleInput("\x1b");
	assert.equal(aborted, 1, "a bash-mode draft's first Esc must reach Pi's own bash-mode onEscape, which clears bash mode");
	assert.equal(editor.getText(), "!ls", "this gate never touches bash-mode text; Pi's own onEscape owns it");
	assert.doesNotMatch(stripAnsi(editor.render(60).join("\n")), /esc again clears it/, "bash mode must bypass the idle-clear gate, matching Pi's own bash-mode Esc");
	editor.dispose();
});

test("idle draft: a bash-mode draft with leading whitespace still bypasses the idle-clear gate", (t) => {
	const { pi, handlers } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: scopedDoubleEscCancelConfigHome(t) });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	editor.setText("  !ls");
	let aborted = 0;
	editor.onEscape = () => { aborted++; };
	editor.handleInput("\x1b");
	assert.equal(aborted, 1, "Pi detects bash mode from the trimmed start of the text, so this gate must match the same rule");
	editor.dispose();
});

// ---------------------------------------------------------------------------
// The double-Esc preference, set from /jhon:customize.
// ---------------------------------------------------------------------------

test("the Double Esc setting writes on and off through the settings", async (t) => {
	const configHome = scopedDoubleEscCancelConfigHome(t);
	const { pi, commands } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: configHome });
	const { ctx, ui } = fakeContext();
	await setViaSettings(commands, ctx, ui, "Double Esc", "on");
	assert.equal(savedPolicy(configHome, "double-esc-cancel"), "on");
	await setViaSettings(commands, ctx, ui, "Double Esc", "off");
	assert.equal(savedPolicy(configHome, "double-esc-cancel"), "off");
});

test("the Double Esc setting updates the in-memory policy so an already-installed prompt picks it up without re-reading the file", async (t) => {
	const configHome = scopedDoubleEscCancelConfigHome(t);
	const { pi, handlers, commands } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: configHome });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	let aborted = 0;
	editor.onEscape = () => { aborted++; };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	editor.handleInput("\x1b");
	assert.equal(aborted, 1, "off by default: the first Esc still aborts");
	await setViaSettings(commands, ctx, ui, "Double Esc", "on");
	editor.handleInput("\x1b");
	assert.equal(aborted, 1, "now on: the same prompt instance must swallow the first Esc instead of aborting");
	assert.match(stripAnsi(editor.render(60).join("\n")), /esc again stops it/);
	editor.dispose();
});

test("Settings reads the double-Esc preference again when opened, so its row and the keypress gate agree", async (t) => {
	const configHome = scopedDoubleEscCancelConfigHome(t);
	const { pi, handlers, commands } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: configHome });
	const { ctx, ui } = fakeContext();
	const editor = installedPrompt(ctx, ui, handlers, escapeKeybindings);
	let aborted = 0;
	editor.onEscape = () => { aborted++; };
	for (const handler of handlers.get("agent_start") ?? []) handler({}, ctx);
	// Another session (or a hand edit) turns the preference on underneath this one.
	homeSettings(configHome).jhon.file["shell.behaviour.double_esc_cancel"] = true;
	// The row steps from what the file holds now (on), so one enter on it turns it off.
	const pending = commands.get("jhon:customize")!.handler("behaviour", ctx);
	const lines = () => ui.overlayView!.render(120).map(stripAnsi);
	for (let i = 0; i < 30 && !lines().some((row) => row.includes("▸ Double Esc")); i++) ui.overlayView!.handleInput("\x1b[B");
	assert.ok(lines().some((row) => row.includes("▸ Double Esc")), "the cursor must reach the Double Esc row");
	assert.ok(lines().some((row) => /Double Esc to cancel\s+● on/.test(row)));
	ui.overlayView!.handleInput("\x1b[C"); // G's → steps to the next value and applies it
	ui.overlayView!.handleInput("\x1b");
	await pending;
	assert.equal(savedPolicy(configHome, "double-esc-cancel"), "off");
	editor.handleInput("\x1b");
	assert.equal(aborted, 1, "off now: the first Esc aborts");
	editor.dispose();
});

test("shellUi leaves an editor another extension already installed", () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const theirs = () => ({});
	const { ctx, ui } = fakeContext({ editorFactory: theirs });
	for (const handler of handlers.get("session_start") ?? []) handler({}, ctx);
	assert.equal(ui.editorFactory, theirs);
	assert.notEqual(ui.workingVisible, false, "a custom owner still needs native working feedback");
});

test("the shell replaces its retained factory on reload so new lifecycle handlers own the prompt", () => {
	const first = fakePi();
	shellUi(first.pi, {});
	const { ctx, ui } = fakeContext();
	const oldEditor = installedPrompt(ctx, ui, first.handlers);
	const previousFactory = ui.editorFactory;
	const next = fakePi();
	shellUi(next.pi, {});
	const editor = installedPrompt(ctx, ui, next.handlers);
	try {
		assert.notEqual(ui.editorFactory, previousFactory);
		for (const handler of next.handlers.get("agent_start") ?? []) handler({}, ctx);
		assert.match(stripAnsi(editor.render(60)[0]), /working/);
	} finally {
		oldEditor.dispose();
		editor.dispose();
	}
});

const footerData = { getGitBranch: () => "main", getExtensionStatuses: () => new Map(), getAvailableProviderCount: () => 1, onBranchChange: () => () => {} };

function renderFooter(ui: FakeUi): string {
	const factory = ui.footerFactory as (tui: unknown, theme: ShellBarTheme, footerData: unknown) => { render(width: number): string[] };
	return factory(fakeTui, plainTheme, footerData).render(160)[0];
}

// T132: a session over a real repository, with the shell's own resolver.
function realRepoSession(t: { after(fn: () => void): void }) {
	const dir = realpathSync(mkdtempSync(join(tmpdir(), "changes-ui-")));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const git = (cwd: string, ...args: string[]) => execFileSync("git", ["-C", cwd, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
	const repo = join(dir, "app");
	git(dir, "init", "-q", "--initial-branch=main", repo);
	writeFileSync(join(repo, "a.txt"), "one\n");
	git(repo, "add", ".");
	git(repo, "commit", "-q", "-m", "init");
	const install = () => {
		const made = fakePi();
		shellUi(made.pi, {}, {
			resolveWorktree: resolveSessionWorktree,
			rootExists: (root) => existsSync(join(root, ".git")),
			lddStatus: async () => undefined,
		});
		return made;
	};
	const session = (cwd = repo, entries: unknown[] = []) => { const made = fakeContext({ entries }); (made.ctx as unknown as { cwd: string }).cwd = cwd; return made; };
	const registrations = (ctx: ExtensionContext) => (ctx.sessionManager.getEntries() as Array<{ customType?: string; data?: { root?: string; evidence?: string; base?: string } }>).filter((entry) => entry.customType === "jhon-shell.session-worktree/v1").map((entry) => entry.data!);
	// fakePi's own `git` (its scripted calls) is not this fixture's.
	return { dir, repo, install, session, registrations, ...install(), git };
}
async function waitFor(check: () => boolean): Promise<void> {
	for (const deadline = Date.now() + 5000; !check(); await new Promise((resolve) => setTimeout(resolve, 10))) if (Date.now() > deadline) throw new Error("timed out waiting");
}

// T132, GAP-1 (L943): a root read against HEAD with a clean working tree is
// not "no changes since session start": its commits are not counted. The
// rail and /jhon:changes say what was compared, and why, root by root.
const railWords = () => railChanges().replace(/\s+/g, " ");

test("session_worktree_register still registers a worktree of the session's clone, once, with its base", async (t) => {
 const s = realRepoSession(t);
 const linked=join(s.dir,"linked");
 s.git(s.repo,"worktree","add","-q","-b","linked",linked);
 const {ctx}=s.session();
 await fire(s.handlers,"session_start",ctx);
 const register=s.tools.get("session_worktree_register")!;
 const result=await register.execute("r",{path:linked},undefined,undefined,ctx) as { content: Array<{ text: string }>; details: unknown };
 assert.deepEqual(result,{content:[{type:"text",text:`${linked} is now one of this session's worktrees.`}],details:{root:linked,sessionId:"shell-session"}});
 await register.execute("again",{path:linked},undefined,undefined,ctx);
 const head=s.git(s.repo,"rev-parse","HEAD");
 assert.deepEqual(s.registrations(ctx),[{sessionId:"shell-session",root:s.repo,evidence:"session:cwd",base:head},{sessionId:"shell-session",root:linked,evidence:"explicit",base:head}]);
 await fire(s.handlers,"session_shutdown",ctx);
});

test("a worktree of another clone is still refused, and registers nothing", async (t) => {
 const s = realRepoSession(t);
 const other=join(s.dir,"other");
 s.git(s.dir,"init","-q",other);
 const {ctx}=s.session();
 await fire(s.handlers,"session_start",ctx);
 await assert.rejects(s.tools.get("session_worktree_register")!.execute("r",{path:other},undefined,undefined,ctx),/Name a worktree that exists in this session's own Git clone;/);
 assert.deepEqual(s.registrations(ctx).map((entry) => entry.root),[s.repo]);
 await fire(s.handlers,"session_shutdown",ctx);
});

test("a turn end never waits for Git", async () => {
 const { pi, handlers } = fakePi();
 (pi as unknown as { exec: () => Promise<never> }).exec = () => new Promise<never>(() => {});
 shellUi(pi,{});
 const {ctx}=fakeContext();
 await fire(handlers,"session_start",ctx);
 await Promise.race([fire(handlers,"agent_end",ctx), new Promise((_, reject) => setTimeout(() => reject(new Error("agent_end waited for Git")), 100))]);
 await fire(handlers,"session_shutdown",ctx);
});

test("explicit registration validates the clone", async () => {
	const { pi, handlers, commands, tools } = fakePi();
	let bootstrapped = false;
	shellUi(pi, { JHON_SHELL_CHANGES_WATCH_MS: "off" }, {
		resolveWorktree: (path) => bootstrapped ? { root: path === "." ? "/repo" : path, commonDir: path === "/foreign" ? "/foreign/git" : "/repo/.git" } : undefined,
	});
	const { ctx, ui } = fakeContext();
	const manager = ctx.sessionManager;
	await fire(handlers, "session_start", ctx);
	const register = tools.get("session_worktree_register")!;
	await assert.rejects(register.execute("before", { path: "/repo" }, undefined, undefined, ctx), /own Git clone/);
	bootstrapped = true;
	await register.execute("after", { path: "/repo" }, undefined, undefined, ctx);
	await register.execute("dedup", { path: "/repo" }, undefined, undefined, ctx);
	await assert.rejects(register.execute("foreign", { path: "/foreign" }, undefined, undefined, ctx), /own Git clone/);
	await assert.rejects(register.execute("foreign", { path: "/foreign" }, undefined, undefined, ctx), /another repository cannot be one of its worktrees/);
	assert.equal(ctx.sessionManager, manager);
	assert.equal(ctx.sessionManager.getEntries().length, 1);
	assert.equal(commands.has("jhon:changes"), false, "T174: no changes view");
	assert.equal(ui.overlay, undefined);
	await fire(handlers, "session_shutdown", ctx);
});

test("new sessions do not inherit another session's worktrees", async () => {
 const h=fakePi(); shellUi(h.pi,{});
 const first=fakeContext();
 await fire(h.handlers,"session_start",first.ctx);
 await h.tools.get("session_worktree_register")!.execute("r",{path:"/linked"},undefined,undefined,first.ctx);
 await fire(h.handlers,"session_shutdown",first.ctx);
 const next=fakeContext({entries:[...first.ctx.sessionManager.getEntries()]});
 (next.ctx.sessionManager as any).getSessionId=()=>"new-session";
 await fire(h.handlers,"session_start",next.ctx);
 const roots=(next.ctx.sessionManager.getEntries() as Array<{ customType?: string; data?: { sessionId?: string; root?: string } }>).filter((entry)=>entry.customType==="jhon-shell.session-worktree/v1"&&entry.data?.sessionId==="new-session").map((entry)=>entry.data!.root);
 assert.ok(!roots.includes("/linked"), JSON.stringify(roots));
 await fire(h.handlers,"session_shutdown",next.ctx);
});

test("openInExternalEditor stops the TUI around the editor and honors $VISUAL over $EDITOR", () => {
	const events: string[] = [];
	const host = { stop: () => events.push("stop"), start: () => events.push("start"), requestRender: (force?: boolean) => events.push(`render:${force}`) };
	const spawn = ((command: string, args: string[]) => {
		events.push(`spawn:${command} ${args.join(" ")}`);
		return { status: 0 } as ReturnType<typeof import("node:child_process").spawnSync>;
	}) as typeof import("node:child_process").spawnSync;
	assert.equal(openInExternalEditor(host, "lib/a.ts", { VISUAL: "nvim -u none", EDITOR: "vi" }, spawn), undefined);
	assert.deepEqual(events, ["stop", 'spawn:sh -c nvim -u none "$@" nvim -u none lib/a.ts', "start", "render:true"]);
	assert.equal(openInExternalEditor(host, "lib/a.ts", {}, spawn), "No editor: set $VISUAL or $EDITOR.");
});

test("the editor command is read by the shell, so quotes and spaces work, and its failure is reported", { skip: process.platform === "win32" }, () => {
	const dir = mkdtempSync(join(tmpdir(), "jhon-editor-"));
	try {
		const bin = join(dir, "my editor");
		writeFileSync(bin, `#!/bin/sh\nprintf '%s|' "$@" > "${join(dir, "args")}"\n`);
		chmodSync(bin, 0o755);
		const host = { stop() {}, start() {}, requestRender() {} };
		assert.equal(openInExternalEditor(host, "a file.ts", { EDITOR: `"${bin}" --wait` }, spawnSync, dir), undefined);
		assert.equal(readFileSync(join(dir, "args"), "utf8"), "--wait|a file.ts|");
		assert.match(openInExternalEditor(host, "a.ts", { EDITOR: "false" }, spawnSync, dir)!, /The editor \(false\) ended with code 1/);
	} finally { rmSync(dir, { recursive: true, force: true }); }
});

test("external editor receives the selected worktree as process cwd", () => {
	let cwd: string | undefined;
	const spawn = ((_command: string, _args: string[], options: { cwd?: string }) => {
		cwd = options.cwd;
		return { status: 0 };
	}) as typeof import("node:child_process").spawnSync;
	openInExternalEditor({ stop() {}, start() {}, requestRender() {} }, "same.ts", { EDITOR: "vi" }, spawn, "/linked");
	assert.equal(cwd, "/linked");
});

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
	const bar = () => sidebarState(tui as unknown as TUI).parts.get("statusbar")!.render(160).map(stripAnsi)[0]!;
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
	const home = scopedDoubleEscCancelConfigHome(t);
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
	const home = scopedDoubleEscCancelConfigHome(t);
	const { pi, commands } = fakePi();
	shellUi(pi, { JHON_SHELL_CONFIG_HOME: home });
	const { ctx, ui } = fakeContext();
	const pending = commands.get("jhon:customize")!.handler("keys", ctx);
	const lines = ui.overlayView!.render(120).map(stripAnsi);
	ui.overlayView!.handleInput("\x1b");
	await pending;
	const text = lines.join("\n");
	assert.match(text, /│ ▸ Keys +│/);
	assert.match(text, /▸ LDD status +alt\+j/);
	assert.doesNotMatch(text, /Animations|Runtimes|Integrations|Profile/);
	assert.match(lines.at(-2)!, /Click · ←\/→ or Tab panes · ↑\/↓ moves · Enter applies · Esc closes/, "the footer");
});

// T15.8 (L98), T15.12d: a click on a rail section's header folds it to that
// header (the card's top row on a transparent rail), and opens it again; the
// fold stays for the session.
test("a click on a Status section's header folds that section alone, and a second opens it", async () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	renderFooter(ui);
	type Part = SidebarRail & { handleMouse(e: unknown): unknown };
	// L174: LDD is its own part, first in the rail; T174: the footer draws nothing in the rail.
	const ldd = sidebarState(fakeTui as unknown as TUI).parts.get("ldd")! as Part;
	const rail = sidebarState(fakeTui as unknown as TUI).parts.get("footer")! as Part;
	const open = ldd.render(46).map(stripAnsi);
	assert.match(open[0]!, /^▾ LDD/);
	assert.ok(open.length >= 2);
	assert.equal(ldd.handleMouse({ type: "click", button: "left", x: 3, y: 1, width: 46, height: open.length }), undefined, "only the header folds");
	assert.deepEqual(ldd.handleMouse({ type: "click", button: "left", x: 3, y: 0, width: 46, height: open.length }), { handled: true, render: true });
	assert.deepEqual(ldd.render(46).map(stripAnsi).filter((line) => line.trim() !== "").map((line) => line.trim()).length, 1, "LDD is its header alone");
	assert.match(stripAnsi(ldd.render(46)[0]!), /^▸ LDD/);
	assert.deepEqual(rail.render(46), [], "no CHANGES, no INTEGRATIONS: the footer's rail is empty");
	ldd.handleMouse({ type: "click", button: "left", x: 3, y: 0, width: 46, height: 1 });
	assert.match(stripAnsi(ldd.render(46)[0]!), /^▾ LDD/);
	await fire(handlers, "session_shutdown", ctx);
});

test("a move over the JOBS or AGENTS header paints it in the hover role; the keys stay off the headers (T15.12c, C6)", async () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	const tui = { terminal: { rows: 40, columns: 180 }, requestRender() {} };
	const tagged = { ...plainTheme, fg: (color: string, value: string) => `<${color}>${value}</${color}>` };
	const footer = (ui.footerFactory as (tui: unknown, theme: unknown, data: unknown) => { render(width: number): string[]; dispose(): void })(tui, tagged, footerData);
	try {
		pi.events.emit("jhon-shell:tasks", { sessionId: ctx.sessionManager.getSessionId(), tasks: [{ id: "job-1", label: "build", startedAt: 1 }, { id: "agent-1", kind: "agent", label: "explore: callers", startedAt: 2 }] });
		type Part = SidebarRail & { handleMouse(e: unknown): unknown };
		for (const [key, title] of [["jobs", "JOBS"], ["agents", "AGENTS"]] as const) {
			const part = sidebarState(tui as unknown as TUI).parts.get(key) as Part;
			assert.doesNotMatch(stripAnsi(part.render(200)[0]!), /alt\+s/, `${title}: no key on the header`);
			part.handleMouse({ type: "move", button: "none", x: 3, y: 0, width: 200, height: 3 });
			assert.match(part.render(200)[0]!, new RegExp(`<warning>${title}</warning>`), title);
			part.handleMouse({ type: "move", button: "none", x: 3, y: 1, width: 200, height: 3 });
			assert.doesNotMatch(part.render(200)[0]!, /<warning>/, `${title}: a body row is no control`);
		}
	} finally { footer.dispose(); await fire(handlers, "session_shutdown", ctx); }
});

test("the rail draws no key line and no / commands: those live only in the prompt's placeholder (L143, C6)", async () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	renderFooter(ui);
	const parts = sidebarState(fakeTui as unknown as TUI).parts;
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
		{ value: "keys", label: "keys", description: "The shell's keys: LDD status, agents; reload" },
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

// T15.9 (L103): JOBS and AGENTS fold by a click on their header too, each
// on its own, and open again.
test("a click on the JOBS or the AGENTS header folds that section, and a second opens it", async () => {
	const { pi, handlers } = fakePi();
	shellUi(pi, {});
	const { ctx, ui } = fakeContext();
	await fire(handlers, "session_start", ctx);
	const tui = { terminal: { rows: 40, columns: 180 }, requestRender() {} };
	const footer = (ui.footerFactory as (tui: unknown, theme: ShellBarTheme, data: unknown) => { render(width: number): string[]; dispose(): void })(tui, plainTheme, {
		getGitBranch: () => "main", getExtensionStatuses: () => new Map(), getAvailableProviderCount: () => 1, onBranchChange: () => () => {},
	});
	try {
		pi.events.emit("jhon-shell:tasks", { sessionId: ctx.sessionManager.getSessionId(), tasks: [{ id: "job-1", label: "build", startedAt: 1 }, { id: "agent-1", kind: "agent", label: "explore: callers", startedAt: 2 }] });
		const parts = sidebarState(tui as unknown as TUI).parts;
		type Part = SidebarRail & { handleMouse(e: unknown): unknown };
		// JOBS is its own part, as Gentle Shell's: the rail orders todo, jobs, agents, Status.
		const jobs = parts.get("jobs") as Part;
		const agents = parts.get("agents") as Part;
		assert.ok(jobs, "a jobs part of its own");
		const draw = (rail: Part) => rail.render(48).map(stripAnsi);
		const click = (rail: Part, y: number) => rail.handleMouse({ type: "click", button: "left", x: 3, y, width: 48, height: draw(rail).length });
		assert.match(draw(jobs)[0]!, /^▾ JOBS /);
		assert.ok(!draw(jobs).some((line) => /AGENTS|callers/.test(line)), "JOBS holds no agent");
		assert.match(draw(agents)[0]!, /^▾ AGENTS /);
		assert.ok(!draw(agents).some((line) => /JOBS|build/.test(line)), "AGENTS holds no job");
		assert.equal(click(jobs, 1), undefined, "a body row folds nothing");
		assert.deepEqual(click(jobs, 0), { handled: true, render: true });
		assert.match(draw(jobs)[0]!, /▸ JOBS/);
		assert.deepEqual(draw(jobs).slice(1).map((line) => line.trim().split(/ +/).slice(0, 3).join(" ")), ["⧗ job-1 build"], "folded JOBS keeps its first job, as G's");
		assert.match(draw(agents)[0]!, /▾ AGENTS/, "AGENTS stays open");
		click(agents, 0);
		assert.match(draw(agents)[0]!, /▸ AGENTS/);
		assert.equal(draw(agents).filter((line) => /callers/.test(line)).length, 1, "folded AGENTS keeps its first row, as G's");
		click(jobs, 0);
		click(agents, 0);
		assert.match(draw(jobs)[0]!, /▾ JOBS/);
		assert.match(draw(agents)[0]!, /▾ AGENTS/);
		assert.ok(draw(agents).some((line) => /callers/.test(line)));
	} finally { footer.dispose(); }
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
		// T174: the changes and usage views are gone too.
		for (const name of ["jhon:usage", "jhon:changes"]) assert.equal(commands.has(name), false, name);
		for (const key of ["alt+u", "alt+g"]) assert.equal(shortcuts.has(key), false, key);
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
		const parts = sidebarState(tui as unknown as TUI).parts;
		assert.equal(parts.has("stats"), false, "no STATS part");
		for (const kept of ["ldd", "jobs", "agents", "footer"]) assert.ok(parts.has(kept), `${kept} stays: ${[...parts.keys()].join(", ")}`);
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
