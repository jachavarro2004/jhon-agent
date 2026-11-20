/**
 * shell/lib/ui/customize/keys.ts — Customize's Keys: the shell's shortcuts,
 * each read at the start (T15.15). A key pi already binds is refused, naming
 * pi's action (T165).
 */
import type { ShellKey } from "../../settings.ts";
import { NEXT_START, saveSetting, textRow, type CustomizeCategory, type CustomizeRow } from "./row.ts";

const OPENS: Record<ShellKey, string> = {
	panel: "the LDD status", agents: "the Agents view",
};

/**
 * The keys pi binds where the shell's shortcuts run: the editor, the app and
 * the fullscreen transcript. pi's extension API exposes no list of them (its
 * RESERVED_KEYBINDINGS_FOR_EXTENSION_CONFLICTS is module-private, and a copy
 * of pi-coding-agent the shell imports does not see pi's keybindings), so this
 * is taken from pi 1.5.2's docs/keybindings.md
 * (~/.local/share/mise/installs/pi/1.5.2/pi/docs/keybindings.md), with the
 * prompt jumps as its source sets them: ctrl+shift+up/down and ctrl+up/down
 * off Windows too. Pickers' own keys (/tree, /resume, the model selectors) and
 * the keys that act only while searching are left out. Each entry: the
 * action, pi's description, its keys, and its keys on Windows and WSL when
 * they differ.
 */
const PI_KEYS: readonly (readonly [id: string, description: string, keys: readonly string[], windows?: readonly string[]])[] = [
	["app.interrupt", "Cancel / abort", ["escape"]],
	["app.clear", "Clear editor (first) / exit (second)", ["ctrl+c"]],
	["app.exit", "Exit (when editor empty)", ["ctrl+d"]],
	["app.suspend", "Suspend to background", ["ctrl+z"]],
	["app.editor.external", "Open in external editor", ["ctrl+g"]],
	["app.clipboard.pasteImage", "Paste files, images, or text from clipboard", ["ctrl+v"], ["alt+v", "ctrl+v"]],
	["app.model.select", "Open model selector", ["ctrl+l"]],
	["app.model.cycleForward", "Cycle to next model", ["ctrl+p"]],
	["app.model.cycleBackward", "Cycle to previous model", ["shift+ctrl+p"], ["alt+p"]],
	["app.thinking.cycle", "Cycle thinking level", ["shift+tab"]],
	["app.thinking.toggle", "Collapse or expand thinking blocks", ["ctrl+t"]],
	["app.tools.expand", "Collapse or expand tool output", ["ctrl+o"]],
	["app.message.copy", "Copy the selected message", ["ctrl+x"]],
	["app.message.followUp", "Queue follow-up message", ["alt+enter"], ["ctrl+q"]],
	["app.message.dequeue", "Restore queued messages to editor", ["alt+up"], ["alt+q"]],
	["tui.altScreen.pageUp", "Scroll the transcript up by one page", ["pageUp"]],
	["tui.altScreen.pageDown", "Scroll the transcript down by one page", ["pageDown"]],
	["tui.altScreen.previousPrompt", "Jump to the previous marked message", ["ctrl+shift+up", "ctrl+up"], ["ctrl+up"]],
	["tui.altScreen.nextPrompt", "Jump to the next marked message", ["ctrl+shift+down", "ctrl+down"], ["ctrl+down"]],
	["tui.altScreen.search", "Search the rendered transcript", ["ctrl+shift+f"], ["ctrl+f"]],
	["tui.altScreen.top", "Scroll to the beginning of the transcript", ["ctrl+home"]],
	["tui.altScreen.bottom", "Scroll to the transcript end and follow new output", ["ctrl+end"]],
	["tui.input.submit", "Submit input", ["enter", "return"]],
	["tui.input.newLine", "Insert new line", ["shift+enter", "ctrl+j"]],
	["tui.input.tab", "Tab or autocomplete", ["tab"]],
	["tui.editor.cursorUp", "Move cursor up", ["up"]],
	["tui.editor.cursorDown", "Move cursor down", ["down"]],
	["tui.editor.cursorLeft", "Move cursor left", ["left", "ctrl+b"]],
	["tui.editor.cursorRight", "Move cursor right", ["right", "ctrl+f"]],
	["tui.editor.cursorWordLeft", "Move cursor word left", ["alt+left", "ctrl+left", "alt+b"]],
	["tui.editor.cursorWordRight", "Move cursor word right", ["alt+right", "ctrl+right", "alt+f"]],
	["tui.editor.cursorLineStart", "Move to line start", ["home", "ctrl+a"]],
	["tui.editor.cursorLineEnd", "Move to line end", ["end", "ctrl+e"]],
	["tui.editor.jumpForward", "Jump forward to character", ["ctrl+]"]],
	["tui.editor.jumpBackward", "Jump backward to character", ["ctrl+alt+]"]],
	["tui.editor.pageUp", "Scroll up by page", ["ctrl+pageUp"]],
	["tui.editor.pageDown", "Scroll down by page", ["ctrl+pageDown"]],
	["tui.editor.deleteCharBackward", "Delete character backward", ["backspace"]],
	["tui.editor.deleteCharForward", "Delete character forward", ["delete"]],
	["tui.editor.deleteWordBackward", "Delete word backward", ["ctrl+w", "alt+backspace"]],
	["tui.editor.deleteWordForward", "Delete word forward", ["alt+d", "alt+delete"]],
	["tui.editor.deleteToLineStart", "Delete to line start", ["ctrl+u"]],
	["tui.editor.deleteToLineEnd", "Delete to line end", ["ctrl+k"]],
	["tui.editor.yank", "Paste most recently deleted text", ["ctrl+y"]],
	["tui.editor.yankPop", "Cycle through deleted text after yank", ["alt+y"]],
	["tui.editor.undo", "Undo last edit", ["ctrl+-"], ["ctrl+z", "alt+z"]],
];

/** pi's windowsKeybindings: native Windows or WSL. */
const onWindows = (): boolean => process.platform === "win32" || Boolean(process.env.WSL_DISTRO_NAME);

/** A key in one spelling: lower case, `esc` as `escape`, modifiers sorted. */
function normalizeKey(key: string): string {
	const text = key.trim().toLowerCase();
	const plus = text.endsWith("++");
	const parts = (plus ? text.slice(0, -2) : text).split("+").filter((p) => p !== "");
	const last = plus ? "+" : parts.pop() ?? "";
	const name = last === "esc" ? "escape" : last;
	return [...parts.sort(), name].join("+");
}

/** pi's action that binds `key`, or undefined when pi leaves it free (and for off or nothing). */
export function piReservedAction(key: string, windows: boolean = onWindows()): { id: string; description: string } | undefined {
	const wanted = normalizeKey(key);
	if (wanted === "" || wanted === "off") return undefined;
	for (const [id, description, keys, windowsKeys] of PI_KEYS) {
		if ((windows && windowsKeys ? windowsKeys : keys).some((k) => normalizeKey(k) === wanted)) return { id, description };
	}
	return undefined;
}

const keyRow = (name: ShellKey, label: string): CustomizeRow => textRow(`shell.keys.${name}`, label, NEXT_START, {
	preview: (v) => ({
		caption: "what the key opens",
		lines: [!v || v === "off" ? `No key opens ${OPENS[name]}.` : `${v} opens ${OPENS[name]}.`],
	}),
	save: (h, value) => {
		const pi = piReservedAction(value);
		if (pi) return `${value.trim()} is pi's key for "${pi.description}" (${pi.id}); pick another key.`;
		return saveSetting(h, `shell.keys.${name}`, value);
	},
});

export const KEYS: CustomizeCategory = {
	id: "keys", title: "Keys", summary: "The shell's keys: LDD status, agents; reload",
	rows: [
		keyRow("panel", "LDD status"),
		keyRow("agents", "Agents"),
		{
			// G's "Reload now to apply keymap changes" (L198): pi's reload, no restart.
			key: "reload", label: "Reload now", kind: "action",
			note: "Reloads pi's extensions so changed keys apply without restarting pi.",
			read: () => "enter reloads",
			save: (h) => { h.reload!(); return undefined; },
			hidden: (h) => !h.reload,
		},
	],
};
