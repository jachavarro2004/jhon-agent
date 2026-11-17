import { truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";

// jhon-shell changes: what a session's agents changed in each of its roots,
// committed or not (T132). A root is compared against its base, the commit
// its HEAD was at when the session first registered it, new files included;
// a root without a usable base reads the working tree against HEAD.
// Git is the source of truth; this module turns raw `git diff --numstat -z`,
// `git diff --name-status -z` and `git status --porcelain -z` output into a
// model and renders the widget. Every list is read with -z, so a name Git
// would quote (not plain ASCII, a tab, a newline) is one name everywhere.

export const CHANGE_STATUS = {
	MODIFIED: "modified",
	ADDED: "added",
	DELETED: "deleted",
	RENAMED: "renamed",
	UNTRACKED: "untracked",
} as const;

export type ChangeStatus = (typeof CHANGE_STATUS)[keyof typeof CHANGE_STATUS];

export interface ChangedFile {
	path: string;
	added: number;
	deleted: number;
	status: ChangeStatus;
	countsUnavailable?: string;
	diffRevision?: string;
	/** The commit the counts are against, and so the file's diff; absent: HEAD. Never set for an untracked file. */
	diffBase?: string;
}

/** Why a root reads against HEAD (uncommitted changes only) instead of the session's base. */
export const CHANGES_FALLBACK = {
	/** The session's entry holds no base: saved before bases were kept, a root with no commit then, or a root known only from a write. */
	NO_BASE: "no-base",
	/** The base commit is no longer in the repository. */
	MISSING: "base-missing",
	/** The base is no longer an ancestor of the root's HEAD (a rebase, a reset, another branch). */
	NOT_ANCESTOR: "base-not-ancestor",
	/** Git could not compare HEAD with the base (no HEAD commit, or the call failed). */
	UNREADABLE: "base-unreadable",
} as const;
export type ChangesFallback = (typeof CHANGES_FALLBACK)[keyof typeof CHANGES_FALLBACK];

/** What a root's files and counts are measured against. */
export interface ChangesComparison {
	against: "base" | "head";
	/** The session's base for the root, when its entry holds one. */
	base?: string;
	fallback?: ChangesFallback;
}

const shortCommit = (commit: string) => commit.slice(0, 7);

/** The view's sentence for a root: what it is compared with, and why when that is not the session's base. */
export function comparisonNote(comparison: ChangesComparison | undefined): string {
	if (!comparison) return "";
	if (comparison.against === "base" && comparison.base) return `Compared with the session's base ${shortCommit(comparison.base)}: committed and uncommitted changes.`;
	return `Compared with HEAD (uncommitted changes only): ${fallbackReason(comparison)}.`;
}

/** Why a root is read against HEAD: the clause every place that says it shares (the view, the rail, the notice). */
export function fallbackReason(comparison: ChangesComparison): string {
	const base = comparison.base ? shortCommit(comparison.base) : "";
	return comparison.fallback === CHANGES_FALLBACK.MISSING ? `the session's base ${base} no longer exists`
		: comparison.fallback === CHANGES_FALLBACK.NOT_ANCESTOR ? `the session's base ${base} is no longer an ancestor of this root's HEAD`
		: comparison.fallback === CHANGES_FALLBACK.UNREADABLE ? `Git could not compare this root's HEAD with the session's base ${base}`
		: "the session kept no base for this root (it was registered before bases were kept or before its first commit, or is known only from an edit)";
}

/** A scanned root that is read against HEAD, listed or not. */
export interface HeadRoot {
	root: string;
	comparison: ChangesComparison;
}

export const NO_CHANGES_SINCE_START = "No changes since the session's start in this session's repository or its sub-agents' worktrees.";

/**
 * /jhon:changes with nothing to list. A root read against HEAD has only its
 * uncommitted changes counted, so the text never says "since the session's
 * start" for it: it names the root, what it was compared with and why;
 * others is how many more roots were read, each against its base.
 */
export function noChangesText(headRoots: readonly HeadRoot[], others: number): string {
	if (headRoots.length === 0) return NO_CHANGES_SINCE_START;
	const lines = headRoots.map(({ root, comparison }) => `No uncommitted changes in ${root}, compared with HEAD: ${fallbackReason(comparison)}.`);
	if (others === 1) lines.push("The session's other root has no changes against its base.");
	else if (others > 1) lines.push(`The session's other ${others} roots have no changes against their base.`);
	return lines.join("\n");
}

/** The root row's short mark for a root that reads against HEAD; nothing for one compared with its base. */
export function comparisonMark(comparison: ChangesComparison | undefined): string {
	return comparison?.against === "head" ? "vs HEAD" : "";
}

export interface ChangesModel {
	files: ChangedFile[];
	added: number;
	deleted: number;
}

export interface ChangesSnapshot {
	numstat: string;
	porcelain: string;
}

export interface ChangesTheme {
	fg(color: string, text: string): string;
}

interface NumstatEntry {
	path: string;
	added: number;
	deleted: number;
}

export const CHANGES_COMMAND = "/jhon:changes";
// One selected file's diff is the only bounded thing in Changes: past either
// limit the panel shows DIFF_TOO_LARGE instead of buffering and coloring it.
export const DIFF_MAX_BYTES = 1024 * 1024;
export const DIFF_MAX_LINES = 20_000;
export const DIFF_TOO_LARGE = "Diff too large — press o to open in editor.";
const WIDGET_GLYPH = "✎";
const STATUS_BY_CODE: Record<string, ChangeStatus> = {
	A: CHANGE_STATUS.ADDED,
	D: CHANGE_STATUS.DELETED,
	R: CHANGE_STATUS.RENAMED,
	C: CHANGE_STATUS.ADDED,
	"?": CHANGE_STATUS.UNTRACKED,
};

export function emptyChanges(): ChangesModel {
	return { files: [], added: 0, deleted: 0 };
}

function count(value: string): number {
	const parsed = Number.parseInt(value, 10);
	return Number.isFinite(parsed) ? parsed : 0;
}

/**
 * `git diff --numstat -z <commit>`: `<added>\t<deleted>\t<path>` records ended
 * by NUL; a rename or copy has an empty path there and its old and new path
 * as the next two records. The path is the new one, as status and
 * name-status give it.
 */
export function parseNumstat(text: string): NumstatEntry[] {
	const entries: NumstatEntry[] = [];
	const records = text.split("\0");
	for (let index = 0; index < records.length; index += 1) {
		const [added, deleted, ...rest] = records[index].split("\t");
		if (added === undefined || deleted === undefined || rest.length === 0) continue;
		let path = rest.join("\t");
		if (path === "") {
			path = records[index + 2] ?? "";
			index += 2;
		}
		if (path) entries.push({ path, added: count(added), deleted: count(deleted) });
	}
	return entries;
}

export function parsePorcelain(text: string): Map<string, ChangeStatus> {
	const statuses = new Map<string, ChangeStatus>();
	const records = text.split("\0").filter((record) => record.length > 0);
	for (let index = 0; index < records.length; index += 1) {
		const record = records[index];
		const code = record.slice(0, 2);
		const path = record.slice(3);
		// A trailing "/" is Git's own marker for an embedded repository (a
		// nested .git directory, e.g. an inner repo inside an outer one) or an
		// excluded directory -- never an individual file. Even with
		// --untracked-files=all, Git reports these as one directory-shaped line
		// instead of expanding into files, so keeping it would show a phantom
		// changed "file" on whatever ancestor repository contains the nested one.
		if (!path.endsWith("/")) statuses.set(path, STATUS_BY_CODE[code[0]] ?? STATUS_BY_CODE[code[1]] ?? CHANGE_STATUS.MODIFIED);
		if (code[0] === "R" || code[0] === "C") index += 1;
	}
	return statuses;
}

/** `git diff --name-status -z <commit>`: each path's status against the commit; a rename or copy is its new path. */
export function parseNameStatus(text: string): Map<string, ChangeStatus> {
	const statuses = new Map<string, ChangeStatus>();
	const fields = text.split("\0");
	for (let index = 0; index < fields.length;) {
		const code = fields[index++];
		if (!code) continue;
		const first = fields[index++];
		const path = code[0] === "R" || code[0] === "C" ? fields[index++] : first;
		if (!path) break;
		statuses.set(path, STATUS_BY_CODE[code[0]] ?? CHANGE_STATUS.MODIFIED);
	}
	return statuses;
}

export function snapshotChanges(snapshot: ChangesSnapshot): ChangedFile[] {
	const counts = new Map(parseNumstat(snapshot.numstat).map((entry) => [entry.path, entry]));
	const files: ChangedFile[] = [];
	for (const [path, status] of parsePorcelain(snapshot.porcelain)) {
		const entry = counts.get(path);
		files.push({ path, added: entry?.added ?? 0, deleted: entry?.deleted ?? 0, status });
	}
	return files;
}

export function changesModel(changed: ChangedFile[]): ChangesModel {
	const files = [...changed].sort((a, b) => a.path.localeCompare(b.path));
	return {
		files,
		added: files.reduce((total, file) => total + file.added, 0),
		deleted: files.reduce((total, file) => total + file.deleted, 0),
	};
}

export function changesSummary(model: ChangesModel): string {
	const noun = model.files.length === 1 ? "file" : "files";
	return `${model.files.length} ${noun} · +${model.added} −${model.deleted}${model.files.some(file => file.countsUnavailable) ? " · partial counts" : ""}`;
}

/** The last finished turn: its number in the session and how long it ran. */
export interface TurnSummary {
	n: number;
	ms: number;
}

/** «2m 14s», «45s», «1h 5m». */
export function turnDuration(ms: number): string {
	const seconds = Math.max(0, Math.round(ms / 1000));
	if (seconds < 60) return `${seconds}s`;
	const minutes = Math.floor(seconds / 60);
	return minutes < 60 ? `${minutes}m ${seconds % 60}s` : `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

// One line: summary, the files joined by dots, and on the right the last
// turn (its number and time) and the command. The file list goes first when
// the terminal is narrow, then the turn.
export function renderChangesWidget(model: ChangesModel, theme: ChangesTheme, width: number, turn?: TurnSummary): string[] {
	if (model.files.length === 0) return [];
	const noun = model.files.length === 1 ? "file" : "files";
	const dot = theme.fg("muted", "·");
	const head = `${theme.fg("accent", WIDGET_GLYPH)} ${theme.fg("text", `${model.files.length} ${noun}`)} ${dot} ${theme.fg("success", `+${model.added}`)} ${theme.fg("error", `−${model.deleted}`)}${model.files.some(file => file.countsUnavailable) ? " · partial counts" : ""}`;
	const command = theme.fg("dim", CHANGES_COMMAND);
	const withTurn = turn ? `${theme.fg("muted", `turn ${turn.n} · ${turnDuration(turn.ms)}`)}   ${command}` : command;
	const list = model.files.map((file) => theme.fg("muted", file.path)).join(` ${dot} `);
	const left = `${head} ${dot} ${list}`;
	for (const [l, hint] of [[left, withTurn], [head, withTurn], [head, command]] as const) {
		const gap = width - visibleWidth(l) - visibleWidth(hint);
		if (gap >= 2) return [`${l}${" ".repeat(gap)}${hint}`];
	}
	return [truncateToWidth(head, width, "…")];
}

export interface GitResult {
	stdout: string;
	code: number;
	/** The runner stopped reading at its output bound; stdout is partial. */
	truncated?: boolean;
}

export type GitRunner = (args: string[]) => Promise<GitResult>;
export type LineCounter = (path: string) => Promise<number>;
/** A cheap content identity (e.g. size and mtime) so same-count edits refresh their diff. */
export type FileRevision = (path: string) => Promise<string | undefined>;

export interface WorktreeChanges {
	root: string;
	branch?: string;
	model: ChangesModel;
	/** What the root's files are measured against; the view says it where the root is shown. */
	comparison?: ChangesComparison;
}

// -z avoids Git's quoting of paths containing whitespace or newlines.
export function parseWorktrees(text: string): Array<{ root: string; branch?: string }> {
	return text.split("\0\0").flatMap((record) => {
		const fields = record.split("\0");
		const root = fields.find((field) => field.startsWith("worktree "))?.slice(9);
		if (!root || fields.some((field) => field === "bare" || field.startsWith("prunable"))) return [];
		const branch = fields.find((field) => field.startsWith("branch "))?.slice(7).replace(/^refs\/heads\//, "");
		return [branch ? { root, branch } : { root }];
	});
}

// A root the view shows as-is when its own HEAD could not be determined at
// all -- distinct from "detached", which means git answered and there really
// is no branch. Exported so callers and tests share one literal.
export const UNKNOWN_BRANCH = "unknown";

// Resolve a foreign clone's own HEAD directly instead of assuming "detached":
// a real branch name, "no commits yet" for an unborn branch (a branch ref
// that exists but has no commit), or undefined for a genuine detached HEAD
// (the caller falls back to "detached" in that case). A GitRunner that
// cannot even ask -- it rejects, e.g. because the root was removed or git
// itself is unavailable -- rejects here too, on purpose: that is a real
// failure to distinguish from git successfully reporting "no branch", and
// callers must not fold the two into the same undefined result.
export async function foreignRootBranch(git: GitRunner): Promise<string | undefined> {
	const symbolic = await git(["symbolic-ref", "--quiet", "--short", "HEAD"]);
	const branch = symbolic.code === 0 ? symbolic.stdout.trim() : "";
	if (!branch) return undefined;
	const verified = await git(["rev-parse", "--verify", "-q", "HEAD"]);
	return verified.code === 0 ? branch : "no commits yet";
}

// /jhon:changes: one group per registered root holding its working tree
// against the root's base in the session (registeredBases), or against HEAD
// for a root without a usable one. A root whose Git fails (timed out,
// rejected) has no group but is listed in failedRoots: a failed scan is never
// reported as a clean root.
export class WorktreeChangesTracker {
	worktrees: WorktreeChanges[] = [];
	failedRoots: string[] = [];
	/** The roots the last scan read against HEAD, with or without changes to list: their commits are not counted. */
	headRoots: HeadRoot[] = [];
	/**
	 * What /jhon:changes shows: the roots with changes and, beside them, each
	 * root read against HEAD that has none to list, as a row with no files (its
	 * commits are not counted, and the view says so there). Empty when no root
	 * has changes: there is no view then, the notice says it.
	 */
	shownRoots: WorktreeChanges[] = [];
	/** The sub-agent roots the last scan did not list, each with the root that lists its work. */
	foldedRoots: Array<{ root: string; into: string }> = [];
	scannedRootCount = 0;
	private inFlight: Promise<ChangesModel> | undefined;
	private readonly discover: GitRunner;
	private readonly gitForRoot: (root: string) => GitRunner;
	private readonly linesForRoot: (root: string) => LineCounter;
	private readonly registeredRoots: () => readonly string[];
	private readonly revisionForRoot: (root: string) => FileRevision | undefined;
	private readonly registeredBases: () => ReadonlyMap<string, string>;
	private readonly subagentRoots: () => ReadonlySet<string>;

	/** registeredBases: each root's base, in the order the session registered the roots. subagentRoots: the roots a sub-agent runs in. */
	constructor(discover: GitRunner, gitForRoot: (root: string) => GitRunner, linesForRoot: (root: string) => LineCounter = () => noLines, registeredRoots: () => readonly string[] = () => [], revisionForRoot: (root: string) => FileRevision | undefined = () => undefined, registeredBases: () => ReadonlyMap<string, string> = () => NO_BASES, subagentRoots: () => ReadonlySet<string> = () => NO_ROOTS) {
		this.subagentRoots = subagentRoots;
		this.discover = discover;
		this.gitForRoot = gitForRoot;
		this.linesForRoot = linesForRoot;
		this.registeredRoots = registeredRoots;
		this.revisionForRoot = revisionForRoot;
		this.registeredBases = registeredBases;
	}

	private modelOf: WorktreeChanges[] | undefined;
	private combined: ChangesModel = emptyChanges();

	// Footers read this every frame; rebuild only when a scan replaced the trees.
	get model(): ChangesModel {
		if (this.modelOf !== this.worktrees) {
			this.modelOf = this.worktrees;
			this.combined = changesModel(this.worktrees.flatMap((tree) => tree.model.files.map((file) => ({
				...file,
				path: this.worktrees.length === 1 ? file.path : `${tree.root}/${file.path}`,
			}))));
		}
		return this.combined;
	}

	async start(): Promise<void> {
		await this.refresh();
	}

	async refresh(): Promise<ChangesModel> {
		// A scan can take longer than the polling interval in a large clone.
		// Share it rather than extending it indefinitely with queued polls.
		if (this.inFlight) return this.inFlight;
		this.inFlight = this.capture();
		try {
			return await this.inFlight;
		} finally {
			this.inFlight = undefined;
		}
	}

	private async capture(): Promise<ChangesModel> {
		// Discovery labels registered roots; it never grants visibility to siblings.
		const roots = new Set(this.registeredRoots());
		// No root to scan means no Git at all, not even branch discovery.
		if (roots.size === 0) {
			this.worktrees = [];
			this.failedRoots = [];
			this.headRoots = [];
			this.shownRoots = [];
			this.foldedRoots = [];
			this.scannedRootCount = 0;
			return this.model;
		}
		// Read once per scan, not per root.
		let bases: ReadonlyMap<string, string> = NO_BASES;
		try { bases = this.registeredBases(); } catch { /* No bases: every root reads against HEAD. */ }
		let subagents: ReadonlySet<string> = NO_ROOTS;
		try { subagents = this.subagentRoots(); } catch { /* None known: every root with changes is listed. */ }
		const result = await this.discover(["worktree", "list", "--porcelain", "-z"]).catch(() => ({ code: 128, stdout: "" }));
		const metadata = new Map((result.code === 0 ? parseWorktrees(result.stdout) : []).map((tree) => [tree.root, tree]));
		// Roots scan in parallel so one slow root does not delay the others.
		const failed: string[] = [];
		const heads: HeadRoot[] = [];
		// Roots read against HEAD with nothing to list: a row with no files in the view.
		const unlisted: WorktreeChanges[] = [];
		// Listed roots whose working tree is clean: all they show is committed.
		const committedOnly = new Set<string>();
		const scanned = await Promise.all([...roots].map(async (root): Promise<WorktreeChanges | undefined> => {
			const known = metadata.get(root);
			try {
				const tracker = new ChangesTracker(this.gitForRoot(root), this.linesForRoot(root), this.revisionForRoot(root), bases.get(root));
				await tracker.start();
				if (!tracker.statusRead) {
					failed.push(root);
					return undefined;
				}
				if (tracker.comparison.against === "head") heads.push({ root, comparison: tracker.comparison });
				if (!tracker.model.files.length) {
					// No further Git call for it: a root the discovery did not describe reads UNKNOWN_BRANCH.
					if (tracker.comparison.against === "head") unlisted.push({ root, branch: known ? known.branch : UNKNOWN_BRANCH, model: tracker.model, comparison: tracker.comparison });
					return undefined;
				}
				if (tracker.clean) committedOnly.add(root);
				// A root missing from this discovery scan belongs to a different Git
				// clone entirely (e.g. an inner repository nested inside an outer
				// one) -- worktree list can never describe a foreign clone, so ask
				// it directly instead of defaulting to "detached". A root the scan
				// DID describe is trusted as-is: its own "detached" marker (or lack
				// of a branch) is already accurate for that clone. The direct ask
				// can fail on its own (root gone, git unavailable) without the
				// change scan above failing -- that must label the root
				// UNKNOWN_BRANCH, not drop real, already-captured changes.
				const branch = known ? known.branch : await foreignRootBranch(this.gitForRoot(root)).catch(() => UNKNOWN_BRANCH);
				return { root, ...(branch ? { branch } : {}), model: tracker.model, comparison: tracker.comparison };
			} catch {
				// A root the caller stops registering mid-scan (e.g. a removed
				// worktree) triggers the rescan below without it, not a failure.
				failed.push(root);
				return undefined;
			}
		}));
		const found = scanned.filter((tree): tree is WorktreeChanges => tree !== undefined);
		// A sub-agent's work merged into another root is counted there, once
		// (T132): its own root is not listed again when all it shows is
		// committed and another listed root both lists every path it lists and
		// has a branch that holds its HEAD. Holding the HEAD alone is not showing
		// the work (a reverted merge, a base that already held it, an edit back
		// to the base's content): then both roots stay listed. The other root
		// must be compared with its base and registered before the child. One
		// Git call for a root that passes all of that, and only then.
		const order = [...bases.keys()];
		const merged = await Promise.all(found.map(async (tree): Promise<string | undefined> => {
			if (!subagents.has(tree.root) || !committedOnly.has(tree.root) || tree.comparison?.against !== "base") return undefined;
			const at = order.indexOf(tree.root);
			const holders = new Map<string, string>();
			for (const other of found) {
				const before = order.indexOf(other.root);
				const branch = metadata.get(other.root)?.branch;
				if (other === tree || other.comparison?.against !== "base" || before < 0 || before >= at || !branch) continue;
				const listed = new Set(other.model.files.map((file) => file.path));
				if (tree.model.files.every((file) => listed.has(file.path))) holders.set(`refs/heads/${branch}`, other.root);
			}
			if (holders.size === 0) return undefined;
			const held = await this.gitForRoot(tree.root)(["for-each-ref", "--contains", HEAD, "--format=%(refname)", ...holders.keys()]).catch(() => undefined);
			if (held?.code !== 0) return undefined;
			return held.stdout.split("\n").map((ref) => holders.get(ref.trim())).find((root) => root !== undefined);
		}));
		const trees = found.filter((_tree, index) => merged[index] === undefined);
		const folded = found.flatMap((tree, index) => merged[index] === undefined ? [] : [{ root: tree.root, into: merged[index] }]);
		const latest = new Set(this.registeredRoots());
		// A root admitted during status must not wait for a later poll (which may
		// be disabled). Ordinary overlapping polls still share exactly one scan.
		if (latest.size !== roots.size || [...latest].some((root) => !roots.has(root))) return this.capture();
		this.worktrees = trees;
		this.failedRoots = failed;
		this.headRoots = heads;
		this.shownRoots = trees.length ? [...trees, ...unlisted] : [];
		this.foldedRoots = folded;
		this.scannedRootCount = roots.size;
		return this.model;
	}
}

const noLines: LineCounter = async () => 0;
const NO_BASES: ReadonlyMap<string, string> = new Map();
const NO_ROOTS: ReadonlySet<string> = new Set();

// Per-file line counts and revision lookups are file IO: a large change set
// runs this many files at a time instead of one by one or all at once.
export const FILE_WORK_CONCURRENCY = 8;

async function eachLimited<T>(items: readonly T[], limit: number, work: (item: T) => Promise<void>): Promise<void> {
	let next = 0;
	const worker = async () => {
		while (next < items.length) await work(items[next++]);
	};
	await Promise.all(Array.from({ length: Math.min(limit, items.length) }, worker));
}

const HEAD = "HEAD";
const numstatArgs = (commit: string) => ["diff", "--numstat", "-z", commit];
const nameStatusArgs = (commit: string) => ["diff", "--name-status", "-z", commit];
const PORCELAIN_ARGS = ["status", "--porcelain=v1", "--untracked-files=all", "-z"];
// One line, `<only in base>\t<only in HEAD>`: both zero, HEAD is at the base;
// the first zero, the base is an ancestor of HEAD.
const aheadArgs = (base: string) => ["rev-list", "--left-right", "--count", `${base}...${HEAD}`];
const AHEAD_COUNTS = /^(\d+)\t(\d+)\s*$/;
const HEAD_ONLY: ChangesComparison = { against: "head", fallback: CHANGES_FALLBACK.NO_BASE };

// Tracks one root's changes against its base in the session, or against HEAD
// without one (T132). Concurrent refreshes coalesce: one git round-trip runs
// at a time and a refresh requested meanwhile triggers exactly one more.
// Git calls per scan, never one per file: two without a base (as before
// bases), one more to place HEAD against the base, and one more for the
// names once HEAD has moved past it (three at the base, four past it).
export class ChangesTracker {
	private readonly git: GitRunner;
	private readonly countLines: LineCounter;
	private readonly revision: FileRevision | undefined;
	private readonly base: string | undefined;
	private compared: ChangesComparison = HEAD_ONLY;
	private unchanged = false;
	private current: ChangesModel = emptyChanges();
	private available = false;
	private inFlight: Promise<ChangesModel> | undefined;
	private queued = false;

	constructor(git: GitRunner, countLines: LineCounter = noLines, revision?: FileRevision, base?: string) {
		this.git = git;
		this.countLines = countLines;
		this.revision = revision;
		this.base = base;
	}

	get model(): ChangesModel {
		return this.current;
	}

	/** What the last read model is measured against. */
	get comparison(): ChangesComparison {
		return this.compared;
	}

	/** True when the last read `git status` reported nothing: no uncommitted change, no untracked file. */
	get clean(): boolean {
		return this.unchanged;
	}

	/** False when the last start could not read `git status`. */
	get statusRead(): boolean {
		return this.available;
	}

	async start(): Promise<void> {
		const files = await this.capture();
		this.available = files !== undefined;
		this.current = files ? changesModel(files) : emptyChanges();
	}

	async refresh(): Promise<ChangesModel> {
		if (!this.available) return this.current;
		if (this.inFlight) {
			this.queued = true;
			return this.inFlight;
		}
		this.inFlight = this.runRefresh();
		try {
			return await this.inFlight;
		} finally {
			this.inFlight = undefined;
		}
	}

	private async runRefresh(): Promise<ChangesModel> {
		do {
			this.queued = false;
			const files = await this.capture();
			if (files) this.current = changesModel(files);
		} while (this.queued);
		return this.current;
	}

	// Where HEAD stands against the session's base decides the comparison.
	private async compare(): Promise<{ comparison: ChangesComparison; ahead: boolean }> {
		const base = this.base;
		if (!base) return { comparison: HEAD_ONLY, ahead: false };
		const counted = await this.git(aheadArgs(base));
		const counts = counted.code === 0 ? AHEAD_COUNTS.exec(counted.stdout) : null;
		if (counts) {
			if (counts[1] !== "0") return { comparison: { against: "head", base, fallback: CHANGES_FALLBACK.NOT_ANCESTOR }, ahead: false };
			return { comparison: { against: "base", base }, ahead: counts[2] !== "0" };
		}
		// Only on that failure: is the base gone, or could HEAD not be compared with it?
		const exists = await this.git(["rev-parse", "--verify", "--quiet", `${base}^{commit}`]);
		return { comparison: { against: "head", base, fallback: exists.code === 0 ? CHANGES_FALLBACK.UNREADABLE : CHANGES_FALLBACK.MISSING }, ahead: false };
	}

	private async capture(): Promise<ChangedFile[] | undefined> {
		const { comparison, ahead } = await this.compare();
		const commit = comparison.against === "base" && comparison.base ? comparison.base : HEAD;
		const [numstat, porcelain, names] = await Promise.all([this.git(numstatArgs(commit)), this.git(PORCELAIN_ARGS), ahead ? this.git(nameStatusArgs(commit)) : undefined]);
		if (porcelain.code !== 0 || (names && names.code !== 0)) return undefined;
		let files: ChangedFile[];
		if (names) {
			// HEAD moved past the base: every tracked file that differs from the
			// base, committed or not, plus the untracked files git status reports.
			const statuses = parseNameStatus(names.stdout);
			for (const [path, status] of parsePorcelain(porcelain.stdout)) if (status === CHANGE_STATUS.UNTRACKED) statuses.set(path, status);
			const counts = new Map(parseNumstat(numstat.stdout).map((entry) => [entry.path, entry]));
			files = [...statuses].map(([path, status]) => ({ path, added: counts.get(path)?.added ?? 0, deleted: counts.get(path)?.deleted ?? 0, status }));
		} else {
			// HEAD is the commit compared with: what git status reports, as before bases.
			files = snapshotChanges({ numstat: numstat.stdout, porcelain: porcelain.stdout });
		}
		if (commit !== HEAD) for (const file of files) if (file.status !== CHANGE_STATUS.UNTRACKED) file.diffBase = commit;
		this.compared = comparison;
		this.unchanged = porcelain.stdout === "";
		// Untracked files never appear in numstat; count their lines directly.
		await eachLimited(files, FILE_WORK_CONCURRENCY, async (file) => {
			const [added, revision] = await Promise.all([
				file.status === CHANGE_STATUS.UNTRACKED ? this.countLines(file.path).catch(() => 0) : undefined,
				this.revision?.(file.path).catch(() => undefined),
			]);
			if (added !== undefined) file.added = added;
			if (revision) file.diffRevision = revision;
		});
		return files;
	}
}
