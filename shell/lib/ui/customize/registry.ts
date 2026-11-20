/**
 * shell/lib/ui/customize/registry.ts — Customize's one registry (T15.15,
 * L179): its categories in order, one file each. The overlay's rows, the
 * /jhon:customize words, its description and its notice for an unknown word
 * all come from here. To add a row: add it to its category's file; a new
 * key also needs its default in SETTINGS_DEFAULTS (lib/settings.ts) and one
 * schema line in engine/internal/settings (shell/README.md, "Adding to
 * Customize"). To add a category: one file here and one entry below.
 */
import type { SettingRow } from "../settings-view.ts";
import { APPEARANCE } from "./appearance.ts";
import { BEHAVIOUR } from "./behaviour.ts";
import { KEYS } from "./keys.ts";
import { PERMISSIONS } from "./permissions.ts";
import { toSettingRow, type CustomizeCategory, type CustomizeHooks } from "./row.ts";
import type { Completion } from "../jhon-commands.ts";

export type { CustomizeCategory, CustomizeHooks, CustomizeRow } from "./row.ts";

/** The categories, in the order the overlay lists them. G keeps its permissions among its Customization rows (L175). */
export const CUSTOMIZE_CATEGORIES: readonly CustomizeCategory[] = Object.freeze([APPEARANCE, BEHAVIOUR, KEYS, PERMISSIONS]);

/** The words /jhon:customize <category> takes. */
export const CUSTOMIZE_WORDS: readonly string[] = Object.freeze(CUSTOMIZE_CATEGORIES.map((c) => c.id));

/** /jhon:customize's description: one short line in pi's `/` list (L194). */
export function customizeDescription(): string {
	return `Customize the shell: ${CUSTOMIZE_WORDS.join(", ")}`;
}

/** /jhon:customize's values: each category with what it holds. */
export function customizeCompletions(): Completion[] {
	return CUSTOMIZE_CATEGORIES.map((c) => ({ value: c.id, label: c.id, description: c.summary }));
}

/** The notice for a word that names no category. */
export function unknownCategoryNotice(word: string): string {
	return `No Customize group "${word}": ${CUSTOMIZE_WORDS.join(", ")}.`;
}

/** The overlay's rows, category by category; a hidden row is left out, and a category with none with it. */
export function customizeRows(h: CustomizeHooks): SettingRow[] {
	return CUSTOMIZE_CATEGORIES.flatMap((category) => category.rows.filter((row) => !row.hidden?.(h)).map((row) => toSettingRow(category, row, h)));
}
