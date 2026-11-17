/**
 * shell/lib/ui/ldd-status.ts — the LDD change the session works on, for the
 * Status card: its slices, where the work is, and the last test run.
 *
 * Read through the binary (`jhon status --json`, read-only), never from the
 * project's files. jhon selects the session's change (the only active one,
 * else the one the branch names); when it selects none, none is shown. An
 * umbrella change, one other changes start from, is never the session's:
 * jhon selects it only when named, and STATUS lists it last (T37).
 */

import { execFile, spawnSync } from "node:child_process";
import { existsSync, realpathSync, statSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import { STATES } from "./primitives/states.ts";
import { THEME_NAME } from "./primitives/paint.ts";
import { gitReadEnv } from "../git-read.ts";

export interface LddStatus {
	change: string;
	/** The slice in progress, `Slice 7: deadline extension`, or undefined when every slice is done. */
	slice?: string;
	/** Each slice's state, in order. */
	slices: Array<"done" | "current" | "pending">;
	/** Each slice's name and tasks, in the same order. */
	detail?: Array<{ name: string; total: number; completed: number; delivers?: string; tasks?: Array<{ text: string; done: boolean }> }>;
	/** The change's checked tasks of all of them. */
	tasks?: { completed: number; total: number };
	/** The project follows Strict TDD (jhon status's strict_tdd). */
	strictTdd?: boolean;
	/** The change's route: standard, or verified (verify runs). */
	route?: string;
	/** The next step jhon status suggests (a phase or an action). */
	next?: string;
	tests?: { passed: boolean; failed: number; at: string; scope?: string };
	/** The change's tasks.md on disk, for the shell to watch; undefined when it cannot be found. */
	tasksFile?: string;
	/**
	 * A change with no folder (T31, L254): where `jhon executions run` keeps
	 * its record, `<git common dir>/jhon/changes/<change>`, for the shell to
	 * watch instead of tasks.md.
	 */
	recordDir?: string;
	/** The worktree the change was found in, when it is not the session's own folder. */
	worktree?: string;
	/** That worktree's path, where its change's commands run. */
	root?: string;
	/** The changes in progress in the repository's other worktrees, the lanes working beside it. */
	lanes?: Lane[];
}

/** A change in progress in another worktree: its name, where, how far, and its next step. */
export interface Lane {
	change: string;
	worktree: string;
	/** That worktree's path, which STATUS watches. */
	root?: string;
	done: number;
	slices: number;
	next?: string;
	/** An umbrella change (jhon status's umbrellas): split into changes, never in progress itself. */
	umbrella?: true;
}

/** rel (jhon status's directory, relative to the project's root) found from cwd or one of its parents. */
export function findUp(cwd: string, rel: string): string | undefined {
	for (let dir = cwd; ; dir = dirname(dir)) {
		const path = join(dir, rel);
		if (existsSync(path)) return path;
		if (dirname(dir) === dir) return undefined;
	}
}

/** The status of one change as `jhon status <change> --json` reports it. */
export function lddFromStatus(change: string, status: any): LddStatus | undefined {
	const tasks = status?.tasks;
	if (!tasks || !Array.isArray(tasks.slices)) return undefined;
	let current = false;
	const slices = tasks.slices.map((s: { total: number; completed: number }) => {
		if (s.total > 0 && s.completed >= s.total) return "done" as const;
		if (!current) { current = true; return "current" as const; }
		return "pending" as const;
	});
	const t = status.tests;
	const detail = tasks.slices.map((s: { name?: unknown; total: number; completed: number; delivers?: unknown; tasks?: unknown }) => ({
		name: typeof s.name === "string" ? s.name : "", total: s.total, completed: s.completed,
		...(typeof s.delivers === "string" && s.delivers ? { delivers: s.delivers } : {}),
		...(Array.isArray(s.tasks) ? { tasks: s.tasks.filter((x: any) => typeof x?.text === "string").map((x: any) => ({ text: x.text, done: x.done === true })) } : {}),
	}));
	const strict = status.strict_tdd;
	return {
		change,
		...(typeof tasks.slice === "string" ? { slice: tasks.slice } : {}),
		slices,
		detail,
		...(typeof status.route?.level === "string" ? { route: status.route.level } : {}),
		...(typeof status.next === "string" && status.next ? { next: status.next } : {}),
		...(typeof tasks.total === "number" ? { tasks: { completed: tasks.completed ?? 0, total: tasks.total } } : {}),
		...((strict && typeof strict === "object" ? strict.value : strict) === true ? { strictTdd: true } : {}),
		...(t && typeof t.timestamp === "string" ? { tests: { passed: t.passed === true, failed: Array.isArray(t.failed) ? t.failed.length : 0, at: t.timestamp, ...(typeof t.scope === "string" ? { scope: t.scope } : {}) } } : {}),
	};
}

/** A read's deadline: jhon status takes about a second in a large repository, more under load. */
const READ_TIMEOUT_MS = 15_000;

// jhon status exits 1 when it selects no change, and still prints its JSON:
// only output that is no JSON (a timeout, a crash) fails the read.
const run = (bin: string, args: string[], cwd: string): Promise<any> => new Promise((resolve, reject) => {
	execFile(bin, args, { cwd, timeout: READ_TIMEOUT_MS, maxBuffer: 4 << 20 }, (err, stdout) => {
		try { resolve(JSON.parse(stdout)); } catch { reject(new Error(`jhon ${args.join(" ")} in ${cwd} gave no status${err ? `: ${err.message}` : ""}`)); }
	});
});

/**
 * The owner's permission mode each folder's last `jhon status --json`
 * reported (S31), with or without a change: the status line's badge reads
 * it from STATUS's own read, never with a read of its own. On globalThis,
 * as the selection below: each extension may load this module on its own.
 */
const MODES = Symbol.for("jhon-shell.permission-modes");
const modes = (): Map<string, string> => ((globalThis as Record<symbol, Map<string, string>>)[MODES] ??= new Map());

/** The mode cwd's last status read reported; undefined before one, or when it reported none. */
export function permissionMode(cwd: string): string | undefined {
	return modes().get(cwd);
}

/**
 * The umbrellas each folder's last `jhon status --json` listed (T37), with or
 * without a change: STATUS lists them after the lanes. On globalThis, as the
 * modes above.
 */
const UMBRELLAS = Symbol.for("jhon-shell.ldd-umbrellas");
const umbrellaReads = (): Map<string, string[]> => ((globalThis as Record<symbol, Map<string, string[]>>)[UMBRELLAS] ??= new Map());

/** The umbrellas cwd's last status read listed; none before one. */
export function umbrellasAt(cwd: string): string[] {
	return umbrellaReads().get(cwd) ?? [];
}

/** An umbrella's next step as STATUS says it: split it into changes. */
export const UMBRELLA_STEP = "start-change";

/**
 * The session's LDD status, or undefined where there is no change to show;
 * it rejects when jhon status gives no answer, which is not "no change".
 * The mode the status reports is kept for cwd (permissionMode).
 */
export async function readLddStatus(cwd: string, bin = process.env.JHON_BIN || "jhon"): Promise<LddStatus | undefined> {
	const status = await run(bin, ["status", "--json"], cwd);
	if (typeof status?.mode === "string" && status.mode) modes().set(cwd, status.mode);
	else modes().delete(cwd);
	const umbrellas = Array.isArray(status?.umbrellas) ? status.umbrellas.filter((u: unknown): u is string => typeof u === "string") : [];
	if (umbrellas.length) umbrellaReads().set(cwd, umbrellas);
	else umbrellaReads().delete(cwd);
	if (typeof status?.change !== "string") return undefined;
	const ldd = lddFromStatus(status.change, status);
	const tasksFile = typeof status.directory === "string" ? findUp(cwd, join(status.directory, "tasks.md")) : undefined;
	if (ldd && tasksFile) return { ...ldd, tasksFile };
	const recordDir = ldd && status.folder === false ? folderlessRecordDir(cwd, status.change) : undefined;
	return ldd && recordDir ? { ...ldd, recordDir } : ldd;
}

/** A change with no folder keeps its record under git's common dir (docs/status-json.md); undefined outside git. */
export function folderlessRecordDir(cwd: string, change: string): string | undefined {
	const common = spawnSync("git", ["-C", cwd, "rev-parse", "--path-format=absolute", "--git-common-dir"], { encoding: "utf8", env: gitReadEnv() });
	if (common.status !== 0 || !common.stdout.trim()) return undefined;
	return join(common.stdout.trim(), "jhon", "changes", change);
}

/** When a change last moved: its tasks.md, or a folderless change's execution record; 0 when unknown. */
const movedAt = (s: LddStatus): number => {
	try { return s.tasksFile ? statSync(s.tasksFile).mtimeMs : s.recordDir ? statSync(join(s.recordDir, "execution-record.json")).mtimeMs : 0; } catch { return 0; }
};

/** Whether a change has work left: an unchecked task, else a slice not done. */
const pendingWork = (s: LddStatus): boolean => s.tasks ? s.tasks.completed < s.tasks.total : s.slices.some((x) => x !== "done");

/**
 * Changes ranked as jhon status ranks the candidates it cannot decide
 * between (T37): pending work first, then the latest movement, then name.
 */
export function rankChanges<T>(items: T[], of: (item: T) => LddStatus): T[] {
	const keyed = items.map((item) => ({ item, s: of(item), pending: pendingWork(of(item)), at: movedAt(of(item)) }));
	keyed.sort((a, b) => Number(b.pending) - Number(a.pending) || b.at - a.at || (a.s.change < b.s.change ? -1 : a.s.change > b.s.change ? 1 : 0));
	return keyed.map((k) => k.item);
}

/**
 * The change in progress among the repository's worktrees (`git worktree
 * list`), cwd's own and those in skip aside: four at most, read together; the
 * first as jhon status ranks them (rankChanges), with its worktree's name. For a session
 * whose folder selects no change and that has worked in no worktree yet.
 */
export async function lddInWorktrees(cwd: string, skip: string[], read: (cwd: string) => Promise<LddStatus | undefined>): Promise<LddStatus | undefined> {
	const real = (p: string) => { try { return realpathSync(p); } catch { return p; } };
	const here = real(cwd);
	const listed = spawnSync("git", ["-C", cwd, "worktree", "list", "--porcelain"], { encoding: "utf8", env: gitReadEnv() });
	if (listed.status !== 0) return undefined;
	const skipped = new Set(skip.map(real));
	const worktrees = listed.stdout.split("\n").filter((l) => l.startsWith("worktree ")).map((l) => l.slice(9))
		.filter((w) => real(w) !== here && !skipped.has(real(w))).slice(0, 4);
	const found = (await Promise.all(worktrees.map(async (w) => ({ w, s: await read(w).catch(() => undefined) })))).filter((x): x is { w: string; s: LddStatus } => x.s !== undefined);
	const ranked = rankChanges(found, (x) => x.s);
	return ranked[0] ? { ...ranked[0].s, worktree: basename(ranked[0].w), root: ranked[0].w } : undefined;
}

/**
 * The session's change: pi's folder when it selects one; else the first the
 * worktrees the session worked in select (roots, the latest first, three at
 * most), with its worktree's name; else, when the session has worked in no
 * worktree yet, the repository's own (lddInWorktrees). A session that worked
 * in a worktree selecting none shows none: another worktree's change is a
 * lane beside it, never the session's. A read that fails rejects: it says
 * nothing of that worktree, so no older one stands in for it.
 */
export async function lddOfSession(cwd: string, roots: string[], read: (cwd: string) => Promise<LddStatus | undefined>, exists: (root: string) => boolean): Promise<LddStatus | undefined> {
	const own = await read(cwd);
	if (own) return own;
	const real = (p: string) => { try { return realpathSync(p); } catch { return p; } };
	const here = real(cwd);
	const worked = roots.filter((r) => real(r) !== here && exists(r)).slice(0, 3);
	for (const root of worked) {
		const found = await read(root);
		if (found) return { ...found, worktree: basename(root), root };
	}
	return worked.length ? undefined : lddInWorktrees(cwd, roots, read);
}

/**
 * The session's selection, one for every surface (T29, L93): STATUS
 * (extensions/ui.ts) computes it at each refresh and writes it here, with
 * the refresh that computes it again; the change detail and its tests
 * (extensions/jhon.ts) read it, refreshed, rather than select on their own,
 * so the two never show different changes. On globalThis, as the shell's
 * other shared slots: each extension may load this module on its own.
 */
interface SelectionSlot {
	session?: string;
	status?: LddStatus;
	refresh?: () => Promise<LddStatus | undefined>;
	/** Who follows the selection as it moves: TODOS shows the change's slice (T49). */
	listeners?: Set<() => void>;
}
const SELECTION = Symbol.for("jhon-shell.ldd-selection");
const selection = (): SelectionSlot => ((globalThis as Record<symbol, SelectionSlot>)[SELECTION] ??= {});

/** STATUS shares session's selection and the refresh that computes it; the returned function withdraws them. */
export function shareSelection(session: string, refresh: () => Promise<LddStatus | undefined>): () => void {
	const slot = selection();
	slot.session = session;
	slot.status = undefined;
	slot.refresh = refresh;
	return () => {
		if (slot.refresh !== refresh) return;
		slot.session = undefined;
		slot.status = undefined;
		slot.refresh = undefined;
	};
}

/** What STATUS selected for session, as it shows it; a selection that moved is told to its listeners. */
export function selectChange(session: string, status: LddStatus | undefined): void {
	const slot = selection();
	if (slot.session !== session) return;
	const moved = JSON.stringify(slot.status) !== JSON.stringify(status);
	slot.status = status;
	if (moved) for (const listener of [...(slot.listeners ?? [])]) { try { listener(); } catch { /* A listener's failure never stops STATUS. */ } }
}

/** listener is called each time the selection moves; the returned function stops it. */
export function onSelection(listener: () => void): () => void {
	const slot = selection();
	(slot.listeners ??= new Set()).add(listener);
	return () => { slot.listeners?.delete(listener); };
}

/** The change STATUS shows for session, with its lanes; undefined when it shows none or shares nothing for it. */
export function selectedChange(session: string): LddStatus | undefined {
	const slot = selection();
	return slot.session === session ? slot.status : undefined;
}

/**
 * session's selection computed again, STATUS updated with it; undefined when
 * STATUS shares none for session (no UI), for the caller to select itself.
 */
export async function refreshSelection(session: string): Promise<{ status?: LddStatus } | undefined> {
	const slot = selection();
	if (slot.session !== session || !slot.refresh) return undefined;
	const status = await slot.refresh();
	return status ? { status } : {};
}

/**
 * What lddLanes read before, for a refresh to re-read only what moved: the
 * `git worktree list` it read, each worktree's status by its real path, and
 * the worktrees (real paths) whose watches fired since their last read;
 * when every lane was last read (the safety net), and each lane's latest
 * read, so an older one finishing last never stands in for it.
 */
export interface LaneReads {
	list?: string;
	read: Map<string, LddStatus | undefined>;
	moved: Set<string>;
	allAt?: number;
	seq: Map<string, number>;
}

/** A fresh LaneReads: every worktree is still to read. */
export const laneReads = (): LaneReads => ({ read: new Map(), moved: new Set(), seq: new Map() });

/** The safety net: every lane is read again this often, for what no watch saw (a watch that died, an event lost). */
export const LANE_REREAD_MS = 5 * 60_000;

/**
 * The changes the repository's other worktrees select (`git worktree list`,
 * twelve at most, read together), the shown change and each change once left
 * out: the lanes working in parallel beside the session's own, ranked as jhon
 * status ranks them (rankChanges), then the umbrellas each worktree's status
 * listed (umbrellas, umbrellasAt), each once, last and marked. With since, a
 * worktree is read again only when it moved, was never read, or the list of
 * worktrees changed, or LANE_REREAD_MS passed since every lane was read (now);
 * a read that fails is read again the next time.
 */
export async function lddLanes(cwd: string, shown: LddStatus | undefined, read: (cwd: string) => Promise<LddStatus | undefined>, since?: LaneReads, now = Date.now(), umbrellas: (cwd: string) => string[] = umbrellasAt): Promise<Lane[]> {
	const listed = spawnSync("git", ["-C", shown?.root ?? cwd, "worktree", "list", "--porcelain"], { encoding: "utf8", env: gitReadEnv() });
	if (listed.status !== 0) return [];
	const worktrees = listed.stdout.split("\n").filter((l) => l.startsWith("worktree ")).map((l) => l.slice(9)).slice(0, 12);
	if (since && since.list !== listed.stdout) { since.list = listed.stdout; since.read.clear(); }
	if (since && (since.allAt === undefined || now - since.allAt >= LANE_REREAD_MS)) { since.allAt = now; since.read.clear(); }
	const real = (p: string) => { try { return realpathSync(p); } catch { return p; } };
	const readLane = async (w: string): Promise<LddStatus | undefined> => {
		if (!since) return read(w).catch(() => undefined);
		const key = real(w);
		if (since.read.has(key) && !since.moved.has(key)) return since.read.get(key);
		since.moved.delete(key);
		const seq = (since.seq.get(key) ?? 0) + 1;
		since.seq.set(key, seq);
		// A newer read of this lane started meanwhile: its answer stands.
		const latest = () => since.seq.get(key) === seq;
		try {
			const s = await read(w);
			if (!latest()) return since.read.get(key);
			since.read.set(key, s);
			return s;
		} catch {
			if (!latest()) return since.read.get(key);
			since.read.delete(key);
			return undefined;
		}
	};
	const found = await Promise.all(worktrees.map(async (w) => ({ w, s: await readLane(w) })));
	const seen = new Set(shown ? [shown.change] : []);
	const lanes: Lane[] = [];
	for (const { w, s } of rankChanges(found.filter((x): x is { w: string; s: LddStatus } => x.s !== undefined), (x) => x.s)) {
		if (seen.has(s.change)) continue;
		seen.add(s.change);
		lanes.push({ change: s.change, worktree: basename(w), root: w, done: s.slices.filter((x) => x === "done").length, slices: s.slices.length, ...(s.next ? { next: s.next } : {}) });
	}
	for (const { w } of found) {
		for (const u of umbrellas(w)) {
			if (seen.has(u)) continue;
			seen.add(u);
			lanes.push({ change: u, worktree: basename(w), root: w, done: 0, slices: 0, next: UMBRELLA_STEP, umbrella: true });
		}
	}
	return lanes;
}

/**
 * What STATUS watches to follow its changes as they move: for pi's folder,
 * the shown change's worktree, and each lane's, its git HEAD (a branch
 * switched) and its reflog (logs/HEAD: a commit, with no switch), its openspec/changes (a change created or archived), and the
 * change's own directory (a task checked off); the shown change's tasks.md
 * directory too, or, for a change with no folder, its record's folder (a
 * run recorded), else its nearest parent that exists, for that one name to
 * appear; and others, worktrees selecting no change, for one to
 * appear. Each entry is a directory, when only one name in it counts that
 * name, and the roots it belongs to.
 */
export function lddWatchTargets(cwd: string, status: LddStatus | undefined, others: string[] = []): Array<[string, string | undefined, string[]]> {
	const targets = new Map<string, { dir: string; only: string | undefined; roots: Set<string> }>();
	// One entry per directory, but git's dir may count two names (HEAD, and jhon/ to appear).
	const add = (dir: string, root: string, only?: string) => {
		const key = only === "jhon" ? `${dir}\0jhon` : dir;
		const t = targets.get(key);
		if (!t) targets.set(key, { dir, only, roots: new Set([root]) });
		else { if (only === undefined) t.only = undefined; t.roots.add(root); }
	};
	const own = status?.root ?? cwd;
	const roots: Array<[string, string | undefined]> = [[cwd, undefined], ...(status ? [[own, status.change] as [string, string]] : []),
		...(status?.lanes ?? []).filter((l) => l.root).map((l) => [l.root!, l.change] as [string, string]),
		...others.map((w) => [w, undefined] as [string, undefined])];
	for (const [root, change] of roots) {
		const gitDir = spawnSync("git", ["-C", root, "rev-parse", "--absolute-git-dir"], { encoding: "utf8", env: gitReadEnv() });
		if (gitDir.status === 0 && gitDir.stdout.trim()) {
			add(gitDir.stdout.trim(), root, "HEAD");
			const logs = join(gitDir.stdout.trim(), "logs");
			if (existsSync(logs)) add(logs, root, "HEAD");
		}
		const changes = findUp(root, join("openspec", "changes")) ?? findUp(root, join("ldd", "changes"));
		if (!changes) continue;
		add(changes, root);
		if (change && existsSync(join(changes, change))) add(join(changes, change), root);
	}
	if (status?.tasksFile) add(dirname(status.tasksFile), own);
	else if (status?.recordDir) {
		let dir = status.recordDir;
		let only: string | undefined;
		// Up to git's common dir at most: <common>/jhon/changes/<change>.
		for (let up = 0; up < 3 && !existsSync(dir); up++) { only = basename(dir); dir = dirname(dir); }
		if (existsSync(dir)) add(dir, own, only);
	}
	return [...targets.values()].map((t) => [t.dir, t.only, [...t.roots]]);
}

/** How long ago iso was: «5 min ago», «2 h ago», «3 d ago». */
export function ago(iso: string, now = Date.now()): string {
	const m = Math.max(0, Math.round((now - Date.parse(iso)) / 60000));
	if (m < 60) return `${m} min ago`;
	if (m < 1440) return `${Math.round(m / 60)} h ago`;
	return `${Math.round(m / 1440)} d ago`;
}

export interface LddPaint {
	value: (text: string) => string;
	label: (text: string) => string;
	fg: (role: string, text: string) => string;
}

/** A slice's name without its «Slice N: » lead. */
const short = (name: string) => name.replace(/^Slice \d+:\s*/, "");

/**
 * The route in the owner's words: whether the change carries an independent
 * verification, never that it passed one; never the bare word, which reads
 * as a state (L93).
 */
export const ROUTE_WORDS: Record<string, string> = { standard: "standard route", verified: "verified route" };

/** The step and, on the verified route, what follows apply: «apply → verify». */
export function stepText(status: LddStatus): string | undefined {
	if (!status.next) return undefined;
	const step = status.next;
	return status.next === "apply" && status.route === "verified" ? `${step} → verify` : step;
}

const clip = (text: string, cells: number) => text.length <= cells ? text : `${text.slice(0, Math.max(0, cells - 1))}…`;

/** The lanes STATUS lists, one block each; the rest are counted. */
const LANES_SHOWN = 4;

/** A block's detail lines sit under its name, by two. */
const DETAIL = "  ";

/** The fewest cells a cut name keeps before the worktrees' column: «◆ », five letters and «…». */
const NAME_MIN = 8;

/** The worktree a block says on its name line: «⎇ <worktree>». */
const worktreeTag = (worktree: string | undefined) => worktree ? `⎇ ${worktree}` : "";

/**
 * A block's name line: «◆ <change>», and its worktree from column col on;
 * a name that does not fit before it is cut first (L110).
 */
function nameLine(change: string, tag: string, col: number, width: number, paint: (mark: string, name: string, gap: string, tag: string) => string): string {
	if (!tag) return paint("◆", clip(change, Math.max(1, width - 2)), "", "");
	const name = clip(change, Math.max(1, col - 3));
	return paint("◆", name, " ".repeat(Math.max(1, col - 2 - name.length)), clip(tag, Math.max(1, width - Math.max(col, name.length + 3))));
}

/** The route in the owner's words, or "" for a change without one. */
export function routeText(status: Pick<LddStatus, "route">): string {
	return status.route ? ROUTE_WORDS[status.route] ?? `${status.route} route` : "";
}

/**
 * The STATUS card's LDD lines, each cut to width (design 02, one fact, one
 * place; L110): one block per change in progress, the session's own first.
 * Each block's name line says the change and, in one column at the right,
 * the worktree it is in when that is not the session's folder; under it, by
 * two, the session's change says its slices done of all of them, its route
 * and the open slice, then the next step and the tests in one vocabulary
 * (✓ passed, ✗ N failed, ⧗ none); a lane, dim, its slices and its next
 * step. Four lanes at most, and how many more (L93). No action line: its keys
 * work only inside the change detail (alt+j). The slices one by one are the change
 * detail's.
 *
 * `routeShown: false` leaves the route to the card's title (the sections
 * style says it there when the session's change is the only one).
 */
export function estadoLines(status: LddStatus | undefined, paint: LddPaint, width = 46, options: { routeShown?: boolean } = {}): string[] {
	if (!status) return [paint.label("No active LDD change")];
	const lanes = status.lanes ?? [];
	const shown = lanes.slice(0, LANES_SHOWN);
	const tags = [worktreeTag(status.worktree), ...shown.map((l) => worktreeTag(l.worktree))];
	// Every worktree starts in one column, the longest ending at the right edge.
	const col = Math.max(NAME_MIN + 1, width - Math.max(0, ...tags.map((t) => t.length)));
	const detail = (text: string) => clip(`${DETAIL}${text}`, width);
	const lines = [nameLine(status.change, tags[0]!, col, width, (mark, name, gap, tag) => `${paint.fg("accent", mark)} ${paint.value(name)}${gap}${tag ? paint.label(tag) : ""}`)];
	const route = options.routeShown === false ? "" : routeText(status);
	const n = status.slices.length;
	const done = status.slices.filter((s) => s === "done").length;
	const at = status.slices.indexOf("current");
	const open = at >= 0 ? short(status.detail?.[at]?.name || status.slice || "") : "";
	const facts = [n ? `${done}/${n} slices` : "", route, at >= 0 ? `on ${at + 1}${open ? `: ${open}` : ""}` : ""].filter(Boolean).join(" · ");
	if (facts) lines.push(paint.label(detail(facts)));
	const step = stepText(status);
	const t = status.tests;
	const verdict = t ? (t.passed ? { state: { glyph: STATES.done.glyph, role: THEME_NAME[STATES.done.role] }, text: "passed" } : { state: { glyph: STATES.failed.glyph, role: THEME_NAME[STATES.failed.role] }, text: t.failed ? `${t.failed} failed` : "failed" }) : { state: { glyph: "⧗", role: THEME_NAME[STATES.pending.role] }, text: "none" };
	const run = `${verdict.state.glyph} ${verdict.text}`;
	const plainNext = `${step ? `next ${step} · ` : ""}tests ${run}`;
	lines.push(DETAIL.length + plainNext.length <= width
		? `${DETAIL}${step ? `${paint.label("next")} ${paint.value(step)}${paint.label(" · ")}` : ""}${paint.label("tests")} ${paint.fg(verdict.state.role, run)}`
		: paint.label(detail(plainNext)));
	shown.forEach((lane, i) => {
		// An umbrella is no change in progress: never ◆ (T37).
		lines.push(nameLine(lane.change, tags[i + 1]!, col, width, (mark, name, gap, tag) => paint.fg("dim", `${lane.umbrella ? "○" : mark} ${name}${gap}${tag}`)));
		if (lane.umbrella) { lines.push(paint.fg("dim", detail(`umbrella · ${lane.next ?? UMBRELLA_STEP}: split it`))); return; }
		const facts = [lane.slices ? `${lane.done}/${lane.slices} slices` : "", lane.next ? `next ${lane.next}` : ""].filter(Boolean).join(" · ");
		if (facts) lines.push(paint.fg("dim", detail(facts)));
	});
	if (lanes.length > LANES_SHOWN) lines.push(paint.fg("dim", `+${lanes.length - LANES_SHOWN} more`));
	return lines;
}

/** How many changes STATUS shows: the session's own and every lane, those past the four listed too; umbrellas are none. */
export const changeCount = (status: LddStatus | undefined): number => status ? 1 + (status.lanes ?? []).filter((l) => !l.umbrella).length : 0;
