/**
 * shell/lib/ask.ts — the `ask_user` tool's work: the model puts one to four
 * questions to the owner and waits for the answers.
 *
 * A question offers two to four options (a label, and what choosing it
 * means), or none, and then takes a typed answer. A single choice is one
 * pick; a multiple choice is put again after each pick, which toggles that
 * option, until the owner picks Done. Every question with options also
 * offers "Write my own answer." (T170): its text comes back as
 * `<text> (typed)`. The questions go one after another; a dismissed one ends
 * the round, and what was answered before it is returned.
 *
 * Where the UI can draw a component (ui.custom), a choice is the question
 * view in the dock (lib/ui/ask-view.ts, design/shell-tui 08); elsewhere pi's
 * select dialog, with the same strings. A typed or free answer is pi's input
 * dialog. Only the interactive main session asks: a sub-agent or a session
 * with no UI has no one to answer, and the call says so.
 */
import { AskView, type AskItem, type AskViewOptions } from "./ui/ask-view.ts";
import { DockedView } from "./ui/overlay.ts";
import { shellSettings } from "./settings.ts";

/** One call's limits: questions per call, options per question (a range), characters in a header. */
export const {
	questions: MAX_QUESTIONS,
	fewestOptions: MIN_OPTIONS,
	mostOptions: MAX_OPTIONS,
	headerChars: MAX_HEADER,
} = { questions: 4, fewestOptions: 2, mostOptions: 4, headerChars: 12 } as const;
/** The row that takes a typed answer (T170). */
export const OTHER = "Write my own answer.";
export const DONE = "Done";

export type AskOption = { label: string; description?: string };
export type AskQuestion = { question: string; header?: string; options: AskOption[]; multiSelect: boolean };
export type AskParams = { questions?: unknown };
export type AskOutcome = { ok: true; text: string } | { ok: false; text: string };

const isText = (value: unknown): value is string => typeof value === "string";
const filled = (value: unknown): value is string => isText(value) && value.trim() !== "";

/** One question's options, or why they cannot be put. */
function readOptions(raw: unknown, at: string): AskOption[] | string {
	const list = raw === undefined ? [] : raw;
	if (!Array.isArray(list) || (list.length > 0 && (list.length < MIN_OPTIONS || list.length > MAX_OPTIONS))) {
		return `ask_user: ${at} offers ${MIN_OPTIONS} to ${MAX_OPTIONS} options, or none for a free answer`;
	}
	const options: AskOption[] = [];
	for (const entry of list as Array<Record<string, unknown> | null>) {
		if (!entry || !filled(entry.label)) return `ask_user: every option of ${at} needs a label`;
		if (entry.description !== undefined && !isText(entry.description)) return `ask_user: an option's description of ${at} must be text`;
		if (options.some((option) => option.label === entry.label)) return `ask_user: ${at} repeats the option "${entry.label}"`;
		options.push(filled(entry.description) ? { label: entry.label, description: entry.description } : { label: entry.label });
	}
	return options;
}

/** The questions `params` asks, or why they cannot be asked. */
export function parseQuestions(params: AskParams): AskQuestion[] | string {
	const list = params.questions;
	if (!Array.isArray(list) || list.length < 1 || list.length > MAX_QUESTIONS) return `ask_user needs 1 to ${MAX_QUESTIONS} questions`;
	const questions: AskQuestion[] = [];
	for (let i = 0; i < list.length; i++) {
		const at = `question ${i + 1}`;
		const raw = list[i] as Record<string, unknown> | null;
		if (!raw || !filled(raw.question)) return `ask_user: ${at} has no question`;
		if (raw.header !== undefined && (!isText(raw.header) || raw.header.length > MAX_HEADER)) return `ask_user: ${at}'s header must be text of at most ${MAX_HEADER} characters`;
		const options = readOptions(raw.options, at);
		if (isText(options)) return options;
		if (raw.multiSelect !== undefined && typeof raw.multiSelect !== "boolean") return `ask_user: ${at}'s multiSelect must be true or false`;
		if (raw.multiSelect === true && options.length === 0) return `ask_user: ${at} is a multiple choice with no options`;
		questions.push({ question: raw.question, ...(filled(raw.header) ? { header: raw.header } : {}), options, multiSelect: raw.multiSelect === true });
	}
	return questions;
}

/** An option as pi's select dialog lists it, and as its pick comes back. */
const asListed = (option: AskOption) => (option.description ? `${option.label} — ${option.description}` : option.label);
/** The heading a dialog shows: `(n/m) ` with more than one question, then `Header: ` when there is one. */
const headingOf = (q: AskQuestion, index: number, count: number) => `${count > 1 ? `(${index + 1}/${count}) ` : ""}${q.header ? `${q.header}: ` : ""}${q.question}`;
const unanswered = (answer: unknown) => answer === undefined || answer === null || (isText(answer) && answer.trim() === "");

/** Puts one choice: its items, and a note for a multiple choice; resolves to the picked item's value. */
type Choose = (items: AskItem[], note?: string) => Promise<unknown>;
type Signal = { signal?: AbortSignal };

/** pi's select dialog: the heading, with the note on the line under it. */
function dialogChoice(ui: any, heading: string, opts: Signal): Choose {
	return (items, note) => ui.select(note === undefined ? heading : `${heading}\n${note}`, items.map((item) => item.value), opts);
}

/** The question view in the dock; an abort of the call dismisses it. */
function viewChoice(ui: any, q: AskQuestion, step: number, total: number, opts: Signal): Choose {
	return (items, note) => ui.custom((tui: { requestRender?(): void } | undefined, theme: AskViewOptions["theme"], _keys: unknown, done: (value: string | undefined) => void) => {
		opts.signal?.addEventListener("abort", () => done(undefined), { once: true });
		const view = new AskView({
			step, total, title: q.header ?? "Question", question: q.question, items, multi: note !== undefined, theme, done,
			ascii: shellSettings().ascii(), requestRender: () => tui?.requestRender?.(),
		});
		return new DockedView(view);
	});
}

const optionItem = (option: AskOption): AskItem => ({ value: asListed(option), label: option.label, ...(option.description ? { description: option.description } : {}) });
const wordItem = (word: string): AskItem => ({ value: word, label: word });

/** The owner's answer to one question, or undefined when they dismissed it. */
async function answer(q: AskQuestion, heading: string, ui: any, opts: Signal, choose: Choose): Promise<string | undefined> {
	const typed = async () => {
		const text = await ui.input(heading, "", opts);
		return unanswered(text) ? undefined : String(text).trim();
	};
	if (q.options.length === 0) return typed();
	if (!q.multiSelect) {
		const picked = await choose([...q.options.map(optionItem), wordItem(OTHER)]);
		if (unanswered(picked)) return undefined;
		if (picked === OTHER) {
			const text = await typed();
			return text === undefined ? undefined : `${text} (typed)`;
		}
		return q.options.find((option) => asListed(option) === picked)?.label;
	}
	const chosen = new Set<string>();
	const typedOnes: string[] = [];
	for (;;) {
		const box = (option: AskOption) => (chosen.has(option.label) ? "[x]" : "[ ]");
		const listed = q.options.map((option) => `${box(option)} ${asListed(option)}`);
		const items = q.options.map((option, i) => ({ ...optionItem(option), value: listed[i]!, label: `${box(option)} ${option.label}` }));
		const picked = await choose([...items, wordItem(OTHER), wordItem(DONE)], `(pick any; ${DONE} when finished)`);
		if (unanswered(picked)) return undefined;
		if (picked === DONE) break;
		if (picked === OTHER) {
			const text = await typed();
			if (text !== undefined) typedOnes.push(`${text} (typed)`);
			continue;
		}
		const option = q.options[listed.indexOf(String(picked))];
		if (!option) continue;
		if (!chosen.delete(option.label)) chosen.add(option.label);
	}
	const all = [...q.options.filter((option) => chosen.has(option.label)).map((option) => option.label), ...typedOnes];
	return all.length ? all.join("; ") : "(none chosen)";
}

export async function askUser(params: AskParams, ctx: any, child: boolean, signal?: AbortSignal): Promise<AskOutcome> {
	const questions = parseQuestions(params);
	if (isText(questions)) return { ok: false, text: questions };
	if (child) {
		return { ok: false, text: "ask_user: this is a sub-agent, and no one can answer it here; decide from the task, or end and report the open question to the session that started you" };
	}
	if (!ctx?.hasUI || !ctx.ui) {
		return { ok: false, text: "ask_user: this session is not interactive, so no one can answer; decide from what you have, or end and report the open question" };
	}
	const drawn = typeof ctx.ui.custom === "function";
	const opts: Signal = signal ? { signal } : {};
	const given: string[] = [];
	for (let i = 0; i < questions.length; i++) {
		const q = questions[i]!;
		const heading = headingOf(q, i, questions.length);
		const choose = drawn ? viewChoice(ctx.ui, q, i + 1, questions.length, opts) : dialogChoice(ctx.ui, heading, opts);
		const reply = await answer(q, heading, ctx.ui, opts, choose);
		if (reply === undefined) {
			const before = given.length ? ` Answered before that:\n${given.join("\n")}` : "";
			return { ok: true, text: `The owner dismissed "${q.question}" without answering, and was asked nothing more.${before}` };
		}
		given.push(`- ${q.question} → ${reply}`);
	}
	return { ok: true, text: `The owner answered:\n${given.join("\n")}` };
}
