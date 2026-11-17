import assert from "node:assert/strict";
import { join } from "node:path";
import { test } from "node:test";
import { notificationEventKey } from "../lib/settings.ts";
import {
	DEFAULT_NOTIFICATION_SETTINGS, NOTIFICATION_EVENTS, NOTIFICATION_PRIORITY, SETTING_EVENTS,
	isNotificationSettings, isNotificationSound,
	resolveNotificationSettings, restoreNotificationPreset, notificationSoundFor,
	writeNotificationSettings, type NotificationSettings,
} from "../lib/ui/notification-policy.ts";
import { fakeSettings, memorySettings } from "./fake-jhon-settings.ts";

/** Settings saved through jhon settings and read back. */
function roundTrip(settings: NotificationSettings): NotificationSettings {
	const store = memorySettings();
	writeNotificationSettings(settings, { settings: store });
	return resolveNotificationSettings({ settings: store }).settings;
}

test("preset cannot be mutated globally at any depth", () => {
	assert.throws(() => { DEFAULT_NOTIFICATION_SETTINGS.enabled = true; }, TypeError);
	assert.throws(() => { DEFAULT_NOTIFICATION_SETTINGS.audio.minimumIntervalMs = 42; }, TypeError);
	assert.throws(() => { DEFAULT_NOTIFICATION_SETTINGS.audio.events["agent.completed"] = null; }, TypeError);
	assert.equal(restoreNotificationPreset(DEFAULT_NOTIFICATION_SETTINGS).audio.events["agent.completed"], "builtin:success");
});

test("missing configuration is off, independent and has no write side effects", (t) => {
	const fake = fakeSettings(t);
	const result = resolveNotificationSettings({ settings: fake.settings });
	assert.equal(result.globalFile, join(fake.dir, "settings.toml"));
	assert.equal(result.source, "default");
	assert.equal(result.malformed, false); assert.equal(result.readError, false);
	assert.equal(result.settings.enabled, false);
	assert.equal(result.settings.audio.minimumIntervalMs, 1000);
	assert.equal(result.settings.audio.coalesceWindowMs, 300);
	assert.deepEqual(result.settings, DEFAULT_NOTIFICATION_SETTINGS);
	result.settings.audio.events["agent.completed"] = null;
	assert.equal(resolveNotificationSettings({ settings: fake.settings }).settings.audio.events["agent.completed"], "builtin:success");
	assert.deepEqual(fake.calls(), [["settings", "list", "--json"]]);
});

test("strict schema rejects unknown keys, bad values and bounded noninteger timings", () => {
	const valid = structuredClone(DEFAULT_NOTIFICATION_SETTINGS);
	assert.equal(isNotificationSettings(valid), true);
	assert.deepEqual(roundTrip(valid), valid);
	const invalid = [null, [], { ...valid, extra: 1 }, { ...valid, enabled: "true" },
		{ ...valid, audio: { ...valid.audio, backend: "shell" } },
		{ ...valid, audio: { ...valid.audio, events: { "agent.unknown": null } } },
		{ ...valid, audio: { ...valid.audio, events: { "agent.started": undefined } } },
	];
	for (const value of invalid) assert.equal(isNotificationSettings(value), false);
	for (const key of ["minimumIntervalMs", "coalesceWindowMs"]) {
		for (const value of [-1, 0.5, Infinity, 60001, "300"])
			assert.equal(isNotificationSettings({ ...valid, audio: { ...valid.audio, [key]: value } }), false);
	}
	assert.equal(isNotificationSettings({ ...valid, schema: "future" }), false);
});

test("omission and null are silence, never preset merging or cross-event fallback", () => {
	const settings = roundTrip({ ...DEFAULT_NOTIFICATION_SETTINGS, enabled: true,
		audio: { ...DEFAULT_NOTIFICATION_SETTINGS.audio, events: { "agent.failed": "builtin:success", "subagent.failed": null } } });
	assert.equal(notificationSoundFor(settings, "agent.failed"), "builtin:success");
	for (const event of NOTIFICATION_EVENTS.filter(event => event !== "agent.failed"))
		assert.equal(notificationSoundFor(settings, event), null);
	assert.equal(NOTIFICATION_PRIORITY["agent.failed"], 3);
	assert.equal(NOTIFICATION_PRIORITY["agent.attention"], 2);
	assert.equal(NOTIFICATION_PRIORITY["agent.completed"], 1);
	assert.equal(NOTIFICATION_PRIORITY["subagent.completed"], 1);
	assert.equal(NOTIFICATION_PRIORITY["session.started"], 0);
});

test("every event that fires is independent, and the timings are constants (T15.3)", () => {
	for (const event of SETTING_EVENTS) {
		const parsed = roundTrip({ ...DEFAULT_NOTIFICATION_SETTINGS, audio: { ...DEFAULT_NOTIFICATION_SETTINGS.audio, events: { [event]: "builtin:attention" } } });
		for (const other of NOTIFICATION_EVENTS)
			assert.equal(notificationSoundFor(parsed, other), other === event ? "builtin:attention" : null);
	}
	const timed = { ...DEFAULT_NOTIFICATION_SETTINGS, audio: { ...DEFAULT_NOTIFICATION_SETTINGS.audio, minimumIntervalMs: 0, coalesceWindowMs: 0 } };
	assert.deepEqual(roundTrip(timed).audio, DEFAULT_NOTIFICATION_SETTINGS.audio, "no setting holds the timings");
	for (const event of NOTIFICATION_EVENTS.filter((e) => !SETTING_EVENTS.includes(e))) assert.equal(DEFAULT_NOTIFICATION_SETTINGS.audio.events[event], null, `${event} never fires`);
	assert.equal(isNotificationSettings({ ...DEFAULT_NOTIFICATION_SETTINGS, audio: { ...DEFAULT_NOTIFICATION_SETTINGS.audio, coalesceWindowMs: 2001 } }), false);
});

test("saved settings are attributed to the settings file, and reading only reads", (t) => {
	const fake = fakeSettings(t, { file: { "shell.notifications.enabled": false } });
	const result = resolveNotificationSettings({ settings: fake.settings });
	assert.equal(result.globalFile, join(fake.dir, "settings.toml"));
	assert.equal(result.source, "global_file"); assert.equal(result.settings.enabled, false);
	assert.deepEqual(fake.calls(), [["settings", "list", "--json"]]);
});

test("sound references accept only known builtins or literal platform-absolute local paths", () => {
	for (const value of [null, "builtin:success", "builtin:error", "builtin:attention", "file:/audio/a b;[x].wav"])
		assert.equal(isNotificationSound(value, "posix"), true);
	for (const value of ["builtin:other", "file:", "file:relative.wav", "file:~/a.wav", "file:/a/$HOME.wav", "file:/a/${HOME}.wav", "file:/a/`whoami`.wav", "file:/a/\u0000.wav", "https://a.wav", "file:https://a.wav", "file://server/a.wav", "file:C:\\audio\\a.wav", 1, {}])
		assert.equal(isNotificationSound(value, "posix"), false);
	assert.equal(isNotificationSound("file:C:\\audio\\a b.wav", "win32"), true);
	for (const value of ["file:C:a.wav", "file:\\audio\\a.wav", "file:\\\\server\\share\\a.wav", "file:C:\\a\\%HOME%.wav"])
		assert.equal(isNotificationSound(value, "win32"), false);
});

test("malformed and unreadable configuration disable without overwriting", (t) => {
	const store = memorySettings({ "shell.notifications.enabled": true, [notificationEventKey("agent.failed")]: "file:relative.wav" });
	const malformed = resolveNotificationSettings({ settings: store, pathFlavor: "posix" });
	assert.equal(malformed.malformed, true); assert.equal(malformed.readError, false);
	assert.equal(malformed.source, "global_file"); assert.equal(malformed.settings.enabled, false);
	assert.equal(store.jhon.file[notificationEventKey("agent.failed")], "file:relative.wav");
	const fake = fakeSettings(t, { file: { "shell.notifications.enabled": true } });
	fake.failNext("open settings.toml: permission denied");
	fake.settings.refresh();
	const unreadable = resolveNotificationSettings({ settings: fake.settings });
	assert.equal(unreadable.readError, true); assert.equal(unreadable.malformed, false);
	assert.equal(unreadable.settings.enabled, false);
	assert.equal(fake.calls().some((call) => call[1] === "set"), false);
});

test("the writer saves every key through jhon settings set, and rejects invalid settings without a call", (t) => {
	const fake = fakeSettings(t);
	const path = writeNotificationSettings(DEFAULT_NOTIFICATION_SETTINGS, { settings: fake.settings });
	assert.equal(path, join(fake.dir, "settings.toml"));
	const sets = fake.calls().filter((call) => call[1] === "set");
	assert.equal(sets.length, 1 + SETTING_EVENTS.length);
	assert.deepEqual(sets[0], ["settings", "set", "shell.notifications.enabled", "false"]);
	assert.deepEqual(sets.at(-1), ["settings", "set", notificationEventKey("subagent.failed"), "builtin:error"]);
	assert.equal(fake.file()[notificationEventKey("session.started")], "none");
	assert.deepEqual(resolveNotificationSettings({ settings: fake.settings }).settings, DEFAULT_NOTIFICATION_SETTINGS);
	const before = fake.calls().length;
	assert.throws(() => writeNotificationSettings({ ...DEFAULT_NOTIFICATION_SETTINGS, enabled: "bad" } as never, { settings: fake.settings }), /Invalid notification settings/);
	assert.equal(fake.calls().length, before);
});

test("a refused write keeps the old values and propagates the failure", () => {
	const store = memorySettings({ "shell.notifications.enabled": true });
	store.jhon.failSet = "open settings.toml: permission denied";
	assert.throws(() => writeNotificationSettings(DEFAULT_NOTIFICATION_SETTINGS, { settings: store }), /permission denied/);
	assert.deepEqual(store.jhon.file, { "shell.notifications.enabled": true });
});

test("explicit preset restore preserves enabled both ways and returns independent settings", () => {
	for (const enabled of [false, true]) {
		const current = { ...structuredClone(DEFAULT_NOTIFICATION_SETTINGS), enabled };
		current.audio.events["agent.completed"] = null;
		const restored = restoreNotificationPreset(current);
		assert.deepEqual(restored, { ...DEFAULT_NOTIFICATION_SETTINGS, enabled });
		restored.audio.events["agent.failed"] = null;
		assert.equal(DEFAULT_NOTIFICATION_SETTINGS.audio.events["agent.failed"], "builtin:error");
		assert.equal(current.audio.events["agent.completed"], null);
	}
});
