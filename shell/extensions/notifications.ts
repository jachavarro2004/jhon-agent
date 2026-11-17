import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { MainNotificationRun, NOTIFICATION_ATTENTION_EVENT, NOTIFICATION_SOURCE_EVENT, isNotificationSourceEvent } from "../lib/ui/notification-events.ts";
import { claimNotificationOwner, type NotificationServiceDependencies } from "../lib/ui/notification-service.ts";

/**
 * The notification service's dependencies. The context has one notice, the
 * fresh-session one (lib/fresh-session.ts, T15.3); nothing here watches it.
 */
export type NotificationExtensionDependencies = NotificationServiceDependencies;

/** Default directory loader is the sole audio owner; customize will retrieve its shared facade. */
export function createNotificationExtension(pi: ExtensionAPI, deps: NotificationExtensionDependencies = {}): () => void {
	const nativeOff: (() => void)[] = []; const busOff: (() => void)[] = [];
	const stopBus = () => { for (const off of busOff.splice(0)) off(); };
	const owner = claimNotificationOwner(() => {
		stopBus(); main.reset(); runId = undefined;
		for (const off of nativeOff.splice(0)) off();
	}, deps);
	const main = new MainNotificationRun(owner.now);
	let runId: string | undefined; let sessionId: string | undefined; let attachedAt = 0;
	const valid = (ctx: ExtensionContext) => owner.allowed(ctx) && ctx.sessionManager.getSessionId() === sessionId;
	const remember = (off: unknown) => { if (typeof off === "function") nativeOff.push(off as () => void); };
	remember(pi.on("session_start", (event, ctx) => {
		if (!owner.current()) return;
		stopBus(); main.reset(); runId = undefined;
		sessionId = ctx.sessionManager.getSessionId();
		if (!owner.attach(ctx)) return;
		attachedAt = owner.now();
		busOff.push(pi.events.on(NOTIFICATION_SOURCE_EVENT, data => {
			if (!valid(ctx) || !isNotificationSourceEvent(data) || data.source !== "subagent"
				|| data.sessionId !== sessionId || data.parentSessionId !== sessionId
				|| data.occurredAt < attachedAt || data.occurredAt > owner.now()) return;
			if (!owner.acceptSource(data.producerId, data.runId, data.sequence)) return;
			owner.enqueue(data);
		}));
		busOff.push(pi.events.on(NOTIFICATION_ATTENTION_EVENT, payload => {
			if (!valid(ctx) || !runId) return;
			const token = runId;
			const occurrence = main.blocked(sessionId!, token, payload);
			if (occurrence) owner.enqueue(occurrence);
		}));
		if (event.reason === "startup" && owner.startup()) owner.enqueue({ sessionId: sessionId!, runId: "process-startup",
			sequence: 1, event: "session.started", occurredAt: owner.now() });
	}));
	remember(pi.on("agent_start", (_event, ctx) => {
		if (!valid(ctx)) return;
		const occurrence = main.begin(sessionId!); runId = occurrence.runId; owner.enqueue(occurrence);
	}));
	// Native events carry no run identity. Capture the local token synchronously: no awaits
	// here, no late callback ever searches for a replacement token. before_settle is latest/final evidence.
	remember(pi.on("agent_before_settle", (event, ctx) => {
		const token = runId;
		if (valid(ctx) && token) main.beforeSettle(sessionId!, token, event.outcome);
	}));
	remember(pi.on("turn_end", (event, ctx) => {
		if (!valid(ctx)) return;
		const token = runId;
		if (token) main.turnEnd(sessionId!, token, event.outcome);
	}));
	remember(pi.on("agent_settled", (_event, ctx) => {
		const token = runId;
		if (!valid(ctx) || !token) return;
		const occurrence = main.settled(sessionId!, token); runId = undefined;
		if (occurrence) owner.enqueue(occurrence);
	}));
	remember(pi.on("session_shutdown", () => {
		if (!owner.current()) return;
		stopBus(); main.reset(); runId = undefined; owner.shutdown();
		sessionId = undefined;
		// No quit sound: mandatory immediate cancellation must not wait for lazy detection.
		// Native lifecycle hooks remain dormant for the next session; lease retirement removes them.
	}));
	return owner.retire;
}
export default function jhonNotifications(pi: ExtensionAPI): void { createNotificationExtension(pi); }
