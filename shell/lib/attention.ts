/**
 * shell/lib/attention.ts — an agent waiting for the owner says so on pi's
 * event bus as `jhon:owner-wait` (T15.4, T24b): `{ active: true, label }`
 * when the wait starts and `{ active: false }` when it settles, answered,
 * dismissed or failed. The shell's notifications read it (agent.attention,
 * once per wait; silent when a host that alerts is announced, lib/host.ts),
 * and so may any other extension, such as a multiplexer's bridge
 * (shell/CONTRACT.md, "Events for other extensions").
 */
import { NOTIFICATION_ATTENTION_EVENT } from "./ui/notification-events.ts";

export const OWNER_WAIT_EVENT = NOTIFICATION_ATTENTION_EVENT;

export interface OwnerWaitBus { emit(name: string, data: unknown): void }

/** Runs wait between one blocked and one settle event; without a bus it only waits. */
export async function waitingForOwner<T>(events: OwnerWaitBus | undefined, label: string, wait: () => Promise<T>): Promise<T> {
	events?.emit(OWNER_WAIT_EVENT, { active: true, label });
	try {
		return await wait();
	} finally {
		events?.emit(OWNER_WAIT_EVENT, { active: false });
	}
}
