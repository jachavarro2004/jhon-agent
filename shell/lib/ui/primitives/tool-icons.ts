/**
 * shell/lib/ui/primitives/tool-icons.ts — the tool icons (design/system.md
 * "Glyph table", the owner's L65 choice): a geometric family by default, one
 * cell each; emoji as an option (two cells, measured with visibleWidth); an
 * ASCII letter that wins over both.
 */

export const TOOL_FAMILY = {
	MEMORY: "memory",
	SEARCH: "search",
	WEB: "web",
	SHELL: "shell",
	EDIT: "edit",
	READ: "read",
	AGENTS: "agents",
	ASK: "ask",
	OTHER: "other",
} as const;

export type ToolFamily = (typeof TOOL_FAMILY)[keyof typeof TOOL_FAMILY];

export type IconStyle = "geometric" | "emoji";

export const TOOL_ICONS: Record<ToolFamily, { geometric: string; emoji: string; ascii: string }> = {
	read: { geometric: "▤", emoji: "📄", ascii: "r" },
	search: { geometric: "⌕", emoji: "🔎", ascii: "s" },
	web: { geometric: "◍", emoji: "🌐", ascii: "w" },
	shell: { geometric: "$", emoji: "$", ascii: "$" },
	edit: { geometric: "✎", emoji: "✎", ascii: "e" },
	memory: { geometric: "▢", emoji: "🧠", ascii: "m" },
	agents: { geometric: "◇", emoji: "🤖", ascii: "a" },
	ask: { geometric: "?", emoji: "❓", ascii: "?" },
	other: { geometric: "·", emoji: "·", ascii: "-" },
};

export function toolGlyph(family: ToolFamily, { ascii = false, style = "geometric" }: { ascii?: boolean; style?: IconStyle } = {}): string {
	const icon = TOOL_ICONS[family];
	return ascii ? icon.ascii : icon[style];
}
