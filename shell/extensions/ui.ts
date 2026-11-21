import { CustomEditor, type ExtensionAPI, type ExtensionContext, type KeybindingsManager } from "@earendil-works/pi-coding-agent";
import { isHomeOrBroadUserDirectory } from "../lib/ui/user-directories.ts";
import { truncateToWidth, visibleWidth, type EditorTheme, type TUI } from "@earendil-works/pi-tui";
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
import { glyph, renderCard, setCardStyle, type Card, type CardTheme } from "../lib/ui/primitives/index.ts";
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

export interface ShellFooterData {
	getGitBranch(): string | null;
	getExtensionStatuses(): ReadonlyMap<string, string>;
	getAvailableProviderCount(): number;
	onBranchChange(callback: () => void): () => void;
}

interface ShellRenderHost {
	requestRender(): void;
	invalidatePane?(): void;
}

export interface ShellDeps {
	activeProfile(): string | undefined;
	now(): number;
	resolveWorktree: WorktreeResolver;
	/** A session root the LDD section may read: removed worktrees and leftover directories are skipped. */
	rootExists(root: string): boolean;
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
}

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

export type ActiveProfileReader = (() => string | undefined) & {
	bind(cwd: string, resolveWorktree: WorktreeResolver, sessionId?: string): boolean;
	refresh(): boolean;
	reset(): void;
};

// The owner's active Jhon profile (settings' pi.profile, else profiles.json), read on
// refresh and never during a frame; `default` shows as no profile. A session
// that uses a profile of its own (T120, `session`) shows that one, marked
// while it differs from the default.
export function createActiveProfileReader(read: () => string = activeProfileName, session: () => string | undefined = () => undefined): ActiveProfileReader {
	let effective: string | undefined;
	const current = () => { const name = sessionMark(session(), read()); return name === "default" ? undefined : name; };
	const reader = (() => effective) as ActiveProfileReader;
	reader.refresh = () => {
		const next = current();
		const changed = next !== effective;
		effective = next;
		return changed;
	};
	reader.bind = () => reader.refresh();
	reader.reset = () => { effective = undefined; };
	return reader;
}

const defaultShellDeps: Omit<ShellDeps, "activeProfile"> = { now: () => Date.now(), resolveWorktree: resolveSessionWorktree, rootExists: (root) => existsSync(join(root, ".git")), lddStatus: (cwd) => readLddStatus(cwd), permissionMode };

interface PromptEditorDeps {
	fg: (color: string, text: string) => string;
	bold: (text: string) => string;
	italic?: (text: string) => string;
	requestRender(): void;
	/** A message waits for the turn to end: the working line says queued. */
	pending(): boolean;
	now(): number;
	/** shell.behaviour.double_esc_cancel, read on every press (Customize changes it in memory). */
	doubleEscCancelEnabled(): boolean;
	/** The messages pi put back in the editor when esc stopped the turn: sent as the next turn instead. */
	dispatchQueuedText(text: string): void;
	/** Run fn after ms, returning its cancel; a timer by default. */
	schedule?: (fn: () => void, ms: number) => () => void;
	/** The wait on the Claude API in place of what the agent does (lib/ui/api-wait.ts), read on every frame. */
	workingLabel?: () => string | undefined;
	/** What the agent does now (thinking, writing, a tool's name), read on every frame. */
	activity?: () => string | undefined;
	/** shell.appearance.ascii; the settings' by default. */
	ascii?: () => boolean;
	/** Rows the shell draws above the working line (the narrow form's Running block), with the hovered one's index. */
	topRows?: (width: number, hovered?: number) => string[];
	/** A left click on one of the top rows (index into topRows). */
	onTopRowClick?: (index: number) => void;
}

export { extractQueuedText };

/**
 * The prompt (design/shell-tui 01, T174-S8): the working line, a rule, the
 * input behind `❯`, a rule (lib/ui/shell-prompt.ts draws them around pi's
 * editor). It adds to pi's editor the esc gate (lib/ui/double-esc-cancel-policy.ts),
 * the selection (lib/ui/prompt-selection.ts) and the rows above the line.
 */
export class JhonPromptEditor extends CustomEditor {
	private readonly deps: PromptEditorDeps;
	private readonly host: TUI;
	// CustomEditor keeps its keybindings private; the esc gate needs app.interrupt's match.
	private readonly keys: KeybindingsManager;
	private readonly selection: PromptSelection;
	private readonly esc: ReturnType<typeof createEscGate>;
	private working = false;
	private startedAt = 0;
	private tick = 0;
	private animationPolicy: AnimationPolicy = "quality";
	private timer: NodeJS.Timeout | undefined;
	private hintRepaint: (() => void) | undefined;
	/** Rows above pi's editor in the last frame: the top rows and the working line. */
	private shellRows = 0;
	private topRowCount = 0;
	private hoveredTopRow: number | undefined;

	constructor(tui: TUI, theme: EditorTheme, keybindings: KeybindingsManager, deps: PromptEditorDeps) {
		super(tui, theme, keybindings);
		this.host = tui;
		this.keys = keybindings;
		this.deps = deps;
		this.selection = new PromptSelection(this);
		this.esc = createEscGate(() => deps.now());
	}

	setAnimationPolicy(policy: AnimationPolicy): void {
		if (this.animationPolicy === policy) return;
		this.animationPolicy = policy;
		this.restartTimer();
		this.deps.requestRender();
	}

	setWorking(working: boolean): void {
		this.working = working;
		if (working) this.startedAt = this.deps.now();
		this.esc.reset();
		this.restartTimer();
		this.deps.requestRender();
	}

	/** While working with animations on, the line moves every frame; off, it starts no timer and moves with pi's own repaints. */
	private restartTimer(): void {
		if (this.timer) clearInterval(this.timer);
		this.timer = undefined;
		this.tick = 0;
		if (!this.working || this.animationPolicy === "potato") return;
		this.timer = setInterval(() => { this.tick += 1; this.deps.requestRender(); }, SHELL_PULSE_MS);
		this.timer.unref?.();
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
		const draft = this.getText();
		super.handleInput(data);
		const queued = extractQueuedText(this.getText(), draft);
		if (queued === undefined || queued === "") return;
		this.setText(draft);
		if (queued.trim() !== "") this.deps.dispatchQueuedText(queued);
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
		if (this.hoveredTopRow === undefined) return;
		this.hoveredTopRow = undefined;
		this.deps.requestRender();
	}

	handleMouse(event: Parameters<CustomEditor["handleMouse"]>[0]): ReturnType<CustomEditor["handleMouse"]> {
		const moving = event.type === "move" && event.button === "none";
		if (event.y < this.topRowCount) {
			// The top rows are the shell's controls: hover, then a click opens the row's view.
			if (moving) {
				setPaneHover(this.host, this, () => this.clearTopHover());
				if (this.hoveredTopRow === event.y) return { handled: true };
				this.hoveredTopRow = event.y;
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
			tick: this.tick,
			animate: this.animationPolicy !== "potato",
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
		const top = this.deps.topRows?.(width, this.hoveredTopRow) ?? [];
		if (this.hoveredTopRow !== undefined && this.hoveredTopRow >= top.length) this.hoveredTopRow = undefined;
		this.topRowCount = top.length;
		this.shellRows = top.length + 1;
		return [...top, line, ...framed];
	}

	dispose(): void {
		if (this.timer) clearInterval(this.timer);
		this.timer = undefined;
		this.hintRepaint?.();
		this.hintRepaint = undefined;
	}
}

// Stable across extension module reloads; never infer ownership from a name.
const PROMPT_OWNER = Symbol.for("jhon-shell.prompt-owner");
type PromptFactory = NonNullable<ReturnType<ExtensionContext["ui"]["getEditorComponent"]>> & { [PROMPT_OWNER]?: boolean };

/**
 * The narrow form of a full-screen terminal (04): no right pane (under 140
 * columns, Status hidden, or a pi-tui without the layout hook): the Running
 * block sits above the editor rule.
 */
function narrowChrome(tui: TUI): boolean {
	if ((tui as TUI & { mode?: string }).mode !== "fullscreen" || !tui.terminal) return false;
	return !paneState(tui).wide?.();
}

function installPrompt(
	ctx: ExtensionContext,
	onCreated: (prompt: JhonPromptEditor) => void,
	promptDeps: { now: () => number; doubleEscCancelEnabled: () => boolean; dispatchQueuedText: (text: string) => void; workingLabel?: () => string | undefined; activity?: () => string | undefined; topRows?: (tui: TUI, width: number, hovered?: number) => string[]; onTopRowClick?: (tui: TUI, index: number) => void },
): boolean {
	const previous = ctx.ui.getEditorComponent() as PromptFactory | undefined;
	if (previous && !previous[PROMPT_OWNER]) return false;
	const factory: PromptFactory = (tui, theme, keybindings) => {
		const prompt = new JhonPromptEditor(tui, theme, keybindings, {
			fg: (color, text) => ctx.ui.theme.fg(color as Parameters<typeof ctx.ui.theme.fg>[0], text),
			bold: (text) => ctx.ui.theme.bold(text),
			italic: (text) => typeof ctx.ui.theme.italic === "function" ? ctx.ui.theme.italic(text) : text,
			requestRender: () => tui.requestRender(),
			pending: () => ctx.hasPendingMessages(),
			now: promptDeps.now,
			doubleEscCancelEnabled: promptDeps.doubleEscCancelEnabled,
			dispatchQueuedText: promptDeps.dispatchQueuedText,
			workingLabel: promptDeps.workingLabel,
			activity: promptDeps.activity,
			topRows: promptDeps.topRows ? (width, hovered) => promptDeps.topRows!(tui, width, hovered) : undefined,
			onTopRowClick: promptDeps.onTopRowClick ? (index) => promptDeps.onTopRowClick!(tui, index) : undefined,
		});
		onCreated(prompt);
		return prompt;
	};
	factory[PROMPT_OWNER] = true;
	ctx.ui.setEditorComponent(factory);
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

interface CardComponentOptions {
	expanded: boolean;
	previewRows?: number;
	hint?: string;
}

function cardComponent(card: Card, theme: CardTheme, options: CardComponentOptions) {
	return {
		render(width: number) {
			return renderCard(card, theme, width, options);
		},
		invalidate() {},
	};
}

// Widgets above the editor sit flush against the prompt frame; a blank line
// after the card keeps the two frames apart.
function spaced(component: { render(width: number): string[]; invalidate(): void }) {
	return {
		render(width: number) {
			return [...component.render(width), ""];
		},
		invalidate() {},
	};
}

export default function jhonShellUi(pi: ExtensionAPI, env: NodeJS.ProcessEnv = process.env, overrides: Partial<ShellDeps> = {}): void {
	// Its commands and shortcuts are counted (lib/ui-use.ts).
	pi = counted(pi);
	installSessionChangeCapture(pi, env, overrides.resolveWorktree ?? resolveSessionWorktree);
	if (!shellEnabled(env)) return;
	// T120: the session's own profile, its branch's at the start and then as extensions/jhon.ts says it.
	let sessionProfile: string | undefined;
	const profileReader = createActiveProfileReader(activeProfileName, () => sessionProfile);
	const deps: ShellDeps = { ...defaultShellDeps, activeProfile: profileReader, ...overrides };
	let closeSettings: (() => void) | undefined;
	let profilePoll: ReturnType<typeof setInterval> | undefined;
	const stopProfilePoll = () => {
		if (profilePoll) clearInterval(profilePoll);
		profilePoll = undefined;
		profileReader.reset();
	};
	let renderHost: ShellRenderHost | undefined;
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
	// Set by abortAndDispatchQueued via dispatchQueuedText when an Esc aborts
	// a turn with a non-empty queue; sent exactly once, from agent_settled,
	// once the aborted run has fully settled. Several aborts
	// before that settle append in order, joined the way Pi joins its own
	// queue, so nothing is overwritten. It belongs to the current session and
	// is dropped on session_shutdown.
	let pendingQueuedText: string | undefined;
	// The owner's settings (lib/settings.ts), loaded once for the process
	// and again at each later session start; every preference below reads
	// them, and every write goes through `jhon settings set`, which loads
	// them again. The Esc gate keeps its policy in memory so the editor never
	// asks per keypress; the /jhon:double-esc-cancel command re-syncs it on
	// every invocation, so status, the no-argument toggle direction, and the
	// gate always describe the same effective policy.
	const settings = shellSettings(env);
	let settingsLoadedOnce = false;
	const animationOptions = { settings };
	let animationPolicy = resolveAnimationPolicy(animationOptions).policy;
	// Conversation cards read the style from a process-wide slot; the saved
	// preference fills it at startup and again on every session start.
	// The Card content preference rides the same kind of slot: the quiet tool
	// renderers read it live, so the saved level applies from the first render.
	let visualSettings = resolveVisualSettings(animationOptions).settings;
	// The Gentle Shell's card style and chrome surface (L153 answers 1-2) live in
	// process-wide slots every extension's cards and the rail read.
	const applyAppearance = () => {
		setCardStyle(settings.cards());
		setChromeSurface(visualSettings.surface);
	};
	const refreshVisual = () => {
		visualSettings = resolveVisualSettings(animationOptions).settings;
		applyAppearance();
		renderHost?.invalidatePane?.();
		renderHost?.requestRender();
	};
	let doubleEscCancelPolicy: DoubleEscCancelPolicy = loadDoubleEscCancelPolicy({ settings });
	const apiWait = createApiWait(() => deps.now());
	const activity = createWorkingActivity();
	// Stall recovery (lib/ui/stall-recovery.ts): past the threshold the turn is
	// aborted as Esc does, and the continue rides the queued-text path above,
	// sent once the aborted run settles.
	const queueAfterSettle = (text: string) => { pendingQueuedText = pendingQueuedText === undefined ? text : `${pendingQueuedText}\n\n${text}`; };
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
	let runningJobs: RunningJobDisplay[] | undefined;
	/** Something runs, or ended less than a minute ago: the clock keeps the pane's times and lingering rows live. */
	const jobsLive = () => (runningJobs ?? []).some((job) => job.endedAt === undefined || deps.now() - job.endedAt < JOBS_FINISHED_TTL_MS);
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
		runningJobs = [...displays(event.tasks).filter((job) => job.endedAt === undefined), ...finished];
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
	pi.registerTool({
		name: "session_worktree_register",
		renderShell: "self",
		label: "Add a session worktree",
		description: "Add a worktree of this Git clone to the session: the LDD section reads the change there too.",
		parameters: { type: "object", required: ["path"], additionalProperties: false, properties: { path: { type: "string", description: "Worktree path to include in this session." } } } as never,
		async execute(_id, params, _signal, _onUpdate, ctx) {
			if (!registry || registry.sessionId !== ctx.sessionManager.getSessionId()) throw new Error("This session has no worktree list yet.");
			const root = registry.register((params as { path: string }).path, "explicit");
			return { content: [{ type: "text", text: `${root} is now one of this session's worktrees.` }], details: { root, sessionId: registry.sessionId } };
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
		runningJobs = undefined;
		stopProfilePoll();
		registry?.close();
		currentContext = ctx;
		registry = new SessionWorktreeRegistry(pi, ctx.sessionManager, ctx.cwd, deps.resolveWorktree);
		registry.start();
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
		visualSettings = resolveVisualSettings(animationOptions).settings;
		applyAppearance();
		if (!overrides.activeProfile) {
			try { sessionProfile = sessionProfileOnBranch((ctx.sessionManager as { getBranch?(): unknown[] }).getBranch?.()); } catch { sessionProfile = undefined; }
			profileReader.bind(ctx.cwd, deps.resolveWorktree, ctx.sessionManager.getSessionId());
			const sessionId = ctx.sessionManager.getSessionId();
			profilePoll = setInterval(() => {
				if (currentContext !== ctx || ctx.sessionManager.getSessionId() !== sessionId) return;
				if (!profileReader.refresh()) return;
				renderHost?.invalidatePane?.();
				renderHost?.requestRender();
			}, 2_000);
			profilePoll.unref?.();
		}
		lanes = laneReads();
		releaseSelection?.();
		releaseSelection = shareSelection(ctx.sessionManager.getSessionId(), () => currentContext === ctx ? refreshLdd(ctx) : Promise.resolve(ldd));
		void refreshLdd(ctx);
		if (ctx.hasUI) watchLddSources(ctx);
		ctx.ui.setFooter((tui, theme, footerData) => {
			const host: ShellRenderHost = { requestRender: () => tui.requestRender(), invalidatePane: () => invalidatePane(tui) };
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
				digest: () => JSON.stringify([runningJobs, settings.ascii(), jobsLive() ? Math.floor(deps.now() / 1000) : 0]),
				render: (width, view) => render(runningJobs, theme, width, { now: deps.now(), ascii: settings.ascii(), ...view }),
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
					const running = (runningJobs ?? []).filter((job) => job.endedAt === undefined).length;
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
		const ownsPrompt = installPrompt(
			ctx,
			(created) => {
				prompt?.dispose();
				prompt = created;
				prompt.setAnimationPolicy(animationPolicy);
			},
			{ now: () => deps.now(), doubleEscCancelEnabled: () => doubleEscCancelPolicy === "on", dispatchQueuedText: queueAfterSettle, workingLabel: () => apiWait.label(), activity: () => activity.current(),
				// The narrow form's Running block (04): the agents and jobs while they run.
				topRows: (tui, width, hovered) => narrowChrome(tui) ? renderRunningBlock(runningJobs, ctx.ui.theme, width, { now: deps.now(), ascii: shellSettings().ascii(), ...(hovered !== undefined ? { hover: hovered } : {}) }).rows : [],
				// A click on one of its rows opens the agents view or the jobs view.
				onTopRowClick: (tui, index) => {
					if (!narrowChrome(tui)) return;
					const view = renderRunningBlock(runningJobs, ctx.ui.theme, tui.terminal?.columns ?? 0, { now: deps.now() }).opens[index];
					if (view) openTasks(ctx, view);
				} },
		);
		// The working line says what pi's Working row would; the thinking blocks stay pi's.
		if (ownsPrompt) ctx.ui.setWorkingVisible(false);
	});
	pi.on("session_shutdown", (_event, ctx) => {
		closeSettings?.();
		runningJobs = undefined;
		if (jobClock) { clearInterval(jobClock); jobClock = undefined; }
		stopLddSources();
		releaseSelection?.();
		releaseSelection = undefined;
		stopProfilePoll();
		pendingQueuedText = undefined;
		prompt?.dispose();
		prompt = undefined;
		if ((ctx.ui.getEditorComponent() as PromptFactory | undefined)?.[PROMPT_OWNER]) {
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
	// One Customize overlay (T15.3; Settings until L174), G's Customization view:
	// every kept setting, grouped; /jhon:customize <group> opens it on that group.
	// Its categories, words and rows come from lib/ui/customize/registry.ts (T15.15).
	// It replaces /jhon:animations and /jhon:double-esc-cancel.
	pi.registerCommand(SETTINGS_COMMAND_NAME, {
		description: customizeDescription(),
		getArgumentCompletions: (prefix) => completeWord(customizeCompletions(), prefix),
		handler: async (args, ctx) => {
			if (ctx.mode !== "tui" || !ctx.hasUI) {
				if (ctx.hasUI) ctx.ui.notify("Customize needs an interactive terminal; `jhon settings` lists and sets them.", "warning");
				return;
			}
			// A group that moved says where it lives; an unknown word lists the groups (T15.9).
			const word = String(args ?? "").trim().toLowerCase();
			if (word && !CUSTOMIZE_WORDS.includes(word)) {
				const moved = SETTINGS_MOVED[word];
				ctx.ui.notify(moved ?? unknownCategoryNotice(word), "warning");
				return;
			}
			closeSettings?.();
			// An explicit open reads the settings again: another session or a hand edit may have changed them.
			settings.refresh();
			let finish: (() => void) | undefined;
			let closed = false;
			const close = () => {
				if (closed) return;
				closed = true;
				finish?.();
				if (closeSettings === close) closeSettings = undefined;
			};
			closeSettings = close;
			// The permission mode, read and set with `jhon mode` (L175), only here (L214): changeMode
			// keeps its rule, super-yolo only after the owner's Yes; the grants are in /jhon:grants.
			const bin = env.JHON_BIN || "jhon";
			const jhonMode = (args: string[]) => new Promise<string>((resolve, reject) =>
				execFile(bin, ["mode", ...args], { cwd: ctx.cwd, encoding: "utf8" }, (error, stdout, stderr) => error ? reject(new Error(`${stderr || stdout || error.message}`.trim())) : resolve(stdout)));
			let modeData: ModeData | undefined;
			const readMode = () => jhonMode(["--json"]).then((text) => { modeData = JSON.parse(text) as ModeData; return modeData; });
			void readMode().then(() => renderHost?.requestRender(), () => undefined);
			const mode = {
				value: () => modeData?.mode ?? deps.permissionMode?.(ctx.cwd) ?? "normal",
				set: async (next: string) => {
					try {
						const current = modeData ?? await readMode();
						const outcome = await changeMode(next, { setMode: async (m, root) => { await jhonMode([m, "--root", root]); }, confirmMode: async (question) => (await ctx.ui.select?.(question, ["Yes", "No"])) === "Yes" }, current);
						if (outcome === "declined") { ctx.ui.notify("Cancelled: nothing changed.", "info"); return undefined; }
						if (outcome === "set") { await readMode(); void refreshLdd(ctx); }
						return undefined;
					} catch (error) {
						return `could not set ${next}: ${error instanceof Error ? error.message : String(error)}`;
					}
				},
			};
			const rows = customizeRows({
				mode,
				settings,
				animations: (policy) => { animationPolicy = policy; prompt?.setAnimationPolicy(policy); },
				doubleEsc: () => { doubleEscCancelPolicy = loadDoubleEscCancelPolicy({ settings }); },
				status: () => { refreshVisual(); pi.events.emit(VISUAL_SETTINGS_CHANGED, { settings: settings.path }); },
				appearance: () => { refreshVisual(); pi.events.emit(VISUAL_SETTINGS_CHANGED, { settings: settings.path }); },
				redraw: () => { renderHost?.invalidatePane?.(); renderHost?.requestRender(); },
				// G's "Reload now" (L198): Customize closes, then pi reloads, so changed keys apply.
				reload: () => {
					close();
					setTimeout(() => { ctx.reload().catch((error: unknown) => ctx.ui.notify(`Reload failed: ${error instanceof Error ? error.message : String(error)}`, "error")); }, 0);
				},
				// G's Themes (L197): pi lists them, applies one and saves it. Rose ink
				// is rewritten with `jhon palette` first, as followPalette does, so a
				// switch back never applies a stale file.
				themes: {
					list: () => ctx.ui.getAllThemes(),
					current: () => ctx.ui.theme.name,
					set: (name) => {
						const apply = () => { const result = ctx.ui.setTheme(name); return result.success ? undefined : result.error ?? `could not apply ${name}`; };
						if (name !== ROSE_INK || !agentDir) return apply();
						switchToRoseInk({
							jhon: bin, agentDir, colors: PALETTE_PATH(env),
							apply: () => { const refused = apply(); renderHost?.requestRender(); return refused; },
							notify: (message) => ctx.ui.notify(`Customize: ${message}`, "error"),
						});
						return undefined;
					},
				},
			});
			try {
				await ctx.ui.custom<null>((tui, theme, _keys, done) => {
					// Closing writes every cell again, as every overlay's close does (overlay.ts).
					const shut = closeWithRepaint(tui, done);
					finish = () => shut(null);
					if (closed) done(null);
					const view = new SettingsView({
						rows, theme, group: String(args ?? "").trim() || undefined, ascii: () => settings.ascii(),
						// G's Customization: 85% of the rows less the card's border, the card itself at most 24.
						height: () => Math.max(0, Math.floor((tui.terminal?.rows ?? 24) * 0.85) - 2),
						// As G's Customization: a refused value is a notice, `<title>: <why>`.
						onError: (message) => { if (!closed) ctx.ui.notify(`Customize: ${message}`, "error"); },
						onClose: close,
						requestRender: () => { if (!closed) tui.requestRender(); },
					});
					return {
						render: (width) => closed ? [] : view.render(width),
						handleInput: (data) => { if (!closed) view.handleInput(data); },
						handleMouse: (event) => closed ? undefined : view.handleMouse(event),
						invalidate: () => view.invalidate(),
						dispose: close,
					};
				}, CARD_OVERLAY);
			} finally { close(); }
		},
	});
	pi.on("agent_start", (_event, ctx) => {
		// A turn can start any other way (the user sending the draft, an
		// extension, a shortcut) before the aborted run's own agent_settled
		// below has delivered the pending text. Nothing is sent from here: Pi
		// is mid-turn, so the text simply waits and goes out, once, when that
		// turn settles. It is never dropped.
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
		// Pi clears its own run-active flag before emitting agent_settled, so
		// this is normally idle; if a run is somehow still in flight the prompt
		// stays working and the pending text waits for the next settle.
		if (!ctx.isIdle()) return;
		prompt?.setWorking(false);
		if (pendingQueuedText === undefined) return;
		const queued = pendingQueuedText;
		pendingQueuedText = undefined;
		try {
			pi.sendUserMessage(queued);
		} catch (error) {
			// Never drop the user's words: put them back in front of the draft,
			// exactly the shape Pi's own restore would have left, and say why.
			if (prompt) {
				const current = prompt.getText();
				prompt.setText([queued, current].filter((text) => text.trim() !== "").join("\n\n"));
			} else {
				pendingQueuedText = queued;
			}
			if (ctx.hasUI) ctx.ui.notify(`Could not send the queued message after cancelling; it went back to the editor: ${error instanceof Error ? error.message : String(error)}`, "error");
		}
	});
	pi.on("agent_end", async (_event, ctx) => {
		apiWait.end();
		stall.sync();
		if (ctx.hasUI) refreshLdd(ctx);
	});
}
