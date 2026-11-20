/**
 * shell/lib/host.ts — the multiplexer the shell runs in, as its bridge says
 * (T24b). Jhon knows no multiplexer: a bridge extension announces one on pi's
 * event bus as `jhon:host` `{ name, alerts?, showsPath? }`, at its own start
 * and in answer to the shell's `jhon:host?` query at session start, so the
 * load order does not matter. `alerts: true` says the host plays its own
 * alert while the owner is waited for (the shell plays no sound, so nothing
 * reads it; CONTRACT.md keeps the field);
 * `showsPath: true` drops the path from the top row (the host shows it).
 * shell/CONTRACT.md, "Events for other extensions".
 */

export const HOST_EVENT = "jhon:host";
export const HOST_QUERY_EVENT = "jhon:host?";

export interface HostAnnouncement { name: string; alerts?: boolean; showsPath?: boolean }

interface HostBus {
	on(name: string, fn: (data: unknown) => void): unknown;
	emit(name: string, data: unknown): void;
}

// One registry per process: pi may load each extension's modules apart.
const KEY = Symbol.for("jhon-shell.hosts");
function hosts(): Map<string, HostAnnouncement> {
	const g = globalThis as unknown as Record<symbol, Map<string, HostAnnouncement> | undefined>;
	return g[KEY] ??= new Map();
}

export function isHostAnnouncement(value: unknown): value is HostAnnouncement {
	if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
	const v = value as Record<string, unknown>;
	return typeof v.name === "string" && v.name.length > 0 && v.name.length <= 128
		&& (v.alerts === undefined || typeof v.alerts === "boolean")
		&& (v.showsPath === undefined || typeof v.showsPath === "boolean");
}

/** Records every well-formed `jhon:host`; a host announcing again replaces what it said. */
export function listenForHosts(events: HostBus | undefined): void {
	events?.on(HOST_EVENT, (data) => {
		if (isHostAnnouncement(data)) hosts().set(data.name, { name: data.name, alerts: data.alerts, showsPath: data.showsPath });
	});
}

/** Asks every bridge already loaded to announce its host. */
export function askHosts(events: HostBus | undefined): void {
	events?.emit(HOST_QUERY_EVENT, {});
}

export function hostShowsPath(): boolean {
	return [...hosts().values()].some((h) => h.showsPath === true);
}

/** Forget every host: for tests. */
export function forgetHosts(): void {
	hosts().clear();
}
