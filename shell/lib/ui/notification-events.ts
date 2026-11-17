import { randomUUID } from "node:crypto";
import type { AgentBeforeSettleEvent, ExtensionAPI, TurnEndEvent } from "@earendil-works/pi-coding-agent";
import type { TaskStatusChange } from "./tasks.ts";
import { NOTIFICATION_EVENTS, type NotificationEvent } from "./notification-policy.ts";
import type { NotificationOccurrence } from "./notification-scheduler.ts";

/** Content-free bus; producers never depend on an audio player or UI. */
export const NOTIFICATION_SOURCE_EVENT = "jhon-shell:notification:source";
export const NOTIFICATION_ATTENTION_EVENT = "jhon:owner-wait";
interface SourceOccurrence extends NotificationOccurrence { producerId: string }
export type NotificationSourceEvent =
	| (SourceOccurrence & { source: "main"; event: Extract<NotificationEvent, `agent.${string}`> })
	| (SourceOccurrence & { source: "session"; event: Extract<NotificationEvent, `session.${string}`> })
	| (SourceOccurrence & { source: "subagent"; event: SubagentEvent; parentSessionId: string; taskId: string; status: SubagentEnd });
/** A sub-agent's ends are its only events (T15.6): it finished or it failed. */
type SubagentEnd = "completed" | "failed";
type SubagentEvent = `subagent.${SubagentEnd}`;
const SUBAGENT_ENDS: readonly string[] = ["completed", "failed"] satisfies SubagentEnd[];

function record(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
function identity(value: unknown): value is string {
	return typeof value === "string" && value.length > 0 && value.length <= 512;
}
/** Validate untrusted bus payloads before forwarding to scheduler; never inspect transcript content. */
export function isNotificationSourceEvent(value: unknown): value is NotificationSourceEvent {
	if (!record(value) || !identity(value.producerId) || !identity(value.sessionId) || !identity(value.runId)
		|| !Number.isSafeInteger(value.sequence) || (value.sequence as number) < 1
		|| typeof value.occurredAt !== "number" || !Number.isFinite(value.occurredAt) || value.occurredAt < 0
		|| !NOTIFICATION_EVENTS.includes(value.event as NotificationEvent)) return false;
	if (value.source === "main") return (value.event as string).startsWith("agent.");
	if (value.source === "session") return (value.event as string).startsWith("session.");
	return value.source === "subagent" && value.parentSessionId === value.sessionId && identity(value.taskId)
		&& SUBAGENT_ENDS.includes(value.status as string) && value.event === `subagent.${value.status}`;
}

export function subagentNotificationEvent(change: TaskStatusChange & { status: SubagentEnd }, occurredAt: number): NotificationSourceEvent {
	return { source: "subagent", producerId: change.producerId, sessionId: change.sessionId,
		parentSessionId: change.parentSessionId, taskId: change.taskId, status: change.status,
		// Scheduler dedupes session/run/sequence. Include instance identity across reloads.
		runId: `${change.producerId}:${change.runId}`, sequence: change.sequence, event: `subagent.${change.status}`, occurredAt };
}


type Outcome = AgentBeforeSettleEvent["outcome"] | TurnEndEvent["outcome"];
function outcomeEvent(outcome: unknown): Extract<NotificationEvent, `agent.${string}`> | undefined {
	return outcome === "completed" ? "agent.completed" : outcome === "error" ? "agent.failed"
		: outcome === "aborted" ? "agent.cancelled" : undefined;
}
interface MainRun {
	sessionId: string;
	runId: string;
	outcome?: Extract<NotificationEvent, `agent.${string}`>;
	boundary: boolean;
	blocked: boolean;
}
/** Pure adapter, NOT a Pi hook registrar. Native events have no runId: owner captures
 * the begin token synchronously and uses it for every boundary; stale callbacks must
 * retain their old token, never look up a replacement run after an await.
 * agent_end is deliberately not an input. reset on EVERY shutdown, including reload.
 */
export class MainNotificationRun {
	private readonly producerId = randomUUID();
	private sequence = 0;
	private run?: MainRun;
	private readonly now: () => number;
	constructor(now: () => number) { this.now = now; }

	begin(sessionId: string): NotificationSourceEvent {
		this.run = { sessionId, runId: `${this.producerId}:${randomUUID()}`, boundary: false, blocked: false };
		return this.emit(this.run, "agent.started");
	}
	reset(): void { this.run = undefined; }

	beforeSettle(sessionId: string, runId: string, outcome: Outcome | unknown): void {
		const run = this.current(sessionId, runId);
		if (!run) return;
		run.boundary = true;
		run.outcome = outcomeEvent(outcome);
	}
	turnEnd(sessionId: string, runId: string, outcome: Outcome | unknown): void {
		const run = this.current(sessionId, runId);
		if (run && !run.boundary) run.outcome = outcomeEvent(outcome);
	}
	settled(sessionId: string, runId: string): NotificationSourceEvent | undefined {
		const run = this.current(sessionId, runId);
		if (!run) return undefined;
		this.run = undefined;
		return run.outcome ? this.emit(run, run.outcome) : undefined;
	}
	/** Only feed the verified owner-wait channel, never ui_prompt_start. */
	blocked(sessionId: string, runId: string, payload: unknown): NotificationSourceEvent | undefined {
		const run = this.current(sessionId, runId);
		if (!run || !record(payload) || typeof payload.active !== "boolean" || payload.active === run.blocked) return undefined;
		run.blocked = payload.active;
		return payload.active ? this.emit(run, "agent.attention") : undefined;
	}
	private current(sessionId: string, runId: string): MainRun | undefined {
		return this.run?.sessionId === sessionId && this.run.runId === runId ? this.run : undefined;
	}
	private emit(run: MainRun, event: Extract<NotificationEvent, `agent.${string}`>): NotificationSourceEvent {
		return { source: "main", producerId: this.producerId, sessionId: run.sessionId, runId: run.runId,
			sequence: ++this.sequence, event, occurredAt: this.now() };
	}
}
