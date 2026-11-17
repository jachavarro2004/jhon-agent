/**
 * shell/lib/announce-stop.ts — T135 (L1055): pi's half of announce-stop.
 *
 * A main-session run that ends with a reply announcing an action of the
 * agent's own ("sigo con el slice 3", "I'll now run the tests") and no tool
 * call is sent back once, as one follow-up user message, to do it or say in
 * one line why not; a reply with no text and no tool call (L1063) is sent
 * back the same way, to continue or say why it stopped. The rule is the
 * engine's, shared with Claude Code's Stop hook: the shell hands
 * `jhon hook announce-stop` the reply ("" when empty) as Claude Code's Stop
 * payload and sends the reason it answers with. At most one send-back per
 * owner turn (the owner's own message re-arms it, never the shell's); never in
 * a child, an aborted or failed run, or with `agents.act_on_announce` off (the
 * engine reads it). A failed or slow check sends nothing: the turn just ends.
 */
import { spawn } from "node:child_process";

/** Runs the engine's announce-stop hook with a payload; resolves its stdout. */
export type StopRunner = (payload: string, cwd: string) => Promise<string>;

/** How long the check may take before the turn ends without it. */
export const ANNOUNCE_CHECK_MS = 5000;

/**
 * The text of a run's final reply: its last assistant message, when that one
 * made no tool call and ended normally, "" when it holds no text (empty, or
 * only thinking); undefined otherwise (a tool call, an error, an abort).
 */
export function finalReply(messages: unknown): string | undefined {
	if (!Array.isArray(messages)) return undefined;
	for (let i = messages.length - 1; i >= 0; i--) {
		const m = messages[i] as { role?: unknown; content?: unknown; stopReason?: unknown; errorMessage?: unknown } | undefined;
		if (m?.role !== "assistant") continue;
		if (m.stopReason === "error" || m.stopReason === "aborted" || m.errorMessage) return undefined;
		const parts = Array.isArray(m.content) ? m.content : typeof m.content === "string" ? [{ type: "text", text: m.content }] : [];
		if (parts.some((p: any) => p?.type === "toolCall")) return undefined;
		const text = parts.filter((p: any) => p?.type === "text" && typeof p.text === "string").map((p: any) => p.text).join("\n").trim();
		return text;
	}
	return undefined;
}

/** The send-back of one main session: its latch, and the last run's reply. */
export class AnnounceSendBack {
	private sent = false;
	private reply: string | undefined;
	private readonly run: StopRunner;
	constructor(run: StopRunner) { this.run = run; }

	/** The owner wrote: a new owner turn may be sent back once. */
	ownerTurn(): void { this.sent = false; }

	/** A run ended (pi's agent_end): its final reply, if any. */
	ended(messages: unknown): void { this.reply = finalReply(messages); }

	/**
	 * The run settled: the reason to send back, or undefined to let the turn
	 * end. Takes the reply, so a settle is judged once.
	 */
	async settled(aborted: boolean, cwd: string): Promise<string | undefined> {
		const reply = this.reply;
		this.reply = undefined;
		if (aborted || this.sent || reply === undefined) return undefined;
		let out: string;
		try {
			out = await this.run(JSON.stringify({ hook_event_name: "Stop", stop_hook_active: false, last_assistant_message: reply, cwd }), cwd);
		} catch {
			return undefined;
		}
		let answer: unknown;
		try { answer = JSON.parse(out); } catch { return undefined; }
		const { decision, reason } = (answer ?? {}) as { decision?: unknown; reason?: unknown };
		if (decision !== "block" || typeof reason !== "string" || !reason.trim()) return undefined;
		this.sent = true;
		return reason;
	}
}

/** The engine's `jhon hook announce-stop`, killed past ANNOUNCE_CHECK_MS. */
export function engineStopRunner(bin: string, timeoutMs = ANNOUNCE_CHECK_MS): StopRunner {
	return (payload, cwd) => new Promise((resolve, reject) => {
		const run = spawn(bin, ["hook", "announce-stop"], { cwd, stdio: ["pipe", "pipe", "ignore"] });
		let out = "";
		const timer = setTimeout(() => { run.kill("SIGKILL"); reject(new Error("announce-stop timed out")); }, timeoutMs);
		timer.unref?.();
		run.stdout.setEncoding("utf8").on("data", (d: string) => { out += d; });
		run.once("error", (e) => { clearTimeout(timer); reject(e); });
		run.once("close", (code: number | null) => { clearTimeout(timer); code === 0 ? resolve(out) : reject(new Error(`announce-stop exited ${code}`)); });
		run.stdin.on("error", () => {});
		run.stdin.end(payload);
	});
}
