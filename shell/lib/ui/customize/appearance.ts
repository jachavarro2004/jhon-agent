/**
 * shell/lib/ui/customize/appearance.ts — Customize's Appearance: the pi
 * theme (themes.ts), the animations, the glyphs, the tool icons, G's card style, surface and
 * density, and the status line (T15.15: one file per category, registry.ts in order).
 */
import { SETTINGS_DEFAULTS } from "../../settings.ts";
import { writeAnimationPolicy, type AnimationPolicy } from "../animation-policy.ts";
import { TOOL_ICONS, type ToolFamily } from "../primitives/index.ts";
import { writeVisualSettings } from "../visual-customization-policy.ts";
import { attempt, boolRow, type CustomizeCategory, type CustomizeHooks } from "./row.ts";
import { jhonThemes, themePreview } from "./themes.ts";

const ICON_FAMILIES: ToolFamily[] = ["read", "search", "web", "shell", "edit"];
/** The card style or the surface changed: apply both, or at least redraw. */
const appearance = (h: CustomizeHooks) => (h.appearance ?? h.redraw)();
/** Every Appearance key and its default: what Reset writes. */
const APPEARANCE_DEFAULTS = Object.entries(SETTINGS_DEFAULTS).filter(([key]) => key.startsWith("shell.appearance."));
const themes = (h: CustomizeHooks) => (h.themes ? jhonThemes(h.themes.list()) : []);

export const APPEARANCE: CustomizeCategory = {
	id: "appearance", title: "Appearance", summary: "Theme, animations, glyphs, tool icons, cards, surface, density, status line, reset",
	rows: [
		{
			// G's Themes (L197): pi's setTheme applies it now and pi saves it.
			key: "theme", label: "Theme", kind: "choice",
			note: "The pi theme: rose ink, or one of shell/themes/. Pi applies and saves it.",
			choices: themes,
			display: (v, h) => (h.themes?.current() === v ? `${v} (current)` : v),
			preview: (v, _ascii, h) => themePreview(h.themes?.list().find((t) => t.name === v)?.path),
			read: (h) => h.themes?.current() ?? "",
			save: (h, v) => h.themes!.set(v),
			hidden: (h) => themes(h).length === 0,
		},
		{
			key: "shell.appearance.animations", label: "Animations", kind: "choice", values: ["full", "off"],
			note: "The prompt's petal and working line: animated, or still.",
			// Static samples (T15.10), so the overlay never animates.
			preview: (v, ascii) => ({
				caption: "static animation sample",
				lines: v === "off"
					? [ascii ? "*  Working...   still" : "力  Working…   still"]
					: [ascii ? "*  Working...   the petal glows, the line shimmers" : "力  Working…   the petal glows, the line shimmers"],
			}),
			read: (h) => h.settings.animations(),
			save: (h, v) => attempt(() => {
				const policy: AnimationPolicy = v === "off" ? "potato" : "quality";
				writeAnimationPolicy(policy, { settings: h.settings });
				h.animations(policy);
			}),
		},
		boolRow("shell.appearance.ascii", "ASCII glyphs", "Plain letters for every glyph: frames, states, icons.", {
			preview: (v) => ({ caption: "the glyph set", lines: [v === "on" ? "+-+ > v #- ~ .   r s w $ e" : "╭─╮ ▸ ▾ ▰▱ … ·   ▤ ⌕ ◍ $ ✎"] }),
			live: (h) => h.redraw(),
		}),
		{
			key: "shell.appearance.icons", label: "Tool icons", kind: "choice", values: ["geometric", "emoji"],
			note: "Each tool call's family icon; ASCII glyphs win over both.",
			preview: (v, ascii) => ({
				caption: "the tool icons",
				lines: [ICON_FAMILIES.map((f) => `${ascii ? TOOL_ICONS[f].ascii : TOOL_ICONS[f][v === "emoji" ? "emoji" : "geometric"]} ${f}`).join("  ")],
			}),
			read: (h) => h.settings.icons(),
			live: (h) => h.redraw(),
		},
		{
			key: "shell.appearance.cards", label: "Cards", kind: "choice", values: ["float", "neon"],
			note: "Notices and tool cards: a ▎ bar on a tint, or the rounded outline.",
			// G's two card styles, drawn as a notice and a tool card are (L153 answer 1).
			preview: (v, ascii) => ({
				caption: "a notice and a tool card",
				lines: v === "neon"
					? ascii
						? ["+- ~ Background job - npm test -----+", "| done in 3s                         |", "+-----------------------------------+"]
						: ["╭─ ⧗ Background job · npm test ─────╮", "│ done in 3s                        │", "╰───────────────────────────────────╯"]
					: ascii
						? [" | ~ Background job - npm test", " | done in 3s"]
						: [" ▎ ⧗ Background job · npm test", " ▎ done in 3s"],
			}),
			read: (h) => h.settings.cards(),
			live: appearance,
		},
		{
			key: "shell.appearance.surface", label: "Surface", kind: "choice", values: ["transparent", "tinted"],
			note: "The rail, the top row and the status bar: on the terminal's background, or one painted panel.",
			// Two surfaces (L153 answer 2): the right pane's sections on the terminal's background (design 01), or one painted panel.
			preview: (v, ascii) => ({
				caption: "the rail",
				lines: v === "tinted"
					? [ascii ? "one painted panel: v TODOS ... 2/5" : "one painted panel: ▾ TODOS … 2/5", "every section's header and rows on it"]
					: ["each section's header and rows,", "on the terminal's own background"],
			}),
			read: (h) => h.settings.surface(),
			live: appearance,
		},
		{
			key: "shell.appearance.density", label: "Density", kind: "choice", values: ["comfortable", "compact", "minimal"],
			note: "The pane's two forms: comfortable draws each of its sections in its own box; compact draws them flat, a blank row apart; minimal draws none between them. The top row and the status bar are one row each at every density. Minimal also keeps tool cards quiet: bash collapses to ↳ ✓ N lines instead of its output tail and duration.",
			// The right pane's two forms (L1495, T174-S5b); the bars have no margin rows (T174-S3, design/shell-tui 01).
			preview: (v, ascii) => ({
				caption: "the right pane",
				lines: v === "comfortable"
					? (ascii ? ["+--------+", "| Agents |", "+--------+"] : ["╭────────╮", "│ Agents │", "╰────────╯"])
					: v === "compact"
						? ["Last run", "a blank row", "Agents"]
						: ["Last run", "Agents", "the sections one under another"],
			}),
			read: (h) => h.settings.density(),
			live: appearance,
		},
		{
			key: "shell.appearance.status", label: "Status line", kind: "choice", values: ["auto", "hidden"],
			note: "The status bar and the rail's Status sections, where they fit; or none.",
			preview: (v, ascii) => ({
				caption: "a status line sample",
				lines: [v === "hidden" ? "hidden: no status bar and no Status sections." : ascii ? "main | YOLO   opus-5-5 - high | ctx ###----- 41% | $0.120 | 5h 84%" : "main │ YOLO   opus-5-5 · high │ ctx ▰▰▰▱▱▱▱▱ 41% │ $0.120 │ 5h 84%"],
			}),
			read: (h) => h.settings.status(),
			save: (h, v) => attempt(() => {
				writeVisualSettings({ statusPlacement: v === "hidden" ? "hidden" : "auto" }, { settings: h.settings });
				h.status();
			}),
		},
		{
			// G's Reset (L198): the shell.appearance.* defaults, and nothing else (the theme is pi's).
			key: "reset-appearance", label: "Reset", kind: "action", confirm: true,
			note: "Every Appearance setting above back to its default; the theme stays.",
			preview: () => ({ caption: "the defaults", lines: [APPEARANCE_DEFAULTS.map(([key, value]) => `${key.slice("shell.appearance.".length)} ${value === false ? "off" : value}`).join(" · ")] }),
			read: () => "to defaults",
			save: (h) => attempt(() => {
				const result = h.settings.setMany(APPEARANCE_DEFAULTS);
				if (!result.ok) throw new Error(result.error);
				h.animations(h.settings.animations() === "off" ? "potato" : "quality");
				appearance(h);
				h.status();
			}),
		},
	],
};
