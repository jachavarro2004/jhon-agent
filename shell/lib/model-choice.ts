/**
 * shell/lib/model-choice.ts — a Jhon session never calls a model nobody chose
 * (T130, L920 B1; the rule as the owner reduced it, L944).
 *
 * pi picks a session's first model in this order (core/model-resolver.js,
 * findInitialModel): an explicit `--model`, the first scoped model, the
 * settings' default when its provider has a credential, else, with no
 * message, the first available model. Jhon renders the active profile's
 * model as that default (settings.json's defaultProvider and defaultModel),
 * so the last step runs the session on a model neither the profile nor the
 * owner chose: with pi-claude-bridge loaded, a bridge model.
 *
 * The shell judges with what it knows for certain at session start and
 * during the process, and never reads a session's history. A session's model
 * is chosen when it is a named default (the project's .pi/settings.json or
 * the agent folder's), when the command line names --model or --provider,
 * when models are scoped (a --models pattern matched), or when the owner
 * said yes to the question or picked a model with pi's picker in this
 * process. Otherwise, when a named default cannot run (pi's own test: it is
 * not in the registry, or its provider has no configured credential), the
 * session is on a model nobody chose. When no named default is broken,
 * nothing is said or held, whatever model the session is on. A registry or
 * settings that cannot be read say nothing.
 *
 * So a resumed session on a model the owner picked in an earlier process,
 * while a named default cannot run, is asked once per process with a UI, and
 * without one ends with the line and needs --model.
 *
 * Held, no model call is made (extensions/jhon.ts): the owner's prompt opens
 * the question, a run any extension starts is aborted in before_agent_start
 * (pi stops it at agent_start, awaited before the first request:
 * core/agent-session.js _handleAgentEvent), and a compaction or a tree
 * summary is cancelled in its before-event.
 */
import { readFileSync } from "node:fs";
import { join } from "node:path";

/** A model as pi names it. */
interface Model { provider: string; id: string }

/** A default some settings file names, and whose it is. */
export interface NamedModel extends Model { from: "profile" | "project" }

/**
 * The defaults that count as chosen (L926): the agent folder's (the profile's,
 * as Jhon rendered it) and, when the project's .pi/settings.json names a
 * provider or a model, the project's, merged key by key over the agent
 * folder's as pi merges them (core/settings-manager.js, deepMergeSettings).
 * pi reads the project's only in a trusted project; untrusted, the session is
 * on the agent folder's. The shell cannot tell which.
 */
export function settingsDefaults(agentDir: string, cwd: string | undefined): NamedModel[] {
	const keys = (file: string): { provider?: string; id?: string } => {
		try {
			const s = JSON.parse(readFileSync(file, "utf8"));
			return { provider: typeof s?.defaultProvider === "string" && s.defaultProvider ? s.defaultProvider : undefined, id: typeof s?.defaultModel === "string" && s.defaultModel ? s.defaultModel : undefined };
		} catch {
			return {};
		}
	};
	const agent = keys(join(agentDir, "settings.json"));
	const project = cwd ? keys(join(cwd, ".pi", "settings.json")) : {};
	const out: NamedModel[] = [];
	const provider = project.provider ?? agent.provider, id = project.id ?? agent.id;
	if ((project.provider || project.id) && provider && id) out.push({ provider, id, from: "project" });
	if (agent.provider && agent.id) out.push({ provider: agent.provider, id: agent.id, from: "profile" });
	return out;
}

/**
 * Whether the command line names the session's model: --model or --provider,
 * as a word or `--model=…`. --models is not one: a pattern that matched
 * scopes the models (ctx.scopedModels, judged apart), and one that matched
 * nothing leaves pi on its fallback (L926 BREAK 1).
 */
function namesModel(argv: readonly string[]): boolean {
	return argv.some((a) => /^--(model|provider)(=|$)/.test(a));
}

const PICKED = Symbol.for("jhon.model-picked");
const SAID = Symbol.for("jhon.model-line-said");
const store = globalThis as Record<symbol, unknown>;

/** What ModelChoice reads of pi's ctx at session_start. */
export interface ChoiceCtx {
	cwd?: unknown;
	model?: { provider?: unknown; id?: unknown };
	modelRegistry?: { find?(provider: string, id: string): unknown; hasConfiguredAuth?(model: unknown): boolean };
	scopedModels?: readonly unknown[];
}

export class ModelChoice {
	private line: string | undefined;
	private used: Model | undefined;
	private readonly read: (cwd?: string) => NamedModel[];

	/** read: the named defaults for a session in cwd (settingsDefaults). */
	constructor(read: (cwd?: string) => NamedModel[]) {
		this.read = read;
	}

	/**
	 * session_start: the line to show when the session stands on a model nobody
	 * chose, undefined otherwise. Until `picked` or the next start, `refused`
	 * gives the same line.
	 */
	start(ctx: ChoiceCtx | undefined, argv: readonly string[]): string | undefined {
		this.line = undefined;
		this.used = undefined;
		try {
			const named = this.read(typeof ctx?.cwd === "string" ? ctx.cwd : undefined);
			const model = ctx?.model;
			if (!named.length || typeof model?.provider !== "string" || typeof model?.id !== "string") return undefined;
			const used = `${model.provider}/${model.id}`;
			// A session on a named default (the profile's, or the project's) is on a chosen model.
			if (named.some((d) => `${d.provider}/${d.id}` === used)) return undefined;
			if (namesModel(argv) || (ctx?.scopedModels?.length ?? 0) > 0 || store[PICKED] === used) return undefined;
			const registry = ctx?.modelRegistry;
			if (typeof registry?.find !== "function" || typeof registry?.hasConfiguredAuth !== "function") return undefined;
			// The line names each named default that cannot run, never one that can. A model both files
			// name is the profile's: true whether or not pi read the project's file (its trust).
			const own = named.filter((d) => d.from === "profile" || !named.some((o) => o.from === "profile" && o.provider === d.provider && o.id === d.id));
			const broken = own.flatMap((d) => {
				const found = registry.find!(d.provider, d.id);
				if (found && registry.hasConfiguredAuth!(found)) return [];
				return [{ who: d.from === "project" ? "the project's .pi/settings.json" : "the profile", model: `${d.provider}/${d.id}`, why: found ? `its provider ${d.provider} has no credential` : "pi has no such model" }];
			});
			if (!broken.length) return undefined;
			const said = broken.length === 1
				? `${broken[0]!.who} names ${broken[0]!.model} for this session, but ${broken[0]!.why}`
				: `${broken.map((b) => `${b.who} names ${b.model} (${b.why})`).join(" and ")} for this session, and neither can run`;
			this.line = `jhon: ${said}; this session is on ${used}, which nobody chose in this run.`;
			this.used = { provider: model.provider, id: model.id };
		} catch {
			// What cannot be read judges nothing: the hold was reset above and is set only at the end.
		}
		return this.line;
	}

	/** The model the held session stands on: pi's fallback, which the owner may take as theirs (`picked`, source set). */
	fallback(): { provider: string; id: string } | undefined {
		return this.line ? this.used : undefined;
	}

	/** model_select, or the owner's yes: a model the owner set or cycled to is their choice, for this process. */
	picked(event: { source?: unknown; model?: { provider?: unknown; id?: unknown } } | undefined): void {
		if (event?.source !== "set" && event?.source !== "cycle") return;
		if (typeof event.model?.provider === "string" && typeof event.model?.id === "string") store[PICKED] = `${event.model.provider}/${event.model.id}`;
		this.line = undefined;
		delete store[SAID];
	}

	/** The line while the session still stands on the model nobody chose: no prompt goes to it. */
	refused(): string | undefined {
		return this.line;
	}

	/**
	 * Whether a session start should say the line. A /new on a held session
	 * holds the next one for the same reason: there (`again`), the line is not
	 * repeated while it is still the last one said in this process. Any other
	 * start (startup, resume, reload: the screen was drawn anew) says it. A
	 * start that holds nothing (undefined) and a pick forget it.
	 */
	static announce(line: string | undefined, again = false): boolean {
		if (!line) { delete store[SAID]; return false; }
		if (again && store[SAID] === line) return false;
		store[SAID] = line;
		return true;
	}

	/** The model the owner picked in this process, if any. */
	static pick(): { provider: string; id: string } | undefined {
		const picked = store[PICKED];
		if (typeof picked !== "string") return undefined;
		const at = picked.indexOf("/");
		return { provider: picked.slice(0, at), id: picked.slice(at + 1) };
	}

	/** The tests' reset of the process state: the owner's pick lives in globalThis, across shells and test cases. */
	static forget(): void {
		delete store[PICKED];
		delete store[SAID];
	}
}

/** What the owner does about it in a session with a UI: true in the TUI and for an RPC client, which has no /model. */
export const UNCHOSEN_HINT = "Your next prompt asks whether to run on it; you can also pick another model, or restart with the credential set.";
/** The question a held prompt asks the owner, and its two answers (L930 N2). */
export const unchosenQuestion = (line: string, fallback: { provider: string; id: string }) => `${line} Run this session on ${fallback.provider}/${fallback.id}?`;
export const UNCHOSEN_YES = "Yes: it is my choice for this session";
export const UNCHOSEN_NO = "No: keep it held";
/** What a run without a UI did about it, and how to choose there. */
export const UNCHOSEN_ENDED = "No model was called: pass --model <provider/id> to choose one.";
