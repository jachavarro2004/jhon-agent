import { posix, win32 } from "node:path";
import { NOTIFICATION_EVENTS, notificationEventKey, shellSettings, type NotificationEvent, type SettingValue, type ShellSettings } from "../settings.ts";

export { NOTIFICATION_EVENTS, type NotificationEvent };

// The notification settings are [shell.notifications] in the owner's
// settings (lib/settings.ts): enabled and one sound per event that fires
// ("none" there is null here). The backend and the two timings are fixed
// (T15.3). The context has no sound: its one notice is the fresh-session one.
/** Each event's sound key, in the settings' order: every event that fires has one. */
export const SETTING_EVENTS: readonly NotificationEvent[] = NOTIFICATION_EVENTS;
export const BUILTIN_NOTIFICATION_IDS = ["success", "error", "attention"] as const;
export type NotificationSound = `builtin:${typeof BUILTIN_NOTIFICATION_IDS[number]}` | `file:${string}` | null;
/** Priority belongs to the event, never the selected sound. */
export const NOTIFICATION_PRIORITY: Readonly<Record<NotificationEvent, number>> = Object.fromEntries(
	NOTIFICATION_EVENTS.map(event => [event, event.endsWith(".failed") ? 3
		: event === "agent.attention" ? 2 : event.endsWith(".completed") ? 1 : 0]),
) as Record<NotificationEvent, number>;
/** Integer milliseconds, inclusive bounds. Zero permits disabling a timing window. TTL belongs to scheduler. */
export const NOTIFICATION_TIMING_LIMITS = {
	minimumIntervalMs: { min: 0, max: 60000 },
	coalesceWindowMs: { min: 0, max: 2000 },
} as const;
export interface NotificationSettings {
	enabled: boolean;
	audio: {
		backend: "auto";
		minimumIntervalMs: number;
		coalesceWindowMs: number;
		events: Partial<Record<NotificationEvent, NotificationSound>>;
	};
}
export const DEFAULT_NOTIFICATION_SETTINGS: NotificationSettings = Object.freeze({
	enabled: false,
	audio: Object.freeze({
		backend: "auto" as const, minimumIntervalMs: 1000, coalesceWindowMs: 300,
		events: Object.freeze(Object.fromEntries(NOTIFICATION_EVENTS.map(event => [event,
			NOTIFICATION_PRIORITY[event] === 3 ? "builtin:error" : NOTIFICATION_PRIORITY[event] === 2 ? "builtin:attention"
				: NOTIFICATION_PRIORITY[event] === 1 ? "builtin:success" : null]))),
	}),
});
export type NotificationPathFlavor = "posix" | "win32";
export interface NotificationOptions {
	settings?: ShellSettings;
	pathFlavor?: NotificationPathFlavor;
}
export interface NotificationResolution {
	settings: NotificationSettings;
	source: "global_file" | "default";
	malformed: boolean;
	readError: boolean;
	globalFile: string;
}
const nativeFlavor: NotificationPathFlavor = process.platform === "win32" ? "win32" : "posix";
function record(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
function keysMatch(value: Record<string, unknown>, keys: readonly string[]): boolean {
	return Object.keys(value).length === keys.length && keys.every(key => Object.hasOwn(value, key));
}
/** Syntax validation only: no stat, WAV parsing or backend detection. No expansion is ever performed. */
export function isNotificationSound(value: unknown, flavor: NotificationPathFlavor = nativeFlavor): value is NotificationSound {
	if (value === null) return true;
	if (typeof value !== "string") return false;
	if (BUILTIN_NOTIFICATION_IDS.some(id => value === `builtin:${id}`)) return true;
	if (!value.startsWith("file:")) return false;
	const path = value.slice(5);
	if (!path || /[\x00-\x1f\x7f$`]|%[^%]+%/.test(path) || path.includes("://")) return false;
	// No network URLs/UNC/device paths, drive-relative paths or shell-home aliases.
	return flavor === "win32"
		? /^[A-Za-z]:[\\/]/.test(path) && win32.isAbsolute(path) && !path.slice(2).includes(":")
		: posix.isAbsolute(path) && !path.startsWith("//");
}
export function isNotificationSettings(value: unknown, flavor: NotificationPathFlavor = nativeFlavor): value is NotificationSettings {
	if (!record(value) || !keysMatch(value, ["enabled", "audio"]) || typeof value.enabled !== "boolean") return false;
	const audio = value.audio;
	if (!record(audio) || !keysMatch(audio, ["backend", "minimumIntervalMs", "coalesceWindowMs", "events"]) || audio.backend !== "auto") return false;
	for (const key of ["minimumIntervalMs", "coalesceWindowMs"] as const) {
		const timing = audio[key]; const { min, max } = NOTIFICATION_TIMING_LIMITS[key];
		if (typeof timing !== "number" || !Number.isInteger(timing) || timing < min || timing > max) return false;
	}
	return record(audio.events) && Object.entries(audio.events).every(([event, sound]) =>
		NOTIFICATION_EVENTS.includes(event as NotificationEvent) && isNotificationSound(sound, flavor));
}
export function notificationSoundFor(settings: NotificationSettings, event: NotificationEvent): NotificationSound {
	return settings.audio.events[event] ?? null;
}
/** Under a host that alerts (jhon:host alerts: true) the owner-wait alert and its sound are the host's: the shell plays none of its own for it, so the owner hears one alert. */
export function heardFromHost(event: NotificationEvent, hostAlerts: boolean): boolean {
	return event === "agent.attention" && hostAlerts;
}
/** Explicit user action only; enabled is preserved, even when currently disabled. */
export function restoreNotificationPreset(settings: NotificationSettings): NotificationSettings {
	return { ...structuredClone(DEFAULT_NOTIFICATION_SETTINGS), enabled: settings.enabled };
}
const KEYS = { enabled: "shell.notifications.enabled" } as const;
export function resolveNotificationSettings(options: NotificationOptions = {}): NotificationResolution {
	const store = options.settings ?? shellSettings();
	const sound = (event: NotificationEvent) => { const v = store.get(notificationEventKey(event)); return v === "none" ? null : v; };
	const { backend, minimumIntervalMs, coalesceWindowMs } = DEFAULT_NOTIFICATION_SETTINGS.audio;
	const candidate = {
		enabled: store.get(KEYS.enabled),
		audio: {
			backend, minimumIntervalMs, coalesceWindowMs,
			events: Object.fromEntries(NOTIFICATION_EVENTS.map(event => [event, SETTING_EVENTS.includes(event) ? sound(event) : null])),
		},
	};
	const keys = [...Object.values(KEYS), ...SETTING_EVENTS.map(notificationEventKey)];
	const source = keys.some(key => store.source(key) !== "default") ? "global_file" : "default";
	if (!isNotificationSettings(candidate, options.pathFlavor)) {
		return { settings: structuredClone(DEFAULT_NOTIFICATION_SETTINGS), source, malformed: true, readError: false, globalFile: store.where };
	}
	return { settings: candidate, source, malformed: false, readError: store.unreadable, globalFile: store.where };
}
/** Explicit persistence operation, with `jhon settings set`; caller owns confirmation before replacing malformed/unreadable config. */
export function writeNotificationSettings(settings: NotificationSettings, options: NotificationOptions = {}): string {
	if (!isNotificationSettings(settings, options.pathFlavor)) throw new TypeError("Invalid notification settings");
	const store = options.settings ?? shellSettings();
	const entries: [string, SettingValue][] = [
		[KEYS.enabled, settings.enabled],
		// An event the settings leave out stays silent, as it always has.
		...SETTING_EVENTS.map((event): [string, SettingValue] => [notificationEventKey(event), settings.audio.events[event] ?? "none"]),
	];
	const result = store.setMany(entries);
	if (!result.ok) throw new Error(result.error);
	return store.path;
}
