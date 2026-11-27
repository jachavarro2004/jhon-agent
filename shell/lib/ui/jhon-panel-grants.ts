/**
 * shell/lib/ui/jhon-panel-grants.ts — /jhon:grants (T15.10, L113, L214):
 * the owner's grants for the session's project, given and revoked from
 * inside pi, where `! jhon grant` cannot run.
 *
 * Each class is one row with its state here; enter gives the chosen class
 * for the chosen time and x revokes it, each only after a second key that
 * confirms. It runs `jhon grant` as the owner's own key press does, never
 * as an agent's tool call: the safety guard still refuses `jhon grant` to
 * every agent. A grant given here lands in the grants' history, which
 * `jhon audit` reads as the owner's authorization.
 *
 * The card shows no permission mode and no key changes it: the mode
 * changes only in Customize → Permissions (L214), through changeMode below,
 * which keeps its one rule: super-yolo only after the owner's Yes.
 */
import { Key, matchesKey } from "@earendil-works/pi-tui";
import { MODE_BADGES } from "./bars.ts";
import { renderChoiceRow, renderListRow, themePainter } from "./primitives/index.ts";

/** The label column of the Duration row. */
const LABEL = 8;


/** `jhon grant --list --json`. */
export interface GrantsData {
	/** The project the session is in, "" outside one. */
	root: string;
	classes: string[];
	grants: Array<{ class: string; root: string; granted_at: string; expires_at: string; live: boolean }>;
}

/** `jhon mode --json`. */
export interface ModeData {
	mode: string;
	/** The project the mode covers, with its worktrees. */
	root: string;
	/** When the owner set it; null when no mode was ever set. */
	since: string | null;
	/** The primary whose mode a workspace member with none of its own takes (T281); absent otherwise. */
	from?: string;
}

/** The modes, Customize's radios, and what each does, in `jhon mode --help`'s words. */
export const MODES: Array<{ mode: string; label: string; words: string }> = [
	{ mode: "yolo", label: "yolo", words: MODE_BADGES.yolo!.detail },
	{ mode: "super-yolo", label: "super yolo", words: MODE_BADGES["super-yolo"]!.detail },
	{ mode: "normal", label: "normal", words: "the guards ask as they do" },
];

/** The one question super-yolo asks before it runs. */
export const SUPER_YOLO_QUESTION = "Turn on SUPER YOLO: destroy will not ask";

/** What a mode change came to: set, the mode on already, or the owner said no to super-yolo's question. */
export type ModeOutcome = "set" | "same" | "declined";

/**
 * The one way the owner changes the permission mode, Customize's
 * Permissions row (L175, L214): the mode on already runs nothing; super-yolo
 * runs only after the owner's Yes to SUPER_YOLO_QUESTION (a No, no answer, a
 * failed question or no one to ask runs nothing); then `jhon mode <mode>
 * --root <root>` through setMode. A failed run rejects with its reason.
 */
export async function changeMode(target: string, deps: { setMode(mode: string, root: string): Promise<void>; confirmMode?(question: string): Promise<boolean> }, current: ModeData, confirmed?: () => void): Promise<ModeOutcome> {
	if (target === current.mode) return "same";
	if (!current.root) throw new Error("this session is not in a project: the mode is per project");
	if (target === "super-yolo") {
		const confirm = deps.confirmMode;
		const yes = await Promise.resolve().then(() => confirm ? confirm(SUPER_YOLO_QUESTION) : false).catch(() => false);
		if (yes !== true) return "declined";
		confirmed?.();
	}
	await deps.setMode(target, current.root);
	return "set";
}

export interface GrantsPaint {
	fg(role: string, text: string): string;
	bold(text: string): string;
}

/** What each class lets an agent do, in the owner's words (internal/guard). */
export const CLASS_WORDS: Record<string, string> = {
	push: "git push: push or delete branches",
	"gh-write": "write to GitHub with gh: PRs, issues, api",
	publish: "publish: jhon publish, packages, images",
	remote: "remote access: ssh, scp, rsync, rclone, uploads with curl",
	deploy: "deploy: aws, gsutil, docker on another host",
	"outside-write": "write outside the project",
	destroy: "delete what loses work: branches, worktrees, docker volumes",
};

/** The durations enter gives, `jhon grant --for`'s values; d cycles them. */
export const DURATIONS: Array<[string, string]> = [["15m", "15 min"], ["30m", "30 min"], ["1h", "1 h"], ["2h", "2 h"], ["4h", "4 h"], ["8h", "8 h"], ["1d", "1 day"]];

export interface GrantsDeps {
	load(): Promise<GrantsData>;
	grant(cls: string, duration: string): Promise<void>;
	revoke(cls: string): Promise<void>;
	requestRender(): void;
}

/** How long until iso: «24 min», «3 h 5 min», «2 d». */
export function until(iso: string, now = Date.now()): string {
	const m = Math.max(0, Math.round((Date.parse(iso) - now) / 60000));
	if (m < 60) return `${m} min`;
	if (m < 1440) return m % 60 ? `${Math.floor(m / 60)} h ${m % 60} min` : `${m / 60} h`;
	return `${Math.round(m / 1440)} d`;
}

/** iso's local day, as the clock beside it is local too. */
function day(iso: string): string {
	const d = new Date(iso);
	return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

function clock(iso: string): string {
	const d = new Date(iso);
	return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

export class GrantsCard {
	private data: GrantsData | undefined;
	private error: string | undefined;
	busy: string | undefined;
	private note: { text: string; ok: boolean } | undefined;
	private cursor = 0;
	private duration = 1;
	/** The action waiting for its confirming key. */
	private pending: { kind: "grant" | "revoke"; cls: string } | undefined;
	loaded = false;
	private readonly deps: GrantsDeps;

	constructor(deps: GrantsDeps) {
		this.deps = deps;
	}

	reload(): void {
		this.busy = "loading…";
		this.deps.requestRender();
		const message = (e: unknown) => e instanceof Error ? e.message : String(e);
		let read: Promise<GrantsData>;
		try {
			read = this.deps.load();
		} catch (e) {
			read = Promise.reject(e);
		}
		void read.then((data) => [this.data, this.error] = [data, undefined], (e) => { this.error = message(e); }).then(() => {
			this.busy = undefined;
			this.loaded = true;
			this.deps.requestRender();
		});
	}

	private live(cls: string) {
		return this.data?.grants.find((g) => g.class === cls && g.root === this.data?.root && g.live);
	}

	private last(cls: string) {
		return this.data?.grants.filter((g) => g.class === cls && g.root === this.data?.root).sort((a, b) => b.expires_at.localeCompare(a.expires_at))[0];
	}

	/** Whether the key was the card's; esc while confirming cancels and stays. */
	handleInput(data: string): boolean {
		if (this.busy) return true;
		const classes = this.data?.classes ?? [];
		if (this.pending) {
			const p = this.pending;
			this.pending = undefined;
			if ((p.kind === "grant" && matchesKey(data, Key.enter)) || (p.kind === "revoke" && data === "x")) this.run(p.kind, p.cls);
			else this.note = { text: "Cancelled: nothing changed.", ok: true };
			this.deps.requestRender();
			return true;
		}
		const cls = classes[this.cursor];
		if (matchesKey(data, Key.up) || data === "k") this.cursor = Math.max(0, this.cursor - 1);
		else if (matchesKey(data, Key.down) || data === "j") this.cursor = Math.min(Math.max(0, classes.length - 1), this.cursor + 1);
		else if (data === "d") this.duration = (this.duration + 1) % DURATIONS.length;
		else if (data === "r") this.reload();
		else if (matchesKey(data, Key.enter) && cls && this.data?.root) this.pending = { kind: "grant", cls };
		else if (data === "x" && cls && this.live(cls)) this.pending = { kind: "revoke", cls };
		else return false;
		this.note = undefined;
		this.deps.requestRender();
		return true;
	}

	/** Esc confirms nothing: it cancels a waiting action instead of closing. */
	cancelPending(): boolean {
		if (!this.pending) return false;
		this.pending = undefined;
		this.note = { text: "Cancelled: nothing changed.", ok: true };
		this.deps.requestRender();
		return true;
	}

	private run(kind: "grant" | "revoke", cls: string): void {
		const [value, label] = DURATIONS[this.duration];
		this.busy = kind === "grant" ? `granting ${cls}…` : `revoking ${cls}…`;
		this.deps.requestRender();
		const settle = (error?: string) => {
			this.busy = undefined;
			this.note = error
				? { text: `Could not ${kind === "grant" ? "grant" : "revoke"} ${cls}: ${error}`, ok: false }
				: { text: kind === "grant" ? `${cls} granted for ${label}.` : `${cls} revoked.`, ok: true };
			this.reload();
		};
		try {
			(kind === "grant" ? this.deps.grant(cls, value) : this.deps.revoke(cls)).then(() => settle(), (e) => settle(e instanceof Error ? e.message : String(e)));
		} catch (e) {
			settle(e instanceof Error ? e.message : String(e));
		}
	}

	lines(paint: GrantsPaint, width = 76, now = Date.now(), ascii = false): string[] {
		const row = themePainter(paint);
		if (!this.data) return [this.error ? paint.fg("error", `Could not read the grants: ${this.error}`) : paint.fg("dim", "loading…")];
		const d = this.data;
		const out: string[] = [];
		const project = d.root ? d.root.split("/").pop() : "";
		out.push(...this.activeLines(paint, now, ascii));
		const head = `Grants${ascii ? " - " : " · "}${this.cursor + 1}/${d.classes.length}`;
		out.push(d.root ? `${paint.bold(paint.fg("accent", head))}  ${paint.fg("dim", `${project ?? d.root} · this project and its worktrees`)}` : paint.fg("warning", "This session is not in a project: grants are per project."));
		const nameWidth = Math.max(...d.classes.map((c) => c.length));
		for (const [i, cls] of d.classes.entries()) {
			const here = i === this.cursor;
			const live = this.live(cls);
			const last = this.last(cls);
			// One row per class, its state glyph as the mark; the description only under the cursor (P8, P10).
			const state = live ? `live ${until(live.expires_at, now)} · until ${clock(live.expires_at)}` : last ? `expired ${clock(last.expires_at)} on ${day(last.expires_at)}` : "not granted";
			out.push(...renderListRow({ width, text: `${cls.padEnd(nameWidth)}  ${paint.fg(live ? "text" : "dim", state)}`, mark: live ? "done" : last ? "failed" : "pending", cursor: here, ascii, paint: row }));
			if (here) out.push(...renderListRow({ width, text: paint.fg("muted", CLASS_WORDS[cls] ?? ""), wrap: true, ascii, paint: row }));
		}
		out.push("", renderChoiceRow({ width, label: "Duration", labelWidth: LABEL, value: DURATIONS[this.duration][1], ascii, paint: row }));
		if (this.pending) {
			const p = this.pending;
			out.push("", p.kind === "grant"
				? paint.fg("warning", `Grant ${p.cls} to ${project} for ${DURATIONS[this.duration][1]}? enter confirms · any other key cancels`)
				: paint.fg("warning", `Revoke ${p.cls} from ${project}? x confirms · any other key cancels`));
		} else if (this.note) out.push("", paint.fg(this.note.ok ? "success" : "error", this.note.text));
		return out;
	}

	/**
	 * Every live grant, here and in other projects (L175: the owner could not
	 * find them): its class, its scope and when it ends; or that there is none.
	 */
	private activeLines(paint: GrantsPaint, now: number, ascii: boolean): string[] {
		const d = this.data;
		if (!d) return [];
		const live = d.grants.filter((g) => g.live).sort((a, b) => Number(b.root === d.root) - Number(a.root === d.root) || a.expires_at.localeCompare(b.expires_at));
		const dot = ascii ? " - " : " · ";
		const out = [paint.bold(paint.fg("accent", `Active grants${dot}${live.length}`))];
		if (!live.length) out.push(`  ${paint.fg("dim", d.root ? "No active grants: enter on a class below gives one." : "No active grants.")}`);
		const nameWidth = Math.max(0, ...live.map((g) => g.class.length));
		const scope = (root: string) => root === d.root ? `${root.split("/").pop() ?? root}${dot}this project` : `${root.split("/").pop() ?? root}${dot}${root}`;
		const scopeWidth = Math.max(0, ...live.map((g) => scope(g.root).length));
		for (const g of live) out.push(`  ${g.class.padEnd(nameWidth)}  ${paint.fg("muted", scope(g.root).padEnd(scopeWidth))}  ${paint.fg("text", `until ${clock(g.expires_at)}${dot}${until(g.expires_at, now)}`)}`);
		out.push("");
		return out;
	}

	/** The chosen class's row in lines(), for the view to keep it and its description in view (after the active grants and the pane header). */
	cursorRow(): number {
		const active = this.data ? Math.max(1, this.data.grants.filter((g) => g.live).length) + 2 : 0;
		return active + 1 + this.cursor;
	}

	/** The card's keys, for the panel's key row. */
	keys(): string[][] {
		if (this.pending) return [[this.pending.kind === "grant" ? "enter" : "x", "confirm"], ["esc", "cancel"]];
		const cls = this.data?.classes[this.cursor];
		return [["↑↓", "choose"], ...(this.data?.root ? [["enter", "grant"]] : []), ...(cls && this.live(cls) ? [["x", "revoke"]] : []), ["d", "duration"], ["r", "refresh"]];
	}
}
