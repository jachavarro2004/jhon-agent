/**
 * shell/lib/attention.ts — an agent waiting for the owner says so on pi's
 * event bus as `jhon:owner-wait` (T15.4, T24b): `{ active: true, label }`
 * when the wait starts and `{ active: false }` when it settles, answered,
 * dismissed or failed. Any other extension may read it, such as a
 * multiplexer's bridge (shell/CONTRACT.md, "Events for other extensions").
 */

export const OWNER_WAIT_EVENT = "jhon:owner-wait";

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
