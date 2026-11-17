/**
 * shell/lib/ui/customize/keys.ts — Customize's Keys: the shell's shortcuts,
 * each read at the start (T15.15).
 */
import type { ShellKey } from "../../settings.ts";
import { NEXT_START, textRow, type CustomizeCategory, type CustomizeRow } from "./row.ts";

const OPENS: Record<ShellKey, string> = {
	panel: "the LDD status", rail: "the panel sheet", todos: "the todo list", tasks: "the Agents view",
	changes: "the changes view", commands: "the command palette",
};

const keyRow = (name: ShellKey, label: string): CustomizeRow => textRow(`shell.keys.${name}`, label, NEXT_START, {
	preview: (v) => ({
		caption: "what the key opens",
		lines: [!v || v === "off" ? `No key opens ${OPENS[name]}.` : `${v} opens ${OPENS[name]}.`],
	}),
});

export const KEYS: CustomizeCategory = {
	id: "keys", title: "Keys", summary: "The shell's keys: LDD status, panel sheet, todos, tasks, changes, palette; reload",
	rows: [
		keyRow("panel", "LDD status"),
		keyRow("rail", "Panel sheet"),
		keyRow("todos", "Todos"),
		keyRow("tasks", "Agents"),
		keyRow("changes", "Changes"),
		keyRow("commands", "Command palette"),
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
