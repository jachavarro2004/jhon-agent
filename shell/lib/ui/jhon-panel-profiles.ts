/**
 * shell/lib/ui/jhon-panel-profiles.ts — /jhon:profile as the owner's mockup
 * (T117d, L694, L695, L720): the profiles as tabs across the top (each with
 * its delegation icon, ⊙ inline or ⇄ writer, ★ on the default, and ● on the
 * profile this session uses while that is not the default, T120), "+ New"
 * last; under them the chosen profile's name, what it is to this session and
 * the legend; its Orchestrator row (model, effort, mode) and one row per agent
 * of this runtime (model, effort).
 *
 * Edits go to a draft, never straight to the file: w switches the mode, enter
 * picks a row's model, e its effort; s saves the draft (`jhon profile mode`
 * and `jhon profile set`, one call per change). u uses the profile in this
 * session only (its sub-agents, its orchestrator's model and its mode; the
 * other sessions and the default are untouched), d makes it the default for
 * new sessions and leaves this one as it is; n (or enter on + New)
 * creates a copy of the chosen profile; x deletes it after a y. The `default`
 * profile is agents.yaml itself: read-only, n makes an editable copy.
 * Fallbacks, the tier and "set every agent" are deferred (L720).
 */
import { Key, matchesKey, visibleWidth, wrapTextWithAnsi } from "@earendil-works/pi-tui";
import { asciiText } from "./primitives/index.ts";
import { EFFORT_WORDS } from "./shell-bar.ts";

/** One agent's model in one runtime, as `jhon profile json` gives it. */
export interface ProfileRow {
	agent: string;
	runtime: string;
	model: string;
	source: string;
}

export type Delegation = "inline" | "writer";

/** `jhon profile json`. */
export interface ProfilesData {
	active: string;
	file: string;
	/** provider: the pi provider the profile runs on (its orchestrator's model's);
	 * delegation: its delegation mode, inline (the default) or writer (T117a). */
	profiles: Array<{ name: string; provider?: string; delegation: "inline" | "writer"; agents: ProfileRow[] }>;
	/** Each runtime's effort values the engine accepts. */
	efforts?: Record<string, string[]>;
}

export interface ProfilesPaint {
	fg(role: string, text: string): string;
	bold(text: string): string;
}

export interface ProfilesDeps {
	/** The runtime this shell is: the rows shown are its. */
	runtime: string;
	load(): Promise<ProfilesData>;
	/** u: use the profile in this session only (T120; T117c: the prompt rebuilt once); why it was refused or failed. */
	useNow(name: string): Promise<string | undefined>;
	/** The profile this session uses, when it has one of its own (T120): what a panel opened again shows; without it, the default until u. */
	sessionProfile?(): string | undefined;
	/** d: make the profile the default for new sessions (`jhon profile use`); the error's text when it failed. */
	makeDefault(name: string): Promise<string | undefined>;
	create(name: string, from: string): Promise<void>;
	/** One agent's model in one runtime (`jhon profile set`); "" puts it back on agents.yaml. */
	set(profile: string, agent: string, runtime: string, value: string): Promise<void>;
	/** The profile's delegation mode (`jhon profile mode <profile> <inline|writer>`). */
	mode(profile: string, mode: Delegation): Promise<void>;
	remove(name: string): Promise<void>;
	/** The models a runtime can take, to choose from. */
	models(runtime: string, data: ProfilesData): string[];
	/** Whether this session's pi lists the model (`provider/id`) as available now: provider loaded, credential set; without it, a save says nothing about it. */
	inSession?(model: string): boolean;
	requestRender(): void;
}

/** The agent whose row is the Orchestrator's (the engine's pi main session). */
export const ORCHESTRATOR = "jhon-orchestrator";
const PICKER_ROWS = 12;
const READ_ONLY = "default is agents.yaml's models, read-only: n makes an editable copy.";

/** «claude-bridge/claude-opus-5-5@medium» as «claude-bridge/claude-opus-5-5 · medium». */
export function modelText(model: string): string {
	const at = model.lastIndexOf("@");
	if (at <= 0) return model;
	const effort = model.slice(at + 1);
	return `${model.slice(0, at)} · ${EFFORT_WORDS[effort] ?? effort}`;
}

/** A model value's parts: «a/b@high» → ["a/b", "high"]. */
function split(value: string): [string, string] {
	const at = value.lastIndexOf("@");
	return at <= 0 ? [value, ""] : [value.slice(0, at), value.slice(at + 1)];
}

type Mode =
	| { kind: "profiles" }
	| { kind: "model"; agent: string; cursor: number; filter: string }
	| { kind: "effort"; agent: string; cursor: number }
	| { kind: "name"; text: string }
	| { kind: "remove"; name: string };

interface Draft { mode?: Delegation; models: Map<string, string> }

/** The rows a note takes at most. */
const NOTE_ROWS = 3;

const printable = (data: string) => data.length === 1 && data >= " " && data !== "\x7f";

export class ProfilesCard {
	private data: ProfilesData | undefined;
	private error: string | undefined;
	busy: string | undefined;
	private note: { text: string; ok: boolean } | undefined;
	private saved = false;
	/** The tab chosen: a profile's index, or profiles.length for + New. */
	private tab = 0;
	/** The row chosen: 0 the Orchestrator, then the agents. */
	private row = 0;
	/** The profile this session uses: the default when the view opened, until u. */
	private session: string | undefined;
	/** T120: the profile the session uses as its own (deps.sessionProfile, or u here); undefined while it follows the default. */
	private own: string | undefined;
	private readonly drafts = new Map<string, Draft>();
	private mode: Mode = { kind: "profiles" };
	private readonly deps: ProfilesDeps;

	constructor(deps: ProfilesDeps) {
		this.deps = deps;
	}

	/** Whether the card takes every key now (esc included): a picker, a name or a confirmation is open. */
	captures(): boolean {
		return this.mode.kind !== "profiles";
	}

	reload(select?: string): void {
		this.busy = "loading…";
		this.deps.requestRender();
		const settle = (data?: ProfilesData, error?: string) => {
			this.busy = undefined;
			if (data) {
				const first = !this.data;
				this.data = data;
				this.error = undefined;
				// The session's own profile when it has one, so it is still the one shown when the panel is opened again.
				if (this.deps.sessionProfile) this.own = this.deps.sessionProfile();
				this.session = this.own ?? this.session ?? data.active;
				const named = data.profiles.findIndex((p) => p.name === (select ?? (first ? data.active : undefined)));
				if (named >= 0) this.tab = named;
				this.tab = Math.min(this.tab, data.profiles.length);
				if (this.tab === data.profiles.length && data.profiles.length && select === undefined && !first) this.tab = data.profiles.length - 1;
				this.row = Math.min(this.row, Math.max(0, this.rows().length - 1));
			} else this.error = error;
			this.deps.requestRender();
		};
		try {
			this.deps.load().then((d) => settle(d), (e) => settle(undefined, e instanceof Error ? e.message : String(e)));
		} catch (e) {
			settle(undefined, e instanceof Error ? e.message : String(e));
		}
	}

	private chosen() {
		return this.data?.profiles[this.tab];
	}

	/** The chosen profile's rows in this runtime: the Orchestrator's first (undefined when it names none), then the agents. */
	private rows(): Array<ProfileRow | undefined> {
		const own = (this.chosen()?.agents ?? []).filter((a) => a.runtime === this.deps.runtime);
		return [own.find((a) => a.agent === ORCHESTRATOR), ...own.filter((a) => a.agent !== ORCHESTRATOR)];
	}

	private draft(name: string): Draft {
		let d = this.drafts.get(name);
		if (!d) this.drafts.set(name, d = { models: new Map() });
		return d;
	}

	/** A row's model as the draft has it. */
	private value(row: ProfileRow): string {
		return this.drafts.get(this.chosen()?.name ?? "")?.models.get(row.agent) ?? row.model;
	}

	private modeOf(p: ProfilesData["profiles"][number]): Delegation {
		return this.drafts.get(p.name)?.mode ?? p.delegation ?? "inline";
	}

	/** The draft's changes against the file: the mode, then each agent's model. */
	private changes(): { mode?: Delegation; models: Array<[string, string]> } {
		const p = this.chosen();
		const d = p && this.drafts.get(p.name);
		if (!p || !d) return { models: [] };
		const rows = new Map(p.agents.filter((a) => a.runtime === this.deps.runtime).map((a) => [a.agent, a.model]));
		return {
			mode: d.mode && d.mode !== (p.delegation ?? "inline") ? d.mode : undefined,
			models: [...d.models].filter(([agent, value]) => rows.get(agent) !== value),
		};
	}

	private dirty(): boolean {
		const c = this.changes();
		return Boolean(c.mode) || c.models.length > 0;
	}

	/** A call through the engine; its outcome as the note; then the profiles read again. */
	private run(busy: string, write: () => Promise<unknown>, ok: string | undefined, fail: string, select?: string, then?: () => void): void {
		this.busy = busy;
		this.note = undefined;
		this.saved = false;
		this.deps.requestRender();
		const settle = (error?: string) => {
			this.busy = undefined;
			this.note = error ? { text: `${fail}: ${error}`, ok: false } : ok ? { text: ok, ok: true } : undefined;
			if (!error) then?.();
			this.reload(select);
		};
		try {
			write().then((r) => settle(typeof r === "string" ? r : undefined), (e) => settle(e instanceof Error ? e.message : String(e)));
		} catch (e) {
			settle(e instanceof Error ? e.message : String(e));
		}
	}

	/** An edit of the chosen profile's draft; refused on default (agents.yaml) and + New. */
	private editable(): ProfilesData["profiles"][number] | undefined {
		const p = this.chosen();
		if (p?.name === "default") this.note = { text: READ_ONLY, ok: false };
		return p && p.name !== "default" ? p : undefined;
	}

	handleInput(data: string): boolean {
		if (this.busy) return true;
		const m = this.mode;
		const up = matchesKey(data, Key.up), down = matchesKey(data, Key.down), enter = matchesKey(data, Key.enter), esc = matchesKey(data, Key.escape);
		switch (m.kind) {
			case "profiles": {
				if (!this.data) return false;
				const count = this.data.profiles.length;
				const p = this.chosen();
				const rows = this.rows();
				const row = rows[this.row];
				const left = matchesKey(data, Key.left), right = matchesKey(data, Key.right);
				if (left || right) {
					this.tab = Math.max(0, Math.min(count, this.tab + (left ? -1 : 1)));
					this.row = 0;
					this.note = undefined;
					this.saved = false;
				} else if (up || data === "k") this.row = Math.max(0, this.row - 1);
				else if (down || data === "j") this.row = Math.min(Math.max(0, rows.length - 1), this.row + 1);
				else if (data === "n" || (enter && !p)) this.mode = { kind: "name", text: "" };
				else if (!p) return false;
				else if (data === "w") {
					if (this.editable()) {
						this.draft(p.name).mode = this.modeOf(p) === "inline" ? "writer" : "inline";
						[this.note, this.saved] = [undefined, false];
					}
				} else if (enter || data === "e") {
					if (!this.editable()) { /* refused, the note says why */ }
					else if (!row) this.note = { text: `${p.name} names no orchestrator model in ${this.deps.runtime}.`, ok: false };
					else if (enter) {
						const bare = split(this.value(row))[0];
						this.mode = { kind: "model", agent: row.agent, cursor: Math.max(0, this.filtered("").indexOf(bare)), filter: "" };
					} else {
						const efforts = this.efforts();
						this.mode = { kind: "effort", agent: row.agent, cursor: Math.max(0, efforts.indexOf(split(this.value(row))[1] || "medium")) };
					}
				} else if (data === "s") {
					if (this.editable()) this.save(p.name);
				} else if (data === "u") {
					const name = p.name;
					this.run(`using ${name}…`, () => this.deps.useNow(name), `${name} is used in this session.`, `Could not use ${name}`, name, () => { this.session = this.own = name; });
				} else if (data === "d") {
					const name = p.name;
					this.run(`making ${name} the default…`, () => this.deps.makeDefault(name), `${name} is the default for new sessions.`, `Could not make ${name} the default`, name);
				} else if (data === "x" || matchesKey(data, Key.delete)) {
					if (p.name === "default") this.note = { text: "default is agents.yaml: it cannot be deleted.", ok: false };
					else if (p.name === this.data.active) this.note = { text: `${p.name} is the default: d makes another the default first.`, ok: false };
					else this.mode = { kind: "remove", name: p.name };
				} else return false;
				break;
			}
			case "model": {
				const models = this.filtered(m.filter);
				if (esc) this.mode = { kind: "profiles" };
				else if (up) m.cursor = Math.max(0, m.cursor - 1);
				else if (down) m.cursor = Math.min(Math.max(0, models.length - 1), m.cursor + 1);
				else if (enter && models[m.cursor]) {
					const row = this.rows().find((r) => r?.agent === m.agent)!;
					const effort = split(this.value(row))[1];
					this.setDraft(m.agent, effort ? `${models[m.cursor]}@${effort}` : models[m.cursor]!);
				} else if (matchesKey(data, Key.backspace)) [m.filter, m.cursor] = [m.filter.slice(0, -1), 0];
				else if (printable(data)) [m.filter, m.cursor] = [m.filter + data, 0];
				break;
			}
			case "effort": {
				const efforts = this.efforts();
				if (esc) this.mode = { kind: "profiles" };
				else if (up || data === "k") m.cursor = Math.max(0, m.cursor - 1);
				else if (down || data === "j") m.cursor = Math.min(Math.max(0, efforts.length - 1), m.cursor + 1);
				else if (enter && efforts[m.cursor]) {
					const row = this.rows().find((r) => r?.agent === m.agent)!;
					this.setDraft(m.agent, `${split(this.value(row))[0]}@${efforts[m.cursor]}`);
				}
				break;
			}
			case "name": {
				if (esc) this.mode = { kind: "profiles" };
				else if (enter && m.text.trim()) {
					const name = m.text.trim(), from = this.chosen()?.name ?? this.data?.active ?? "default";
					this.mode = { kind: "profiles" };
					this.run(`creating ${name}…`, () => this.deps.create(name, from), `${name} created as a copy of ${from}.`, `Could not create ${name}`, name);
				} else if (matchesKey(data, Key.backspace)) m.text = m.text.slice(0, -1);
				else if (printable(data) && m.text.length < 64) m.text += data;
				break;
			}
			case "remove": {
				const name = m.name;
				this.mode = { kind: "profiles" };
				if (data === "y") {
					this.drafts.delete(name);
					this.run(`deleting ${name}…`, () => this.deps.remove(name), `${name} deleted.`, `Could not delete ${name}`);
				} else this.note = { text: "Cancelled: nothing changed.", ok: true };
				break;
			}
		}
		this.deps.requestRender();
		return true;
	}

	private setDraft(agent: string, value: string): void {
		this.draft(this.chosen()!.name).models.set(agent, value);
		this.mode = { kind: "profiles" };
		[this.note, this.saved] = [undefined, false];
	}

	/** s: the draft's mode (deps.mode), then each changed agent (deps.set), one call each; Saved when all went through. */
	private save(profile: string): void {
		const c = this.changes();
		if (!c.mode && !c.models.length) {
			this.note = { text: "Nothing to save: the draft is the file.", ok: true };
			return;
		}
		const runtime = this.deps.runtime;
		// A model this session's pi does not list as available: its provider is not loaded here (its package comes with
		// the save and loads in the next session) or has no credential, which no new session fixes (L920 N6).
		const later = [...new Set(c.models.map(([, value]) => split(value)[0]))].filter((model) => model && this.deps.inSession?.(model) === false);
		this.run(`saving ${profile}…`, async () => {
			if (c.mode) await this.deps.mode(profile, c.mode);
			for (const [agent, value] of c.models) await this.deps.set(profile, agent, runtime, value);
		}, undefined, `Could not save ${profile}`, profile, () => {
			this.drafts.delete(profile);
			this.saved = true;
			if (later.length) this.note = { text: `${later.join(", ")} ${later.length === 1 ? "is" : "are"} not among this session's available models: ${later.length === 1 ? "it runs" : "they run"} once pi lists ${later.length === 1 ? "it" : "them"}, with ${later.length === 1 ? "its provider" : "their providers"} loaded and a credential for ${later.length === 1 ? "it" : "each"}.`, ok: true };
		});
	}

	private efforts(): string[] {
		return this.data?.efforts?.[this.deps.runtime] ?? [];
	}

	private filtered(filter: string): string[] {
		const all = this.data ? this.deps.models(this.deps.runtime, this.data) : [];
		const words = filter.toLowerCase().split(/\s+/).filter(Boolean);
		return words.length ? all.filter((model) => words.every((w) => model.toLowerCase().includes(w))) : all;
	}

	/** The line the panel keeps in view: the chosen row, or the picker's chosen option. */
	cursorRow(): number {
		const at = this.row === 0 ? 4 : 6 + this.row;
		const m = this.mode;
		if (m.kind === "model") return at + 2 + Math.min(m.cursor, PICKER_ROWS);
		if (m.kind === "effort") return at + 2 + m.cursor;
		return at;
	}

	/** A delegation mode's icon. */
	private icon(mode: Delegation, ascii: boolean): string {
		return mode === "writer" ? (ascii ? "=" : "⇄") : (ascii ? "o" : "⊙");
	}

	lines(paint: ProfilesPaint, width = 76, ascii = false): string[] {
		if (!this.data) return [this.error ? paint.fg("error", `Could not read the profiles: ${this.error}`) : paint.fg("dim", "loading…")];
		const d = this.data;
		const m = this.mode;
		const p = this.chosen();
		// The tabs: ` name icon★ `, the chosen one underlined by the rule's heavy run.
		// T120: the session's profile carries its mark while it is not the default.
		const own = this.own !== d.active ? this.own : undefined;
		const dot = ascii ? "@" : "●";
		const tabs = [...d.profiles.map((q) => `${q.name} ${this.icon(this.modeOf(q), ascii)}${q.name === d.active ? (ascii ? "*" : "★") : ""}${q.name === own ? dot : ""}`), "+ New"];
		let at = 0, from = 0, span = 0;
		const painted = tabs.map((label, i) => {
			const seg = ` ${label} `;
			if (i === this.tab) [from, span] = [at, visibleWidth(seg)];
			at += visibleWidth(seg);
			return i === this.tab ? paint.bold(paint.fg("accent", seg)) : paint.fg(i === tabs.length - 1 ? "accent" : "muted", seg);
		});
		const thin = ascii ? "-" : "─", heavy = ascii ? "=" : "━";
		const out: string[] = [
			painted.join(""),
			paint.fg("border", thin.repeat(from)) + paint.fg("accent", heavy.repeat(span)) + paint.fg("border", thin.repeat(Math.max(0, width - from - span))),
		];
		const legend = `${this.icon("inline", ascii)} inline · ${this.icon("writer", ascii)} writer · w switches${own ? ` · ${dot} this session only` : ""}`;
		if (!p) {
			out.push(`${paint.fg("accent", "New profile")}   ${paint.fg("dim", `enter or n names one, a copy of ${d.active}.`)}`);
		} else {
			const here = p.name === this.session, def = p.name === d.active;
			const said = here && def ? "Used in this session. Default for new sessions." : here ? "Used in this session." : def ? "Default for new sessions." : "Not in use.";
			const what = p.name === "default" ? `${said} agents.yaml, read-only.` : said;
			const left = `${paint.fg("accent", p.name)}   ${paint.fg("dim", what)}`;
			out.push(`${left}${" ".repeat(Math.max(2, width - visibleWidth(left) - visibleWidth(legend)))}${paint.fg("dim", legend)}`);
			out.push("");
			const rows = this.rows();
			const agents = rows.slice(1) as ProfileRow[];
			const nameW = Math.max(visibleWidth("Orchestrator"), ...agents.map((a) => visibleWidth(a.agent)));
			const modelW = Math.max(visibleWidth("Model ▾"), ...rows.filter(Boolean).map((r) => visibleWidth(split(this.value(r!))[0])));
			const pad = (s: string, w: number) => s + " ".repeat(Math.max(0, w - visibleWidth(s)));
			const mark = (i: number) => i === this.row ? paint.fg("accent", ascii ? "> " : "▸ ") : "  ";
			const changed = (r: ProfileRow) => this.drafts.get(p.name)?.models.has(r.agent) && this.value(r) !== r.model;
			const orch = rows[0];
			const mode = this.modeOf(p);
			const modeText = paint.fg("accent", `${this.icon(mode, ascii)} ${mode}`);
			if (orch) {
				const [model, effort] = split(this.value(orch));
				const text = changed(orch) ? paint.bold(model) : model;
				out.push(`${mark(0)}${paint.fg("accent", pad("Orchestrator", nameW))}  ${pad(text, modelW + visibleWidth(text) - visibleWidth(model))}  ${paint.fg("accent", effort || "-")}  ${modeText}`);
			} else out.push(`${mark(0)}${paint.fg("accent", pad("Orchestrator", nameW))}  ${paint.fg("dim", pad(`no model in ${this.deps.runtime}`, modelW))}  ${modeText}`);
			if (this.row === 0) out.push(...this.picker(paint, ascii));
			out.push(paint.fg("border", thin.repeat(Math.min(width, 2 + nameW + 2 + modelW + 2 + 8))));
			out.push(paint.fg("dim", `  ${pad("Agent", nameW)}  ${pad(`Model ${ascii ? "v" : "▾"}`, modelW)}  Effort ${ascii ? "v" : "▾"}`));
			for (const [i, a] of agents.entries()) {
				const [model, effort] = split(this.value(a));
				const text = changed(a) ? paint.bold(model) : model;
				out.push(`${mark(i + 1)}${pad(a.agent, nameW)}  ${pad(text, modelW + visibleWidth(text) - visibleWidth(model))}  ${paint.fg("accent", effort || "-")}`);
				if (this.row === i + 1) out.push(...this.picker(paint, ascii));
			}
		}
		if (m.kind === "name") out.push("", `${paint.fg("accent", "new profile name:")} ${m.text}${paint.fg("accent", ascii ? "|" : "▏")} ${paint.fg("dim", `· copy of ${p?.name ?? d.active}`)}`);
		if (m.kind === "remove") out.push("", paint.fg("warning", `Delete profile ${m.name}? y deletes · any other key cancels`));
		// The actions bar: Save dim while the draft is the file; Saved at the right after a save.
		const dirty = this.dirty();
		const bar = p ? `${paint.fg("accent", "[ u · Use in this session ]")}  ${paint.fg("accent", "[ d · Make default ]")}  ${paint.fg(dirty ? "accent" : "dim", "[ s · Save ]")}` : "";
		const right = this.busy ? paint.fg("dim", this.busy) : this.saved ? paint.fg("success", "Saved") : "";
		out.push("", right ? `${bar}${" ".repeat(Math.max(2, width - visibleWidth(bar) - visibleWidth(right)))}${right}` : bar);
		// The note wraps onto further rows, NOTE_ROWS at most, the last one cut: a refusal's second half says what to do.
		if (this.note) {
			const text = (ascii ? asciiText(this.note.text) : this.note.text).replace(/\s*[\r\n]+\s*/g, " ");
			const rows = wrapTextWithAnsi(text, Math.max(1, width));
			const last = rows[NOTE_ROWS - 1] ?? "";
			const shown = rows.length > NOTE_ROWS ? [...rows.slice(0, NOTE_ROWS - 1), `${visibleWidth(last) < width ? last : wrapTextWithAnsi(last, Math.max(1, width - 1))[0] ?? ""}${ascii ? "~" : "…"}`] : rows;
			out.push(...shown.map((row) => paint.fg(this.note!.ok ? "success" : "warning", row)));
		}
		return out;
	}

	/** The open picker's rows, under the row it sets. */
	private picker(paint: ProfilesPaint, ascii: boolean): string[] {
		const m = this.mode;
		const out: string[] = [];
		const cur = ascii ? "> " : "▸ ";
		if (m.kind === "model") {
			const models = this.filtered(m.filter);
			out.push(`    ${paint.fg("accent", "model:")} ${m.filter || paint.fg("dim", "type to filter")}${paint.fg("accent", ascii ? "|" : "▏")} ${paint.fg("dim", models.length === 1 ? "1 available" : `${models.length} available`)}`);
			const start = Math.max(0, Math.min(m.cursor - Math.floor(PICKER_ROWS / 2), models.length - PICKER_ROWS));
			for (const [i, model] of models.slice(start, start + PICKER_ROWS).entries()) {
				const here = start + i === m.cursor;
				out.push(`    ${here ? paint.fg("accent", cur) : "  "}${here ? paint.bold(model) : model}`);
			}
			if (!models.length) out.push(paint.fg("dim", "      none match"));
		} else if (m.kind === "effort") {
			out.push(`    ${paint.fg("accent", "effort:")}`);
			for (const [i, e] of this.efforts().entries()) {
				const here = i === m.cursor;
				out.push(`    ${here ? paint.fg("accent", cur) : "  "}${here ? paint.bold(e) : e}`);
			}
		}
		return out;
	}

	/** The keys, `[key, action]`; the panel adds esc close. */
	keys(): string[][] {
		switch (this.mode.kind) {
			case "model":
				return [["↑↓", "model"], ["enter", "choose"], ["esc", "back"]];
			case "effort":
				return [["↑↓", "effort"], ["enter", "choose"], ["esc", "back"]];
			case "name":
				return [["enter", "create"], ["esc", "cancel"]];
			case "remove":
				return [["y", "delete"], ["esc", "cancel"]];
		}
		if (this.data && !this.chosen()) return [["←→", "profile"], ["enter", "new"]];
		return [["←→", "profile"], ["↑↓", "agent"], ["enter", "model"], ["e", "effort"], ["w", "mode"], ["s", "save"], ["u", "use"]];
	}
}
