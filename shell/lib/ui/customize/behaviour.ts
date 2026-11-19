/**
 * shell/lib/ui/customize/behaviour.ts — Customize's Behaviour: compaction,
 * the fresh-session notice, stall recovery (and its thinking threshold) and double Esc (T15.15).
 */
import { writeDoubleEscCancelPolicy, type DoubleEscCancelPolicy } from "../double-esc-cancel-policy.ts";
import { attempt, boolRow, textRow, type CustomizeCategory } from "./row.ts";

export const BEHAVIOUR: CustomizeCategory = {
	id: "behaviour", title: "Behaviour", summary: "Compaction, the fresh-session notice, stall recovery, double Esc",
	rows: [
		// T155 (L1394): the context ladder is these two keys; the notices before compaction come from them.
		textRow("shell.behaviour.compact_at", "Compact at (%)", "The share of the window at which the session compacts; the fresh-session notice comes ten points before it. 100 leaves compaction to pi.", {
			preview: (v) => ({
				caption: "what happens",
				lines: [Number(v) >= 100
					? "100: pi decides when the session compacts; the fresh-session notice comes at 90%."
					: `At ${v}% of the context window the session compacts; from ${Math.max(1, Number(v) - 10)}% the context notice suggests a fresh session.`],
			}),
		}),
		// T115a (L691): a share of a very large window never comes, so a token budget sits beside it.
		textRow("shell.behaviour.compact_tokens", "Compact at (tokens)", "Past this many tokens the session compacts, whichever comes first with the share; the fresh-session notice comes at three quarters of it, the delegation notice at half (or 15% of the window). 0 turns all three off.", {
			preview: (v) => {
				const n = Number(v);
				return {
					caption: "what happens",
					lines: n === 0
						? ["0: off; only the shares compact and suggest a fresh session, and no delegation notice."]
						: [`Past ${v} tokens the session compacts, or at the share if that comes first.`, `Past ${Math.floor((n * 3) / 4)} tokens the context notice suggests a fresh session; past ${Math.floor(n / 2)} tokens, or 15% of the window, the orchestrator hears once to delegate.`],
				};
			},
		}),
		// T122 (L762 F2): a sub-agent's wrap-up notice; never a cap (T155).
		textRow("shell.behaviour.child_backstop_tokens", "Sub-agent wrap-up at (tokens)", "Past this many tokens, or 40% of the window if that comes first, a sub-agent hears once to wrap up and return path:line evidence; 0 turns it off.", {
			preview: (v) => ({
				caption: "what happens",
				lines: [Number(v) === 0 ? "0: off; no sub-agent wrap-up notice." : `Past ${v} tokens, or 40% of the window if that comes first, a sub-agent hears once to wrap up and return.`],
			}),
		}),
		textRow("shell.behaviour.stall_seconds", "Stall recovery (s)", "Seconds without an answer before the shell retries the turn; 0 turns it off.", {
			preview: (v) => ({
				caption: "what happens",
				lines: [Number(v) === 0 ? "0: off; a silent turn waits." : `After ${v} s without an answer the shell retries the turn.`],
			}),
		}),
		textRow("shell.behaviour.thinking_stall_seconds", "Thinking stall recovery (s)", "The same on claude-bridge or while the model thinks; 0 never retries there.", {
			preview: (v) => ({
				caption: "what happens",
				lines: [Number(v) === 0 ? "0: a thinking or claude-bridge turn waits." : `On claude-bridge or while the model thinks, after ${v} s without an answer the shell retries the turn.`],
			}),
		}),
		boolRow("shell.behaviour.double_esc_cancel", "Double Esc to cancel", "While the agent works, Esc cancels only when pressed twice.", {
			preview: (v) => ({ caption: "what happens", lines: [v === "on" ? "Esc Esc cancels the agent's work; one Esc does not." : "One Esc cancels the agent's work."] }),
			read: (h) => (h.settings.doubleEscCancel() ? "on" : "off"),
			save: (h, v) => attempt(() => {
				const policy: DoubleEscCancelPolicy = v === "on" ? "on" : "off";
				writeDoubleEscCancelPolicy(policy, { settings: h.settings });
				h.doubleEsc(policy);
			}),
		}),
	],
};
