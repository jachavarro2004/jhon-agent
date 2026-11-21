/**
 * shell/lib/ui/customize/themes.ts — the pi themes Jhon registers, for
 * Appearance's Theme row (T15.18, L197): rose ink, which `jhon palette`
 * writes into the agent dir, and every JSON in shell/themes/, which
 * package.json's pi.themes hands to pi. Adding a theme is one file there.
 * The preview reads a theme's own file, as G's sourcePalettePreview: its
 * accent as a swatch and its text.
 */
import { readFileSync, statSync } from "node:fs";
import { dirname, join, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

/** shell/themes/: the themes the shell's package registers with pi. */
export const PACKAGED_THEMES = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "..", "themes");
/** The theme `jhon palette` writes and the render sets (engine/internal/palette ThemeName). */
export const ROSE_INK = "rose-ink";

/** What a theme file may hold: at most 250 KB (256,000 bytes), colours as `#rrggbb`, and names of its own vars to follow. */
const THEME_LIMITS = { bytes: 256_000, colour: /^#[0-9a-fA-F]{6}$/, varName: /^[a-zA-Z][a-zA-Z0-9_-]*$/ } as const;

/** Of pi's themes, Jhon's names: rose ink first, then the packaged ones by name; each once. */
export function jhonThemes(all: readonly { name: string; path: string | undefined }[]): string[] {
	const packaged = resolve(PACKAGED_THEMES) + sep;
	const names = new Set(all.filter((t) => t.name !== ROSE_INK && t.path && resolve(t.path).startsWith(packaged)).map((t) => t.name));
	return [...(all.some((t) => t.name === ROSE_INK) ? [ROSE_INK] : []), ...[...names].sort()];
}

/** A theme file's accent and text, as a swatch line and a line of values. */
export function themePreview(path: string | undefined): { caption: string; lines: string[] } {
	if (!path) throw new Error("The theme has no file to preview.");
	if (statSync(path).size > THEME_LIMITS.bytes) throw new Error("Theme file too large.");
	const document = JSON.parse(readFileSync(path, "utf8")) as { colors?: Record<string, unknown>; vars?: Record<string, unknown> };
	const colors = document.colors ?? {};
	const vars = document.vars ?? {};
	const color = (role: "accent" | "text"): string | number => {
		let value = colors[role];
		const seen = new Set<string>();
		while (typeof value === "string" && THEME_LIMITS.varName.test(value) && !seen.has(value)) {
			seen.add(value);
			value = vars[value];
		}
		if ((typeof value === "string" && THEME_LIMITS.colour.test(value)) || (typeof value === "number" && Number.isInteger(value) && value >= 0 && value <= 255)) return value;
		throw new Error(`The theme's ${role} is not a color.`);
	};
	const sgr = (value: string | number, background: boolean) => {
		const prefix = background ? 48 : 38;
		if (typeof value === "number") return `\x1b[${prefix};5;${value}m`;
		return `\x1b[${prefix};2;${parseInt(value.slice(1, 3), 16)};${parseInt(value.slice(3, 5), 16)};${parseInt(value.slice(5, 7), 16)}m`;
	};
	const accent = color("accent");
	const text = color("text");
	return {
		caption: "accent and text",
		lines: [`${sgr(accent, true)}  \x1b[0m ${sgr(text, false)}Aa  sample text\x1b[0m`, `accent ${String(accent).toLowerCase()} · text ${String(text).toLowerCase()}`],
	};
}
