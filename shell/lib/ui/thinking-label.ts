/**
 * shell/lib/ui/thinking-label.ts — the thinking label (design/shell-tui 01, T174-S7).
 *
 * A hidden thinking block is one row, `▸ Thinking…` (`> Thinking...` under
 * shell.appearance.ascii). pi draws that row and its look (italic, the
 * theme's thinkingText) and toggles the block on a click; the one thing it
 * lets an extension set is the label (ctx.ui.setHiddenThinkingLabel), one
 * label for every row, so the row cannot carry each block's own words.
 * extensions/pretty.ts keeps pi-pretty's animated label off, so this one
 * holds; the session's end gives pi its own label back.
 */
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { shellSettings } from "../settings.ts";
import { shellEnabled } from "./panel.ts";

/** The label pi draws for a hidden thinking block. */
export function thinkingLabel(ascii: boolean): string {
	return ascii ? "> Thinking..." : "▸ Thinking…";
}

type LabelUi = { setHiddenThinkingLabel?(label?: string): void };
type LabelContext = { mode?: string; ui?: LabelUi };

/** Sets the label in the shell's TUI when a session starts, again before a run when the ASCII setting changed it. */
export function installThinkingLabel(pi: Pick<ExtensionAPI, "on">, env: NodeJS.ProcessEnv = process.env): void {
	if (!shellEnabled(env) || typeof pi?.on !== "function") return;
	let shown: string | undefined;
	const settable = (ctx: LabelContext | undefined): ctx is LabelContext & { ui: Required<LabelUi> } =>
		ctx?.mode === "tui" && typeof ctx.ui?.setHiddenThinkingLabel === "function";
	const apply = (ctx: LabelContext | undefined) => {
		if (!settable(ctx)) return;
		let ascii = false;
		try { ascii = shellSettings().ascii(); } catch { /* Unicode. */ }
		const label = thinkingLabel(ascii);
		if (label === shown) return;
		shown = label;
		ctx.ui.setHiddenThinkingLabel(label);
	};
	const on = pi.on.bind(pi) as (event: string, handler: (event: unknown, ctx: LabelContext) => unknown) => void;
	on("session_start", (_event, ctx) => { shown = undefined; apply(ctx); });
	on("agent_start", (_event, ctx) => apply(ctx));
	on("session_shutdown", (_event, ctx) => {
		if (shown === undefined || !settable(ctx)) return;
		shown = undefined;
		ctx.ui.setHiddenThinkingLabel(undefined);
	});
}
