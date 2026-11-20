import { CustomEditor, type ExtensionAPI, type ExtensionContext, type KeybindingsManager } from "@earendil-works/pi-coding-agent";
import { isHomeOrBroadUserDirectory } from "../lib/ui/user-directories.ts";
import { truncateToWidth, visibleWidth, type EditorTheme, type TUI } from "@earendil-works/pi-tui";
import { execFile, spawnSync } from "node:child_process";
import { existsSync, realpathSync, statSync, watch } from "node:fs";
import { lstat, readFile } from "node:fs/promises";
import * as os from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { renderShellBar, renderShellChromeBar, renderShellStatusBar, renderShellStatusLayout, renderShellTitleRow, renderShellTitleRows, type StatusHover, type StatusSection, type StatusTarget, agentsWorkSummary, renderShellAgentsSection, renderShellJobsSection, JOBS_FINISHED_TTL_MS, shellEnabled, shellJobsCount, type ShellBarModel, type RunningJobDisplay } from "../lib/ui/shell-bar.ts";
import { SettingsView } from "../lib/ui/settings-view.ts";
import { ROSE_INK } from "../lib/ui/customize/themes.ts";
import { PALETTE_PATH } from "../lib/desktop-palette.ts";
import { switchToRoseInk } from "../lib/rose-ink.ts";
import { CUSTOMIZE_WORDS, customizeCompletions, customizeDescription, customizeRows, unknownCategoryNotice } from "../lib/ui/customize/registry.ts";
import { completeWord } from "../lib/ui/jhon-commands.ts";
import { backgroundTasksKey } from "../lib/background-tasks-tools.ts";
import { changeMode, type ModeData } from "../lib/ui/jhon-panel-grants.ts";
import { laneReads, lddLanes, lddOfSession, lddWatchTargets, permissionMode, readLddStatus, selectChange, shareSelection, type LddStatus } from "../lib/ui/ldd-status.ts";
import { CHANGE_STATUS, DIFF_MAX_BYTES, DIFF_MAX_LINES, DIFF_TOO_LARGE, WorktreeChangesTracker, fallbackReason, noChangesText, renderChangesWidget, type ChangedFile, type ChangesModel, type TurnSummary, type FileRevision, type GitRunner, type LineCounter, type WorktreeChanges } from "../lib/ui/shell-changes.ts";
import { WorktreeChangesView } from "../lib/ui/shell-changes-view.ts";
import { openInExternalEditor, type ExternalEditorHost } from "../lib/ui/jhon-panel-editor.ts";
import { SessionWorktreeRegistry, resolveSessionWorktree, worktreeGitEnvironment, type WorktreeResolver, type WorktreeIdentity } from "../lib/ui/session-worktree-registry.ts";
import { gitReadEnv } from "../lib/git-read.ts";
import { createNativeFullscreenInteraction } from "../lib/ui/native-fullscreen-interaction.ts";
import { cardStyle, renderCard, setCardStyle, type Card, type CardTheme } from "../lib/ui/shell-card.ts";
import { resolveVisualSettings } from "../lib/ui/visual-customization-policy.ts";
import { DOUBLE_ESC_CANCEL_HINT, floatPromptRow, framePromptLines, panelPromptRow, resolvePromptLayout, IDLE_ESC_CLEAR_HINT, PROMPT_HINT, PROMPT_STATE, SHELL_PULSE_MS, withPromptHint, type PromptState } from "../lib/ui/shell-prompt.ts";
import { createApiWait } from "../lib/ui/api-wait.ts";
import { createStallRecovery, stallContinueText, stallNotice, stallSeconds, stallThreshold, type Schedule } from "../lib/ui/stall-recovery.ts";
import { activeProfileName } from "../lib/profiles.ts";
import { SESSION_PROFILE_CHANGED, SessionProfile, sessionMark, sessionProfileOnBranch, type ProfilesJson } from "../lib/session-profile.ts";
import { knownAgents, resolveAgent } from "../lib/agents.ts";
import { OPEN_TASKS_EVENT, TASKS_EVENT } from "../lib/events.ts";
import { retiredNotice, shellSettings, type ShellSettings } from "../lib/settings.ts";
import { resolveAnimationPolicy, type AnimationPolicy } from "../lib/ui/animation-policy.ts";
import {
	DOUBLE_ESC_CANCEL_WINDOW_MS,
	resolveDoubleEscCancelPolicy,
	type DoubleEscCancelPolicy,
} from "../lib/ui/double-esc-cancel-policy.ts";
import { NAN_PROVIDER, NAN_QUOTA_URL, parseCodexRateLimitEvent, parseNanQuota, parseProviderUsage, parseUsageHeaders, parseUsageSource, usageScopeProviders, UsageSourceRegistry, UsageStore, USAGE_SOURCE_EVENT, type ProviderUsage, type UsageSource } from "../lib/ui/shell-usage.ts";
import { UsageView } from "../lib/ui/shell-usage-view.ts";
import { setSidebarHover, sidebarHeader, sidebarPart, sidebarSlot, sidebarState, VISUAL_SETTINGS_CHANGED, type SidebarRail } from "../lib/ui/shell-sidebar.ts";
import { installSidebar, invalidateSidebar, SIDEBAR_BREAKPOINT } from "../lib/ui/shell-sidebar-layout.ts";
import type { PanelSummary } from "../lib/ui/shell-panel.ts";
import { renderWorkRow, setChromeSurface, workRowLead } from "../lib/ui/shell-panel.ts";
import { recentSessionRoots, sessionChangeRoots, sessionRootBases, sessionSubagentRoots } from "../lib/ui/session-changes.ts";
import { installSessionChangeCapture } from "../lib/ui/session-change-capture.ts";
import { SelectionEngine } from "../lib/ui/selection-engine.ts";
import { withOverlayRepaint } from "../lib/ui/overlay-repaint.ts";
import { HOST_EVENT, hostShowsPath } from "../lib/host.ts";
import { counted, recordUse } from "../lib/ui-use.ts";

// jhon-shell's UI: the visual layer Jhon puts on top of pi. It installs the
// status bar, the prompt, the working-tree changes widget and overlay, the
// subscription usage view, and customization.

export interface ShellFooterData {
	getGitBranch(): string | null;
	getExtensionStatuses(): ReadonlyMap<string, string>;
	getAvailableProviderCount(): number;
	onBranchChange(callback: () => void): () => void;
}

interface ShellRenderHost {
	requestRender(): void;
	invalidateSidebar?(): void;
}

interface BuildOptions {
	jobs?: RunningJobDisplay[];
	profile?: string;
	home?: string;
	dirty?: number;
	usage?: ProviderUsage;
}

export interface ShellDeps {
	activeProfile(): string | undefined;
	fetch: typeof fetch;
	now(): number;
	resolveWorktree: WorktreeResolver;
	/** maxBuffer bounds one diff; status and discovery stay unbounded. */
	gitRunner(cwd: string, maxBuffer?: number): GitRunner;
	/** A root Changes may scan: removed worktrees and leftover directories are skipped. */
	rootExists(root: string): boolean;
	/** The LDD change the session works on, through `jhon status` (lib/ui/ldd-status.ts). */
	lddStatus(cwd: string): Promise<LddStatus | undefined>;
	/** The owner's permission mode the last lddStatus read of cwd reported (S31), for the status line's badge. */
	permissionMode?(cwd: string): string | undefined;
	/** How STATUS watches a directory: node's fs.watch. */
	watch?: typeof watch;
	/** The stall recovery's one-shot timer (lib/ui/stall-recovery.ts); returns its cancel. */
	schedule?: Schedule;
	/** `jhon profile json`, parsed: a session profile's sub-agent models, for the usage scope (T149). */
	profilesJson?(cwd: string): Promise<ProfilesJson>;
}

/** `jhon profile json`, read without blocking pi, as extensions/jhon.ts reads it for the session's profile. */
function readProfilesJson(cwd: string): Promise<ProfilesJson> {
	return new Promise((resolve, reject) =>
		execFile(process.env.JHON_BIN || "jhon", ["profile", "json"], { cwd, encoding: "utf8", maxBuffer: 8 << 20, timeout: 10_000 }, (error, stdout) => {
			if (error) return reject(error);
			try { resolve(JSON.parse(stdout) as ProfilesJson); } catch (cause) { reject(cause); }
		}));
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

const defaultShellDeps: Omit<ShellDeps, "activeProfile"> = { fetch: (input, init) => globalThis.fetch(input, init), now: () => Date.now(), resolveWorktree: resolveSessionWorktree, gitRunner: (cwd, maxBuffer) => shellGitRunner(cwd, process.env, execFile, maxBuffer), rootExists: (root) => existsSync(join(root, ".git")), lddStatus: (cwd) => readLddStatus(cwd), permissionMode, profilesJson: readProfilesJson };

interface AssistantUsageEntry {
	type: string;
	message?: {
		role?: string;
		usage?: {
			cost?: { total?: number };
		};
	};
}

function shortenHome(cwd: string, home: string | undefined): string {
	if (home && cwd.startsWith(home)) return `~${cwd.slice(home.length)}`;
	return cwd;
}

function sessionCost(ctx: ExtensionContext): number {
	let total = 0;
	for (const entry of ctx.sessionManager.getEntries() as AssistantUsageEntry[]) {
		if (entry.type !== "message" || entry.message?.role !== "assistant") continue;
		total += entry.message.usage?.cost?.total ?? 0;
	}
	return total;
}

type ShellSessionStats = { costTotal: number; usage: ReturnType<ExtensionContext["getContextUsage"]> };
const shellSessionStats = new WeakMap<object, {
	sessionId: string | undefined;
	leafId: string | null;
	entryCount: number | undefined;
	model: ExtensionContext["model"];
	contextWindow: number | undefined;
	stats: ShellSessionStats;
}>();

function sessionStats(ctx: ExtensionContext): ShellSessionStats {
	const manager = ctx.sessionManager as ExtensionContext["sessionManager"] & { getEntryCount?(): number };
	const compute = (): ShellSessionStats => ({ costTotal: sessionCost(ctx), usage: ctx.getContextUsage() });
	// Append-only session entries move the leaf, including compaction and branch
	// switches. A virtual model's routed physical model comes from the latest
	// response, which is appended as an entry, so the leaf covers it as well.
	// Older/test hosts without that identity must stay uncached.
	if (typeof manager.getLeafId !== "function") return compute();
	const sessionId = manager.getSessionId();
	const leafId = manager.getLeafId();
	const entryCount = manager.getEntryCount?.();
	const model = ctx.model;
	const contextWindow = model?.contextWindow;
	const cached = shellSessionStats.get(manager);
	if (cached && cached.sessionId === sessionId && cached.leafId === leafId &&
		cached.entryCount === entryCount && cached.model === model && cached.contextWindow === contextWindow) return cached.stats;
	const stats = compute();
	shellSessionStats.set(manager, { sessionId, leafId, entryCount, model, contextWindow, stats });
	return stats;
}

export function buildShellBarModel(
	pi: ExtensionAPI,
	ctx: ExtensionContext,
	footerData: ShellFooterData,
	options: BuildOptions = {},
): ShellBarModel {
	const home = options.home ?? os.homedir();
	const { usage, costTotal } = sessionStats(ctx);
	const model = ctx.model;
	const entries = Array.from(footerData.getExtensionStatuses().entries())
		.sort(([a], [b]) => a.localeCompare(b));
	const statuses = entries.map(([, text]) => text);
	return {
		cwd: shortenHome(ctx.sessionManager.getCwd(), home),
		profile: options.profile,
		branch: footerData.getGitBranch(),
		dirty: options.dirty,
		sessionName: ctx.sessionManager.getSessionName(),
		modelId: model?.id ?? "no-model",
		effort: model?.reasoning ? pi.getThinkingLevel() : undefined,
		contextPercent: usage?.percent ?? null,
		contextWindow: usage?.contextWindow ?? model?.contextWindow ?? 0,
		costTotal,
		subscription: model ? ctx.modelRegistry.isUsingOAuth(model) : false,
		usage: options.usage,
		...(options.jobs !== undefined ? { jobs: options.jobs } : {}),
		statuses,
		statusKeys: entries.map(([key]) => key),
	};
}

interface PromptEditorDeps {
	fg: (color: string, text: string) => string;
	bg?: (color: string, text: string) => string;
	bold: (text: string) => string;
	requestRender(): void;
	pending(): boolean;
	now(): number;
	/** Read fresh on every keypress: the command handler updates this in-memory, the editor never re-reads the file. */
	doubleEscCancelEnabled(): boolean;
	/** Hand off text reconstructed from Pi's Esc-abort restore so it is sent as the next turn instead of sitting in the editor. */
	dispatchQueuedText(text: string): void;
	/** Run fn after ms, returning its cancel; a timer by default. */
	schedule?: (fn: () => void, ms: number) => () => void;
	/** The working line's words in place of "working…" (lib/ui/api-wait.ts), read on every frame. */
	workingLabel?: () => string | undefined;
	/** The fullscreen chrome: the editor is G's panel editor (a titled rule over `›` rows); elsewhere the float or neon card. */
	panelChrome?: () => boolean;
	/** Rows the shell draws above the rule (the work row), with the hovered one's index. */
	topRows?: (width: number, hovered?: number) => string[];
	/** A left click on one of the top rows (index into topRows). */
	onTopRowClick?: (index: number) => void;
}

const PROMPT_FRAME_ROLE = "border";
// Matches Pi's own idle double-Esc window (empty editor -> /tree or /fork);
// this is the same muscle memory applied to clearing a non-empty draft.
const IDLE_ESC_CLEAR_WINDOW_MS = 500;

/**
 * Pi's own Esc-abort handler (`restoreQueuedMessagesToEditor({ abort: true
 * })`) rebuilds the editor text as
 * `[queuedText, currentText].filter((t) => t.trim()).join("\n\n")`, where
 * `currentText` is the draft captured just before the abort. Reverse that
 * join to recover the queued text alone, so the draft can be restored by
 * itself and the queued text dispatched as the next turn. `draft` is empty
 * (including whitespace-only) whenever `.trim() === ""`, matching the
 * `filter` predicate above exactly.
 *
 * Returns `""` only for the genuine no-queue case (`combined === draft`, or
 * both empty). Returns `undefined` when `combined` does not match Pi's join
 * shape at all — a future Pi change, or anything else that touched the
 * editor during the abort. That distinction matters to the caller: an empty
 * queue means "nothing to restore," while an unrecognized shape means "do
 * not touch what Pi already wrote," so a mismatch is never silently treated
 * as an empty queue.
 */
export function extractQueuedText(combined: string, draft: string): string | undefined {
	if (combined === draft) return "";
	if (draft.trim() === "") return combined;
	const suffix = `\n\n${draft}`;
	return combined.endsWith(suffix) ? combined.slice(0, combined.length - suffix.length) : undefined;
}

export class JhonPromptEditor extends CustomEditor {
	private promptState: PromptState = PROMPT_STATE.IDLE;
	private tick = 0;
	private animationPolicy: AnimationPolicy = "quality";
	private pulse: NodeJS.Timeout | undefined;
	private readonly deps: PromptEditorDeps;
	private frameMouseLayout?: ReturnType<typeof resolvePromptLayout>;
	// The Gentle Shell's narrow editor (T15.12c): rows above the rule (the work
	// row), each a control with its hover.
	private readonly pointerTui: TUI;
	private topRowCount = 0;
	private hoveredTopRow: number | undefined;
	// Native selection engine (shift+home/end, alt+a, replace-on-key): ported
	// from pi-select-del so the petal prompt owns the feature without factory
	// composition. Constructed with `this`; the internals probe degrades to
	// passthrough on pi drift, costing only the selection features.
	private readonly selectionEngine: SelectionEngine;
	// CustomEditor keeps its own `keybindings` private, so this class holds
	// its own reference to run the same app.interrupt match before deciding
	// whether to swallow the keystroke.
	private readonly keybindingsManager: KeybindingsManager;
	private pendingEscapeCancelDeadline: number | undefined;
	private pendingIdleClearDeadline: number | undefined;
	// Repaints when an Esc confirmation window closes, so its hint leaves the screen.
	private cancelHintExpiry: (() => void) | undefined;
	// Snapshot of the draft at the first Esc; the second Esc only clears when
	// the text is still exactly this, so an edit in between never gets
	// silently discarded.
	private pendingIdleClearText: string | undefined;

	constructor(tui: TUI, theme: EditorTheme, keybindings: KeybindingsManager, deps: PromptEditorDeps) {
		super(tui, theme, keybindings);
		this.pointerTui = tui;
		this.selectionEngine = new SelectionEngine(this);
		this.deps = deps;
		this.keybindingsManager = keybindings;
	}

	setAnimationPolicy(policy: AnimationPolicy): void {
		if (this.animationPolicy === policy) return;
		this.animationPolicy = policy;
		this.stopPulse();
		if (this.promptState === PROMPT_STATE.WORKING) this.startPulse();
		this.deps.requestRender();
	}

	private startPulse(): void {
		if (this.animationPolicy === "potato") return;
		this.pulse = setInterval(() => {
			this.tick += 1;
			this.deps.requestRender();
		}, SHELL_PULSE_MS);
		this.pulse.unref();
	}

	setWorking(working: boolean): void {
		this.promptState = working ? PROMPT_STATE.WORKING : PROMPT_STATE.IDLE;
		this.stopPulse();
		if (!working) {
			this.pendingEscapeCancelDeadline = undefined;
		} else {
			this.pendingIdleClearDeadline = undefined;
			this.pendingIdleClearText = undefined;
			this.startPulse();
		}
		this.deps.requestRender();
	}

	/**
	 * Swallow the first Esc while working, opt-in via
	 * doubleEscCancelEnabled(). `pi.registerShortcut("escape")` is not viable
	 * here: Pi reserves app.interrupt and skips colliding extension
	 * shortcuts, so this has to sit in front of CustomEditor's own
	 * handleInput instead. The Esc that actually aborts the turn (the single
	 * Esc when double-esc-cancel is off, or the confirming second Esc when
	 * it is on) always goes through abortAndDispatchQueued so the queued
	 * text Pi would otherwise dump back into the editor is sent as the next
	 * turn instead. Idle double-Esc (tree/fork), bash-mode
	 * Esc, and autocomplete cancel are all decided by CustomEditor/onEscape
	 * and never reach this branch.
	 */
	override handleInput(data: string): void {
		// The selection handler can consume input before the native chain;
		// any intervening key invalidates an idle Esc confirmation.
		if (this.pendingIdleClearDeadline !== undefined && !this.keybindingsManager.matches(data, "app.interrupt")) {
			this.pendingIdleClearDeadline = undefined;
			this.pendingIdleClearText = undefined;
		}
		// Native selection runs before the prompt's existing input chain, but
		// a registered extension shortcut still owns its chord.
		this.selectionEngine.handleInput(data, (d) => this.handleInputNative(d), (d) => this.onExtensionShortcut?.(d) === true);
	}

	private handleInputNative(data: string): void {
		if (
			this.promptState === PROMPT_STATE.WORKING &&
			!this.isShowingAutocomplete() &&
			this.keybindingsManager.matches(data, "app.interrupt")
		) {
			if (!this.deps.doubleEscCancelEnabled()) {
				this.abortAndDispatchQueued(data);
				return;
			}
			if (this.isPendingEscapeCancel()) {
				this.pendingEscapeCancelDeadline = undefined;
				this.abortAndDispatchQueued(data);
				return;
			}
			// Pi's own idle double-Esc (empty editor -> /tree or /fork) uses a
			// 500ms window; canceling a running turn is a heavier, harder-to-undo
			// action, so this confirmation deliberately gets double that time.
			this.pendingEscapeCancelDeadline = this.deps.now() + DOUBLE_ESC_CANCEL_WINDOW_MS;
			this.expireHintAfter(DOUBLE_ESC_CANCEL_WINDOW_MS);
			this.deps.requestRender();
			return;
		}
		// Idle with a non-empty draft: Pi's own idle double-Esc only acts on an
		// empty editor (tree/fork), so a draft's first Esc would otherwise do
		// nothing. Mirror the same swallow-then-confirm shape as the
		// working-cancel gate above, on the same 500ms window as Pi's own idle
		// double-Esc. Bash-mode drafts ("!...", the same rule
		// Pi's own interactive-mode uses to detect bash mode) are Pi's own
		// bash-mode Esc territory and must never reach this gate.
		if (
			this.promptState === PROMPT_STATE.IDLE &&
			!this.isShowingAutocomplete() &&
			this.keybindingsManager.matches(data, "app.interrupt")
		) {
			const text = this.getText();
			if (text.trim() !== "" && !text.trimStart().startsWith("!")) {
				// The second Esc only clears when the text is still exactly what
				// it was at the first Esc; an edit in between starts a fresh
				// first press on the new text instead of silently discarding it.
				if (this.isPendingIdleClear() && this.pendingIdleClearText === text) {
					this.pendingIdleClearDeadline = undefined;
					this.pendingIdleClearText = undefined;
					this.addToHistory(text);
					this.setText("");
					this.deps.requestRender();
					return;
				}
				this.pendingIdleClearDeadline = this.deps.now() + IDLE_ESC_CLEAR_WINDOW_MS;
				this.pendingIdleClearText = text;
				this.expireHintAfter(IDLE_ESC_CLEAR_WINDOW_MS);
				this.deps.requestRender();
				return;
			}
		}
		super.handleInput(data);
	}

	/**
	 * Runs the Esc that actually aborts the turn. Pi's own onEscape (invoked
	 * synchronously by `super.handleInput`) restores `queuedText + draft`
	 * into the editor and aborts; snapshot the draft first, reconstruct the
	 * queued text from what comes back, restore the draft alone, and hand
	 * the queued text to the dispatcher so it is sent once the aborted run
	 * settles (see the `agent_settled` handler in `jhonShellUi`). Images
	 * inside queued messages are already dropped by Pi's own restore, before
	 * this code ever sees the text.
	 *
	 * `extractQueuedText` returning `undefined` means the restored text does
	 * not match Pi's own join shape; Pi's own text wins and is left exactly
	 * as it is, nothing is dispatched. An empty string means a genuine empty
	 * queue: there is nothing to restore, so `setText` is not called at all
	 * on the common no-queue path. Only a recognized, non-empty queue
	 * restores the draft and dispatches.
	 */
	private abortAndDispatchQueued(data: string): void {
		const draft = this.getText();
		super.handleInput(data);
		const queued = extractQueuedText(this.getText(), draft);
		// undefined: unrecognized shape, Pi's own text stays untouched.
		if (queued === undefined) return;
		// "": nothing was queued, and the editor already holds the draft, so no
		// redundant write. Anything else was recognized: the draft comes back
		// alone, and only real text (not whitespace) is worth a turn.
		if (queued !== "") this.setText(draft);
		if (queued.trim() === "") return;
		this.deps.dispatchQueuedText(queued);
	}

	private clearEditorHover(): void {
		const changed = this.hoveredTopRow !== undefined;
		this.hoveredTopRow = undefined;
		if (changed) this.deps.requestRender();
	}

	handleMouse(event: Parameters<CustomEditor["handleMouse"]>[0]): ReturnType<CustomEditor["handleMouse"]> {
		if (event.type === "move" && event.button === "none") {
			if (event.y >= 0 && event.y < this.topRowCount) setSidebarHover(this.pointerTui, this, () => this.clearEditorHover());
			else setSidebarHover(this.pointerTui);
		}
		// Rows drawn above the rule belong to the shell, not to pi's editor.
		if (this.topRowCount > 0) {
			if (event.y < this.topRowCount) {
				if (event.type === "move" && event.button === "none") {
					if (this.hoveredTopRow === event.y) return { handled: true };
					this.hoveredTopRow = event.y;
					this.deps.requestRender();
					return { handled: true, render: true };
				}
				if (event.type !== "click" || event.button !== "left") return undefined;
				this.deps.onTopRowClick?.(event.y);
				return { handled: true, render: true };
			}
			if (this.hoveredTopRow !== undefined) { this.hoveredTopRow = undefined; this.deps.requestRender(); }
			event = { ...event, y: event.y - this.topRowCount, height: event.height - this.topRowCount };
		}
		const layout = this.frameMouseLayout;
		if (!layout) return super.handleMouse(event);
		// Row coordinates stay native: rule (or float hint)=0, content=1…N, the last row N+1; x is translated past the lead.
		if (event.x < layout.prefixWidth || event.x >= (layout.panel ? layout.prefixWidth + layout.nativeWidth : layout.width - 1)) return undefined;
		return super.handleMouse({ ...event, x: event.x - layout.prefixWidth, width: layout.nativeWidth });
	}

	render(width: number): string[] {
		const layout = resolvePromptLayout(width, cardStyle(), this.deps.bg, this.deps.panelChrome?.() ?? false);
		this.frameMouseLayout = layout.background || layout.panel ? layout : undefined;
		const inner = layout.nativeWidth;
		const lines = super.render(inner);
		if (this.getText() === "" && lines.length === 3) lines[1] = withPromptHint(lines[1], PROMPT_HINT, this.deps.fg);
		const state = this.promptState === PROMPT_STATE.WORKING && this.deps.pending() ? PROMPT_STATE.QUEUED : this.promptState;
		// The rule follows Pi's live public borderColor callback so thinking/bash
		// mode accents survive; the narrow frame keeps the shell's border role.
		// Selection is applied only to the owned Pi editor rows, before the
		// frames them. No second editor factory or private layout mutation.
		const editorLines = this.selectionEngine.decorateRows(lines, inner, 0);
		// SAFETY: this optional host hint is accepted only as an integer within the
		// rendered row bounds below; absent/invalid hints fall back to all rows.
		const visibleCount = (this as unknown as { renderedVisibleLineCount?: number }).renderedVisibleLineCount;
		const borderEnd = Number.isInteger(visibleCount) && visibleCount! >= 1 && visibleCount! + 2 <= editorLines.length
			? visibleCount! + 2 : editorLines.length;
		const framed = framePromptLines(editorLines.slice(0, borderEnd), width, {
			state,
			tick: this.tick,
			workingLabel: this.promptState === PROMPT_STATE.WORKING ? this.deps.workingLabel?.() : undefined,
			borderColor: (text) => layout.background || layout.panel ? this.borderColor(text) : this.deps.fg(PROMPT_FRAME_ROLE, text),
			fg: this.deps.fg,
			bold: this.deps.bold,
			escHint: this.promptState === PROMPT_STATE.WORKING && this.isPendingEscapeCancel()
				? DOUBLE_ESC_CANCEL_HINT
				: this.promptState === PROMPT_STATE.IDLE && this.isPendingIdleClear()
					? IDLE_ESC_CLEAR_HINT
					: undefined,
		}, layout);
		framed[framed.length - 1] = layout.panel
			? panelPromptRow(this.selectionEngine.decorateBottomRule("", layout.nativeWidth, ""), layout)
			: layout.background
				? floatPromptRow(this.selectionEngine.decorateBottomRule("", layout.nativeWidth, ""), layout, (text) => this.borderColor(text))
				: this.selectionEngine.decorateBottomRule(framed[framed.length - 1] ?? "", width, "╯");
		// Pi places autocomplete after its bottom border. Keep those rows below
		// the shell's frame and preserve their terminal width and row coordinates.
		for (const line of editorLines.slice(borderEnd)) {
			if (layout.panel) {
				framed.push(panelPromptRow(line, layout));
				continue;
			}
			if (layout.background) {
				framed.push(floatPromptRow(line, layout, (text) => this.borderColor(text)));
				continue;
			}
			const available = Math.max(0, width);
			const border = available >= 2 ? "│" : "";
			const inner = available - border.length * 2;
			const clipped = truncateToWidth(line, inner, "");
			framed.push(this.deps.fg(PROMPT_FRAME_ROLE, border) + clipped +
				" ".repeat(Math.max(0, inner - visibleWidth(clipped))) + this.deps.fg(PROMPT_FRAME_ROLE, border));
		}
		// What the shell draws above the rule (the work row).
		const topRows = this.deps.topRows?.(width, this.hoveredTopRow) ?? [];
		if (this.hoveredTopRow !== undefined && this.hoveredTopRow >= topRows.length) this.hoveredTopRow = undefined;
		this.topRowCount = topRows.length;
		framed.unshift(...topRows);
		return framed;
	}

	dispose(): void {
		this.stopPulse();
		this.cancelHintExpiry?.();
	}

	/** Repaint once the window closes, so an "esc again" hint never outlives it. */
	private expireHintAfter(ms: number): void {
		this.cancelHintExpiry?.();
		const schedule = this.deps.schedule ?? ((fn: () => void, wait: number) => {
			const timer = setTimeout(fn, wait);
			timer.unref?.();
			return () => clearTimeout(timer);
		});
		// One millisecond past the deadline, when the pending check reads it as closed.
		this.cancelHintExpiry = schedule(() => { this.cancelHintExpiry = undefined; this.deps.requestRender(); }, ms + 1);
	}

	private isPendingEscapeCancel(): boolean {
		return this.pendingEscapeCancelDeadline !== undefined && this.deps.now() < this.pendingEscapeCancelDeadline;
	}

	private isPendingIdleClear(): boolean {
		return (
			this.pendingIdleClearDeadline !== undefined &&
			this.deps.now() < this.pendingIdleClearDeadline &&
			this.pendingIdleClearText === this.getText()
		);
	}

	private stopPulse(): void {
		if (this.pulse) clearInterval(this.pulse);
		this.pulse = undefined;
		this.tick = 0;
	}
}

// Stable across extension module reloads; never infer ownership from a name.
const PROMPT_OWNER = Symbol.for("jhon-shell.prompt-owner");
type PromptFactory = NonNullable<ReturnType<ExtensionContext["ui"]["getEditorComponent"]>> & { [PROMPT_OWNER]?: boolean };

/**
 * The Gentle Shell's narrow chrome (T15.12c): a fullscreen terminal whose
 * rail is not shown (under 140 columns, or Status hidden): live work folds
 * into the work row above the editor rule.
 */
function narrowChrome(tui: TUI): boolean {
	if ((tui as TUI & { mode?: string }).mode !== "fullscreen" || !tui.terminal) return false;
	const state = sidebarState(tui);
	return !(state.active && state.ownsHost?.());
}

/** The live-work digests the work row folds, agents first then the todo list (the Gentle Shell's order). */
function workParts(tui: TUI): Array<{ part: SidebarRail; summary: NonNullable<ReturnType<NonNullable<SidebarRail["summary"]>>> }> {
	if (!tui.terminal) return [];
	return ["agents", "todo"].flatMap((key) => {
		const part = sidebarState(tui).parts.get(key);
		const summary = part?.summary?.();
		return part && summary ? [{ part, summary }] : [];
	});
}

function installPrompt(
	ctx: ExtensionContext,
	onCreated: (prompt: JhonPromptEditor) => void,
	promptDeps: { now: () => number; doubleEscCancelEnabled: () => boolean; dispatchQueuedText: (text: string) => void; workingLabel?: () => string | undefined; panelChrome?: (tui: TUI) => boolean; topRows?: (tui: TUI, width: number, hovered?: number) => string[]; onTopRowClick?: (tui: TUI, index: number) => void },
): boolean {
	const previous = ctx.ui.getEditorComponent() as PromptFactory | undefined;
	if (previous && !previous[PROMPT_OWNER]) return false;
	const factory: PromptFactory = (tui, theme, keybindings) => {
		const prompt = new JhonPromptEditor(tui, theme, keybindings, {
			fg: (color, text) => ctx.ui.theme.fg(color as Parameters<typeof ctx.ui.theme.fg>[0], text),
			bg: (color, text) => ctx.ui.theme.bg(color as Parameters<typeof ctx.ui.theme.bg>[0], text),
			bold: (text) => ctx.ui.theme.bold(text),
			requestRender: () => tui.requestRender(),
			pending: () => ctx.hasPendingMessages(),
			now: promptDeps.now,
			doubleEscCancelEnabled: promptDeps.doubleEscCancelEnabled,
			dispatchQueuedText: promptDeps.dispatchQueuedText,
			workingLabel: promptDeps.workingLabel,
			panelChrome: promptDeps.panelChrome ? () => promptDeps.panelChrome!(tui) : undefined,
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

/** Settings is Customize (L174): /jhon:customize opens it; /jhon:settings is gone (L195). */
const SETTINGS_COMMAND_NAME = "jhon:customize";
/** The groups that left the shell's Customize (L96), and where each lives now. */
const SETTINGS_MOVED: Record<string, string> = {
	runtimes: "The runtimes are set in `jhon setup` (or `jhon settings set runtimes.<name>`).",
	integrations: "The integrations are set in `jhon setup` (or `jhon settings set integrations.<name>`).",
	palette: "The palette, Rose Ink or Desktop, is set in `jhon setup` (or `jhon settings set integrations.theme.palette`).",
	profile: "The model profile is chosen in /jhon:profile.",
};

const CHANGES_WIDGET_KEY = "jhon-shell-changes";
const CHANGES_COMMAND_NAME = "jhon:changes";
const CHANGES_POLL_DEFAULT_MS = 2000;
const GIT_TIMEOUT_MS = 5000;

export function shellGitRunner(cwd: string, env: NodeJS.ProcessEnv = process.env, run: typeof execFile = execFile, maxBuffer = Infinity): GitRunner {
	// Pi exec cannot replace the inherited environment. Use argv directly and
	// a complete sanitized environment for discovery, status, and lazy diffs.
	const childEnv = gitReadEnv(worktreeGitEnvironment(env));
	return (args) => new Promise((resolve) => {
		run("git", ["-C", cwd, ...args], {
			env: childEnv,
			encoding: "utf8",
			shell: false,
			windowsHide: true,
			timeout: GIT_TIMEOUT_MS,
			// Pi exec accumulates output without a maxBuffer cap. In particular,
			// large porcelain inventories must not become partial successful scans.
			// Only a single diff passes a bound, and is told when it hit it.
			maxBuffer,
		}, (error, stdout) => {
			if ((error as { code?: unknown } | null)?.code === "ERR_CHILD_PROCESS_STDIO_MAXBUFFER") return resolve({ stdout, code: 1, truncated: true });
			resolve({ stdout, code: error ? typeof error.code === "number" ? error.code : 1 : 0 });
		});
	});
}

// A file's diff is against what its counts are against (T132): the session's
// base for its root when the tracker compared with it, else HEAD.
export async function loadFileDiff(git: GitRunner, file: ChangedFile): Promise<string> {
	const args = file.status === CHANGE_STATUS.UNTRACKED ? ["diff", "--no-index", "--", "/dev/null", file.path] : ["diff", file.diffBase ?? "HEAD", "--", file.path];
	const result = await git(args);
	if (result.truncated || result.stdout.length > DIFF_MAX_BYTES || result.stdout.split("\n", DIFF_MAX_LINES + 2).length > DIFF_MAX_LINES + 1) return DIFF_TOO_LARGE;
	return result.code === 0 || result.code === 1 ? result.stdout : "";
}

// Untracked files have no numstat; count their lines without reading past the diff bound.
const untrackedLines = (root: string): LineCounter => async (path) => {
	const target = join(root, path);
	const info = await lstat(target);
	if (!info.isFile() || info.size > DIFF_MAX_BYTES) return 0;
	const text = await readFile(target, "utf8");
	return text === "" ? 0 : text.split("\n").length - (text.endsWith("\n") ? 1 : 0);
};
// Same-count edits still change size or mtime, so an open diff reloads.
const fileRevision = (root: string): FileRevision => async (path) => {
	const info = await lstat(join(root, path));
	return `${info.size}:${info.mtimeMs}`;
};

// The editor launch is shared with the change detail's o (tasks.md); it lives beside it.
export { openInExternalEditor, type ExternalEditorHost } from "../lib/ui/jhon-panel-editor.ts";

/** The key that opens /jhon:changes, shell.keys.changes (JHON_SHELL_CHANGES_KEY overrides it), or off. */
export function changesShortcut(settings: ShellSettings = shellSettings()): string | undefined {
	return settings.key("changes");
}

/**
 * The key that opens /jhon:usage (T93, L659), shell.keys.usage (T167): alt+u,
 * another (JHON_SHELL_USAGE_KEY overrides it), or off.
 */
export function usageShortcut(settings: ShellSettings = shellSettings()): string | undefined {
	return settings.key("usage");
}

function positiveMs(value: string | undefined, fallback: number): number {
	const parsed = Number.parseInt(value ?? "", 10);
	return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function changesPollMs(env: NodeJS.ProcessEnv): number {
	return positiveMs(env.JHON_SHELL_CHANGES_POLL_MS, CHANGES_POLL_DEFAULT_MS);
}

// One bounded window per provider refresh: a credential lookup or fetch that
// never answers must not hold the other providers — or the panel opening on
// them — past it. Tunable (tests, slow networks); invalid values fall back.
const USAGE_FETCH_TIMEOUT_DEFAULT_MS = 10_000;
const usageFetchTimeoutMs = (env: NodeJS.ProcessEnv): number => positiveMs(env.JHON_SHELL_USAGE_TIMEOUT_MS, USAGE_FETCH_TIMEOUT_DEFAULT_MS);

function changesFingerprint(model: ChangesModel): string {
	return model.files.map((file) => `${file.path}:${file.status}:${file.added}:${file.deleted}:${file.diffRevision ?? ""}:${file.countsUnavailable ?? ""}`).join("|");
}

interface OverlayDeps {
	loadDiff(root: string, file: ChangedFile): Promise<string>;
	worktrees(): WorktreeChanges[];
	refresh(): Promise<ChangesModel>;
	apply(ctx: ExtensionContext, model: ChangesModel): void;
	pollMs: number;
	/** A folder whose worktree it opens on (/jhon:changes <dir>, the change detail's d). */
	focus?: string;
}

// The overlay polls each root's Git state while it is open; diffs load lazily.
async function showChangesOverlay(ctx: ExtensionContext, deps: OverlayDeps): Promise<void> {
	let host: ExternalEditorHost | undefined;
	let view: WorktreeChangesView | undefined;
	const refresh = async () => {
		const latest = await deps.refresh();
		view?.update(deps.worktrees());
		deps.apply(ctx, latest);
	};
	const poll = setInterval(() => void refresh().catch(() => {}), deps.pollMs);
	poll.unref();
	try {
		const chosen = await ctx.ui.custom<{ root: string; file: ChangedFile } | null>(
			(tui, theme, _keybindings, done) => {
				const close = withOverlayRepaint(tui, done);
				host = tui;
				const opened = new WorktreeChangesView(deps.worktrees(), {
					theme,
					rows: () => Math.max(0, tui.terminal.rows),
					loadDiff: deps.loadDiff,
					onOpen: (root, file) => close({ root, file }),
					onRefresh: () => void refresh().catch(() => {}),
					onClose: () => close(null),
					requestRender: () => tui.requestRender(),
					ascii: shellSettings().ascii(),
				});
				// /jhon:changes <dir> (the change detail's d): that worktree alone.
				if (deps.focus) opened.focus(deps.focus, true);
				view = opened;
				// As G's: the native fullscreen wrapper keeps keyboard, hover, click and wheel on the view.
				const interaction = createNativeFullscreenInteraction({ keyboardTarget: opened, requestRender: () => tui.requestRender() });
				interaction.addChild(opened);
				return interaction;
			},
			// G's changes view: the whole terminal.
			{ overlay: true, overlayOptions: { width: "100%", maxHeight: "100%", margin: 0, anchor: "center" } },
		);
		if (!chosen || !host) return;
		const failed = openInExternalEditor(host, chosen.file.path, process.env, spawnSync, chosen.root);
		if (failed) ctx.ui.notify(failed, "warning");
	} finally {
		clearInterval(poll);
		view?.dispose();
		view = undefined;
	}
}

function showChanges(ctx: ExtensionContext, model: ChangesModel, visible = true, turn: () => TurnSummary | undefined = () => undefined): void {
	if (!visible || model.files.length === 0) {
		ctx.ui.setWidget(CHANGES_WIDGET_KEY, undefined);
		return;
	}
	ctx.ui.setWidget(
		CHANGES_WIDGET_KEY,
		(tui, theme) => sidebarPart(tui, "changes", {
			render(width: number) {
				return renderChangesWidget(model, theme, width, turn());
			},
			invalidate() {},
		}),
		{ placement: "belowEditor" },
	);
}

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

const USAGE_REFRESH_MS = 5 * 60_000;

// The NaN Cloud quota endpoint is the one the official dashboard reads with the
// same API key pi already holds. The key travels in the header only: the request
// refuses redirects so it cannot be replayed to another origin, asks for no
// stored copy, and nothing here logs, renders, or persists it.
export async function fetchNanUsage(apiKey: string | undefined, fetchFn: typeof fetch, now: number): Promise<ProviderUsage | undefined> {
	if (!apiKey) return undefined;
	try {
		const response = await fetchFn(NAN_QUOTA_URL, {
			redirect: "error",
			cache: "no-store",
			headers: { Authorization: `Bearer ${apiKey}`, Accept: "application/json", "User-Agent": "jhon-shell" },
		});
		if (!response.ok) return undefined;
		const parsed = parseNanQuota(await response.json(), now);
		return parsed.limits.length > 0 ? parsed : undefined;
	} catch {
		return undefined;
	}
}

// A registered source is foreign code running inside a fire-and-forget
// refresh: it must degrade exactly like the built-in NaN fetcher above, never
// throw past this call, and never leave an unhandled rejection behind.
async function fetchFromSource(source: UsageSource, apiKey: string | undefined, fetchFn: typeof fetch, now: number): Promise<ProviderUsage | undefined> {
	try {
		const result = await source.fetch(apiKey, fetchFn, now);
		return result === undefined ? undefined : parseProviderUsage(result, source.provider);
	} catch {
		return undefined;
	}
}

// Changes refreshes started without awaiting (turn end, worktree registration)
// are tracked so tests can wait for them instead of guessing a delay.
const backgroundChangeRefreshes = new Set<Promise<void>>();
function trackChangeRefresh(refresh: Promise<void>): void {
	const tracked = refresh.catch(() => {}).finally(() => { backgroundChangeRefreshes.delete(tracked); });
	backgroundChangeRefreshes.add(tracked);
}

/** Resolves once every background Changes refresh has finished, or after `timeoutMs` for one that never settles. */
export async function backgroundChangeRefreshesSettled(timeoutMs = 2000): Promise<void> {
	let timer: ReturnType<typeof setTimeout> | undefined;
	const deadline = new Promise<void>((resolve) => { timer = setTimeout(resolve, timeoutMs); });
	const settled = (async () => { while (backgroundChangeRefreshes.size > 0) await Promise.all([...backgroundChangeRefreshes]); })();
	await Promise.race([settled, deadline]);
	clearTimeout(timer);
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
	const usage = new UsageStore();
	// Providers jhon-shell has never heard of get a usage source too, when
	// the extension that owns them registers one on pi.events; see the
	// USAGE_SOURCE_EVENT subscription below.
	const usageSources = new UsageSourceRegistry();
	let renderHost: ShellRenderHost | undefined;
	// The 5-minute rule is per provider: one provider's fetch cannot leave the
	// next one waiting for an interval it never used.
	const usageFetchedAt = new Map<string, number>();
	// Providers whose latest settled refresh answered nothing, which the usage
	// panel says; a snapshot recorded later clears them.
	const usageFailures = new Set<string>();
	// Monotonic per-provider generation: each dispatched refresh takes the next
	// number, and only the newest one for a provider may mutate its state. Two
	// overlapping refreshes of the SAME source (or of builtins) can otherwise
	// let an older failure settle after a newer success and poison it.
	const usageGenerations = new Map<string, number>();
	const usageFetchTimeout = usageFetchTimeoutMs(env);
	// The models Jhon's sub-agents run on (agents/<name>.md in the agent dir
	// `jhon start pi` sets), so their providers' usage shows beside the
	// session's own. Without that dir there are no Jhon sub-agents to read.
	const agentDir = env.PI_CODING_AGENT_DIR;
	// T149 (L1339): a session on a profile of its own runs its sub-agents on that
	// profile's models, not the rendered files' (lib/session-profile.ts), so the
	// scope reads them the way extensions/jhon.ts launches them. The rows are read
	// when the session's profile is known; a refresh waits for that read.
	const routingProfile = new SessionProfile((cwd) => (deps.profilesJson ?? readProfilesJson)(cwd));
	let routingRead: Promise<unknown> = Promise.resolve();
	const followRoutingProfile = (name: string | undefined, cwd: string) => {
		routingProfile.start(name);
		routingRead = agentDir && name ? routingProfile.refresh(cwd).catch(() => undefined) : Promise.resolve();
	};
	const activeRoutingModels = (): Array<string | undefined> => agentDir ? knownAgents(agentDir).map((name) => {
		try { return routingProfile.agent(resolveAgent(agentDir, name)).model; } catch { return undefined; }
	}) : [];
	// A bare model id names a provider only when exactly one provider in the
	// registry carries that id; anything else stays untargeted rather than guessed.
	const bareModelProvider = (ctx: ExtensionContext, modelId: string): string | undefined => {
		try {
			const providers = new Set(ctx.modelRegistry.getAll().filter((model) => model.id === modelId).map((model) => model.provider));
			return providers.size === 1 ? [...providers][0] : undefined;
		} catch {
			return undefined;
		}
	};
	const usageScopeFor = (ctx: ExtensionContext): string[] =>
		usageScopeProviders(ctx.model?.provider, activeRoutingModels(), (modelId) => bareModelProvider(ctx, modelId));
	// The scope is Git/filesystem work, so it is resolved once per refresh and
	// per registration, never per render.
	const resolveUsageScope = usageScopeFor;
	// One notification per settled provider — snapshot or failure — so the
	// shell and the open overlay repaint immediately instead of waiting for
	// the whole refresh, whose slowest member is the bounded window itself.
	const notifyUsageSettled = () => {
		renderHost?.invalidateSidebar?.();
		renderHost?.requestRender();
	};
	// One bounded, self-contained refresh per provider. Providers run
	// concurrently: a stalled credential lookup or fetch for one can never hold
	// the others — or the overlay opening on them — past its own window.
	const refreshProvider = async (ctx: ExtensionContext, provider: string, source: UsageSource | undefined, force: boolean, now: number): Promise<boolean> => {
		if (!source && provider !== NAN_PROVIDER) return false;
		if (!force && now - (usageFetchedAt.get(provider) ?? 0) < USAGE_REFRESH_MS) return false;
		usageFetchedAt.set(provider, now);
		const generation = (usageGenerations.get(provider) ?? 0) + 1;
		usageGenerations.set(provider, generation);
		// The window covers credential resolution and the fetch together. The
		// abort signal reaches the underlying fetch through the wrapped fetchFn —
		// built-ins and registered sources alike — so an expired provider is
		// actually cancelled when its caller honors the signal. The race is the
		// only path to state mutation: whatever the work resolves after the
		// window expired is a value nobody reads, so a late answer — success or
		// failure — cannot mutate anything.
		const controller = new AbortController();
		let expire: (() => void) | undefined;
		const expired = new Promise<"timeout">((resolve) => { expire = () => resolve("timeout"); });
		const timer = setTimeout(() => {
			controller.abort();
			expire?.();
		}, usageFetchTimeout);
		// The window's signal composes with whatever the caller already carries —
		// init.signal, or a Request input's own signal — instead of replacing it:
		// either side aborting still aborts, exactly like a plain fetch. With no
		// caller signal the window's signal passes through unchanged.
		const boundedFetch: typeof fetch = (input, init) => {
			const callerSignal = init?.signal ?? (typeof Request !== "undefined" && input instanceof Request ? input.signal : undefined);
			return deps.fetch(input, {
				...init,
				signal: callerSignal ? AbortSignal.any([callerSignal, controller.signal]) : controller.signal,
			});
		};
		const work = (async () => {
			const apiKey = await ctx.modelRegistry.getApiKeyForProvider(provider).catch(() => undefined);
			// The window may have expired while the credential was resolving: an
			// aborted provider must not start its fetch at all.
			if (controller.signal.aborted) return undefined;
			return source ? fetchFromSource(source, apiKey, boundedFetch, deps.now()) : fetchNanUsage(apiKey, boundedFetch, deps.now());
		})();
		// The race's loser still runs to completion in the background: guard it so
		// its rejection (a foreign source escaping its own catch) can never
		// surface as unhandled, and clear the timer so nothing dangles.
		const guarded = work.catch(() => undefined);
		const settled = await Promise.race([guarded, expired]);
		clearTimeout(timer);
		// Discard before any state mutation: a generation mismatch means a newer
		// refresh for this provider was dispatched and owns the state, and a
		// replaced source's answer belongs to neither. A late success must not
		// overwrite the newer snapshot, and a late failure must not mark the
		// provider failed after its replacement succeeded.
		if (usageGenerations.get(provider) !== generation) return false;
		if (source && usageSources.get(provider) !== source) return false;
		if (settled === "timeout" || !settled) {
			usageFailures.add(provider);
			notifyUsageSettled();
			return false;
		}
		usage.record(settled);
		usageFailures.delete(provider);
		notifyUsageSettled();
		return true;
	};
	const refreshUsage = async (ctx: ExtensionContext, force: boolean, only?: string) => {
		await routingRead;
		const scope = resolveUsageScope(ctx);
		const now = deps.now();
		// Concurrent per provider; each settle notifies the shell (and repaints
		// the open overlay) on its own, so a fast provider never waits for the
		// slowest one's window. A discarded late answer notifies nothing: the
		// refresh that owns the state notifies at its own settle.
		await Promise.all(
			scope
				.filter((provider) => only === undefined || provider === only)
				.map((provider) => refreshProvider(ctx, provider, usageSources.get(provider), force, now)),
		);
	};
	// Subscribed once, for the life of the extension: a registration can
	// arrive before the first session_start (the owning extension's factory
	// runs first) or after it (its own session_start fires later, or it
	// registers lazily). Either order is fine: a registration for the
	// currently active provider forces exactly one refresh, so the panel
	// never waits for the 5-minute window or the next turn to notice it.
	// A host announced late (jhon:host) may drop the title row's path: redraw.
	pi.events.on(HOST_EVENT, () => { renderHost?.requestRender(); });
	// T120: `u` in the profiles panel, or a session profile that is gone: the bar and the rail show it at once.
	pi.events.on(SESSION_PROFILE_CHANGED, (payload) => {
		const said = payload as { sessionId?: unknown; profile?: unknown } | undefined;
		let current: string | undefined;
		try { current = currentContext?.sessionManager.getSessionId(); } catch { current = undefined; }
		if (!said || current === undefined || said.sessionId !== current) return;
		sessionProfile = typeof said.profile === "string" && said.profile ? said.profile : undefined;
		followRoutingProfile(sessionProfile, currentContext?.cwd ?? process.cwd());
		if (!profileReader.refresh()) return;
		renderHost?.invalidateSidebar?.();
		renderHost?.requestRender();
	});
	pi.events.on(USAGE_SOURCE_EVENT, (payload) => {
		const source = parseUsageSource(payload);
		if (!source) return;
		usageSources.register(source);
		if (!currentContext) return;
		// A registration is interesting when its provider is targeted at all —
		// the session's own or a subagent route of the active profile — and then
		// only that provider is forced, never the whole scope.
		if (resolveUsageScope(currentContext).includes(source.provider)) void refreshUsage(currentContext, true, source.provider);
	});
	// pi's event names no provider. The one answering is the session's model's:
	// anthropic itself, or claude-bridge re-emitting the same headers. Keyed
	// any other way, the bar and the panel (which look the session's provider
	// up) never find it.
	const recordReplyUsage = (parsed: ProviderUsage | undefined) => {
		if (!parsed) return;
		usage.record(parsed);
		usageFailures.delete(parsed.provider);
		renderHost?.invalidateSidebar?.();
		renderHost?.requestRender();
	};
	pi.on("after_provider_response", (event, ctx) => {
		recordReplyUsage(parseUsageHeaders(event.headers, deps.now(), ctx?.model?.provider));
	});
	// T149: Codex's WebSocket transport (pi's default) sends no reply headers to
	// after_provider_response; its windows come as a `codex.rate_limits` stream
	// event, which names its provider. Every other event is one type check.
	pi.on("provider_stream_event", (event, ctx) => {
		recordReplyUsage(parseCodexRateLimitEvent(event.data, deps.now(), event.provider ?? ctx?.model?.provider));
	});
	// /jhon:usage and alt+u (T93, L659): every configured provider's windows and
	// resets, the session's provider first with 力; it opens at once on what the
	// store holds and repaints as the forced refresh settles.
	const openUsage = (ctx: ExtensionContext) =>
		ctx.ui.custom<null>(
			(tui, theme, _keybindings, done) => {
				let scope = resolveUsageScope(ctx);
				const view = new UsageView(usage, {
					theme,
					now: () => deps.now(),
					active: () => (ctx.model ? { provider: ctx.model.provider } : undefined),
					registry: () => usageSources,
					scope: () => ({ providers: scope, failed: new Set(usageFailures) }),
					// The session profile's routes may still be reading: the scope follows them.
					onRefresh: async () => {
						await routingRead;
						scope = resolveUsageScope(ctx);
						await refreshUsage(ctx, true);
					},
					onClose: () => withOverlayRepaint(tui, done)(null),
					requestRender: () => tui.requestRender(),
					ascii: shellSettings().ascii(),
				});
				view.refresh();
				return view;
			},
			{ overlay: true, overlayOptions: { anchor: "center", width: "70%", minWidth: 60, maxHeight: "85%" } },
		);
	pi.registerCommand("jhon:usage", {
		description: "Every configured provider's subscription usage windows and resets; r refreshes",
		handler: async (_args, ctx) => { await openUsage(ctx); },
	});
	const usageKey = usageShortcut(shellSettings(env));
	if (usageKey) {
		pi.registerShortcut(usageKey as Parameters<ExtensionAPI["registerShortcut"]>[0], {
			description: "Shows every configured provider's subscription usage",
			handler: async (ctx) => { await openUsage(ctx); },
		});
	}
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
	let sidebarTui: TUI | undefined;
	/** The rail cards folded by a click this session (STATUS, JOBS, AGENTS; TODOS folds with its own key). */
	const railFolded = new Set<string>();
	// The Gentle Shell's card style and chrome surface (L153 answers 1-2) live in
	// process-wide slots every extension's cards and the rail read.
	const applyAppearance = () => {
		setCardStyle(settings.cards());
		setChromeSurface(visualSettings.surface);
	};
	const refreshVisual = () => {
		visualSettings = resolveVisualSettings(animationOptions).settings;
		applyAppearance();
		if (currentContext && changes) showChanges(currentContext, changes.model, true, () => lastTurn);
		renderHost?.invalidateSidebar?.();
		renderHost?.requestRender();
	};
	let doubleEscCancelPolicy: DoubleEscCancelPolicy = resolveDoubleEscCancelPolicy({ settings }).policy;
	let changes: WorktreeChangesTracker | undefined;
	// The session's finished turns, for the changes line: «turno 6 · 2m 14s».
	let turns = 0;
	let turnStartedAt: number | undefined;
	let lastTurn: TurnSummary | undefined;
	const apiWait = createApiWait(() => deps.now());
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
	// This session's running background tasks (lib/background-tasks-tools.ts).
	let runningJobs: RunningJobDisplay[] | undefined;
	// The jobs that finished lately, which JOBS shows for a minute (L168).
	let finishedJobs: RunningJobDisplay[] = [];
	let jobClock: ReturnType<typeof setInterval> | undefined;
	const lingering = () => finishedJobs.some((job) => deps.now() - (job.endedAt ?? 0) < JOBS_FINISHED_TTL_MS);
	const displays = (items: unknown): RunningJobDisplay[] => Array.isArray(items)
		? items.filter((item): item is RunningJobDisplay => typeof item?.id === "string" && typeof item.label === "string" && typeof item.startedAt === "number")
		: [];
	pi.events.on(TASKS_EVENT, (value) => {
		const event = value as { sessionId?: unknown; tasks?: unknown; finished?: unknown } | undefined;
		if (!currentContext || event?.sessionId !== currentContext.sessionManager.getSessionId() || !Array.isArray(event.tasks)) return;
		runningJobs = displays(event.tasks);
		finishedJobs = displays(event.finished).filter((job) => typeof job.endedAt === "number");
		// The cards' times run live: one redraw a second while a task runs or a
		// finished job still shows, none when idle.
		if ((runningJobs.length || lingering()) && !jobClock) {
			jobClock = setInterval(() => {
				if (!runningJobs?.length && !lingering() && jobClock) { clearInterval(jobClock); jobClock = undefined; }
				renderHost?.invalidateSidebar?.();
				renderHost?.requestRender();
			}, 1000);
			jobClock.unref?.();
		} else if (!runningJobs.length && !lingering() && jobClock) {
			clearInterval(jobClock);
			jobClock = undefined;
		}
		renderHost?.invalidateSidebar?.();
		renderHost?.requestRender();
	});
	let shown = "";
	const applyChanges = (ctx: ExtensionContext, model: ChangesModel) => {
		const fingerprint = changesFingerprint(model);
		if (fingerprint === shown) return;
		shown = fingerprint;
		showChanges(ctx, model, true, () => lastTurn);
	};
	const gitForRoot = (root: string) => deps.gitRunner(root);
	// Changes is the Git state of the session's roots that still exist, each
	// against the base its registration holds (T132): read at turn end, on
	// panel open and by the panel's poll, never at startup.
	const changesTracker = (ctx: ExtensionContext) => {
		const sessionId = ctx.sessionManager.getSessionId();
		const roots = () => {
			try { return sessionChangeRoots(sessionId, ctx.sessionManager.getEntries()).filter((root) => deps.rootExists(root) && !isHomeOrBroadUserDirectory(root)); }
			catch { return []; }
		};
		const bases = () => sessionRootBases(sessionId, ctx.sessionManager.getEntries());
		const subagents = () => sessionSubagentRoots(sessionId, ctx.sessionManager.getEntries());
		return new WorktreeChangesTracker(gitForRoot(ctx.cwd), gitForRoot, untrackedLines, roots, fileRevision, bases, subagents);
	};
	const refreshChanges = async (ctx: ExtensionContext) => {
		const tracker = changes;
		if (!tracker || !ctx.hasUI || registry?.sessionId !== ctx.sessionManager.getSessionId()) return;
		const model = await tracker.refresh();
		if (changes === tracker) applyChanges(ctx, model);
	};
	pi.registerTool({
		name: "session_worktree_register",
		renderShell: "self",
		label: "Register session worktree",
		description: "Add a worktree of this Git clone to the session: /jhon:changes then lists what changes there from now on, committed or not.",
		parameters: { type: "object", required: ["path"], additionalProperties: false, properties: { path: { type: "string", description: "Worktree path to include in this session." } } } as never,
		async execute(_id, params, _signal, _onUpdate, ctx) {
			if (!registry || registry.sessionId !== ctx.sessionManager.getSessionId()) throw new Error("No active session worktree registry.");
			const root = registry.register((params as { path: string }).path, "explicit");
			trackChangeRefresh(refreshChanges(ctx));
			return { content: [{ type: "text", text: `Registered session worktree: ${root}` }], details: { root, sessionId: registry.sessionId } };
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
	// Another session can move the change on (a worker, a Claude Code
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
			renderHost?.invalidateSidebar?.();
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
		finishedJobs = [];
		stopProfilePoll();
		registry?.close();
		currentContext = ctx;
		changes = undefined;
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
			followRoutingProfile(sessionProfile, ctx.cwd);
			profileReader.bind(ctx.cwd, deps.resolveWorktree, ctx.sessionManager.getSessionId());
			const sessionId = ctx.sessionManager.getSessionId();
			profilePoll = setInterval(() => {
				if (currentContext !== ctx || ctx.sessionManager.getSessionId() !== sessionId) return;
				if (!profileReader.refresh()) return;
				renderHost?.invalidateSidebar?.();
				renderHost?.requestRender();
			}, 2_000);
			profilePoll.unref?.();
		}
		changes = changesTracker(ctx);
		lanes = laneReads();
		releaseSelection?.();
		releaseSelection = shareSelection(ctx.sessionManager.getSessionId(), () => currentContext === ctx ? refreshLdd(ctx) : Promise.resolve(ldd));
		void refreshLdd(ctx);
		if (ctx.hasUI) watchLddSources(ctx);
		const tracker = changes;
		ctx.ui.setFooter((tui, theme, footerData) => {
			sidebarTui = tui;
			const host: ShellRenderHost = { requestRender: () => tui.requestRender(), invalidateSidebar: () => invalidateSidebar(tui) };
			renderHost = host;
			const unsubscribeBranch = footerData.onBranchChange(() => {
				host.invalidateSidebar?.();
				host.requestRender();
			});
			// The Status card paints live session state that no event re-registers a
			// part for: model, effort, context, cost, session name and extension
			// statuses. The digest is what keeps the fullscreen memo honest.
			const footerModel = (): ShellBarModel => ({
				...buildShellBarModel(pi, ctx, footerData, { dirty: tracker.model.files.length, usage: usage.get(ctx.model?.provider ?? ""), profile: deps.activeProfile(), jobs: runningJobs }),
				changes: { files: tracker.model.files.length, added: tracker.model.added, deleted: tracker.model.deleted, ...(tracker.headRoots.length ? { headOnly: tracker.headRoots.map(({ root, comparison }) => `${basename(root)}: ${fallbackReason(comparison)}.`) } : {}), ...(tracker.failedRoots.length ? { failed: { roots: tracker.failedRoots.length, of: tracker.scannedRootCount } } : {}) },
				...(ldd ? { ldd } : {}),
				...(mode ? { mode } : {}),
				...(hostShowsPath() ? { hostShowsPath: true } : {}),
				// The rail's TODOS card (todo.ts) shows the agent's list: STATUS leaves the slice's tasks to it.
				...(tui.terminal && sidebarState(tui).parts.has("todo") ? { planShown: true } : {}),
			});
			// The Gentle Shell's split chrome (L153 answer 3): a title row on top and the
			// status bar at the bottom. Wide (the rail shown) each sits between two
			// margin rows; without the rail in fullscreen exactly one status row, G's,
			// with `⧗ N jobs` in it, the model in the title row, the integrations in
			// the rail only. A regular terminal draws G's one
			// compact bar instead.
			const fullscreen = () => (tui as TUI & { mode?: string }).mode === "fullscreen";
			const wide = () => fullscreen() && (tui.terminal?.columns ?? 0) >= SIDEBAR_BREAKPOINT && visualSettings.statusPlacement !== "hidden";
			// A regular terminal draws G's compact bar, integrations included (L160).
			// Fullscreen, the footer always steps aside (the rail, or the statusbar row narrow).
			const bottomBar = {
				render: (width: number) => renderShellBar(footerModel(), theme, width),
				invalidate() {},
				dispose() { unsubscribeBranch(); },
			};
			// A click on a card's header folds or opens it; TODOS folds with its
			// own key too. Kept for the session (T15.8).
			const fold = (title: string) => {
				if (railFolded.has(title)) railFolded.delete(title); else railFolded.add(title);
				invalidateSidebar(tui);
				return { handled: true, render: true };
			};
			// Every header is a control, as the Gentle Shell's: a move over one paints
			// it in the hover role, a move anywhere else clears it (setSidebarHover).
			const hoverTo = <T>(owner: object, current: T | undefined, next: T | undefined, set: (value: T | undefined) => void) => {
				if (next !== undefined) setSidebarHover(tui, owner, () => { set(undefined); invalidateSidebar(tui); tui.requestRender(); });
				else setSidebarHover(tui);
				if (next === current) return { handled: true };
				set(next);
				invalidateSidebar(tui);
				return { handled: true, render: true };
			};
			// The rail's Status sections, as the Gentle Shell's (L153), each folding
			// on its own: LDD leads the rail as its own part (L174), CHANGES and
			// INTEGRATIONS stay in the footer's place; the CHANGES row opens /jhon:changes.
			const statusRail = (only: readonly StatusSection[]): SidebarRail => {
				let statusAt: StatusTarget[] = [];
				const statusGroups = new Map<number, Array<{ start: number; end: number }>>();
				let statusHover: StatusHover | undefined;
				const statusOwner = {};
				return {
					surface: "panel",
					digest: () => JSON.stringify([footerModel(), tracker.model, visualSettings, [...railFolded], statusHover ?? ""]),
					render: (width) => {
						const layout = renderShellStatusLayout(footerModel(), theme, width, railFolded, statusHover, only, visualSettings.density);
						statusAt = layout.targets;
						statusGroups.set(width, layout.groups);
						return layout.lines;
					},
					cardGroups: (width) => { const groups = statusGroups.get(width); return groups ? { groups } : undefined; },
					invalidate() {},
					handleMouse(event) {
						const target = statusAt.find((candidate) => candidate.row === event.y);
						if (event.type === "move" && event.button === "none") {
							return hoverTo<StatusHover>(statusOwner, statusHover, !target ? undefined : target.kind === "toggle" ? target.section : "open-changes", (value) => { statusHover = value; });
						}
						if (event.type !== "click" || event.button !== "left" || !target) return undefined;
						if (target.kind === "open-changes") {
							// T171: the click is counted, as /jhon:changes is (lib/ui-use.ts).
							recordUse(pi, "click changes");
							void openChanges(ctx);
							return { handled: true, render: true };
						}
						return fold(target.section);
					},
				};
			};
			const disposeLdd = sidebarSlot(tui, "ldd", statusRail(["LDD"]));
			const part = sidebarPart(tui, "footer", bottomBar, statusRail(["CHANGES", "INTEGRATIONS"]));
			// Background work gets its own rail sections, out of Status, each its own
			// part as in Gentle Shell: JOBS (commands and monitors) and AGENTS (sub-agents) (L168).
			const workPart = (key: string, title: string, render: typeof renderShellJobsSection, summary?: () => PanelSummary | undefined, open?: () => void) => {
				let hovered = false;
				const owner = {};
				return sidebarPart(tui, key, { render: (): string[] => [], invalidate() {}, dispose() {} }, {
					surface: "panel",
					digest: () => JSON.stringify([runningJobs, finishedJobs, runningJobs?.length || lingering() ? Math.floor(deps.now() / 1000) : 0, railFolded.has(title), hovered]),
					render: (width) => render([...(runningJobs ?? []), ...finishedJobs], theme, width, deps.now(), railFolded.has(title), hovered),
					...(summary ? { summary } : {}),
					...(open ? { open } : {}),
					invalidate() {},
					handleMouse(event) {
						if (event.type === "move" && event.button === "none") return hoverTo<true>(owner, hovered || undefined, event.y === 0 ? true : undefined, (value) => { hovered = value ?? false; });
						if (event.type !== "click" || event.button !== "left" || event.y !== 0) return undefined;
						return fold(title);
					},
				});
			};
			const jobsPart = workPart("jobs", "JOBS", renderShellJobsSection, undefined, () => { pi.events.emit(OPEN_TASKS_EVENT, { ctx, view: "jobs" }); });
			// The work row's lead while sub-agents run, as the Gentle Shell's: `◐ N agents`.
			const agentsPart = workPart("agents", "AGENTS", renderShellAgentsSection, () => agentsWorkSummary([...(runningJobs ?? []), ...finishedJobs], theme, deps.now()), () => { pi.events.emit(OPEN_TASKS_EVENT, { ctx }); });
			// The status line spans the width under the rail (L97); the rail has
			// no key line, "/ commands" is the prompt's alone (L143). Narrow, it is
			// G's one status row, `⧗ N jobs` in it and the model in the title row,
			// a sibling row under the native layout while the footer steps aside.
			const disposeStatusbar = sidebarSlot(tui, "statusbar", {
				digest: () => JSON.stringify([footerModel(), visualSettings, fullscreen() && !wide()]),
				render: (width) => visualSettings.statusPlacement === "hidden" ? []
					: fullscreen() && !wide() ? renderShellStatusBar(footerModel(), theme, width, { jobs: true, modelDetails: false }).rows
					: renderShellChromeBar(footerModel(), theme, width, {}, visualSettings.density).rows,
				invalidate() {},
			});
			// The title row: the brand, the project and its path, the session name;
			// narrow, the model · effort · profile in the session name's place.
			const disposeHeader = sidebarHeader(tui, {
				digest: () => JSON.stringify([footerModel(), visualSettings, tui.terminal?.columns]),
				render: (width) => wide() ? renderShellTitleRows(footerModel(), theme, width, undefined, visualSettings.density) : [renderShellTitleRow(footerModel(), theme, width, undefined, { model: true })],
				invalidate() {},
			});
			const uninstall = installSidebar(tui, theme, () => visualSettings.statusPlacement, () => visualSettings.density);
			// Without the rail, live work folds into the editor's work row (T15.12c).
			if (tui.terminal) sidebarState(tui).compactWork = () => narrowChrome(tui);
			return { ...part,
				render(width: number) {
					if (visualSettings.statusPlacement === "hidden" && (tui as TUI & { mode?: string }).mode !== "fullscreen" && runningJobs?.length) {
						return [truncateToWidth(theme.fg("muted", shellJobsCount(runningJobs.length)), width, "…")];
					}
					return part.render(width);
				},
				dispose() { disposeHeader(); disposeStatusbar(); disposeLdd(); uninstall(); jobsPart.dispose(); agentsPart.dispose(); part.dispose(); if (sidebarTui === tui) sidebarTui = undefined; } };
		});
		void refreshUsage(ctx, true);
		const ownsPrompt = installPrompt(
			ctx,
			(created) => {
				prompt?.dispose();
				prompt = created;
				prompt.setAnimationPolicy(animationPolicy);
			},
			{ now: () => deps.now(), doubleEscCancelEnabled: () => doubleEscCancelPolicy === "on", dispatchQueuedText: queueAfterSettle, workingLabel: () => apiWait.label(),
				// Without the rail the work row above the rule folds TODOS and AGENTS.
				// The editor joins the fullscreen chrome, rail or not; a regular terminal keeps the float or neon card, as G's.
				panelChrome: (tui) => (tui as TUI & { mode?: string }).mode === "fullscreen" && Boolean(tui.terminal),
				topRows: (tui, width, hovered) => {
					if (!narrowChrome(tui)) return [];
					const row = renderWorkRow(workParts(tui).map((work) => work.summary), ctx.ui.theme, width, hovered === 0, shellSettings().ascii());
					return row ? [row] : [];
				},
				// A tap on the work row opens the leading section's own view (sub-agents
				// open /jhon:agents); a section without one, such as the todo list,
				// opens nothing (T169: the panel sheet is gone).
				onTopRowClick: (tui, index) => {
					if (!narrowChrome(tui) || index !== 0) return;
					const parts = workParts(tui);
					parts[workRowLead(parts.map((work) => work.summary))]?.part.open?.();
				} },
		);
		// Hide native feedback only when our petal replaces it. Native transcript
		// thinking blocks remain Pi-owned; this changes only the supported loader UI.
		if (ownsPrompt) ctx.ui.setWorkingVisible(false);
		if (changes !== tracker) return;
		shown = "";
		applyChanges(ctx, tracker.model);
	});
	pi.on("session_shutdown", (_event, ctx) => {
		closeSettings?.();
		runningJobs = undefined;
		finishedJobs = [];
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
		changes = undefined;
		currentContext = undefined;
		turns = 0;
		turnStartedAt = undefined;
		lastTurn = undefined;
		apiWait.end();
		openPrompts = 0;
		stall.sync();
	});
	const openChanges = async (ctx: ExtensionContext, focus?: string) => {
		if (!changes) return;
		const tracker = changes;
		const model = await tracker.refresh();
		if (changes !== tracker) return;
		applyChanges(ctx, model);
		// A failed root scan is unknown, never clean: say so whenever one failed.
		const failed = tracker.failedRoots.length;
		const roots = tracker.scannedRootCount;
		const heads = tracker.headRoots;
		if (model.files.length === 0) {
			if (failed > 0 && failed === roots) ctx.ui.notify(`Could not read the Git status of ${failed} ${failed === 1 ? "root" : "roots"}; the session's changes are unknown.`, "error");
			// A root read against HEAD never reads "since the session's start": its commits are not counted (T132).
			else if (failed > 0) ctx.ui.notify(heads.length ? `${noChangesText(heads, roots - failed - heads.length)}\nThe Git status failed in ${failed} of ${roots} roots.` : `No changes since the session's start in the roots Git could read; the Git status failed in ${failed} of ${roots} roots.`, "warning");
			else ctx.ui.notify(noChangesText(heads, roots - heads.length), "info");
			return;
		}
		if (failed > 0) ctx.ui.notify(`The Git status failed in ${failed} ${failed === 1 ? "root" : "roots"}; their changes are not in the list.`, "warning");
		// /jhon:changes <dir> on a sub-agent's root whose work another root lists (T132): say it, and open on that root.
		const folded = focus ? tracker.foldedRoots.filter(({ root }) => focus === root || focus.startsWith(root.endsWith("/") ? root : `${root}/`)).sort((a, b) => b.root.length - a.root.length)[0] : undefined;
		if (folded) ctx.ui.notify(`${folded.root} has no row of its own: its work is merged into ${folded.into} and is listed there.`, "info");
		const open = folded?.into ?? focus;
		// The view lists the roots with changes and, with no files, each root read against HEAD that has none.
		await showChangesOverlay(ctx, { loadDiff: (root, file) => loadFileDiff(deps.gitRunner(root, DIFF_MAX_BYTES), file), worktrees: () => tracker.shownRoots, refresh: () => tracker.refresh(), apply: applyChanges, pollMs: changesPollMs(env), ...(open ? { focus: open } : {}) });
	};
	pi.registerCommand(CHANGES_COMMAND_NAME, {
		description: "Git changes since the session's start, committed or not, in its worktrees",
		handler: async (args, ctx) => openChanges(ctx, String(args ?? "").trim() || undefined),
	});
	const shortcut = changesShortcut(settings);
	if (shortcut) {
		pi.registerShortcut(shortcut as Parameters<ExtensionAPI["registerShortcut"]>[0], {
			description: "Opens the session's Git changes, committed or not",
			handler: async (ctx) => openChanges(ctx),
		});
	}
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
				doubleEsc: () => { doubleEscCancelPolicy = resolveDoubleEscCancelPolicy({ settings }).policy; },
				status: () => { refreshVisual(); pi.events.emit(VISUAL_SETTINGS_CHANGED, { settings: settings.path }); },
				appearance: () => { refreshVisual(); pi.events.emit(VISUAL_SETTINGS_CHANGED, { settings: settings.path }); },
				redraw: () => { renderHost?.invalidateSidebar?.(); renderHost?.requestRender(); },
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
					finish = () => done(null);
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
				}, { overlay: true, overlayOptions: { anchor: "center", width: "70%", minWidth: 60, maxHeight: "85%" } });
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
		turnStartedAt = deps.now();
		apiWait.start();
		stall.sync();
	});
	// A provider request in flight with no stream event for a minute is named
	// on the working line; a running tool is not a wait on the API. Every
	// change re-arms or clears the stall recovery's timer.
	pi.on("turn_start", () => { apiWait.start(); stall.sync(); });
	pi.on("message_start", () => { apiWait.event(); stall.sync(); });
	pi.on("message_update", () => { apiWait.event(); stall.sync(); });
	pi.on("message_end", () => { apiWait.event(); stall.sync(); });
	pi.on("tool_execution_start", () => { apiWait.toolStart(); stall.sync(); });
	pi.on("tool_execution_end", () => { apiWait.toolEnd(); stall.sync(); });
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
		if (turnStartedAt !== undefined) {
			lastTurn = { n: ++turns, ms: deps.now() - turnStartedAt };
			turnStartedAt = undefined;
		}
		// A slow or failing Git never holds the turn end.
		trackChangeRefresh(refreshChanges(ctx));
		void refreshUsage(ctx, false);
		if (ctx.hasUI) refreshLdd(ctx);
	});
}
