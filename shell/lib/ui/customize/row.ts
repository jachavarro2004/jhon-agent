/**
 * shell/lib/ui/customize/row.ts — what a Customize row declares (T15.15,
 * L179). Each category file (appearance.ts, behaviour.ts…) exports one
 * category of these; registry.ts collects them in order and turns them into
 * the overlay's rows (settings-view.ts SettingRow). A row saves its settings
 * key with `jhon settings set`, or through its own handler (the writer that
 * already owns the key, or a `jhon` command such as the mode's changeMode).
 */
import type { SettingsKey, SettingValue, ShellSettings } from "../../settings.ts";
import type { AnimationPolicy } from "../appearance-policy.ts";
import type { DoubleEscCancelPolicy } from "../double-esc-cancel-policy.ts";
import type { SettingRow } from "../settings-view.ts";

/** What the shell gives the rows: the settings and the hooks that apply a change live. */
export interface CustomizeHooks {
	settings: ShellSettings;
	/** Apply live; each runs after its value was saved. */
	animations(policy: AnimationPolicy): void;
	doubleEsc(policy: DoubleEscCancelPolicy): void;
	status(): void;
	/** The card style or the surface changed: apply both and redraw. */
	appearance?(): void;
	redraw(): void;
	/** The project's permission mode, read and changed through changeMode, when the shell can run `jhon mode`. */
	mode?: { value(): string; set(mode: string): Promise<string | undefined> };
	/** pi's reload (extensions, keys, themes), when the shell can ask for it. */
	reload?(): void;
	/** pi's themes (ctx.ui.getAllThemes), the active one's name, and pi's setTheme: undefined when set, else why not. */
	themes?: { list(): { name: string; path: string | undefined }[]; current(): string | undefined; set(name: string): string | undefined };
}

export type Preview = (value: string, ascii: boolean, h: CustomizeHooks) => ReturnType<NonNullable<SettingRow["preview"]>>;
export type SaveResult = string | undefined | Promise<string | undefined>;

/** One row of Customize: the setting it saves, how it is chosen, shown and applied. */
export type CustomizeRow = {
	/** The settings key it saves (the parity test holds a shell.* key to the engine's schema), or a handler row's name. */
	key: string;
	label: string;
	/** What the setting does; a key that only applies later says so here. */
	note?: string;
	/** A choice steps through `values` with ←/→; a text row is typed in place; enter runs an action's `save` (Reset, Reload now). */
	kind: "choice" | "text" | "action";
	values?: readonly string[];
	/** The values when they are only known in the session (the themes pi lists); wins over `values`. */
	choices?(h: CustomizeHooks): readonly string[];
	/** A value's name as shown (the value is kept). */
	display?(value: string, h: CustomizeHooks): string;
	/** The Preview block's sample for a value. */
	preview?: Preview;
	/** The current value, as the row shows it. */
	read(h: CustomizeHooks): string;
	/** The value as `jhon settings set` stores it; the string itself by default. */
	encode?(value: string): SettingValue;
	/** Saves through this handler instead of `jhon settings set <key>`: undefined when done, else why not. */
	save?(h: CustomizeHooks, value: string): SaveResult;
	/** Applies the saved value now; a row without it applies at the next start (unless its handler does). */
	live?(h: CustomizeHooks): void;
	/** ←/→ only hold a pending choice and enter applies it (the permission mode); an action's first enter only holds it. */
	confirm?: boolean;
	/** Hidden when it does not apply here. */
	hidden?(h: CustomizeHooks): boolean;
};

export interface CustomizeCategory {
	/** The /jhon:customize word. */
	id: string;
	/** The overlay's category title. */
	title: string;
	/** One line on what it holds: /jhon:customize's completion for it (L194). */
	summary: string;
	rows: readonly CustomizeRow[];
}

export const ON_OFF = ["on", "off"] as const;
export const NEXT_START = "Applies at the next start; off turns it off.";

/** A failed write's message, or undefined when it saved. */
export function attempt(run: () => void): string | undefined {
	try {
		run();
		return undefined;
	} catch (error) {
		return error instanceof Error ? error.message : String(error);
	}
}

/** `jhon settings set <key> <value>`, then `then`: undefined when saved, else the refusal. */
export function saveSetting(h: CustomizeHooks, key: string, value: SettingValue, then?: () => void): string | undefined {
	return attempt(() => {
		const result = h.settings.set(key, value);
		if (!result.ok) throw new Error(result.error);
		then?.();
	});
}

/** An on/off row over a bool key, saved with `jhon settings set`. */
export function boolRow(key: SettingsKey, label: string, note?: string, extra: Partial<CustomizeRow> = {}): CustomizeRow {
	return {
		key, label, note, kind: "choice", values: ON_OFF,
		read: (h) => (h.settings.bool(key) ? "on" : "off"),
		encode: (v) => v === "on",
		...extra,
	};
}

/** A typed row over a key, saved with `jhon settings set`. */
export function textRow(key: SettingsKey, label: string, note: string, extra: Partial<CustomizeRow> = {}): CustomizeRow {
	return { key, label, note, kind: "text", read: (h) => String(h.settings.get(key)), ...extra };
}

/** The overlay's row for a declared one. */
export function toSettingRow(category: CustomizeCategory, row: CustomizeRow, h: CustomizeHooks): SettingRow {
	return {
		group: category.title, key: row.key, label: row.label, note: row.note,
		options: row.choices ? row.choices(h) : row.values, display: row.display && ((v) => row.display!(v, h)), preview: row.preview && ((v, ascii) => row.preview!(v, ascii, h)), confirm: row.confirm, action: row.kind === "action" || undefined,
		value: () => row.read(h),
		apply: (v) => row.save
			? row.save(h, v)
			: saveSetting(h, row.key, row.encode ? row.encode(v) : v, row.live ? () => row.live!(h) : undefined),
	};
}
