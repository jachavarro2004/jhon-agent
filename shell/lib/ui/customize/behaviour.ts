/**
 * shell/lib/ui/customize/behaviour.ts — Customize's Behaviour: compaction,
 * the fresh-session notice, stall recovery (and its thinking threshold) and double Esc (T15.15).
 */
import { writeDoubleEscCancelPolicy, type DoubleEscCancelPolicy } from "../double-esc-cancel-policy.ts";
import { attempt, boolRow, textRow, type CustomizeCategory } from "./row.ts";

export const BEHAVIOUR: CustomizeCategory = {
	id: "behaviour", title: "Behaviour", summary: "Compaction, the fresh-session notice, stall recovery, double Esc",
	rows: [
		textRow("shell.behaviour.compact_at", "Compact at (%)", "The share of the window at which the session compacts; 100 leaves it to pi.", {
			preview: (v) => ({
				caption: "what happens",
				lines: [Number(v) >= 100 ? "100: pi decides when the session compacts." : `At ${v}% of the context window the session compacts.`],
			}),
		}),
		// T115a (L691): a share of a very large window never comes, so a token budget sits beside it.
		textRow("shell.behaviour.compact_tokens", "Compact at (tokens)", "Past this many tokens the session compacts, whichever comes first with the share; 0 turns it off.", {
			preview: (v) => ({
				caption: "what happens",
				lines: [Number(v) === 0 ? "0: off; only the share compacts." : `Past ${v} tokens the session compacts, or at the share if that comes first.`],
			}),
		}),
		textRow("shell.behaviour.fresh_session_at", "Fresh session at (%)", "From this share of the window the context notice suggests a fresh session.", {
			preview: (v) => ({ caption: "what happens", lines: [`From ${v}% of the window the context notice suggests a fresh session.`] }),
		}),
		textRow("shell.behaviour.fresh_session_tokens", "Fresh session at (tokens)", "Past this many tokens the context notice suggests a fresh session, whichever comes first with the share; 0 turns it off.", {
			preview: (v) => ({
				caption: "what happens",
				lines: [Number(v) === 0 ? "0: off; only the share brings the notice." : `Past ${v} tokens the context notice suggests a fresh session, or at the share if that comes first.`],
			}),
		}),
		// T115e (L715): the ladder's first rung, before the fresh-session notice and compaction.
		textRow("shell.behaviour.backstop_tokens", "Delegate at (tokens)", "Past this many tokens, or 15% of the window if that comes first, the orchestrator hears once to delegate its next several-step unit; 0 turns it off.", {
			preview: (v) => ({
				caption: "what happens",
				lines: [Number(v) === 0 ? "0: off; no delegation backstop." : `Past ${v} tokens, or 15% of the window if that comes first, the orchestrator hears once to delegate its next several-step unit.`],
			}),
		}),
		// T122 (L762 F2): a sub-agent child's own ladder.
		textRow("shell.behaviour.child_backstop_tokens", "Sub-agent wrap-up at (tokens)", "Past this many tokens, or 40% of the window if that comes first, a sub-agent hears once to wrap up and return path:line evidence; 0 turns it off.", {
			preview: (v) => ({
				caption: "what happens",
				lines: [Number(v) === 0 ? "0: off; no sub-agent wrap-up notice." : `Past ${v} tokens, or 40% of the window if that comes first, a sub-agent hears once to wrap up and return.`],
			}),
		}),
		textRow("shell.behaviour.child_cap_tokens", "Sub-agent cap at (tokens)", "Past this many tokens, or 75% of the window if that comes first, a sub-agent may call no more tools and returns what it has; 0 turns it off.", {
			preview: (v) => ({
				caption: "what happens",
				lines: [Number(v) === 0 ? "0: off; no sub-agent context cap." : `Past ${v} tokens, or 75% of the window if that comes first, a sub-agent may call no more tools and returns.`],
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
