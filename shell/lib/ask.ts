/**
 * pi/ask.ts — the `ask_user` tool: the model puts one to four questions to
 * the owner, as Claude Code's AskUserQuestion does.
 *
 * Each question offers two to four options, each a label and an optional
 * description, or none for a free answer. A single choice is one selection;
 * a multiple choice toggles options until the owner picks Done. Every
 * question with options also offers "Write my own answer.", which takes typed text. The
 * questions go through pi's own dialogs, one after another; dismissing one
 * ends the round, and the answers given so far are returned.
 *
 * Only the interactive main session can ask: a child or a session without a
 * UI has no one to answer, so the call fails and says so.
 *
 * A choice is asked in the question view (lib/ui/ask-view.ts), drawn as the
 * Gentle Shell's questionnaire in the dock; a UI without ui.custom keeps
 * pi's select dialog.
 * Both return the same strings.
 */

import { AskView, type AskItem, type AskViewOptions } from "./ui/ask-view.ts";
import { shellSettings } from "./settings.ts";
import { createNativeFullscreenInteraction } from "./ui/native-fullscreen-interaction.ts";

export const MAX_QUESTIONS = 4;
export const MIN_OPTIONS = 2;
export const MAX_OPTIONS = 4;
export const MAX_HEADER = 12;
/** G's always-available free-text row (its CUSTOM_ROW_LABEL). */
export const OTHER = "Write my own answer.";
export const DONE = "Done";

export type AskOption = { label: string; description?: string };
export type AskQuestion = { question: string; header?: string; options: AskOption[]; multiSelect: boolean };
export type AskParams = { questions?: unknown };
export type AskOutcome = { ok: true; text: string } | { ok: false; text: string };

/** The questions `params` asks, or why they cannot be asked. */
export function parseQuestions(params: AskParams): AskQuestion[] | string {
  const list = params.questions;
  if (!Array.isArray(list) || list.length < 1 || list.length > MAX_QUESTIONS) return `ask_user needs 1 to ${MAX_QUESTIONS} questions`;
  const out: AskQuestion[] = [];
  for (const [i, raw] of list.entries()) {
    const at = `question ${i + 1}`;
    const q = raw as Record<string, unknown> | null;
    if (!q || typeof q.question !== "string" || !q.question.trim()) return `ask_user: ${at} has no question`;
    if (q.header !== undefined && (typeof q.header !== "string" || q.header.length > MAX_HEADER)) return `ask_user: ${at}'s header must be text of at most ${MAX_HEADER} characters`;
    const options = q.options === undefined ? [] : q.options;
    if (!Array.isArray(options) || (options.length > 0 && (options.length < MIN_OPTIONS || options.length > MAX_OPTIONS))) {
      return `ask_user: ${at} offers ${MIN_OPTIONS} to ${MAX_OPTIONS} options, or none for a free answer`;
    }
    const parsed: AskOption[] = [];
    for (const o of options as Array<Record<string, unknown> | null>) {
      if (!o || typeof o.label !== "string" || !o.label.trim()) return `ask_user: every option of ${at} needs a label`;
      if (o.description !== undefined && typeof o.description !== "string") return `ask_user: an option's description of ${at} must be text`;
      if (parsed.some((p) => p.label === o.label)) return `ask_user: ${at} repeats the option "${o.label}"`;
      parsed.push({ label: o.label, ...(typeof o.description === "string" && o.description.trim() ? { description: o.description } : {}) });
    }
    if (q.multiSelect !== undefined && typeof q.multiSelect !== "boolean") return `ask_user: ${at}'s multiSelect must be true or false`;
    if (q.multiSelect === true && parsed.length === 0) return `ask_user: ${at} is a multiple choice with no options`;
    out.push({ question: q.question, ...(typeof q.header === "string" && q.header.trim() ? { header: q.header } : {}), options: parsed, multiSelect: q.multiSelect === true });
  }
  return out;
}

const shown = (o: AskOption) => (o.description ? `${o.label} — ${o.description}` : o.label);
const title = (q: AskQuestion, i: number, n: number) => `${n > 1 ? `(${i + 1}/${n}) ` : ""}${q.header ? `${q.header}: ` : ""}${q.question}`;
const blank = (answer: unknown) => answer === undefined || answer === null || (typeof answer === "string" && !answer.trim());

/** Puts one choice to the owner: the items, and a note for a multiple choice; resolves to the picked item's value. */
type Pick = (items: AskItem[], note?: string) => Promise<unknown>;

/** pi's select dialog: the heading, then the note under it. */
const selectPick = (ui: any, heading: string, opts: { signal?: AbortSignal }): Pick =>
  (items, note) => ui.select(note ? `${heading}\n${note}` : heading, items.map((item) => item.value), opts);

/** The question view, through ui.custom in the dock (never an overlay, as G's) inside G's native fullscreen interaction; an abort dismisses it. */
const viewPick = (ui: any, q: AskQuestion, step: number, total: number, opts: { signal?: AbortSignal }): Pick =>
  (items, note) => ui.custom((tui: { requestRender?(): void } | undefined, theme: AskViewOptions["theme"], _keys: unknown, done: (value: string | undefined) => void) => {
    const view = new AskView({ step, total, title: q.header ?? "Question", question: q.question, items, multi: note !== undefined, theme, done, ascii: shellSettings().ascii() });
    opts.signal?.addEventListener("abort", () => done(undefined), { once: true });
    // As G's ask_user_question: keys go to the view, the pointer through native Container dispatch.
    const interaction = createNativeFullscreenInteraction({ keyboardTarget: view, requestRender: () => tui?.requestRender?.() });
    interaction.addChild(view);
    return interaction;
  });

const optionItem = (o: AskOption): AskItem => ({ value: shown(o), label: o.label, ...(o.description ? { description: o.description } : {}) });
const plainItem = (value: string): AskItem => ({ value, label: value });

/** The owner's answer to one question, or undefined when they dismissed it. */
async function ask(q: AskQuestion, heading: string, ui: any, opts: { signal?: AbortSignal }, pick: Pick): Promise<string | undefined> {
  const typed = async () => {
    const text = await ui.input(heading, "", opts);
    return blank(text) ? undefined : `${String(text).trim()} (typed)`;
  };
  if (q.options.length === 0) {
    const text = await ui.input(heading, "", opts);
    return blank(text) ? undefined : String(text).trim();
  }
  if (!q.multiSelect) {
    const choice = await pick([...q.options.map(optionItem), plainItem(OTHER)]);
    if (blank(choice)) return undefined;
    if (choice === OTHER) return typed();
    return q.options.find((o) => shown(o) === choice)?.label;
  }
  // A multiple choice toggles one option per selection, until Done.
  const picked = new Set<string>();
  const typedAnswers: string[] = [];
  for (;;) {
    const mark = (o: AskOption) => (picked.has(o.label) ? "[x]" : "[ ]");
    const rows = q.options.map((o) => `${mark(o)} ${shown(o)}`);
    const items = q.options.map((o, i) => ({ ...optionItem(o), value: rows[i]!, label: `${mark(o)} ${o.label}` }));
    const choice = await pick([...items, plainItem(OTHER), plainItem(DONE)], `(pick any; ${DONE} when finished)`);
    if (blank(choice)) return undefined;
    if (choice === DONE) break;
    if (choice === OTHER) {
      const text = await typed();
      if (text) typedAnswers.push(text);
      continue;
    }
    const option = q.options[rows.indexOf(String(choice))];
    if (!option) continue;
    if (picked.has(option.label)) picked.delete(option.label);
    else picked.add(option.label);
  }
  const answers = [...q.options.filter((o) => picked.has(o.label)).map((o) => o.label), ...typedAnswers];
  return answers.length ? answers.join("; ") : "(none chosen)";
}

export async function askUser(params: AskParams, ctx: any, child: boolean, signal?: AbortSignal): Promise<AskOutcome> {
  const questions = parseQuestions(params);
  if (typeof questions === "string") return { ok: false, text: questions };
  if (child) {
    return { ok: false, text: "ask_user: this is a sub-agent, and no one can answer it here; decide from the task, or end and report the open question to the session that started you" };
  }
  if (!ctx?.hasUI || !ctx.ui) {
    return { ok: false, text: "ask_user: this session is not interactive, so no one can answer; decide from what you have, or end and report the open question" };
  }
  const answers: string[] = [];
  // The question view, through ui.custom; a UI without it gets pi's select dialog.
  const sections = typeof ctx.ui.custom === "function";
  for (const [i, q] of questions.entries()) {
    const opts = signal ? { signal } : {};
    const heading = title(q, i, questions.length);
    const pick = sections ? viewPick(ctx.ui, q, i + 1, questions.length, opts) : selectPick(ctx.ui, heading, opts);
    const answer = await ask(q, heading, ctx.ui, opts, pick);
    if (answer === undefined) {
      const given = answers.length ? ` Answered before that:\n${answers.join("\n")}` : "";
      return { ok: true, text: `The owner dismissed "${q.question}" without answering, and was asked nothing more.${given}` };
    }
    answers.push(`- ${q.question} → ${answer}`);
  }
  return { ok: true, text: `The owner answered:\n${answers.join("\n")}` };
}
