/**
 * A command's prompt (/jhon-ldd, /jhon:video) as the owner's message, in
 * every mode pi runs (T8f, odd/tasks/studio-integration-v1.md L47).
 *
 * pi.sendUserMessage() only starts the run: its prompt goes through the input
 * handlers before the session counts as busy (pi 1.5.2, AgentSession.prompt,
 * then _startRun), and the command returns at once. In the terminal and in
 * RPC that is right: the session lives on and the run with it. pi's print and
 * json modes (`pi -p`, `--mode json`) print the last answer and dispose the
 * session as soon as the command returns, so the run went on in a disposed
 * session: nothing was printed and every extension's handler got pi's "ctx is
 * stale after session replacement or reload". There the command waits for the
 * run it started to settle, follow-ups included (ctx.waitForIdle, asked while
 * the run is busy); a prompt that starts no run (an input handler took it)
 * ends the wait once the session has stayed idle for NO_RUN_MS.
 */

/**
 * How long a headless command waits, idle, for its prompt to start a run.
 * The input handlers run before the session counts as busy, so this bound
 * covers them too: an extension whose `input` handler takes longer than this
 * for the command's prompt lets the wait end before the run starts, and the
 * stale-ctx failure comes back for that run. Jhon's own input handler is
 * quicker (a compaction it awaits counts as busy, so it is waited for).
 */
export const NO_RUN_MS = 5_000;
const POLL_MS = 25;

/** A run pi ends when the prompt returns: print and json mode. */
export function headless(ctx: any): boolean {
	try { return ctx?.mode === "print" || ctx?.mode === "json"; } catch { return false; }
}

/** Whether a run is going; a stale context runs none. */
function busy(ctx: any): boolean {
	try { return ctx?.isIdle?.() === false; } catch { return false; }
}

export type SendPrompt = (ctx: any, text: string) => Promise<void>;

/**
 * The sender of commands' prompts on the main session's `pi`. While the agent
 * works the prompt steers the turn, as a typed one does. A refusal pi throws
 * (a compaction running) is the caller's to say.
 */
export function commandPrompts(pi: any, wait: { noRunMs?: number; pollMs?: number } = {}): SendPrompt {
	const noRunMs = wait.noRunMs ?? NO_RUN_MS;
	const pollMs = wait.pollMs ?? POLL_MS;
	/** Set while a headless command waits for its run to start. */
	let started: (() => void) | undefined;
	pi.on("agent_start", () => { started?.(); return undefined; });

	return async (ctx, text) => {
		const options = busy(ctx) ? { deliverAs: "steer" } : undefined;
		if (!headless(ctx)) { pi.sendUserMessage(text, options); return; }
		let poll: ReturnType<typeof setInterval> | undefined;
		// Every way out of the wait goes through end: the run's own end, the idle bound, a send pi refused.
		let end: () => void = () => {};
		const settled = new Promise<void>((resolve) => {
			end = () => { started = undefined; clearInterval(poll); resolve(); };
			// Asked at agent_start, while the run is busy: pi resolves it after the settle and what it deferred.
			// A waitForIdle that throws or rejects (a stale ctx) ends the wait as its resolving does.
			started = () => {
				started = undefined;
				clearInterval(poll);
				let idle: unknown;
				try { idle = ctx.waitForIdle?.(); } catch { idle = undefined; }
				Promise.resolve(idle).then(end, end);
			};
			let idleFor = 0;
			poll = setInterval(() => {
				idleFor = busy(ctx) ? 0 : idleFor + pollMs;
				if (idleFor >= noRunMs) end();
			}, pollMs);
		});
		try { pi.sendUserMessage(text, options); }
		catch (error) { end(); throw error; }
		await settled;
	};
}
