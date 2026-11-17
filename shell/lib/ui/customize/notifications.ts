/**
 * shell/lib/ui/customize/notifications.ts — Customize's Notifications: the
 * sounds switch and one sound per event that fires (T15.15). Both save
 * through the notification service's own writer when audio runs in this
 * session, so it applies at once.
 */
import { notificationEventKey, type SettingsKey } from "../../settings.ts";
import { SETTING_EVENTS, type NotificationSound } from "../notification-policy.ts";
import { boolRow, saveSetting, type CustomizeCategory, type CustomizeRow } from "./row.ts";

const SOUNDS = ["none", "builtin:success", "builtin:error", "builtin:attention"] as const;
const soundName = (sound: string) => sound.replace(/^builtin:/, "");
const UNSAVED = "The notification settings could not be saved.";

function soundRow(event: (typeof SETTING_EVENTS)[number]): CustomizeRow {
	const key = notificationEventKey(event) as SettingsKey;
	return {
		key, label: `Sound · ${event}`, kind: "choice", values: SOUNDS, display: soundName,
		preview: (v) => ({ caption: "the sound", lines: [v === "none" ? `No sound on ${event}.` : `${soundName(v)} plays on ${event}.`] }),
		read: (h) => String(h.settings.get(key)),
		save: (h, v) => {
			const service = h.notifications;
			if (!service) return saveSetting(h, key, v);
			const next = structuredClone(service.get());
			next.audio.events[event] = (v === "none" ? null : v) as NotificationSound;
			return service.set(next) ? undefined : UNSAVED;
		},
	};
}

export const NOTIFICATIONS: CustomizeCategory = {
	id: "notifications", title: "Notifications", summary: "Sounds on or off, and the sound of each event",
	rows: [
		boolRow("shell.notifications.enabled", "Sounds", "Audio for the events below.", {
			preview: (v) => ({ caption: "what happens", lines: [v === "on" ? "Each event below plays its sound." : "Silent: no event plays a sound."] }),
			save: (h, v) => {
				const service = h.notifications;
				if (!service) return saveSetting(h, "shell.notifications.enabled", v === "on");
				return service.set({ ...structuredClone(service.get()), enabled: v === "on" }) ? undefined : UNSAVED;
			},
		}),
		...SETTING_EVENTS.map(soundRow),
	],
};
