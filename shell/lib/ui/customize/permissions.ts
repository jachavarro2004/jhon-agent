/**
 * shell/lib/ui/customize/permissions.ts — Customize's Permissions: the
 * project's permission mode (L175), saved through the shell's mode hook,
 * which runs changeMode (super yolo only after the owner's Yes); the one
 * place the mode changes (L214). A confirm row: ←/→ hold the choice, enter
 * applies it (L184). Hidden when the shell cannot run `jhon mode`; the
 * grants are in /jhon:grants.
 */
import { MODES } from "../jhon-panel-grants.ts";
import type { CustomizeCategory } from "./row.ts";

const label = (value: string) => MODES.find((m) => m.mode === value)?.label ?? value;

export const PERMISSIONS: CustomizeCategory = {
	id: "permissions", title: "Permissions", summary: "The project's permission mode",
	rows: [
		{
			key: "mode", label: "Permission mode", kind: "choice", values: ["normal", "yolo", "super-yolo"], display: label,
			// Never a side effect of moving the selection: ←/→ hold the choice, enter applies it.
			confirm: true,
			note: "What the guards ask in this project. The grants: /jhon:grants.",
			preview: (v) => ({ caption: "what it means", lines: [`${label(v)}: ${MODES.find((m) => m.mode === v)?.words ?? ""}`] }),
			read: (h) => h.mode!.value(),
			save: (h, v) => h.mode!.set(v),
			hidden: (h) => !h.mode,
		},
	],
};
