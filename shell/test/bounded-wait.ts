// Bounded waits for tests that use real sockets, workers and timers (T97).
// A stuck wait must fail with a name instead of hanging the suite, and polling
// replaces fixed sleeps so a CPU-starved machine only makes the test slower.
import type { Server, Socket } from "node:net";

export const BOUNDED_WAIT_MS = 20_000;

/** Resolves with the promise, or rejects with a named error after timeoutMs. */
export async function withTimeout<T>(promise: Promise<T>, label: string, timeoutMs = BOUNDED_WAIT_MS): Promise<T> {
	let timer: NodeJS.Timeout | undefined;
	const deadline = new Promise<never>((_resolve, reject) => {
		timer = setTimeout(() => reject(new Error(`bounded wait timed out after ${timeoutMs} ms: ${label}`)), timeoutMs);
	});
	try { return await Promise.race([promise, deadline]); } finally { clearTimeout(timer); }
}

/** Polls predicate on the real clock until it holds, or fails with a named error. */
export async function waitFor(predicate: () => boolean, label: string, timeoutMs = BOUNDED_WAIT_MS): Promise<void> {
	const start = Date.now();
	while (!predicate()) {
		if (Date.now() - start > timeoutMs) throw new Error(`bounded wait timed out after ${timeoutMs} ms: ${label}`);
		await new Promise(resolve => setTimeout(resolve, 5));
	}
}

/** Tracks a server's connections so close can destroy them instead of waiting for peers. */
export function trackConnections(server: Server): Set<Socket> {
	const sockets = new Set<Socket>();
	server.on("connection", socket => { sockets.add(socket); socket.once("close", () => sockets.delete(socket)); });
	return sockets;
}

/** Closes a server after destroying its open connections, bounded by timeoutMs. */
export async function closeServer(server: Server, sockets: Set<Socket>, label: string, timeoutMs = BOUNDED_WAIT_MS): Promise<void> {
	for (const socket of sockets) socket.destroy();
	await withTimeout(new Promise<void>(resolve => server.close(() => resolve())), `close ${label}`, timeoutMs);
}
