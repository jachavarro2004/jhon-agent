import type { CommandPaletteGroup, CommandPaletteItem } from "./command-palette.ts";

// The curated command set for the command palette, in the Gentle Shell's
// groups (Configuration, Session, Diagnostics) with G's labels where G has the
// command: a grouped menu, not a raw listing of every registered command.
// Only command + label live here; the live description and any shortcut
// hint are attached at build time by buildCommandPaletteGroups, from the
// actual registration and the shell's configured shortcuts. A command may
// carry its argument (`jhon:profile list`): the row runs it as typed, and its
// registration is the first word's.

export interface CommandPaletteCatalogItem {
	command: string;
	label: string;
}

export interface CommandPaletteCatalogGroup {
	title: string;
	items: readonly CommandPaletteCatalogItem[];
}

export const COMMAND_PALETTE_CATALOG: readonly CommandPaletteCatalogGroup[] = [
	{
		title: "Configuration",
		items: [
			{ command: "jhon:customize", label: "Customize" },
			{ command: "jhon:profile", label: "Models and profiles" },
		],
	},
	{
		title: "Session",
		items: [
			{ command: "jhon:grants", label: "Grants" },
			{ command: "jhon:changes", label: "Browse changes" },
			{ command: "jhon:agents", label: "Agents" },
			{ command: "jhon:jobs", label: "Jobs" },
			{ command: "jhon:usage", label: "Subscription usage" },
			{ command: "jhon:panel", label: "Panel" },
		],
	},
	{
		title: "Diagnostics",
		items: [
			{ command: "jhon:status", label: "LDD status" },
			{ command: "jhon:dashboard", label: "Dashboard" },
		],
	},
];

/**
 * Build the palette's groups for one session: keep only catalog entries
 * whose command is actually registered (so a missing extension never shows
 * a dead row), attach that registration's live description and an optional
 * shortcut hint, and drop any group left with no items. Catalog order is
 * preserved throughout.
 */
export function buildCommandPaletteGroups(registered: readonly { name: string; description?: string }[], shortcuts: Readonly<Record<string, string | undefined>>): CommandPaletteGroup[] {
	const byName = new Map(registered.map((command) => [command.name, command]));
	const groups: CommandPaletteGroup[] = [];
	for (const group of COMMAND_PALETTE_CATALOG) {
		const items: CommandPaletteItem[] = [];
		for (const entry of group.items) {
			const found = byName.get(entry.command.split(" ")[0]!);
			if (!found) continue;
			items.push({ command: entry.command, label: entry.label, description: found.description, shortcut: shortcuts[entry.command] });
		}
		if (items.length > 0) groups.push({ title: group.title, items });
	}
	return groups;
}
