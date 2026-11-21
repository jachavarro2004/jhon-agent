/**
 * The worktrees a session works in. Each is a root of the session's own Git
 * clone, recorded once in the session file as a custom entry; the LDD section
 * reads the change in every one of them. The session's folder is the first;
 * the `session_worktree_register` tool adds one by hand, and the `subagent`
 * tool adds the worktree a child runs in (T132).
 *
 * Git, never the spelling of a path, says which clone and root a path is in.
 * Its answer is memoized for startup, when several extensions ask the same
 * question, and every memoized answer is checked against the filesystem
 * before it is used again.
 */
import { execFileSync } from "node:child_process";
import { lstatSync, realpathSync, statSync, type Stats } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";

export const SESSION_WORKTREE_ENTRY = "jhon-shell.session-worktree/v1";
export const SESSION_WORKTREE_CHANGED = "jhon-shell:session-worktree-changed";
/** The evidence the `subagent` tool records for the worktree a child runs in. */
export const SUBAGENT_WORKTREE_EVIDENCE = "subagent:spawn";

export interface WorktreeIdentity { root: string; commonDir: string }
export type WorktreeResolver = (path: string, cwd: string) => WorktreeIdentity | undefined;
/** The commit a root's HEAD is at, or undefined when it has none yet or Git cannot say. */
export type WorktreeBaseResolver = (root: string) => string | undefined;

export const isCommitId = (value: unknown): value is string => typeof value === "string" && /^[0-9a-f]{40,64}$/.test(value);

interface SessionReader {
	getSessionId(): string;
	getEntries(): readonly { type: string; customType?: string; data?: unknown }[];
}
interface RegistryHost {
	appendEntry(type: string, data: unknown): void;
	events: { emit(name: string, data: unknown): void };
}
/**
 * One recorded root. `base` (T132) is the commit its HEAD was at when the
 * session first recorded it; an entry from before bases were kept, or a root
 * with no commit yet, has none.
 */
interface Registration { sessionId: string; root: string; evidence: string; base?: string }

const MESSAGES = {
	inactive: "This session is no longer the active one: it takes no new worktree.",
	foreign: "Name a worktree that exists in this session's own Git clone; a folder of another repository cannot be one of its worktrees.",
	moved: "The session's repository is not the one it started in; the worktree is not registered.",
} as const;

/**
 * The environment for a Git child, given whole and not merged: every GIT_*
 * variable (any case) is left out, so no ambient GIT_DIR, work tree or config
 * override sends a path to another repository.
 */
export function worktreeGitEnvironment(env: NodeJS.ProcessEnv = process.env): NodeJS.ProcessEnv {
	const clean: NodeJS.ProcessEnv = {};
	for (const [name, value] of Object.entries(env)) if (!name.toUpperCase().startsWith("GIT_")) clean[name] = value;
	return clean;
}

/** A path as pi spells it (a leading @, odd spaces, ~), made real; a file stands for its folder. */
function folderOf(path: string, cwd: string): string {
	let spelled = path.startsWith("@") ? path.slice(1) : path;
	spelled = spelled.replace(/[\u00a0\u2000-\u200a\u202f\u205f\u3000]/g, " ");
	if (spelled === "~" || spelled.startsWith("~/")) spelled = homedir() + spelled.slice(1);
	const real = realpathSync(resolve(cwd, spelled));
	return statSync(real).isDirectory() ? real : dirname(real);
}

/** One `git -C <folder>` question, hidden, bounded, with a clean environment; its answer without the final newline. */
function askGit(run: typeof execFileSync, folder: string, args: string[], trim: "newline" | "all"): string {
	const out = String(run("git", ["--no-optional-locks", "-C", folder, ...args], {
		encoding: "utf8", timeout: 5000, stdio: ["ignore", "pipe", "pipe"], shell: false, windowsHide: true, env: worktreeGitEnvironment(),
	}));
	return trim === "all" ? out.trim() : out.replace(/\r?\n$/, "");
}

export function resolveSessionWorktreeWithGit(path: string, cwd: string, run: typeof execFileSync = execFileSync): WorktreeIdentity | undefined {
	try {
		const folder = folderOf(path, cwd);
		const absolute = (flag: string) => realpathSync(askGit(run, folder, ["rev-parse", "--path-format=absolute", flag], "newline"));
		const root = absolute("--show-toplevel");
		return { root, commonDir: absolute("--git-common-dir") };
	} catch {
		return undefined;
	}
}

export function resolveWorktreeHeadWithGit(root: string, run: typeof execFileSync = execFileSync): string | undefined {
	try {
		const head = askGit(run, root, ["rev-parse", "--verify", "--quiet", "HEAD^{commit}"], "all");
		return isCommitId(head) ? head : undefined;
	} catch {
		return undefined;
	}
}

const samePath = (a: string, b: string) => (process.platform === "win32" ? a.toLowerCase() === b.toLowerCase() : a === b);
const identityOf = (entry: Stats) => `${entry.dev}:${entry.ino}`;

/**
 * What Git's answer rested on, as the filesystem shows it: no `.git` between
 * the folder and its root, then the root, its `.git` and the common dir by
 * device and inode (a linked worktree's `.git` file by size and time too,
 * since it can be rewritten in place to point at another clone). Undefined
 * when any part cannot be shown, and then the answer is not memoized.
 */
function filesystemProof(folder: string, identity: WorktreeIdentity): string | undefined {
	try {
		let current = folder;
		while (!samePath(current, identity.root)) {
			if (dirname(current) === current) return undefined;
			let nested = true;
			try { lstatSync(join(current, ".git")); } catch (error) { nested = (error as NodeJS.ErrnoException).code !== "ENOENT"; }
			if (nested) return undefined;
			current = dirname(current);
		}
		const marker = lstatSync(join(identity.root, ".git"));
		const proof = [identityOf(statSync(identity.root)), identityOf(marker), identityOf(statSync(identity.commonDir))];
		if (marker.isFile()) proof.push(`${marker.size}:${marker.mtimeMs}`);
		return proof.join("|");
	} catch {
		return undefined;
	}
}

export interface WorktreeMemoOptions { ttlMs?: number; now?: () => number }
interface Remembered { identity: WorktreeIdentity; folder: string; proof: string; until: number }
const MEMO_LIMIT = 256;
const memoClears = new Set<() => void>();

/**
 * A resolver that remembers what it found. Only an answer the filesystem
 * proves is kept; each use checks the proof again, so a removed or replaced
 * clone, a new nested `.git` or a folder that became a repository (never
 * kept while it was not one) shows at once. The time limit covers what the
 * filesystem cannot show, such as an edited Git config.
 */
export function memoizeWorktreeResolver(resolver: WorktreeResolver, { ttlMs = 5000, now = Date.now }: WorktreeMemoOptions = {}): WorktreeResolver {
	const remembered = new Map<string, Remembered>();
	memoClears.add(() => remembered.clear());
	const stillTrue = (path: string, cwd: string, known: Remembered): boolean => {
		try {
			const folder = folderOf(path, cwd);
			return folder === known.folder && filesystemProof(folder, known.identity) === known.proof;
		} catch {
			return false;
		}
	};
	return (path, cwd) => {
		const key = `${path}\u0000${cwd}`;
		const known = remembered.get(key);
		if (known && known.until > now() && stillTrue(path, cwd, known)) return { ...known.identity };
		remembered.delete(key);
		const identity = resolver(path, cwd);
		if (!identity) return undefined;
		try {
			const folder = folderOf(path, cwd);
			const proof = filesystemProof(folder, identity);
			if (proof !== undefined) {
				if (remembered.size >= MEMO_LIMIT) remembered.clear();
				remembered.set(key, { identity: { ...identity }, folder, proof, until: now() + ttlMs });
			}
		} catch { /* Returned as Git said, never remembered. */ }
		return identity;
	};
}

/** Forgets every remembered worktree; the next question goes to Git. */
export function clearSessionWorktreeMemo(): void {
	for (const clear of memoClears) clear();
}

export const resolveSessionWorktree: WorktreeResolver = memoizeWorktreeResolver(resolveSessionWorktreeWithGit);

/**
 * A session's worktree list. It keeps no store of its own: the session file
 * is the record, read whole each time, so a root recorded by another
 * extension, or while the shell was off, counts too. Every extension appends
 * through its own pi API.
 */
export class SessionWorktreeRegistry {
	readonly sessionId: string;
	private readonly host: RegistryHost;
	private readonly session: SessionReader;
	private readonly cwd: string;
	private readonly resolver: WorktreeResolver;
	private readonly resolveBase: WorktreeBaseResolver;
	/** The clone the session started in; set later only when its own folder becomes a repository. */
	private identity: WorktreeIdentity | undefined;
	private readonly known = new Set<string>();
	private open = true;

	constructor(host: RegistryHost, session: SessionReader, cwd: string, resolver: WorktreeResolver = resolveSessionWorktree, resolveBase: WorktreeBaseResolver = resolveWorktreeHeadWithGit) {
		this.host = host;
		this.session = session;
		this.cwd = cwd;
		this.resolver = resolver;
		this.resolveBase = resolveBase;
		this.sessionId = session.getSessionId();
		this.identity = resolver(cwd, cwd);
	}

	/** Records the session's own root, when its folder is in a repository. */
	start(): void {
		if (this.identity) this.register(this.identity.root, "session:cwd");
	}

	close(): void {
		this.open = false;
	}

	private stillActive(): boolean {
		if (!this.open || this.sessionId.length === 0) return false;
		try { return this.session.getSessionId() === this.sessionId; } catch { return false; }
	}

	/**
	 * The clone the session works in, checked again now. A folder that became
	 * a repository after the start is taken (only the session's own folder,
	 * never a path it is asked about); a clone that changed is refused.
	 */
	private currentClone(): WorktreeIdentity | undefined {
		const now = this.resolver(this.cwd, this.cwd);
		const started = this.identity;
		if (started && (!now || now.root !== started.root || now.commonDir !== started.commonDir)) throw new Error(MESSAGES.moved);
		if (!started && now) this.identity = { ...now };
		return this.identity;
	}

	/** The root `path` is in, when it is a worktree of the session's own clone; throws otherwise. */
	validate(path: string): string {
		if (!this.stillActive()) throw new Error(MESSAGES.inactive);
		const clone = this.currentClone();
		const target = this.resolver(path, this.cwd);
		if (!clone || !target || target.commonDir !== clone.commonDir) throw new Error(MESSAGES.foreign);
		return target.root;
	}

	/** Reads the roots the session file already records for this session. */
	private readRecorded(): void {
		for (const entry of this.session.getEntries()) {
			if (entry.type !== "custom" || entry.customType !== SESSION_WORKTREE_ENTRY) continue;
			const data = entry.data as Partial<Registration> | null | undefined;
			if (!data || typeof data !== "object") continue;
			if (data.sessionId === this.sessionId && typeof data.root === "string" && typeof data.evidence === "string") this.known.add(data.root);
		}
	}

	/** Records the root `path` is in, once; returns the root. */
	register(path: string, evidence: string): string {
		const root = this.validate(path);
		this.readRecorded();
		if (this.known.has(root)) return root;
		// Its base is read once, now: a resumed session finds the root recorded
		// and keeps the base the first entry holds.
		let base: string | undefined;
		try { base = this.resolveBase(root); } catch { /* No base: the root reads against its HEAD. */ }
		const registration: Registration = { sessionId: this.sessionId, root, evidence, ...(isCommitId(base) ? { base } : {}) };
		// appendEntry is synchronous, so two tools ending together cannot both
		// append; the root counts as known only once the append went through.
		this.host.appendEntry(SESSION_WORKTREE_ENTRY, registration);
		this.known.add(root);
		this.host.events.emit(SESSION_WORKTREE_CHANGED, { sessionId: this.sessionId });
		return root;
	}

	/** The root a sub-agent runs in joins the list; the session's own root is on it already, and adds nothing. */
	registerChild(path: string): string | undefined {
		const root = this.validate(path);
		return root === this.identity?.root ? undefined : this.register(root, SUBAGENT_WORKTREE_EVIDENCE);
	}

	/** The recorded roots that still are worktrees of the clone; a removed one stays recorded but is not listed. */
	roots(): string[] {
		if (!this.stillActive()) return [];
		this.readRecorded();
		const roots = new Set<string>();
		for (const root of this.known) {
			try { roots.add(this.validate(root)); } catch { /* Gone or pruned: kept on record, not listed. */ }
		}
		return [...roots];
	}
}

/**
 * The `subagent` tool's record of the worktree it made or was given for a
 * child (T132): the LDD section looks for the child's change there. It never
 * stops a launch: when the session cannot record it, nothing is recorded.
 */
export function registerSubagentWorktree(host: Partial<RegistryHost>, session: Partial<SessionReader> | undefined, sessionCwd: string, childCwd: string): string | undefined {
	try {
		const usable = typeof host.appendEntry === "function" && typeof host.events?.emit === "function"
			&& typeof session?.getSessionId === "function" && typeof session.getEntries === "function";
		return !usable ? undefined : new SessionWorktreeRegistry(host as RegistryHost, session as SessionReader, sessionCwd).registerChild(childCwd);
	} catch {
		return undefined;
	}
}
