import { CustomEditor, type ExtensionAPI, type ExtensionContext, type KeybindingsManager } from "@earendil-works/pi-coding-agent";
import { truncateToWidth, type EditorTheme, type TUI } from "@earendil-works/pi-tui";
import { execFile } from "node:child_process";
import { existsSync, realpathSync, statSync, watch } from "node:fs";
import { join } from "node:path";
import { JOBS_FINISHED_TTL_MS, renderAgentsSection, renderJobsSection, renderLddPanel, renderRunningBlock, shellEnabled, shellJobsCount, type RunningJobDisplay } from "../lib/ui/panel.ts";
import { barModel, renderStatusBar, renderTopRow, type BarModel } from "../lib/ui/bars.ts";
import { SettingsView } from "../lib/ui/settings-view.ts";
import { ROSE_INK } from "../lib/ui/customize/themes.ts";
import { PALETTE_PATH } from "../lib/desktop-palette.ts";
import { switchToRoseInk } from "../lib/rose-ink.ts";
import { CUSTOMIZE_WORDS, customizeCompletions, customizeDescription, customizeRows, unknownCategoryNotice } from "../lib/ui/customize/registry.ts";
import { completeWord } from "../lib/ui/jhon-commands.ts";
import { backgroundTasksKey } from "../lib/background-tasks-tools.ts";
import { changeMode, type ModeData } from "../lib/ui/jhon-panel-grants.ts";
import { laneReads, lddLanes, lddOfSession, lddWatchTargets, permissionMode, readLddStatus, selectChange, shareSelection, type LddStatus } from "../lib/ui/ldd-status.ts";
import { SessionWorktreeRegistry, resolveSessionWorktree, type WorktreeResolver } from "../lib/ui/session-worktree-registry.ts";
import { glyph, setCardStyle } from "../lib/ui/primitives/index.ts";
import { resolveAnimationPolicy, resolveVisualSettings, type AnimationPolicy } from "../lib/ui/appearance-policy.ts";
import { CARD_OVERLAY, closeWithRepaint } from "../lib/ui/overlay.ts";
import { createWorkingActivity, DOUBLE_ESC_CANCEL_HINT, framePrompt, IDLE_ESC_CLEAR_HINT, PROMPT_HINT, PROMPT_INDENT, PROMPT_STATE, renderWorkingLine, SHELL_PULSE_MS, withPlaceholder } from "../lib/ui/shell-prompt.ts";
import { createApiWait } from "../lib/ui/api-wait.ts";
import { createStallRecovery, stallContinueText, stallNotice, stallSeconds, stallThreshold, type Schedule } from "../lib/ui/stall-recovery.ts";
import { activeProfileName } from "../lib/profiles.ts";
import { SESSION_PROFILE_CHANGED, sessionMark, sessionProfileOnBranch } from "../lib/session-profile.ts";
import { OPEN_TASKS_EVENT, SANDBOX_EVENT, TASKS_EVENT, VISUAL_SETTINGS_CHANGED } from "../lib/events.ts";
import { retiredNotice, shellSettings } from "../lib/settings.ts";
import { createEscGate, extractQueuedText, loadDoubleEscCancelPolicy, type DoubleEscCancelPolicy } from "../lib/ui/double-esc-cancel-policy.ts";
import { foldablePart, installPane, invalidatePane, paneSlot, paneState, renderStrip, setPaneHover, stripModel } from "../lib/ui/pane.ts";
import { setChromeSurface } from "../lib/ui/shell-panel.ts";
import { recentSessionRoots, SessionChanges } from "../lib/ui/session-changes.ts";
import type { ProfilesJson } from "../lib/session-profile.ts";
import { installSessionChangeCapture } from "../lib/ui/session-change-capture.ts";
import { PromptSelection } from "../lib/ui/prompt-selection.ts";
import { drawnInputRows } from "../lib/ui/editor-access.ts";
import { HOST_EVENT, hostShowsPath } from "../lib/host.ts";
import { counted } from "../lib/ui-use.ts";
import { installUsage } from "../lib/ui/usage-shell.ts";

// jhon-shell's UI: the visual layer Jhon puts on top of pi. It installs the
// status bar, the right pane, the prompt and customization.

/** The screen the footer was drawn on, once pi has drawn it: what a change outside a frame asks to repaint. */
interface Repaint {
	requestRender(): void;
	invalidatePane?(): void;
}

/** What the shell reads from outside: the clock, Git, the filesystem, `jhon status`, timers and the network; tests replace any of them. */
export type ShellDeps = {
	/** The profile the bar names, none on the default. */
	activeProfile: () => string | undefined;
	/** The clock, and Git's answer to which worktree a path is in (lib/ui/session-worktree-registry.ts). */
	now: () => number; resolveWorktree: WorktreeResolver;
	/** A session root the LDD section may read: removed worktrees and leftover directories are skipped. */
	rootExists: (root: string) => boolean;
	/** The LDD change the session works on, through `jhon status` (lib/ui/ldd-status.ts). */
	lddStatus(cwd: string): Promise<LddStatus | undefined>;
	/** The owner's permission mode the last lddStatus read of cwd reported (S31), for the status line's badge. */
	permissionMode?(cwd: string): string | undefined;
	/** How STATUS watches a directory: node's fs.watch. */
	watch?: typeof watch;
	/** The stall recovery's one-shot timer (lib/ui/stall-recovery.ts); returns its cancel. */
	schedule?: Schedule;
	/** What a registered usage source fetches with (lib/ui/usage.ts); the global fetch by default. */
	fetch?: typeof fetch;
	/** `jhon profile json`, for the usage scope of a session on its own profile; JHON_BIN's by default. */
	profilesJson?(cwd: string): Promise<ProfilesJson>;
};

// The `±N` beside the branch: the files the session's own writes changed
// (lib/ui/session-change-capture.ts records them as session entries), counted
// again only when the session has new entries: the bar draws every frame.
const changedCache = new WeakMap<object, { key: string; files: number }>();
function sessionChangedFiles(ctx: ExtensionContext): number {
	try {
		const manager = ctx.sessionManager;
		const entries = manager.getEntries();
		const key = `${manager.getSessionId()}\0${entries.length}`;
		const cached = changedCache.get(manager);
		if (cached?.key === key) return cached.files;
		const files = new SessionChanges(manager.getSessionId(), entries as never).model.files.length;
		changedCache.set(manager, { key, files });
		return files;
	} catch { return 0; }
}

/**
 * The profile the bar names: the owner's active Jhon profile (settings'
 * pi.profile, else profiles.json), or the session's own (T120), marked while
 * it differs from the default; `default` names none. It is read on refresh
 * and by its poll, never during a frame.
 */
export type ProfileReader = (() => string | undefined) & {
	/** Reads the profile again; true when the name shown changed. */
	refresh: () => boolean;
	/** Reads it for a session starting in `cwd`; the folder and the id change nothing yet: the profile is the owner's, or the session's own. */
	bind: (cwd: string, resolveWorktree: WorktreeResolver, sessionId?: string) => boolean;
	/** Forgets the name: nothing shows until the next read. */
	reset: () => void;
	/** Reads it now, then every `everyMs` (2 s) while `live` holds, calling `changed` when the name moves. */
	follow: (live: () => boolean, changed: () => void, everyMs?: number) => void;
	/** Ends the poll and forgets the name, until the next follow. */
	stop: () => void;
};

export function createActiveProfileReader(read: () => string = activeProfileName, session: () => string | undefined = () => undefined): ProfileReader {
	let shown: string | undefined;
	let poll: ReturnType<typeof setInterval> | undefined;
	const refresh = (): boolean => {
		const marked = sessionMark(session(), read());
		const before = shown;
		shown = marked === "default" ? undefined : marked;
		return shown !== before;
	};
	const reset = (): void => {
		shown = undefined;
	};
	const stop = (): void => {
		if (poll !== undefined) clearInterval(poll);
		poll = undefined;
		reset();
	};
	const follow = (live: () => boolean, changed: () => void, everyMs = 2_000): void => {
		stop();
		refresh();
		poll = setInterval(() => { if (live() && refresh()) changed(); }, everyMs);
		poll.unref?.();
	};
	return Object.assign(() => shown, { refresh, bind: () => refresh(), reset, follow, stop });
}

/** The shell's own dependencies around the profile reader; the caller's overrides (tests) replace any of them. */
function shellDeps(activeProfile: ShellDeps["activeProfile"], overrides: Partial<ShellDeps>): ShellDeps {
	const own: ShellDeps = {
		activeProfile,
		now: () => Date.now(), resolveWorktree: resolveSessionWorktree,
		rootExists: (root) => existsSync(join(root, ".git")),
		lddStatus: (cwd) => readLddStatus(cwd),
		permissionMode,
	};
	return { ...own, ...overrides };
}

/** What the prompt asks of the session, whoever builds it (claimPrompt, or a test). */
type PromptSession = {
	now: () => number;
	/** shell.behaviour.double_esc_cancel, read on every press (Customize changes it in memory). */
	doubleEscCancelEnabled: () => boolean;
	/** The messages pi put back in the editor when esc stopped the turn: sent as the next turn instead. */
	dispatchQueuedText: (text: string) => void;
	/** The wait on the Claude API, shown in place of what the agent does (lib/ui/api-wait.ts); read on every frame. */
	workingLabel?(): string | undefined;
	/** What the agent does now (thinking, writing, a tool's name), read on every frame. */
	activity?: () => string | undefined;
};

/** The prompt's dependencies: the session's, pi's theme painters and repaint, and the rows above the working line. */
type PromptEditorDeps = PromptSession & {
	fg: (color: string, text: string) => string;
	bold: (text: string) => string;
	italic?: (text: string) => string;
	requestRender: () => void;
	/** A message waits for the turn to end: the working line says queued. */
	pending: () => boolean;
	/** Run fn after ms, returning its cancel; a timer by default. */
	schedule?: (fn: () => void, ms: number) => () => void;
	/** shell.appearance.ascii; the settings' by default. */
	ascii?: () => boolean;
	/** Rows the shell draws above the working line (the narrow form's Running block), given the hovered one's index. */
	topRows?(width: number, hovered?: number): string[];
	/** A left click on top row `index`. */
	onTopRowClick?(index: number): void;
};

export { extractQueuedText };

/**
 * The prompt (design/shell-tui 01, T174-S8): the working line, a rule, the
 * input behind `❯`, a rule (lib/ui/shell-prompt.ts draws them around pi's
 * editor). It adds to pi's editor the esc gate (lib/ui/double-esc-cancel-policy.ts),
 * the selection (lib/ui/prompt-selection.ts) and the rows above the line.
 */
export class JhonPromptEditor extends CustomEditor {
	private readonly deps: Readonly<PromptEditorDeps>;
	private readonly host: TUI;
	// CustomEditor keeps its keybindings private; the esc gate needs app.interrupt's match.
	private readonly keys: KeybindingsManager;
	private readonly selection: PromptSelection;
	private readonly esc: ReturnType<typeof createEscGate>;
	private working = false;
	private startedAt = 0;
	/** The working line's motion: the frame it is on, its pulse timer, and whether it moves at all (potato: only with pi's repaints). */
	private readonly motion: { tick: number; timer?: NodeJS.Timeout; policy: AnimationPolicy } = { tick: 0, policy: "quality" };
	private hintRepaint: (() => void) | undefined;
	/** Rows above pi's editor in the last frame: the top rows and the working line. */
	private shellRows = 0;
	/** The rows drawn above the working line in the last frame, and the one under the pointer. */
	private readonly top: { count: number; hovered?: number } = { count: 0 };

	constructor(tui: TUI, theme: EditorTheme, keybindings: KeybindingsManager, deps: PromptEditorDeps) {
		super(tui, theme, keybindings);
		this.host = tui;
		this.keys = keybindings;
		this.deps = deps;
		this.selection = new PromptSelection(this);
		this.esc = createEscGate(() => deps.now());
	}

	setAnimationPolicy(next: AnimationPolicy): void {
		if (this.motion.policy === next) return;
		this.motion.policy = next;
		this.restartTimer();
		this.deps.requestRender();
	}

	/** The turn starts or ends: the clock restarts, the esc gate disarms, the line animates or stops. */
	setWorking(on: boolean): void {
		if (on) this.startedAt = this.deps.now();
		this.working = on;
		this.esc.reset();
		this.restartTimer();
		this.deps.requestRender();
	}

	/** While working with animations on, the line moves every frame; off, it starts no timer and moves with pi's own repaints. */
	private restartTimer(): void {
		const motion = this.motion;
		if (motion.timer !== undefined) clearInterval(motion.timer);
		motion.timer = undefined;
		motion.tick = 0;
		if (!this.working || motion.policy === "potato") return;
		motion.timer = setInterval(() => { motion.tick += 1; this.deps.requestRender(); }, SHELL_PULSE_MS);
		motion.timer.unref?.();
	}

	override handleInput(data: string): void {
		if (!this.keys.matches(data, "app.interrupt")) this.esc.other();
		// The selection keys come first; an extension's own shortcut still wins its chord.
		this.selection.input(data, (key) => this.handleKey(key), (key) => this.onExtensionShortcut?.(key) === true);
	}

	private handleKey(data: string): void {
		if (!this.keys.matches(data, "app.interrupt")) { super.handleInput(data); return; }
		const text = this.getText();
		const outcome = this.esc.press({ working: this.working, completing: this.isShowingAutocomplete(), doubleEsc: this.deps.doubleEscCancelEnabled(), text });
		if (outcome === "stop") { this.stopTurn(data); return; }
		if (outcome === "clear") {
			this.addToHistory(text);
			this.setText("");
			this.deps.requestRender();
			return;
		}
		if (outcome === "arm") { this.repaintAfter(this.esc.window()); this.deps.requestRender(); return; }
		super.handleInput(data);
	}

	/** pi stops the turn and puts its queued messages back ahead of the draft; the draft stays, the queue goes out as the next turn. */
	private stopTurn(data: string): void {
		const typed = this.getText();
		super.handleInput(data);
		// What pi restored, less the owner's own draft, is the queue it gave back.
		const restored = this.getText();
		const queued = extractQueuedText(restored, typed);
		if (!queued) return;
		this.setText(typed);
		if (queued.trim()) this.deps.dispatchQueuedText(queued);
	}

	/** One repaint just after the window closes, so its hint leaves the screen. */
	private repaintAfter(ms: number): void {
		this.hintRepaint?.();
		const schedule = this.deps.schedule ?? ((fn: () => void, wait: number) => {
			const timer = setTimeout(fn, wait);
			timer.unref?.();
			return () => clearTimeout(timer);
		});
		this.hintRepaint = schedule(() => { this.hintRepaint = undefined; this.deps.requestRender(); }, ms + 1);
	}

	private clearTopHover(): void {
		if (this.top.hovered === undefined) return;
		delete this.top.hovered;
		this.deps.requestRender();
	}

	handleMouse(event: Parameters<CustomEditor["handleMouse"]>[0]): ReturnType<CustomEditor["handleMouse"]> {
		const moving = event.type === "move" && event.button === "none";
		if (event.y < this.top.count) {
			// The top rows are the shell's controls: hover, then a click opens the row's view.
			if (moving) {
				setPaneHover(this.host, this, () => this.clearTopHover());
				if (this.top.hovered === event.y) return { handled: true };
				this.top.hovered = event.y;
				this.deps.requestRender();
				return { handled: true, render: true };
			}
			// pi makes a click only for the component that took its press.
			if (event.type === "press" && event.button === "left") return { handled: true };
			if (event.type !== "click" || event.button !== "left") return undefined;
			this.deps.onTopRowClick?.(event.y);
			return { handled: true, render: true };
		}
		if (moving) setPaneHover(this.host);
		this.clearTopHover();
		// The working line takes no pointer; below it, pi's rows past the `❯` lead.
		if (event.y < this.shellRows || event.x < PROMPT_INDENT) return undefined;
		return super.handleMouse({ ...event, y: event.y - this.shellRows, height: event.height - this.shellRows, x: event.x - PROMPT_INDENT, width: Math.max(1, event.width - PROMPT_INDENT) });
	}

	render(width: number): string[] {
		const ascii = this.deps.ascii ? this.deps.ascii() : shellSettings().ascii();
		const theme = { fg: this.deps.fg, bold: this.deps.bold, ...(this.deps.italic ? { italic: this.deps.italic } : {}) };
		const inner = Math.max(1, width - PROMPT_INDENT);
		const rows = this.selection.paint(super.render(inner), inner);
		const text = this.getText();
		// pi's own count of the input rows it just drew; without it, every row between the rules.
		const inputRows = drawnInputRows(this, rows.length) ?? Math.max(0, rows.length - 2);
		if (text === "" && inputRows === 1) rows[1] = withPlaceholder(rows[1]!, PROMPT_HINT, (hint) => this.deps.fg("dim", hint));
		const state = !this.working ? PROMPT_STATE.IDLE : this.deps.pending() ? PROMPT_STATE.QUEUED : PROMPT_STATE.WORKING;
		const hint = this.esc.hint(this.working, text);
		const line = renderWorkingLine(width, {
			state,
			tick: this.motion.tick,
			animate: this.motion.policy !== "potato",
			elapsedMs: this.deps.now() - this.startedAt,
			activity: this.working ? this.deps.activity?.() : undefined,
			wait: this.working ? this.deps.workingLabel?.() : undefined,
			escHint: hint === "stop" ? DOUBLE_ESC_CANCEL_HINT : hint === "clear" ? IDLE_ESC_CLEAR_HINT : undefined,
			doubleEsc: this.deps.doubleEscCancelEnabled(),
			ascii,
		}, theme);
		const framed = framePrompt(width, {
			rows,
			inputRows,
			border: (rule) => this.borderColor(rule),
			theme,
			ascii,
			decorateBottom: (rule) => this.selection.hint(rule, width, this.borderColor(glyph("rule", ascii))),
		});
		const above = this.deps.topRows?.(width, this.top.hovered) ?? [];
		if ((this.top.hovered ?? -1) >= above.length) delete this.top.hovered;
		this.top.count = above.length;
		this.shellRows = above.length + 1;
		return [...above, line, ...framed];
	}

	dispose(): void {
		if (this.motion.timer !== undefined) clearInterval(this.motion.timer);
		this.motion.timer = undefined;
		this.hintRepaint?.();
		this.hintRepaint = undefined;
	}
}

/**
 * Whether an editor factory is the shell's. The mark is a global symbol, so
 * a factory made by an earlier load of this module (pi reloads extensions)
 * still counts as the shell's; no name or class is trusted for it.
 */
const SHELL_PROMPT = Symbol.for("jhon-shell.prompt-owner");
type EditorFactory = NonNullable<ReturnType<ExtensionContext["ui"]["getEditorComponent"]>>;
type ShellPromptFactory = EditorFactory & { [SHELL_PROMPT]?: boolean };
const isShellPrompt = (factory: unknown): boolean => typeof factory === "function" && (factory as ShellPromptFactory)[SHELL_PROMPT] === true;

/**
 * The narrow form of a full-screen terminal (04): no right pane (under 140
 * columns, Status hidden, or a pi-tui without the layout hook): the Running
 * block sits above the editor rule.
 */
function narrowChrome(tui: TUI): boolean {
	if ((tui as TUI & { mode?: string }).mode !== "fullscreen" || !tui.terminal) return false;
	return !paneState(tui).wide?.();
}

/** What the shell's prompt needs from the session, beyond pi's theme and the context. */
type PromptWiring = PromptSession & {
	topRows?: (tui: TUI, width: number, hovered?: number) => string[];
	onTopRowClick?: (tui: TUI, index: number) => void;
};

/**
 * Makes the shell's prompt pi's editor, unless another extension's editor is
 * there: that one stays and the shell draws no prompt. A factory of the
 * shell's own (this load's or an earlier one's) is replaced, so the new
 * session's handlers own the prompt. `created` gets each editor pi builds.
 */
function claimPrompt(ctx: ExtensionContext, created: (prompt: JhonPromptEditor) => void, wiring: PromptWiring): boolean {
	const current = ctx.ui.getEditorComponent();
	if (current && !isShellPrompt(current)) return false;
	const ui = ctx.ui;
	const build = ((tui: TUI, theme: EditorTheme, keybindings: KeybindingsManager) => {
		const prompt = new JhonPromptEditor(tui, theme, keybindings, {
			fg: (color, text) => ui.theme.fg(color as Parameters<typeof ui.theme.fg>[0], text),
			bold: (text) => ui.theme.bold(text),
			italic: (text) => (typeof ui.theme.italic === "function" ? ui.theme.italic(text) : text),
			requestRender: () => tui.requestRender(),
			pending: () => ctx.hasPendingMessages(),
			now: wiring.now,
			doubleEscCancelEnabled: wiring.doubleEscCancelEnabled,
			dispatchQueuedText: wiring.dispatchQueuedText,
			workingLabel: wiring.workingLabel,
			activity: wiring.activity,
			topRows: wiring.topRows ? (width, hovered) => wiring.topRows!(tui, width, hovered) : undefined,
			onTopRowClick: wiring.onTopRowClick ? (index) => wiring.onTopRowClick!(tui, index) : undefined,
		});
		created(prompt);
		return prompt;
	}) as ShellPromptFactory;
	build[SHELL_PROMPT] = true;
	ui.setEditorComponent(build);
	return true;
}

/** The widget above the editor that carries the top row and the strip where the pane host does not (lib/ui/pane.ts). */
const CHROME_WIDGET = "jhon-chrome";

/** Settings is Customize (L174): /jhon:customize opens it; /jhon:settings is gone (L195). */
const SETTINGS_COMMAND_NAME = "jhon:customize";
/** The groups that left the shell's Customize (L96), and where each lives now. */
const SETTINGS_MOVED: Record<string, string> = {
	runtimes: "The runtimes are set in `jhon setup` (or `jhon settings set runtimes.<name>`).",
	integrations: "The integrations are set in `jhon setup` (or `jhon settings set integrations.<name>`).",
	palette: "The palette, Rose Ink or Desktop, is set in `jhon setup` (or `jhon settings set integrations.theme.palette`).",
	profile: "The model profile is chosen in /jhon:profile.",
};

export default function jhonShellUi(pi: ExtensionAPI, env: NodeJS.ProcessEnv = process.env, overrides: Partial<ShellDeps> = {}): void {
	// Its commands and shortcuts are counted (lib/ui-use.ts).
	pi = counted(pi);
	// The change capture runs with the shell off too; its resolver defaults to the shared one.
	installSessionChangeCapture(pi, env, overrides.resolveWorktree);
	if (!shellEnabled(env)) return;
	// T120: the session's own profile, its branch's at the start and then as extensions/jhon.ts says it.
	let sessionProfile: string | undefined;
	const profileReader = createActiveProfileReader(activeProfileName, () => sessionProfile);
	const deps = shellDeps(profileReader, overrides);
	let closeSettings: (() => void) | undefined;
	let renderHost: Repaint | undefined;
	// The agent dir `jhon start pi` sets: Customize's Rose ink theme is written there.
	const agentDir = env.PI_CODING_AGENT_DIR;
	// A host announced late (jhon:host) may drop the top row's path: redraw.
	pi.events.on(HOST_EVENT, () => { renderHost?.requestRender(); });
	// What the session's sandbox pin means for the commands (lib/sandbox.ts sandboxShown), for the
	// status bar: on, off, or not running and why (L1489). An emitter that names the mode alone says on or off.
	let sandbox: BarModel["sandbox"];
	let sandboxWhy: string | undefined;
	pi.events.on(SANDBOX_EVENT, (payload) => {
		const said = payload as { mode?: unknown; state?: unknown; why?: unknown } | undefined;
		const state = said?.state ?? said?.mode;
		sandbox = state === "on" || state === "off" || state === "not running" ? state : undefined;
		sandboxWhy = sandbox === "not running" && typeof said?.why === "string" && said.why ? said.why : undefined;
		renderHost?.invalidatePane?.();
		renderHost?.requestRender();
	});
	// T120: `u` in the profiles panel, or a session profile that is gone: the bar and the rail show it at once.
	pi.events.on(SESSION_PROFILE_CHANGED, (payload) => {
		const said = payload as { sessionId?: unknown; profile?: unknown } | undefined;
		let current: string | undefined;
		try { current = currentContext?.sessionManager.getSessionId(); } catch { current = undefined; }
		if (!said || current === undefined || said.sessionId !== current) return;
		sessionProfile = typeof said.profile === "string" && said.profile ? said.profile : undefined;
		if (!profileReader.refresh()) return;
		renderHost?.invalidatePane?.();
		renderHost?.requestRender();
	});
	let prompt: JhonPromptEditor | undefined;
	// What waits to be sent once the run an Esc (or the stall recovery) stopped
	// has settled: the messages pi had queued, given back by the prompt, and
	// the stall's continue. Sent once, as one message, from agent_settled;
	// texts that arrive before it are kept in order, one blank line apart, as
	// pi joins its own queue. A session's text never reaches the next one.
	const afterStop = {
		text: undefined as string | undefined,
		add(next: string) { this.text = this.text === undefined ? next : `${this.text}\n\n${next}`; },
		take(): string | undefined { const text = this.text; this.text = undefined; return text; },
	};
	// The owner's settings (lib/settings.ts): read once here and again at each
	// later session start and each Customize open; every write is a `jhon
	// settings set`. The preferences below come from them.
	const settings = shellSettings(env);
	let settingsLoadedOnce = false;
	const fromSettings = { settings };
	let animationPolicy = resolveAnimationPolicy(fromSettings).policy;
	let visualSettings = resolveVisualSettings(fromSettings).settings;
	// The card style and the chrome surface (L153 answers 1-2) sit in
	// process-wide slots that every extension's cards and the pane read; the
	// saved values fill them at each session start and on each change.
	const applyAppearance = () => {
		const { surface } = visualSettings;
		setCardStyle(settings.cards());
		setChromeSurface(surface);
	};
	const refreshVisual = () => {
		visualSettings = resolveVisualSettings(fromSettings).settings;
		applyAppearance();
		renderHost?.invalidatePane?.();
		renderHost?.requestRender();
	};
	// The Esc gate's policy is kept here, not read on every key; Customize's row sets it again.
	let doubleEscCancelPolicy: DoubleEscCancelPolicy = loadDoubleEscCancelPolicy({ settings });
	const apiWait = createApiWait(() => deps.now());
	const activity = createWorkingActivity();
	// Stall recovery (lib/ui/stall-recovery.ts): past the threshold the turn is
	// aborted as Esc does, and the continue rides the queued-text path above,
	// sent once the aborted run settles.
	const queueAfterSettle = (text: string) => afterStop.add(text);
	// A thinking model or the claude-bridge provider waits thinking_stall_seconds (T38, L382).
	const stallSecs = { stall: stallSeconds(env, settings), thinking: settings.thinkingStallSeconds() };
	// The continue texts the shell sent: a follow-up carrying one is not an owner message.
	const stallContinues = new Set<string>();
	let openPrompts = 0;
	const stall = createStallRecovery({
		wait: apiWait,
		seconds: () => stallThreshold(currentContext?.model, pi.getThinkingLevel(), stallSecs),
		schedule: deps.schedule ?? ((fn, ms) => { const timer = setTimeout(fn, ms); timer.unref?.(); return () => clearTimeout(timer); }),
		held: () => openPrompts > 0,
		recover: (seconds) => {
			const ctx = currentContext;
			if (!ctx) return;
			try { ctx.abort(); } catch { return; }
			const text = stallContinueText(seconds);
			stallContinues.add(text);
			queueAfterSettle(text);
			if (ctx.hasUI) ctx.ui.notify(stallNotice(seconds), "warning");
		},
	});
	let registry: SessionWorktreeRegistry | undefined;
	let currentContext: ExtensionContext | undefined;
	// The subscription usage (T174-S5b): the status bar's windows and /jhon:usage, alt+u.
	const usage = installUsage(pi, env, {
		settings, now: () => deps.now(), fetch: deps.fetch, current: () => currentContext,
		repaint: () => { renderHost?.invalidatePane?.(); renderHost?.requestRender(); },
		// T149: a session on its own profile runs its sub-agents on that profile's models.
		sessionProfile: () => sessionProfile,
		...(deps.profilesJson ? { profilesJson: deps.profilesJson } : {}),
	});
	// This session's background tasks (lib/background-tasks-tools.ts): those
	// running, and those that ended in the last minute, which the pane keeps.
	let shownJobs: RunningJobDisplay[] | undefined;
	/** Something runs, or ended less than a minute ago: the clock keeps the pane's times and lingering rows live. */
	const jobsLive = () => (shownJobs ?? []).some((job) => job.endedAt === undefined || deps.now() - job.endedAt < JOBS_FINISHED_TTL_MS);
	let jobClock: ReturnType<typeof setInterval> | undefined;
	/** Opens the agents view or the jobs view (lib/background-tasks-tools.ts). */
	const openTasks = (ctx: ExtensionContext, view: "agents" | "jobs") => { pi.events.emit(OPEN_TASKS_EVENT, { ctx, view }); };
	const displays = (items: unknown): RunningJobDisplay[] => Array.isArray(items)
		? items.filter((item): item is RunningJobDisplay => typeof item?.id === "string" && typeof item.label === "string" && typeof item.startedAt === "number")
		: [];
	pi.events.on(TASKS_EVENT, (value) => {
		const event = value as { sessionId?: unknown; tasks?: unknown; finished?: unknown } | undefined;
		if (!currentContext || event?.sessionId !== currentContext.sessionManager.getSessionId() || !Array.isArray(event.tasks)) return;
		const finished = displays(event.finished).filter((job) => typeof job.endedAt === "number");
		shownJobs = [...displays(event.tasks).filter((job) => job.endedAt === undefined), ...finished];
		// The elapsed times run live: one redraw a second while a task runs or a
		// finished one still shows, none when idle.
		if (jobsLive() && !jobClock) {
			jobClock = setInterval(() => {
				if (!jobsLive() && jobClock) { clearInterval(jobClock); jobClock = undefined; }
				renderHost?.invalidatePane?.();
				renderHost?.requestRender();
			}, 1000);
			jobClock.unref?.();
		} else if (!jobsLive() && jobClock) {
			clearInterval(jobClock);
			jobClock = undefined;
		}
		renderHost?.invalidatePane?.();
		renderHost?.requestRender();
	});
	// A worktree added by hand: the LDD section reads its change too. The
	// registry is the current session's (made at its start), never another's.
	const WORKTREE_TOOL = {
		name: "session_worktree_register",
		label: "Add a session worktree",
		description: "Add a worktree of this Git clone to the session: the LDD section reads the change there too.",
		parameters: { type: "object", required: ["path"], additionalProperties: false, properties: { path: { type: "string", description: "Worktree path to include in this session." } } },
	};
	pi.registerTool({
		...WORKTREE_TOOL,
		renderShell: "self",
		parameters: WORKTREE_TOOL.parameters as never,
		async execute(_id, params, _signal, _onUpdate, ctx) {
			const list = registry;
			if (list === undefined || list.sessionId !== ctx.sessionManager.getSessionId()) throw new Error("This session has no worktree list yet.");
			const root = list.register((params as { path: string }).path, "explicit");
			return { content: [{ type: "text", text: `${root} is now one of this session's worktrees.` }], details: { root, sessionId: list.sessionId } };
		},
	});
	// The Status card's LDD change: read at the start and after each turn,
	// never in the render path. It is the session's one selection: the Change
	// panel reads it from the shared slot, refreshed, never selecting its own
	// (shareSelection, T29).
	let ldd: LddStatus | undefined;
	// The owner's permission mode (S31), from the same reads: the status line's badge.
	let mode: string | undefined;
	let releaseSelection: (() => void) | undefined;
	// Another session can move the change on (a jhon-builder, a Claude Code
	// session): STATUS re-reads it at each turn's end too, at most every 30 s,
	// since jhon status computes the route from the diff (~2 s in a large repo).
	let lddReadAt = -Infinity;
	// STATUS follows its changes as they move, outside the agent's turns too:
	// for pi's folder, the shown change's worktree, and each lane's, a branch
	// switched (git's HEAD), a change created or archived (openspec/changes),
	// and a task checked off (the change's directory, watched whole since
	// editors replace tasks.md rather than write it) (lddWatchTargets). The
	// watches follow what STATUS shows after each read, and a re-read every
	// minute covers the session's own change. A multiplexer's sidebar may poll; without
	// this, STATUS kept an archived change until the next turn.
	// Each read of a lane costs a `jhon status`: a lane is read again only
	// when its watches fired since (lanes.moved), it was never read, the list
	// of worktrees changed, or every lane is read again (LANE_REREAD_MS, for
	// what no watch saw) (lddLanes); the session's own change is read at every
	// refresh. A watch that cannot be made or that dies is dropped, to be made
	// again at the next refresh, and its roots read again there.
	const lddWatches = new Map<string, { close(): void; roots: string[] }>();
	let lanes = laneReads();
	let lddWatching = false;
	let tasksTimer: ReturnType<typeof setTimeout> | undefined;
	let lddPoll: ReturnType<typeof setInterval> | undefined;
	const realRoot = (p: string) => { try { return realpathSync(p); } catch { return p; } };
	const syncLddWatches = (ctx: ExtensionContext, targets: Array<[string, string | undefined, string[]]>, since: number) => {
		if (!lddWatching) return;
		const wanted = new Map(targets.map(([dir, only, roots]) => [`${dir}\0${only ?? ""}`, [dir, only, roots.map(realRoot)] as const]));
		for (const [key, w] of lddWatches) if (!wanted.has(key)) { w.close(); lddWatches.delete(key); }
		const soon = () => {
			if (tasksTimer) clearTimeout(tasksTimer);
			tasksTimer = setTimeout(() => { if (currentContext === ctx) refreshLdd(ctx); }, 300);
		};
		for (const [key, [dir, only, roots]] of wanted) {
			const known = lddWatches.get(key);
			if (known) { known.roots = roots; continue; }
			const entry = { close: () => {}, roots };
			const moved = () => { for (const r of entry.roots) lanes.moved.add(r); soon(); };
			// Without a watch its roots are read at the next refresh, which makes
			// it again: not sooner, or a watch that keeps failing reads in a loop.
			const lost = () => {
				if (lddWatches.get(key) === entry) lddWatches.delete(key);
				entry.close();
				for (const r of entry.roots) lanes.moved.add(r);
			};
			try {
				const watcher = (deps.watch ?? watch)(dir, { persistent: false }, (_event, name) => {
					if (only && name !== null && String(name) !== only) return;
					moved();
				});
				entry.close = () => { entry.close = () => {}; try { watcher.close(); } catch { /* Closed already. */ } };
				watcher.on("error", lost);
				lddWatches.set(key, entry);
				// What moved between the read and this watch, no watch saw.
				try { if (statSync(only ? join(dir, only) : dir).mtimeMs > since) moved(); } catch { /* Gone: its root's next read says so. */ }
			} catch { lost(); }
		}
	};
	const stopLddSources = () => {
		lddWatching = false;
		for (const w of lddWatches.values()) w.close();
		lddWatches.clear();
		if (tasksTimer) { clearTimeout(tasksTimer); tasksTimer = undefined; }
		if (lddPoll) { clearInterval(lddPoll); lddPoll = undefined; }
	};
	const watchLddSources = (ctx: ExtensionContext) => {
		stopLddSources();
		lddWatching = true;
		syncLddWatches(ctx, lddWatchTargets(ctx.cwd, ldd), Date.now());
		lddPoll = setInterval(() => { if (currentContext === ctx) refreshLdd(ctx); }, 60_000);
		lddPoll.unref?.();
	};
	// The session's folder first; when it selects no change (pi on main, the
	// work in a worktree), the worktrees the session worked in, the latest
	// first, and never another worktree's change in their place (lddOfSession).
	const lddOf = async (ctx: ExtensionContext, read: (cwd: string) => Promise<LddStatus | undefined>): Promise<LddStatus | undefined> => {
		let roots: string[] = [];
		try { roots = recentSessionRoots(ctx.sessionManager.getSessionId(), ctx.sessionManager.getEntries()); } catch { /* No session entries: the folder alone. */ }
		return lddOfSession(ctx.cwd, roots, read, deps.rootExists);
	};
	// The selection it computed, with its lanes; on a read that fails, the one shown.
	const refreshLdd = (ctx: ExtensionContext): Promise<LddStatus | undefined> => {
		lddReadAt = deps.now();
		const since = Date.now();
		// The folders the session's own change was read from are read once per
		// refresh: as lanes, they take that read (moved), not a second one.
		const fresh = new Map<string, Promise<LddStatus | undefined>>();
		const readOnce = (dir: string) => {
			const key = realRoot(dir);
			if (!fresh.has(key)) fresh.set(key, deps.lddStatus(dir));
			return fresh.get(key)!;
		};
		const reads = lanes;
		// The lanes beside it: the changes the repository's other worktrees select.
		return lddOf(ctx, readOnce).then(async (own) => {
			for (const key of fresh.keys()) reads.moved.add(key);
			const found = own ? await lddLanes(ctx.cwd, own, readOnce, reads, deps.now()).catch(() => []) : [];
			const status = own && found.length ? { ...own, lanes: found } : own;
			if (currentContext !== ctx) return status;
			// The worktrees no lane stands for (none selected, or a change shown
			// already) are watched too, for their next read.
			const laneRoots = new Set(found.map((l) => realRoot(l.root ?? "")));
			const others = [...reads.read.keys()].filter((w) => !laneRoots.has(w));
			syncLddWatches(ctx, lddWatchTargets(ctx.cwd, status, others), since);
			selectChange(ctx.sessionManager.getSessionId(), status);
			const read = deps.permissionMode?.(ctx.cwd);
			const modeMoved = read !== mode;
			mode = read;
			if (!modeMoved && JSON.stringify(status) === JSON.stringify(ldd)) return status;
			ldd = status;
			renderHost?.invalidatePane?.();
			renderHost?.requestRender();
			return status;
		}, () => ldd);
	};
	pi.on("turn_end", (_event, ctx) => {
		if (ctx.hasUI && currentContext === ctx && deps.now() - lddReadAt >= 30_000) refreshLdd(ctx);
	});
	pi.on("session_start", async (_event, ctx) => {
		closeSettings?.();
		shownJobs = undefined;
		profileReader.stop();
		registry?.close();
		currentContext = ctx;
		const worktrees = new SessionWorktreeRegistry(pi, ctx.sessionManager, ctx.cwd, deps.resolveWorktree);
		registry = worktrees;
		worktrees.start();
		if (settingsLoadedOnce) settings.refresh();
		settingsLoadedOnce = true;
		if (!ctx.hasUI) return;
		// Retired keys (T15.3) are dropped once, backed up, and the owner told what went.
		if (settings.retired.length) {
			const retired = settings.retired;
			const result = settings.migrateRetired();
			if (result.ok) { if (retired.some((r) => !r.silent)) ctx.ui.notify(retiredNotice(retired, result.lines), "warning"); }
			else ctx.ui.notify(`Settings: the retired keys could not be dropped: ${result.error}. \`jhon settings migrate\` drops them.`, "warning");
		}
		visualSettings = resolveVisualSettings(fromSettings).settings;
		applyAppearance();
		if (!overrides.activeProfile) {
			try { sessionProfile = sessionProfileOnBranch((ctx.sessionManager as { getBranch?(): unknown[] }).getBranch?.()); } catch { sessionProfile = undefined; }
			// Polled: the owner may switch profiles from another terminal. A poll that
			// outlives its session (another started, or the id moved) stays quiet.
			const owner = ctx.sessionManager.getSessionId();
			const live = () => currentContext === ctx && ctx.sessionManager.getSessionId() === owner;
			profileReader.follow(live, () => { renderHost?.invalidatePane?.(); renderHost?.requestRender(); });
		}
		lanes = laneReads();
		releaseSelection?.();
		releaseSelection = shareSelection(ctx.sessionManager.getSessionId(), () => currentContext === ctx ? refreshLdd(ctx) : Promise.resolve(ldd));
		void refreshLdd(ctx);
		if (ctx.hasUI) watchLddSources(ctx);
		ctx.ui.setFooter((tui, theme, footerData) => {
			const host: Repaint = { requestRender: () => tui.requestRender(), invalidatePane: () => invalidatePane(tui) };
			renderHost = host;
			const unsubscribeBranch = footerData.onBranchChange(() => {
				host.invalidatePane?.();
				host.requestRender();
			});
			// The bars paint live session state no event re-registers a part for:
			// model, effort, context, cost, profile, mode, sandbox. Their digests
			// keep the fullscreen memo honest.
			const bars = (): BarModel => barModel(pi, ctx, footerData, {
				...(deps.activeProfile() ? { profile: deps.activeProfile() } : {}),
				...(sandbox ? { sandbox } : {}),
				...(sandboxWhy ? { sandboxWhy } : {}),
				...(sessionChangedFiles(ctx) ? { dirty: sessionChangedFiles(ctx) } : {}),
				...(mode ? { mode } : {}),
				...(hostShowsPath() ? { hostShowsPath: true } : {}),
				...(usage.bar(ctx).length ? { usage: usage.bar(ctx) } : {}),
			});
			// Design 01 and 04 (T174-S3, S4): the top row above everything and the
			// status bar at the bottom, one row each; the status bar takes its
			// narrow form under 140 columns. Where the pane host lays the terminal
			// out (lib/ui/pane.ts), both are its rows and the footer steps aside;
			// elsewhere (a regular terminal, a pi-tui without the layout hook) the
			// footer is the status bar and the chrome widget the top row and strip.
			// The pane's LDD section (01, 02): the change, its slices, the last run;
			// its digest moves each minute for the run's age.
			// A click on a section's title folds it; the pointer over its title (and
			// over Agents' and Jobs' rows) paints it (lib/ui/pane.ts foldablePart).
			const disposeLdd = paneSlot(tui, "ldd", foldablePart(tui, {
				digest: () => JSON.stringify([ldd, visualSettings, settings.ascii(), Math.floor(deps.now() / 60_000)]),
				render: (width, view) => renderLddPanel(ldd, theme, width, { now: deps.now(), ascii: settings.ascii(), ...view }),
				// Without a change its one dim line is no title: nothing to fold.
				foldable: () => ldd !== undefined,
				// The narrow form's strip (04): the change, its slice, the next step, the last run.
				strip: (width) => ldd ? renderStrip(stripModel(ldd, deps.now(), settings.string("shell.keys.panel")), theme, width, { ascii: settings.ascii() }) : undefined,
			}));
			// Agents and Jobs, each its own section while something runs or ended in
			// the last minute (01); a click on one of its rows opens its view
			// (/jhon:agents, /jhon:jobs).
			const workPart = (key: "agents" | "jobs", render: typeof renderJobsSection) => paneSlot(tui, key, foldablePart(tui, {
				digest: () => JSON.stringify([shownJobs, settings.ascii(), jobsLive() ? Math.floor(deps.now() / 1000) : 0]),
				render: (width, view) => render(shownJobs, theme, width, { now: deps.now(), ascii: settings.ascii(), ...view }),
				rows: true,
				open: () => { openTasks(ctx, key); return true; },
			}));
			const disposeAgents = workPart("agents", renderAgentsSection);
			const disposeJobs = workPart("jobs", renderJobsSection);
			// The status bar spans the width under the rail (L97); the rail has no
			// key line, "/ commands" is the prompt's alone (L143).
			const disposeStatusbar = paneSlot(tui, "statusbar", {
				digest: () => JSON.stringify([bars(), visualSettings, tui.terminal?.columns]),
				render: (width) => visualSettings.statusPlacement === "hidden" ? [] : [renderStatusBar(bars(), theme, width)],
				invalidate() {},
			});
			// The top row: the project's name and its path, nothing else.
			const disposeHeader = paneSlot(tui, "header", {
				digest: () => JSON.stringify([ctx.sessionManager.getCwd(), hostShowsPath(), visualSettings, tui.terminal?.columns]),
				render: (width) => [renderTopRow(bars(), theme, width)],
				invalidate() {},
			});
			const uninstall = installPane(tui, theme, {
				placement: () => visualSettings.statusPlacement,
				density: () => visualSettings.density,
				ascii: () => settings.ascii(),
				note: (text) => ctx.ui.notify(text, "warning"),
			});
			return {
				render(width: number) {
					if (paneState(tui).hosted?.()) return [];
					if (visualSettings.statusPlacement !== "hidden") return [renderStatusBar(bars(), theme, width)];
					const running = (shownJobs ?? []).filter((job) => job.endedAt === undefined).length;
					if ((tui as TUI & { mode?: string }).mode !== "fullscreen" && running) {
						return [truncateToWidth(theme.fg("muted", shellJobsCount(running)), width, "…")];
					}
					return [];
				},
				invalidate() {},
				dispose() { disposeHeader(); disposeStatusbar(); disposeLdd(); uninstall(); disposeJobs(); disposeAgents(); unsubscribeBranch(); },
			};
		});
		// The narrow form through pi's public slots (04, L1489): where the pane host
		// does not lay the terminal out (a regular terminal, a pi-tui without the
		// layout hook), the top row and the strip sit above the editor, the status
		// bar under it in the footer. Hosted, the host draws them and this is empty.
		ctx.ui.setWidget(CHROME_WIDGET, (tui) => ({
			render: (width: number) => {
				const state = paneState(tui);
				if (state.hosted?.()) return [];
				const top = state.parts.get("header")?.render(width) ?? [];
				const strip = visualSettings.statusPlacement === "hidden" ? undefined : state.parts.get("ldd")?.strip?.(width);
				return strip ? [...top, strip] : top;
			},
			invalidate() {},
		}));
		// Each editor pi builds replaces the last one, with the session's animation policy.
		const adopt = (built: JhonPromptEditor) => {
			prompt?.dispose();
			prompt = built;
			built.setAnimationPolicy(animationPolicy);
		};
		const wiring: PromptWiring = {
			now: () => deps.now(),
			doubleEscCancelEnabled: () => doubleEscCancelPolicy === "on",
			dispatchQueuedText: queueAfterSettle,
			workingLabel: () => apiWait.label(),
			activity: () => activity.current(),
			// The narrow form's Running block (04): the agents and jobs while they run.
			topRows: (tui, width, hovered) => {
				if (!narrowChrome(tui)) return [];
				return renderRunningBlock(shownJobs, ctx.ui.theme, width, { now: deps.now(), ascii: shellSettings().ascii(), ...(hovered !== undefined ? { hover: hovered } : {}) }).rows;
			},
			// A click on one of its rows opens the agents view or the jobs view.
			onTopRowClick: (tui, index) => {
				if (!narrowChrome(tui)) return;
				const view = renderRunningBlock(shownJobs, ctx.ui.theme, tui.terminal?.columns ?? 0, { now: deps.now() }).opens[index];
				if (view) openTasks(ctx, view);
			},
		};
		// The working line says what pi's Working row would, so that row hides; the thinking blocks stay pi's.
		if (claimPrompt(ctx, adopt, wiring)) ctx.ui.setWorkingVisible(false);
	});
	pi.on("session_shutdown", (_event, ctx) => {
		closeSettings?.();
		shownJobs = undefined;
		if (jobClock) { clearInterval(jobClock); jobClock = undefined; }
		stopLddSources();
		releaseSelection?.();
		releaseSelection = undefined;
		profileReader.stop();
		afterStop.take();
		prompt?.dispose();
		prompt = undefined;
		// pi's own editor and Working row come back, unless the editor is another extension's.
		if (isShellPrompt(ctx.ui.getEditorComponent())) {
			ctx.ui.setEditorComponent(undefined);
			ctx.ui.setWorkingVisible(true);
		}
		registry?.close();
		registry = undefined;
		currentContext = undefined;
		apiWait.end();
		openPrompts = 0;
		stall.sync();
	});
	// Customize (T15.3; called Settings until L174): one overlay with every
	// setting the shell keeps, by group; `/jhon:customize <group>` opens it on
	// that group. Its groups, words and rows are lib/ui/customize/registry.ts's
	// (T15.15). It took the place of /jhon:animations and /jhon:double-esc-cancel.
	/** The jhon binary, as the environment names it when Customize runs. */
	const jhonBin = () => env.JHON_BIN || "jhon";

	/** `jhon mode <args>` in the folder `cwd`: its output, or its error text (L175). */
	const jhonMode = (args: string[], cwd: string) => new Promise<string>((resolve, reject) => {
		execFile(jhonBin(), ["mode", ...args], { cwd, encoding: "utf8" }, (error, stdout, stderr) => {
			if (error) reject(new Error(`${stderr || stdout || error.message}`.trim()));
			else resolve(stdout);
		});
	});

	/**
	 * The Permission mode row (L214, its only place): read with `jhon mode
	 * --json`, set through changeMode, which keeps its rules and asks Yes or No
	 * before super-yolo. The grants are in /jhon:grants.
	 */
	const modeRow = (ctx: ExtensionContext) => {
		let known: ModeData | undefined;
		const read = async () => { known = JSON.parse(await jhonMode(["--json"], ctx.cwd)) as ModeData; return known; };
		read().then(() => renderHost?.requestRender(), () => undefined);
		const setMode = async (next: string, root: string) => { await jhonMode([next, "--root", root], ctx.cwd); };
		const confirmMode = async (question: string) => (await ctx.ui.select?.(question, ["Yes", "No"])) === "Yes";
		return {
			value: () => known?.mode ?? deps.permissionMode?.(ctx.cwd) ?? "normal",
			set: async (next: string) => {
				try {
					const outcome = await changeMode(next, { setMode, confirmMode }, known ?? await read());
					if (outcome === "declined") ctx.ui.notify("Cancelled: nothing changed.", "info");
					if (outcome === "set") { await read(); void refreshLdd(ctx); }
					return undefined;
				} catch (error) {
					return `could not set ${next}: ${error instanceof Error ? error.message : String(error)}`;
				}
			},
		};
	};

	/**
	 * The Themes row (L197): pi lists the themes, applies one and saves it.
	 * Rose ink is written again with `jhon palette` first, as followPalette
	 * does, so switching back to it never applies an old file.
	 */
	const themeRow = (ctx: ExtensionContext) => {
		const apply = (name: string) => {
			const result = ctx.ui.setTheme(name);
			return result.success ? undefined : result.error ?? `could not apply ${name}`;
		};
		return {
			list: () => ctx.ui.getAllThemes(),
			current: () => ctx.ui.theme.name,
			set: (name: string) => {
				if (name !== ROSE_INK || !agentDir) return apply(name);
				switchToRoseInk({
					jhon: jhonBin(), agentDir, colors: PALETTE_PATH(env),
					apply: () => { const refused = apply(name); renderHost?.requestRender(); return refused; },
					notify: (message) => ctx.ui.notify(`Customize: ${message}`, "error"),
				});
				return undefined;
			},
		};
	};

	const repaintPane = () => { renderHost?.invalidatePane?.(); renderHost?.requestRender(); };
	const visualChanged = () => { refreshVisual(); pi.events.emit(VISUAL_SETTINGS_CHANGED, { settings: settings.path }); };

	type CommandContext = Parameters<Parameters<ExtensionAPI["registerCommand"]>[1]["handler"]>[1];
	const openCustomize = async (args: string | undefined, ctx: CommandContext) => {
		if (ctx.mode !== "tui" || !ctx.hasUI) {
			if (ctx.hasUI) ctx.ui.notify("Customize needs an interactive terminal; `jhon settings` lists and sets them.", "warning");
			return;
		}
		// A group that left Customize says where it lives now; any other unknown word lists the groups (T15.9).
		const group = String(args ?? "").trim();
		const word = group.toLowerCase();
		if (word && !CUSTOMIZE_WORDS.includes(word)) {
			ctx.ui.notify(SETTINGS_MOVED[word] ?? unknownCategoryNotice(word), "warning");
			return;
		}
		closeSettings?.();
		// Opening reads the settings again: another session or a hand edit may have changed them.
		settings.refresh();
		let shut: (() => void) | undefined;
		let closed = false;
		const close = () => {
			if (closed) return;
			closed = true;
			shut?.();
			if (closeSettings === close) closeSettings = undefined;
		};
		closeSettings = close;
		const rows = customizeRows({
			mode: modeRow(ctx),
			settings,
			animations: (policy) => { animationPolicy = policy; prompt?.setAnimationPolicy(policy); },
			doubleEsc: () => { doubleEscCancelPolicy = loadDoubleEscCancelPolicy({ settings }); },
			status: visualChanged,
			appearance: visualChanged,
			redraw: repaintPane,
			// "Reload now" (L198): Customize closes first, then pi reloads, so the changed keys apply.
			reload: () => {
				close();
				setTimeout(() => {
					ctx.reload().catch((error: unknown) => ctx.ui.notify(`Reload failed: ${error instanceof Error ? error.message : String(error)}`, "error"));
				}, 0);
			},
			themes: themeRow(ctx),
		});
		try {
			await ctx.ui.custom<null>((tui, theme, _keys, done) => {
				// Closing writes every cell again, as every overlay does (overlay.ts).
				const end = closeWithRepaint(tui, done);
				shut = () => end(null);
				if (closed) done(null);
				const view = new SettingsView({
					rows, theme, group: group || undefined, ascii: () => settings.ascii(),
					// 85% of the terminal's rows, less the card's two borders.
					height: () => Math.max(0, Math.floor((tui.terminal?.rows ?? 24) * 0.85) - 2),
					// A value that is refused is a notice: `Customize: <why>`.
					onError: (message) => { if (!closed) ctx.ui.notify(`Customize: ${message}`, "error"); },
					onClose: close,
					requestRender: () => { if (!closed) tui.requestRender(); },
				});
				return {
					render: (width) => (closed ? [] : view.render(width)),
					handleInput: (data) => { if (!closed) view.handleInput(data); },
					handleMouse: (event) => (closed ? undefined : view.handleMouse(event)),
					invalidate: () => view.invalidate(),
					dispose: close,
				};
			}, CARD_OVERLAY);
		} finally {
			close();
		}
	};

	pi.registerCommand(SETTINGS_COMMAND_NAME, {
		description: customizeDescription(),
		getArgumentCompletions: (prefix) => completeWord(customizeCompletions(), prefix),
		handler: openCustomize,
	});
	pi.on("agent_start", (_event, ctx) => {
		// A run can start before the stopped one settled (the owner sent the
		// draft, an extension or a key started one): what waits after a stop is
		// not sent here, mid-run; it goes out once, when this run settles.
		prompt?.setWorking(true);
		activity.start();
		apiWait.start();
		stall.sync();
	});
	// A provider request in flight with no stream event for a minute is named
	// on the working line; a running tool is not a wait on the API. Every
	// change re-arms or clears the stall recovery's timer.
	pi.on("turn_start", () => { apiWait.start(); stall.sync(); });
	pi.on("message_start", () => { apiWait.event(); stall.sync(); });
	pi.on("message_update", (event) => { activity.stream(event.assistantMessageEvent?.type); apiWait.event(); stall.sync(); });
	pi.on("message_end", () => { apiWait.event(); stall.sync(); });
	pi.on("tool_execution_start", (event) => { activity.toolStart(event.toolCallId, event.toolName); apiWait.toolStart(); stall.sync(); });
	pi.on("tool_execution_end", (event) => { activity.toolEnd(event.toolCallId); apiWait.toolEnd(); stall.sync(); });
	pi.on("ui_prompt_start", () => { openPrompts += 1; stall.sync(); });
	pi.on("ui_prompt_end", () => { openPrompts = Math.max(0, openPrompts - 1); stall.sync(); });
	// Recovery runs once per owner message: the continue it sent is not one.
	pi.on("before_agent_start", (event) => {
		if (event.messageType === "user" && !stallContinues.has(event.prompt)) stall.ownerMessage();
		return undefined;
	});
	pi.on("agent_settled", (_event, ctx) => {
		// pi is idle by the time it settles; a run still going keeps the prompt
		// working, and what waits goes out at the settle after it.
		if (!ctx.isIdle()) return;
		prompt?.setWorking(false);
		const waiting = afterStop.take();
		if (waiting === undefined) return;
		try {
			pi.sendUserMessage(waiting);
		} catch (error) {
			// The owner's words are never lost: they go back ahead of the draft, as
			// pi's own restore would have put them, or wait for the next settle
			// when there is no prompt; and the owner is told why.
			if (prompt) prompt.setText([waiting, prompt.getText()].filter((text) => text.trim() !== "").join("\n\n"));
			else afterStop.add(waiting);
			const why = error instanceof Error ? error.message : String(error);
			if (ctx.hasUI) ctx.ui.notify(`Could not send the queued message after cancelling; it went back to the editor: ${why}`, "error");
		}
	});
	pi.on("agent_end", async (_event, ctx) => {
		apiWait.end();
		stall.sync();
		if (ctx.hasUI) refreshLdd(ctx);
	});
}
