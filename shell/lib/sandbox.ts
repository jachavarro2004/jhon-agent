/**
 * shell/lib/sandbox.ts — the OS sandbox around the commands the model chooses
 * (T128b, L894).
 *
 * At session start the shell asks `jhon sandbox status --json` once, in the
 * session's folder, and keeps the answer for the session: `sandbox.mode`,
 * whether the sandbox can run here, the reason and the fix when it cannot,
 * and the project (the folder that holds `.git`). With the mode on, `bash`,
 * `bash_background` and `monitor` run their command as
 *
 *   jhon sandbox run --project <pinned> --profile <profile> -- <shell> -c <command>
 *
 * where the profile is what `jhon sandbox profile -- <command>` names for the
 * command (T128g, L1303 G1): `unsandboxed` for a line whose programs are all
 * git with a network subcommand or gh with a listed subcommand, which `jhon
 * sandbox run` then runs with no sandbox (git over SSH and gh cannot run in
 * one, L1220, L1302); `work` for any other line, and whenever the engine
 * gives no other answer. Claude Code's wrap takes the same answer from the
 * same engine function (sandbox.ProfileFor). Either way `jhon sandbox run`
 * ends a failure on a read-only path with the stop note on stderr, which is
 * the tool result's end, and records a jhon.sandbox event (G3, G4).
 * `inspect`, which starts the program the model names with no shell, runs
 *
 *   jhon sandbox run --project <pinned> --profile work -- <program> <args...>
 *
 * The command is one argument of that argv: no shell reads it before the one
 * inside the sandbox. With the mode on where the sandbox cannot run, or with
 * no answer to pin, the call is refused and nothing runs. With the mode off
 * nothing here touches a command: `bash` stays pi's built-in tool.
 *
 * The guards judge the original command first (extensions/jhon.ts); this
 * wraps only what they let through. The owner's `!` commands and the owner's
 * own jobs are theirs and run as before.
 *
 * One pin per session (L894; L925, E5): a session that pinned a project
 * exports it as JHON_SANDBOX_PROJECT, the variable `jhon start` sets, so
 * the guards it calls and the children it starts read the same folder. A
 * sub-agent is a child pi that loads this extension in a folder the model
 * may choose (the `subagent` tool's `cwd`, a worktree of the session's
 * repository): it asks the status for its parent's project (`--project`),
 * never for its own folder's, and is refused where that project is gone.
 * A session start that does not end with a pin (no answer, the mode off, a
 * sandbox that cannot run, no project) takes the variable out of the
 * environment, so no child inherits an earlier session's project (L941, G1).
 */
import { spawn } from "node:child_process";
import { constants as osConstants } from "node:os";
import { accessSync } from "node:fs";
import { delimiter, join } from "node:path";
import { createBashToolDefinition, getAgentDir, getShellConfig, SettingsManager, type BashOperations } from "@earendil-works/pi-coding-agent";
import { HOOK_TIMEOUT_MS, runAsync, type Started } from "./guards.ts";
import { BASH_BACKGROUND_TOOL, MONITOR_TOOL } from "./ui/background-jobs.ts";

/** `jhon sandbox status --json`, as the session keeps it; `unread` when it gave no answer. */
export type SandboxPin =
	| { mode: "on" | "off"; canRun: boolean; reason: string; fix: string; project: string | undefined }
	| { unread: string };

/** What the pinned answer means for one command: run it as before, run it in the sandbox, or refuse it. */
export type SandboxVerdict = { run: "plain" } | { run: "sandbox"; project: string } | { refuse: string };

/** The tools whose command the model chooses and a shell runs. */
export const SANDBOXED_TOOLS: readonly string[] = ["bash", BASH_BACKGROUND_TOOL, MONITOR_TOOL];

/** The variable that carries a session's pinned project to its guards and its children (`jhon start` sets it; engine: sandbox.ProjectEnv). */
export const SANDBOX_PROJECT_ENV = "JHON_SANDBOX_PROJECT";

/**
 * Ask `jhon sandbox status --json` in `cwd`: the folder `jhon sandbox run`
 * would take as the project from there, or, with `project`, that folder as
 * the pinned one (a child asking for its parent's).
 */
export async function readSandboxStatus(config: { jhon: string }, cwd: string, timeoutMs = HOOK_TIMEOUT_MS, project?: string): Promise<SandboxPin> {
	const r = await runAsync(config.jhon, ["sandbox", "status", "--json", ...(project ? ["--project", project] : [])], { cwd, timeoutMs });
	if (r.timedOut) return { unread: `no answer in ${timeoutMs / 1000}s` };
	if (r.status !== 0) return { unread: (r.stderr ?? "").trim().split("\n")[0] || (r.error ? r.error.message : `exit ${r.status}`) };
	let s: any;
	try { s = JSON.parse(r.stdout); } catch { return { unread: "its output is not JSON" }; }
	if ((s?.mode !== "on" && s?.mode !== "off") || typeof s.can_run !== "boolean") return { unread: "its output carries no mode or can_run" };
	const line = (v: unknown) => (typeof v === "string" ? v : "");
	return { mode: s.mode, canRun: s.can_run, reason: line(s.reason), fix: line(s.fix), project: typeof s.project === "string" ? s.project : undefined };
}

export function sandboxVerdict(pin: SandboxPin): SandboxVerdict {
	if ("unread" in pin) {
		return { refuse: `jhon: \`jhon sandbox status --json\` gave no answer (${pin.unread}), so whether this command runs in the sandbox is unknown; the command is refused, never run unsandboxed in silence; run \`jhon doctor\`` };
	}
	if (pin.mode === "off") return { run: "plain" };
	if (!pin.canRun) {
		return { refuse: `jhon: sandbox.mode is on and the sandbox cannot run here: ${pin.reason || "no reason given"}; the command is refused, never run unsandboxed in silence. Fix: ${pin.fix || "run `jhon doctor`"}` };
	}
	if (pin.project === undefined) {
		return { refuse: "jhon: sandbox.mode is on and `jhon sandbox status --json` names no project to pin, so the command is refused, never run unpinned. Fix: run `jhon sync`" };
	}
	if (pin.project === "") {
		return { refuse: "jhon: sandbox.mode is on and this session is in no git repository, so there is no project to pin and the command is refused, never run unpinned. Fix: start the session inside a repository (the owner may run `git init` here and start it again)" };
	}
	return { run: "sandbox", project: pin.project };
}

/**
 * How a command line runs in `cwd`, as the engine names it (`jhon sandbox
 * profile [--project <pinned>] -- <command>`; a git line must work inside the
 * pinned project): `unsandboxed` only when it answers exactly that, `work` for
 * any other answer, an error, or no answer in time (fail closed).
 */
export async function sandboxProfile(config: { jhon: string }, command: string, cwd: string, timeoutMs = HOOK_TIMEOUT_MS, project?: string): Promise<"work" | "unsandboxed"> {
	const r = await runAsync(config.jhon, ["sandbox", "profile", ...(project ? ["--project", project] : []), "--", command], { cwd, timeoutMs });
	return !r.timedOut && r.status === 0 && r.stdout === "unsandboxed\n" ? "unsandboxed" : "work";
}

/** pi's shell environment for a command that names none: the process's, with pi's bin folder on PATH. */
function shellEnv(): NodeJS.ProcessEnv {
	const bin = join(getAgentDir(), "bin");
	const key = Object.keys(process.env).find((k) => k.toLowerCase() === "path") ?? "PATH";
	const current = process.env[key] ?? "";
	return { ...process.env, [key]: current.split(delimiter).includes(bin) ? current : [bin, current].filter(Boolean).join(delimiter) };
}

/** As pi's shell tools: the whole process group, so nothing the command started outlives it. */
function killGroup(pid: number): void {
	try { process.kill(-pid, "SIGKILL"); } catch {
		try { process.kill(pid, "SIGKILL"); } catch { /* Already gone. */ }
	}
}

const MAX_TIMEOUT_MS = 2_147_483_647;
/** After the program's exit, how long its quiet pipes may stay open (a descendant holding them) before the call settles. */
const EXIT_STDIO_GRACE_MS = 100;

/**
 * pi's bash operations with the command run in the sandbox: the same shell pi
 * would start (`shellPath`, else pi's own choice) and the same contract as
 * pi's local operations (the exit code, 128 + signal, `aborted`,
 * `timeout:<seconds>`), started as `jhon sandbox run ... --profile <profile>
 * -- <shell> -c <command>` (sandboxProfile) in a process group of its own,
 * which an abort or the timeout kills whole. The project is always pinned (`--project`): without it a
 * command that removed `.git` would make an enclosing repository the project.
 */
export function sandboxOperations(o: { jhon: string; project: string; shellPath?: string }): BashOperations {
	return {
		exec: async (command, cwd, { onData, signal, timeout, env }) => {
			let timeoutMs: number | undefined;
			if (timeout !== undefined) {
				if (!Number.isFinite(timeout) || timeout <= 0) throw new Error("Invalid timeout: must be a finite number of seconds");
				timeoutMs = timeout * 1000;
				if (timeoutMs > MAX_TIMEOUT_MS) throw new Error(`Invalid timeout: maximum is ${MAX_TIMEOUT_MS / 1000} seconds`);
			}
			if (signal?.aborted) throw new Error("aborted");
			const shell = getShellConfig(o.shellPath);
			// Only a Windows launcher reads its command from stdin, and the sandbox is Linux's.
			if (shell.commandTransport === "stdin") throw new Error(`jhon: ${shell.shell} reads its command from stdin, which the sandbox does not run`);
			try { accessSync(cwd); } catch { throw new Error(`Working directory does not exist: ${cwd}\nCannot execute bash commands.`); }
			const profile = await sandboxProfile({ jhon: o.jhon }, command, cwd, HOOK_TIMEOUT_MS, o.project);
			if (signal?.aborted) throw new Error("aborted");
			const child = spawn(o.jhon, ["sandbox", "run", "--project", o.project, "--profile", profile, "--", shell.shell, ...shell.args, command], {
				cwd, detached: true, env: env ?? shellEnv(), stdio: ["ignore", "pipe", "pipe"],
			});
			let timedOut = false;
			let timer: ReturnType<typeof setTimeout> | undefined;
			const kill = () => { if (child.pid) killGroup(child.pid); };
			if (timeoutMs !== undefined) timer = setTimeout(() => { timedOut = true; kill(); }, timeoutMs);
			signal?.addEventListener("abort", kill, { once: true });
			try {
				const exitCode = await new Promise<number | null>((resolve, reject) => {
					let settled = false, exited = false, code: number | null = null, open = 2;
					let grace: ReturnType<typeof setTimeout> | undefined;
					const finish = () => {
						if (settled) return;
						settled = true;
						if (grace) clearTimeout(grace);
						child.stdout.destroy();
						child.stderr.destroy();
						resolve(code);
					};
					// Output still arriving after the exit is read to its end; a quiet inherited pipe is not waited for.
					const idle = () => { if (grace) clearTimeout(grace); grace = setTimeout(finish, EXIT_STDIO_GRACE_MS); };
					const data = (d: Buffer) => { onData(d); if (exited && !settled) idle(); };
					const ended = () => { open -= 1; if (exited && open === 0) finish(); };
					child.stdout.on("data", data);
					child.stderr.on("data", data);
					child.stdout.on("end", ended);
					child.stderr.on("end", ended);
					child.on("error", (e) => { if (!settled) { settled = true; reject(e); } });
					child.on("exit", (c) => { exited = true; code = c; if (open === 0) finish(); else idle(); });
				});
				if (signal?.aborted) throw new Error("aborted");
				if (timedOut) throw new Error(`timeout:${timeout}`);
				const killedBy = child.signalCode;
				return { exitCode: exitCode ?? (killedBy ? 128 + (osConstants.signals[killedBy] ?? 0) : 1) };
			} finally {
				if (timer) clearTimeout(timer);
				signal?.removeEventListener("abort", kill);
			}
		},
	};
}

export interface SandboxWiring {
	/** A session starts in `cwd`: ask, and keep the answer for it. */
	start(cwd: string): Promise<SandboxPin>;
	/** The refusal for a call to `toolName` in this session, or undefined when it may run. Asks when no session start did. */
	gate(toolName: string, cwd: string): Promise<string | undefined>;
	/** The operations a background command runs through: the sandbox's, or undefined to run as before. Throws the refusal. */
	jobOperations(shellPath: string | undefined): BashOperations | undefined;
	/** How `inspect` starts the program the model named: as given with the mode off, as `jhon sandbox run ... -- <program> <args>` with it on, or the refusal the gate gives bash. Asks when no session start did. */
	startArgv(program: string, args: string[], cwd: string): Promise<Started>;
}

/**
 * Wire the sandbox on `pi` for the binary the guards call (`jhon`; with none
 * the guards already refuse every command, and nothing is asked): the status
 * is pinned at each session start, and where it says to sandbox, `bash` is
 * registered again from pi's own definition, the same name, description,
 * schema, prompt lines and renderers, with only its operations replaced.
 * With the mode off no tool is registered: `bash` is pi's built-in one.
 */
export function wireSandbox(pi: any, o: { jhon: string | undefined; timeoutMs?: number; env?: NodeJS.ProcessEnv }): SandboxWiring {
	let asked: Promise<SandboxPin> | undefined;
	let pinned: SandboxPin | undefined;
	let bashRegistered = false;
	const OFF: SandboxPin = { mode: "off", canRun: false, reason: "", fix: "", project: undefined };
	// The environment this process's guards and children inherit (tests give their own).
	const env = o.env ?? process.env;
	// A sub-agent child (lib/agents.ts: childEnv) asks for the project its parent pinned and exported.
	const inherited = env.JHON_PI_CHILD === "1" ? env[SANDBOX_PROJECT_ENV] || undefined : undefined;

	const ask = (cwd: string): Promise<SandboxPin> => {
		const jhon = o.jhon;
		// The earlier session's pin is not this one's: until this ask ends with a pin, none is exported.
		delete env[SANDBOX_PROJECT_ENV];
		const mine: Promise<SandboxPin> = (jhon ? readSandboxStatus({ jhon }, cwd, o.timeoutMs, inherited) : Promise.resolve(OFF)).then((pin) => {
			// A later session start replaced this ask: its answer is the session's.
			if (asked === mine) {
				pinned = pin;
				// One pin per session: the guards this session calls and the children it starts read this one.
				const verdict = sandboxVerdict(pin);
				if ("run" in verdict && verdict.run === "sandbox") env[SANDBOX_PROJECT_ENV] = verdict.project;
				else delete env[SANDBOX_PROJECT_ENV];
			}
			return pin;
		});
		asked = mine;
		return mine;
	};

	const registerBash = () => {
		if (bashRegistered) return;
		bashRegistered = true;
		pi.registerTool({
			// pi's own definition: what the model reads and the chat draws are the built-in's.
			...createBashToolDefinition(process.cwd()),
			async execute(id: string, params: any, signal: AbortSignal | undefined, onUpdate: any, ctx: any) {
				const cwd = ctx?.cwd ?? process.cwd();
				const verdict = sandboxVerdict(await (asked ?? ask(cwd)));
				if ("refuse" in verdict) throw new Error(verdict.refuse);
				// The configured shell and prefix, read as the background commands read them.
				const settings = SettingsManager.create(cwd, getAgentDir());
				const shellPath = settings.getShellPath();
				const operations = verdict.run === "sandbox" && o.jhon ? sandboxOperations({ jhon: o.jhon, project: verdict.project, shellPath }) : undefined;
				return createBashToolDefinition(cwd, { commandPrefix: settings.getShellCommandPrefix(), shellPath, ...(operations ? { operations } : {}) })
					.execute(id, params, signal, onUpdate, ctx);
			},
		});
	};

	const start = async (cwd: string) => {
		const pin = await ask(cwd);
		const verdict = sandboxVerdict(pin);
		if ("run" in verdict && verdict.run === "sandbox") registerBash();
		return pin;
	};
	pi.on("session_start", async (_event: unknown, ctx: any) => { await start(ctx?.cwd ?? process.cwd()); return undefined; });

	return {
		start,
		async gate(toolName, cwd) {
			if (!SANDBOXED_TOOLS.includes(toolName)) return undefined;
			const verdict = sandboxVerdict(await (asked ?? ask(cwd)));
			if ("refuse" in verdict) return verdict.refuse;
			// Pinned by this call, not by a session start: pi's built-in bash would run it unwrapped.
			if (verdict.run === "sandbox" && toolName === "bash" && !bashRegistered) {
				registerBash();
				return "jhon: the sandbox was pinned by this call, after the session started, so this command did not run; run it again";
			}
			return undefined;
		},
		jobOperations(shellPath) {
			if (!o.jhon) return undefined;
			const verdict = sandboxVerdict(pinned ?? { unread: "not asked yet in this session" });
			if ("refuse" in verdict) throw new Error(verdict.refuse);
			return verdict.run === "sandbox" ? sandboxOperations({ jhon: o.jhon, project: verdict.project, shellPath }) : undefined;
		},
		async startArgv(program, args, cwd) {
			if (!o.jhon) return { program, args };
			const verdict = sandboxVerdict(await (asked ?? ask(cwd)));
			if ("refuse" in verdict) return verdict;
			if (verdict.run === "plain") return { program, args };
			return { program: o.jhon, args: ["sandbox", "run", "--project", verdict.project, "--profile", "work", "--", program, ...args] };
		},
	};
}
