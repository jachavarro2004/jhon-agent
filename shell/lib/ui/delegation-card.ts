import { Text } from "@earendil-works/pi-tui";
import type { ToolRenderers } from "@earendil-works/pi-coding-agent";
import { shellSettings } from "../settings.ts";
import { sanitizeTerminalText } from "./terminal-theme.ts";
import { ownsIcon } from "./tool-icons.ts";

// A delegation's card, as the Gentle Shell draws its subagent_run call
// (extensions/gentle-agents.ts, ACTION_HEADING.run and its agent chips): the
// heading `→ Delegating · <agent> · <provider/model> · <thinking>`, and the
// launch's result (`Started background sub-agent agent-N …`) as its body.

type CardTheme = { fg(color: string, text: string): string; bold(text: string): string };

/** What a delegation runs on: the agent's model and thinking, when its definition can be read. */
export type DelegationProfile = (agent: string) => { model?: string; thinking?: string } | undefined;

function clean(value: unknown): string {
	return sanitizeTerminalText(String(value ?? "")).replace(/\s+/g, " ").trim();
}

/** The heading's text: the verb, then the agent, its model and its thinking, as far as they are known. */
export function delegationHeading(verb: string, agent: string, profile: DelegationProfile): string {
	const name = clean(agent);
	const known = name ? profile(name) : undefined;
	const arrow = shellSettings().ascii() ? "->" : "→";
	return [`${arrow} ${verb}`, name, clean(known?.model), clean(known?.thinking)].filter(Boolean).join(" · ");
}

/** The renderers of a tool that launches a sub-agent: its argument `key` names the agent. */
export function delegationRenderers(verb: string, key: string, profile: DelegationProfile): Pick<ToolRenderers, "renderCall" | "renderResult"> {
	return {
		renderCall: ownsIcon((args, theme) => {
			const t = theme as unknown as CardTheme;
			const agent = (args as Record<string, unknown> | undefined)?.[key];
			return new Text(t.fg("toolTitle", t.bold(delegationHeading(verb, typeof agent === "string" ? agent : "", profile))), 0, 0);
		}),
		renderResult(result, options, theme, context) {
			const t = theme as unknown as CardTheme;
			const body = sanitizeTerminalText((result?.content ?? []).map((part: any) => (part?.type === "text" ? part.text : "")).join("\n")).trim();
			if (options.isPartial) return new Text(t.fg("muted", body), 0, 0);
			const shown = options.expanded ? body : body.split("\n")[0] ?? "";
			return new Text(t.fg(context?.isError ? "error" : "muted", shown), 0, 0);
		},
	};
}
