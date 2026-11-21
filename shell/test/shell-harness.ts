/**
 * A stand-in for pi around extensions/ui.ts, for the prompt, Customize and
 * worktree tests. Each shell gets settings of its own, in memory (as
 * `jhon settings` would give them, the JHON_SHELL_* variables of its env
 * applied), so no test reads or writes the owner's.
 */
import "./isolated-env.ts";
import { initTheme, type ExtensionAPI, type ExtensionContext } from "@earendil-works/pi-coding-agent";
import installShellUi, { type JhonPromptEditor, type ShellDeps } from "../extensions/ui.ts";
import { installShellSettings, type SettingValue } from "../lib/settings.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { envOverrides, memorySettings } from "./fake-jhon-settings.ts";

initTheme("dark");

type Handler = (event: unknown, ctx: ExtensionContext) => unknown;
type Settings = ReturnType<typeof memorySettings>;
interface Command { description?: string; handler(args: string, ctx: ExtensionContext): Promise<void> | void }
interface Tool { renderShell?: string; execute(id: string, params: unknown, signal: undefined, update: undefined, ctx: ExtensionContext): Promise<unknown> }

const homes = new Map<string, Settings>();
let made = 0;

/** The settings a home holds; `file` seeds a home the first time it is named. */
export function homeSettings(home: string, file: Record<string, SettingValue> = {}): Settings {
	let settings = homes.get(home);
	if (!settings) { settings = memorySettings(file); homes.set(home, settings); }
	return settings;
}

/** A home nobody else uses. */
export const freshHome = () => `harness-home-${++made}`;

/** What a home's settings file holds for the animations or the double-Esc preference, as the old preference files named it. */
export function saved(home: string, what: "animations" | "double-esc"): string | undefined {
	const file = homeSettings(home).jhon.file;
	if (what === "animations") {
		const value = file["shell.appearance.animations"];
		return value === undefined ? undefined : ({ full: "quality", off: "potato" } as Record<string, string>)[value as string];
	}
	const value = file["shell.behaviour.double_esc_cancel"];
	return value === undefined ? undefined : value ? "on" : "off";
}

const plain = { fg: (_role: string, text: string) => text, bold: (text: string) => text, bg: (_role: string, text: string) => text, getBgAnsi: () => "" };
export const tui = { terminal: { rows: 40, columns: 120 }, requestRender() {} };
const fullscreen = Object.create(tui, { mode: { value: "fullscreen" } });
const editorTheme = { borderColor: (text: string) => text, selectList: {} };
/** Keybindings where nothing matches, and where only Esc is the interrupt. */
export const noKeys = { matches: () => false };
export const escKey = { matches: (data: string, action: string) => action === "app.interrupt" && data === "\x1b" };

/** The UI extension on a stand-in pi. */
export function shell(env: NodeJS.ProcessEnv = {}, deps: Partial<ShellDeps> = {}) {
	const handlers = new Map<string, Handler[]>();
	const commands = new Map<string, Command>();
	const tools = new Map<string, Tool>();
	const sent: Array<{ content: string; options?: { deliverAs?: string } }> = [];
	/** Every command name registered, in order, repeats included. */
	const registered: string[] = [];
	const listeners = new Map<string, (data: unknown) => void>();
	const pi = {
		on: (name: string, handler: Handler) => { handlers.set(name, [...(handlers.get(name) ?? []), handler]); },
		appendEntry: () => {},
		events: { on: (name: string, fn: (data: unknown) => void) => { listeners.set(name, fn); return () => listeners.delete(name); }, emit: (name: string, data: unknown) => listeners.get(name)?.(data) },
		registerTool: (tool: Tool & { name: string }) => { tools.set(tool.name, tool); },
		registerCommand: (name: string, command: Command) => { registered.push(name); commands.set(name, command); },
		registerShortcut: () => {},
		registerMessageRenderer: () => {},
		getThinkingLevel: () => "medium",
		getCommands: () => [],
		sendUserMessage: (content: string, options?: { deliverAs?: string }) => { sent.push({ content, options }); },
		exec: async () => ({ stdout: "", stderr: "", code: 0, killed: false }),
	} as unknown as ExtensionAPI & { sendUserMessage(content: string): void };
	const home = env.JHON_SHELL_CONFIG_HOME ?? freshHome();
	const settings = homeSettings(home);
	settings.jhon.env = envOverrides(env);
	settings.refresh();
	installShellSettings(settings);
	installShellUi(pi, { ...env, JHON_SHELL_CONFIG_HOME: home }, { resolveWorktree: (path: string) => ({ root: path.startsWith("/repo") || path === "." ? "/repo" : path, commonDir: "/clone/git" }), rootExists: () => true, lddStatus: async () => undefined, ...deps });
	/** Runs every handler of an event, in order. */
	const fire = async (name: string, ctx: ExtensionContext, payload: unknown = {}) => { for (const handler of handlers.get(name) ?? []) await handler(payload, ctx); };
	/** The same, without waiting: for the synchronous handlers the prompt tests drive. */
	const emit = (name: string, ctx: ExtensionContext) => { for (const handler of handlers.get(name) ?? []) handler({}, ctx); };
	return { pi, home, settings, handlers, commands, registered, tools, sent, fire, emit };
}
export type Shell = ReturnType<typeof shell>;

export interface SessionUi {
	editor: unknown;
	workingVisible: boolean | undefined;
	notices: string[];
	view: { render(width: number): string[]; handleInput(data: string): void } | undefined;
}

/** A session context, and what the shell set on its UI. */
export function session(options: { hasUI?: boolean; idle?: boolean; pending?: () => boolean; editor?: unknown; theme?: object; entries?: unknown[]; id?: string } = {}) {
	const ui: SessionUi = { editor: options.editor, workingVisible: undefined, notices: [], view: undefined };
	const entries = options.entries ?? [];
	const ctx = {
		hasUI: options.hasUI ?? true,
		mode: "tui",
		hasPendingMessages: options.pending ?? (() => false),
		isIdle: () => options.idle ?? true,
		cwd: "/repo",
		model: { id: "claude-opus-5-5", provider: "anthropic", reasoning: true, contextWindow: 272_000 },
		sessionManager: { getCwd: () => "/repo", getSessionName: () => "s", getEntries: () => entries, getSessionId: () => options.id ?? "shell-session", getBranch: () => [] },
		getContextUsage: () => ({ tokens: 1, contextWindow: 272_000, percent: 0 }),
		ui: {
			theme: options.theme ?? plain,
			getAllThemes: () => [{ name: "dark", path: undefined }],
			setTheme: () => ({ success: true }),
			setFooter: () => {},
			setWidget: () => {},
			setEditorComponent: (factory: unknown) => { ui.editor = factory; },
			getEditorComponent: () => ui.editor,
			notify: (message: string) => { ui.notices.push(message); },
			setWorkingVisible: (visible: boolean) => { ui.workingVisible = visible; },
			custom<T>(factory: (tui: unknown, theme: unknown, keys: unknown, done: (value: T) => void) => { render(width: number): string[]; handleInput(data: string): void }) {
				return new Promise<T | null>((resolve) => { ui.view = factory(tui, { ...plain, bg: undefined }, noKeys, (value: T) => resolve(value)); });
			},
		},
	} as unknown as ExtensionContext;
	return { ctx, ui };
}

/** Starts the session and builds the shell's prompt as pi would, in a full-screen terminal. */
export function prompt(s: Shell, ctx: ExtensionContext, ui: SessionUi, keys: unknown = noKeys): JhonPromptEditor {
	s.emit("session_start", ctx);
	return (ui.editor as (tui: unknown, theme: unknown, keys: unknown) => JhonPromptEditor)(fullscreen, editorTheme, keys);
}

/** The prompt's rows without colour. */
export const rows = (editor: JhonPromptEditor, width = 60) => editor.render(width).map(stripAnsi);
export const shown = (editor: JhonPromptEditor, width = 60) => rows(editor, width).join("\n");

/**
 * Opens /jhon:customize, goes to the group that holds `label`, then to its
 * row, and steps it with ←/→ until it reads `● value`; closes, and returns
 * the rows as they were before closing.
 */
export async function customize(s: Shell, ctx: ExtensionContext, ui: SessionUi, label: string, value: string): Promise<string[]> {
	const open = s.commands.get("jhon:customize")!.handler("", ctx);
	const view = ui.view!;
	const lines = () => view.render(120).map(stripAnsi);
	for (let i = 0; i < 6 && !lines().some((line) => line.includes(`  ${label}`)); i++) view.handleInput("\x1b[B");
	view.handleInput("\x1b[C");
	for (let i = 0; i < 12 && !lines().some((line) => line.includes(`▸ ${label}`)); i++) view.handleInput("\x1b[B");
	const row = () => lines().find((line) => line.includes(`▸ ${label}`)) ?? "";
	for (const key of ["\x1b[C", "\x1b[C", "\x1b[C", "\x1b[D", "\x1b[D", "\x1b[D"]) {
		if (new RegExp(`● ${value}(\\s|$)`).test(row())) break;
		view.handleInput(key);
	}
	const before = view.render(100).map(stripAnsi);
	view.handleInput("\x1b");
	await open;
	return before;
}
