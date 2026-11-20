import { execFileSync } from "node:child_process";
import { lstatSync, realpathSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";

export const SESSION_WORKTREE_ENTRY = "jhon-shell.session-worktree/v1";
export const SESSION_WORKTREE_CHANGED = "jhon-shell:session-worktree-changed";
/** The registration the `subagent` tool writes for the worktree a child runs in. */
export const SUBAGENT_WORKTREE_EVIDENCE = "subagent:spawn";
export interface WorktreeIdentity { root: string; commonDir: string }
export type WorktreeResolver = (path: string, cwd: string) => WorktreeIdentity | undefined;
/** The commit a root's HEAD is at, or undefined when it has none (no commit yet) or Git cannot say. */
export type WorktreeBaseResolver = (root: string) => string | undefined;
const COMMIT_ID = /^[0-9a-f]{40,64}$/;
export const isCommitId = (value: unknown): value is string => typeof value === "string" && COMMIT_ID.test(value);
interface SessionReader {
	getSessionId(): string;
	getEntries(): readonly { type: string; customType?: string; data?: unknown }[];
}
interface RegistryHost {
	appendEntry(type: string, data: unknown): void;
	events: { emit(name: string, data: unknown): void };
}
// base (T132): the commit the root's HEAD was at when this session first
// registered it, kept with the root. Optional: an entry
// saved before it was kept, or a root with no commit yet, has none.
interface Registration { sessionId: string; root: string; evidence: string; base?: string }

// Pass this as a complete child environment, not an overlay on inherited env.
export function worktreeGitEnvironment(env: NodeJS.ProcessEnv = process.env): NodeJS.ProcessEnv {
	return Object.fromEntries(Object.entries(env).filter(([key]) => !key.toUpperCase().startsWith("GIT_")));
}

// Match Pi's ordinary path spelling; Git, not the argument, establishes identity.
function canonicalDirectory(path: string, cwd: string): string {
	let spelling = path.replace(/^@/, "").replace(/[\u00a0\u2000-\u200a\u202f\u205f\u3000]/g, " ");
	if (spelling === "~" || spelling.startsWith("~/")) spelling = homedir() + spelling.slice(1);
	const canonical = realpathSync(resolve(cwd, spelling));
	return statSync(canonical).isDirectory() ? canonical : dirname(canonical);
}

export function resolveSessionWorktreeWithGit(path: string, cwd: string, run: typeof execFileSync = execFileSync): WorktreeIdentity | undefined {
	try {
		const directory = canonicalDirectory(path, cwd);
		// Ambient Git routing must not redirect a path into another repository.
		const env = worktreeGitEnvironment();
		const git = (arg: string) => String(run("git", ["--no-optional-locks", "-C", directory, "rev-parse", "--path-format=absolute", arg], { encoding: "utf8", timeout: 5000, stdio: ["ignore", "pipe", "pipe"], shell: false, windowsHide: true, env })).replace(/\r?\n$/, "");
		return { root: realpathSync(git("--show-toplevel")), commonDir: realpathSync(git("--git-common-dir")) };
	} catch {
		return undefined;
	}
}

export function resolveWorktreeHeadWithGit(root: string, run: typeof execFileSync = execFileSync): string | undefined {
	try {
		const head = String(run("git", ["--no-optional-locks", "-C", root, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"], { encoding: "utf8", timeout: 5000, stdio: ["ignore", "pipe", "pipe"], shell: false, windowsHide: true, env: worktreeGitEnvironment() })).trim();
		return isCommitId(head) ? head : undefined;
	} catch {
		return undefined;
	}
}

// Filesystem identity of everything Git used to answer: no `.git` between the
// directory and its root, and the same root, `.git` entry and common dir.
// Undefined when any of it cannot be proven, so the answer is not memoized.
function worktreeFingerprint(directory: string, identity: WorktreeIdentity): string | undefined {
	const same = (a: string, b: string) => process.platform === "win32" ? a.toLowerCase() === b.toLowerCase() : a === b;
	try {
		for (let current = directory; !same(current, identity.root); current = dirname(current)) {
			if (dirname(current) === current) return undefined;
			try { lstatSync(join(current, ".git")); return undefined; }
			catch (error) { if ((error as NodeJS.ErrnoException).code !== "ENOENT") return undefined; }
		}
		const marker = lstatSync(join(identity.root, ".git"));
		const parts = [statSync(identity.root), marker, statSync(identity.commonDir)].map(entry => `${entry.dev}:${entry.ino}`);
		// A linked worktree's `.git` file can be rewritten in place to another clone.
		if (marker.isFile()) parts.push(`${marker.size}:${marker.mtimeMs}`);
		return parts.join("|");
	} catch {
		return undefined;
	}
}

export interface WorktreeMemoOptions { ttlMs?: number; now?: () => number }
interface MemoEntry { identity: WorktreeIdentity; directory: string; fingerprint: string; expiresAt: number }
const memoInvalidators = new Set<() => void>();

// Startup asks the same (cwd, cwd) question from several extensions; each
// answer costs two synchronous Git children. Only proven repositories are
// memoized, and every hit re-checks their filesystem identity, so removal,
// replacement, a nested `.git` and bootstrap (never cached) are seen at once.
// The TTL bounds what stat cannot see, such as Git config edits.
export function memoizeWorktreeResolver(resolver: WorktreeResolver, { ttlMs = 5000, now = Date.now }: WorktreeMemoOptions = {}): WorktreeResolver {
	const memo = new Map<string, MemoEntry>();
	memoInvalidators.add(() => memo.clear());
	return (path, cwd) => {
		const key = `${path}\u0000${cwd}`;
		const cached = memo.get(key);
		if (cached && cached.expiresAt > now()) {
			try {
				const directory = canonicalDirectory(path, cwd);
				if (directory === cached.directory && worktreeFingerprint(directory, cached.identity) === cached.fingerprint) return { ...cached.identity };
			} catch { /* Fall through to a fresh resolution. */ }
		}
		memo.delete(key);
		const identity = resolver(path, cwd);
		if (!identity) return undefined;
		try {
			const directory = canonicalDirectory(path, cwd);
			const fingerprint = worktreeFingerprint(directory, identity);
			if (fingerprint !== undefined) {
				if (memo.size >= 256) memo.clear();
				memo.set(key, { identity: { ...identity }, directory, fingerprint, expiresAt: now() + ttlMs });
			}
		} catch { /* Unprovable identity is returned but never memoized. */ }
		return identity;
	};
}

/** Drops every memoized worktree identity; the next lookup asks Git again. */
export function clearSessionWorktreeMemo(): void {
	for (const invalidate of memoInvalidators) invalidate();
}

export const resolveSessionWorktree: WorktreeResolver = memoizeWorktreeResolver(resolveSessionWorktreeWithGit);

// No global store: both extensions append through their bound Pi API. Reading
// all entries also catches a launch that happened while the shell was absent.
export class SessionWorktreeRegistry {
	readonly sessionId: string;
	private readonly host: RegistryHost;
	private readonly session: SessionReader;
	private readonly cwd: string;
	private readonly resolver: WorktreeResolver;
	private readonly resolveBase: WorktreeBaseResolver;
	private identity: WorktreeIdentity | undefined;
	private readonly recorded = new Set<string>();
	private active = true;

	constructor(host: RegistryHost, session: SessionReader, cwd: string, resolver: WorktreeResolver = resolveSessionWorktree, resolveBase: WorktreeBaseResolver = resolveWorktreeHeadWithGit) {
		this.host = host;
		this.session = session;
		this.cwd = cwd;
		this.resolver = resolver;
		this.resolveBase = resolveBase;
		this.sessionId = session.getSessionId();
		this.identity = resolver(cwd, cwd);
	}

	start(): void {
		if (this.identity) this.register(this.identity.root, "session:cwd");
	}

	close(): void { this.active = false; }

	private isCurrent(): boolean {
		try { return this.active && this.sessionId.length > 0 && this.session.getSessionId() === this.sessionId; }
		catch { return false; }
	}

	validate(path: string): string {
		if (!this.isCurrent()) throw new Error("This session is no longer the active one: it takes no new worktree.");
		const identity = this.refreshIdentity();
		const target = this.resolver(path, this.cwd);
		if (!target || !identity || target.commonDir !== identity.commonDir) throw new Error("Name a worktree that exists in this session's own Git clone; a folder of another repository cannot be one of its worktrees.");
		return target.root;
	}

	private refreshIdentity(): WorktreeIdentity | undefined {
		// Bootstrap can establish authority only at the original session cwd.
		// Never adopt a requested target, or replace an already established clone.
		const current = this.resolver(this.cwd, this.cwd);
		if (this.identity && (!current || current.root !== this.identity.root || current.commonDir !== this.identity.commonDir)) {
			throw new Error("The session's repository is not the one it started in; the worktree is not registered.");
		}
		if (!this.identity && current) this.identity = { ...current };
		return this.identity;
	}

	private restore(): void {
		for (const entry of this.session.getEntries()) {
			if (entry.type !== "custom" || entry.customType !== SESSION_WORKTREE_ENTRY || !entry.data || typeof entry.data !== "object") continue;
			const data = entry.data as Partial<Registration>;
			if (data.sessionId === this.sessionId && typeof data.root === "string" && typeof data.evidence === "string") this.recorded.add(data.root);
		}
	}

	register(path: string, evidence: string): string {
		const root = this.validate(path);
		this.restore();
		if (this.recorded.has(root)) return root;
		// Read once, at the first registration only: a resumed session finds its
		// root recorded above and keeps the base its entry holds.
		let base: string | undefined;
		try { base = this.resolveBase(root); } catch { /* No base: the root reads against HEAD. */ }
		// appendEntry is synchronous, so concurrent tool completions dedupe before
		// yielding. Mark locally only after a successful durable append.
		this.host.appendEntry(SESSION_WORKTREE_ENTRY, { sessionId: this.sessionId, root, evidence, ...(isCommitId(base) ? { base } : {}) } satisfies Registration);
		this.recorded.add(root);
		this.host.events.emit(SESSION_WORKTREE_CHANGED, { sessionId: this.sessionId });
		return root;
	}

	/** The root a sub-agent runs in joins the session's roots; the session's own root is already one and adds nothing. */
	registerChild(path: string): string | undefined {
		const root = this.validate(path);
		return root === this.identity?.root ? undefined : this.register(root, SUBAGENT_WORKTREE_EVIDENCE);
	}

	roots(): string[] {
		if (!this.isCurrent()) return [];
		this.restore();
		const roots = new Set<string>();
		for (const recorded of this.recorded) {
			try { roots.add(this.validate(recorded)); }
			catch { /* Missing/prunable roots remain durable but cannot be scanned. */ }
		}
		return [...roots];
	}
}

/**
 * The `subagent` tool's registration (T132): the worktree it creates for a
 * child, or is given for one, joins the session's roots, where the LDD
 * section looks for the change the child works on. Bookkeeping never refuses a
 * launch: a session that cannot record it registers nothing.
 */
export function registerSubagentWorktree(host: Partial<RegistryHost>, session: Partial<SessionReader> | undefined, sessionCwd: string, childCwd: string): string | undefined {
	try {
		if (typeof host.appendEntry !== "function" || typeof host.events?.emit !== "function" || typeof session?.getSessionId !== "function" || typeof session.getEntries !== "function") return undefined;
		return new SessionWorktreeRegistry(host as RegistryHost, session as SessionReader, sessionCwd).registerChild(childCwd);
	} catch {
		return undefined;
	}
}
