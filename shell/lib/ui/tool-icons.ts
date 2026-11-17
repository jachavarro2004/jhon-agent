import type { ToolRenderers } from "@earendil-works/pi-coding-agent";
import { Text, truncateToWidth, visibleWidth, type Component } from "@earendil-works/pi-tui";
import { shellSettings, type ShellSettings } from "../settings.ts";
import { sanitizeTerminalText } from "./terminal-theme.ts";
import { TOOL_FAMILY, toolGlyph, type IconStyle, type ToolFamily } from "./primitives/tool-icons.ts";

// Tool-call icons (T33, L97): every call's header starts with the icon of its
// tool's family, as in the Gentle Shell beside Jhon's pi. Quiet tools draw it
// in their own card; every other tool gets it from one renderer resolver
// (extensions/quiet.ts), which also draws memory calls as one line and their
// result as `↳ ✓ <summary>`. shell.appearance.ascii (JHON_SHELL_ASCII=1)
// trades the icons for letters. The table is primitives/tool-icons.ts:
// geometric by default, emoji when shell.appearance.icons says so (T15.3).

export { TOOL_FAMILY, type ToolFamily } from "./primitives/tool-icons.ts";

const MCP_NAME = /^mcp__(.+?)__(.+)$/;
const ICON_ROLE = "accent";

/** A tool's family; an MCP tool (`mcp__<server>__<tool>`) by its server and tool. */
export function toolFamily(toolName: string): ToolFamily {
	const mcp = MCP_NAME.exec(toolName);
	const server = mcp?.[1] ?? "";
	const name = mcp?.[2] ?? toolName;
	if (server === "engram" || /^mem_/.test(name) || /engram/.test(name)) return TOOL_FAMILY.MEMORY;
	if (name === "grep" || name === "find" || server === "codegraph" || /^codegraph/.test(name)) return TOOL_FAMILY.SEARCH;
	if (name === "web_search" || name === "fetch_content") return TOOL_FAMILY.WEB;
	if (name === "bash") return TOOL_FAMILY.SHELL;
	if (name === "edit" || name === "write") return TOOL_FAMILY.EDIT;
	if (name === "read") return TOOL_FAMILY.READ;
	if (/^subagent/.test(name) || name === "agent") return TOOL_FAMILY.AGENTS;
	if (/^ask_user/.test(name)) return TOOL_FAMILY.ASK;
	return TOOL_FAMILY.OTHER;
}

/** The icon a tool's call starts with: its family's icon in shell.appearance.icons (geometric by default), or a plain letter under shell.appearance.ascii. */
export function toolIcon(toolName: string, settings: ShellSettings = shellSettings(), style: IconStyle = settings.icons()): string {
	return toolGlyph(toolFamily(toolName), { ascii: settings.ascii(), style });
}

type IconTheme = { fg(color: string, text: string): string; bold(text: string): string };

/** A call component with the icon before its first row; continuation rows keep the header's column. */
class IconLead implements Component {
	readonly icon: string;
	readonly inner: Component;
	private readonly theme: IconTheme;

	constructor(icon: string, inner: Component, theme: IconTheme) {
		this.icon = icon;
		this.inner = inner;
		this.theme = theme;
	}

	render(width: number): string[] {
		const target = Math.max(0, Math.floor(width));
		if (target === 0) return [];
		const lead = visibleWidth(this.icon) + 1;
		const fit = (line: string) => visibleWidth(line) <= target ? line : truncateToWidth(line, target, "");
		if (target <= lead) return this.inner.render(target).map(fit);
		return this.inner.render(target - lead).map((row, index) => fit(`${index === 0 ? `${this.theme.fg(ICON_ROLE, this.icon)} ` : " ".repeat(lead)}${row}`));
	}

	invalidate(): void { this.inner.invalidate(); }
}

const COLLAPSED_ARGS_CHARS = 120;

/** pi's own header for a tool without a call renderer: the name in bold, then its arguments. */
function fallbackCall(toolName: string, args: unknown, theme: IconTheme, expanded: boolean): string {
	const header = theme.fg("toolTitle", theme.bold(toolName));
	if (args == null) return header;
	const entries = typeof args === "object" && !Array.isArray(args) ? Object.entries(args as Record<string, unknown>) : [["args", args] as [string, unknown]];
	if (entries.length === 0) return header;
	if (expanded) {
		const lines = entries.map(([key, value]) => `  ${key}: ${(typeof value === "string" ? value : JSON.stringify(value, null, 2) ?? String(value)).split("\n").join("\n    ")}`);
		return `${header}\n${theme.fg("muted", sanitizeTerminalText(lines.join("\n")))}`;
	}
	const pairs = entries.map(([key, value]) => `${key}=${JSON.stringify(value) ?? String(value)}`).join(" ");
	const preview = pairs.length > COLLAPSED_ARGS_CHARS ? `${pairs.slice(0, COLLAPSED_ARGS_CHARS - 3)}...` : pairs;
	return `${header} ${theme.fg("muted", sanitizeTerminalText(preview))}`;
}

/** Renderers whose call already draws its icon: quiet cards and the memory line. */
const OWN_ICON = new WeakSet<object>();

/** Marks a call renderer that draws its own icon, so the resolver leaves it alone. */
type RenderCall = NonNullable<ToolRenderers["renderCall"]>;

export function ownsIcon(renderCall: RenderCall): RenderCall {
	OWN_ICON.add(renderCall);
	return renderCall;
}

/**
 * The tool's renderers with its family icon before the call's header. The
 * tool gets its own last component back, so renderers that reuse it keep
 * working; the result renderer and the shell stay the tool's. Undefined (pi's
 * plain fallback for an unknown tool) stays undefined, and pi's bash, whose
 * header is already `$ <command>`, stays as it is.
 */
export function withToolIcon(toolName: string, renderers: ToolRenderers | undefined, settings: ShellSettings = shellSettings()): ToolRenderers | undefined {
	if (!renderers) return undefined;
	const own = renderers.renderCall;
	if (own && OWN_ICON.has(own)) return renderers;
	// pi's own bash header already starts with its `$` prompt.
	if (toolName === "bash") return renderers;
	const icon = toolIcon(toolName, settings);
	const renderCall: RenderCall = (args, theme, context) => {
		const last = context.lastComponent instanceof IconLead ? context.lastComponent.inner : context.lastComponent;
		const inner = own
			? own(args, theme, { ...context, lastComponent: last })
			: new Text(fallbackCall(toolName, args, theme as IconTheme, context.expanded === true), 0, 0);
		return new IconLead(icon, inner, theme as IconTheme);
	};
	return { ...renderers, renderCall: ownsIcon(renderCall) };
}

// ── Memory: one line per call, `↳ ✓ <summary>` per result ────────────────

const MEMORY_ARG_KEYS = ["title", "query", "id", "observation_id", "new_title", "content", "project"] as const;
const ID_KEYS = new Set(["id", "observation_id"]);
const ARG_CHARS = 48;
const SUMMARY_CHARS = 64;

function oneLine(value: unknown, max: number): string {
	const text = sanitizeTerminalText(String(value ?? "")).replace(/\s+/g, " ").trim();
	return text.length <= max ? text : `${text.slice(0, Math.max(0, max - 1))}…`;
}

/** `mem_get_observation` reads as `get observation`; an MCP tool by its own name. */
function memoryAction(toolName: string): string {
	const name = MCP_NAME.exec(toolName)?.[2] ?? toolName;
	return name.replace(/^mem_/, "").replace(/_/g, " ");
}

function memoryArg(args: unknown): string {
	if (!args || typeof args !== "object") return "";
	const record = args as Record<string, unknown>;
	for (const key of MEMORY_ARG_KEYS) {
		const value = record[key];
		if (value === undefined || value === null || value === "") continue;
		return ID_KEYS.has(key) ? `#${oneLine(value, ARG_CHARS)}` : `“${oneLine(value, ARG_CHARS)}”`;
	}
	return "";
}

function resultText(result: { content?: unknown }): string {
	const content = Array.isArray(result?.content) ? result.content : [];
	return content.flatMap((part: any) => (part?.type === "text" && typeof part.text === "string" ? [part.text] : [])).join("\n");
}

/** A memory call as one line, `🧠 save “title”`, and its result as `↳ ✓ <first line>` or `↳ ✗ <error>`. */
export function memoryToolRenderers(toolName: string, settings: ShellSettings = shellSettings()): ToolRenderers {
	const icon = toolIcon(toolName, settings);
	return {
		renderCall: ownsIcon((args, theme) => {
			const t = theme as unknown as IconTheme;
			const arg = memoryArg(args);
			return new IconLead(icon, new Text(`${t.fg("toolTitle", t.bold(memoryAction(toolName)))}${arg ? ` ${t.fg("muted", arg)}` : ""}`, 0, 0), t);
		}),
		renderResult(result, options, theme, context) {
			const t = theme as unknown as IconTheme;
			const text = sanitizeTerminalText(resultText(result)).trim();
			const first = text.split("\n").find((line) => line.trim() !== "") ?? "";
			if (options.isPartial) return new Text(t.fg("dim", `↳ ${memoryAction(toolName)}…`), 0, 0);
			if (context?.isError) return new Text(t.fg("error", `↳ ✗ ${oneLine(first || "error", SUMMARY_CHARS)}`), 0, 0);
			const head = `${t.fg("dim", "↳")} ${t.fg("success", "✓")} ${t.fg("muted", oneLine(first || "done", SUMMARY_CHARS))}`;
			return new Text(options.expanded && text ? `${head}\n\n${t.fg("toolOutput", text)}` : head, 0, 0);
		},
	};
}

/** Whether a tool's calls draw as memory lines. */
export function isMemoryTool(toolName: string): boolean {
	return toolFamily(toolName) === TOOL_FAMILY.MEMORY;
}
