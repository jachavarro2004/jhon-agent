/**
 * Everything ask_user shows the model, captured by scripted rounds: the
 * registered tool (name, label, description, schema), every refusal, and
 * every result, through the question view (ui.custom) and through pi's own
 * dialogs (a UI without ui.custom). ask-model-text.test.ts compares it with
 * the snapshot taken before the T174-S13 rewrite, so the rewrite changes no
 * word the model reads, `<text> (typed)` included.
 *
 * Nothing here reads the environment or the clock: the extension loads with
 * an empty agents directory of its own, made and removed by the capture.
 */
import "./isolated-env.ts";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import jhon from "../extensions/jhon.ts";
import { askUser, parseQuestions } from "../lib/ask.ts";

const plain = { fg: (_role: string, text: string) => text, bg: (_role: string, text: string) => text, bold: (text: string) => text };
const KEY = { up: "\x1b[A", down: "\x1b[B", enter: "\r", space: " ", esc: "\x1b" } as const;
// eslint-disable-next-line no-control-regex
const strip = (text: string) => text.replace(/\x1b\[[0-9;:]*m/g, "");

/** One step of a scripted view: keys, a click on the option labelled so, or an abort of the round. */
type Step = Array<keyof typeof KEY> | { click: string } | "abort";

interface Round {
	name: string;
	params: unknown;
	/** What each question view does, in turn (the view path only). */
	view?: Step[];
	/** What pi's select dialog answers, in turn (the dialog path only). */
	select?: Array<string | undefined>;
	/** What pi's input dialog answers, in turn. */
	input?: Array<string | undefined>;
	child?: boolean;
	ctx?: "headless" | "no-ui";
}

const ROUNDS: Round[] = [
	{ name: "single by keys", params: { questions: [{ question: "Which route?", header: "Route", options: [{ label: "A", description: "the first" }, { label: "B" }] }] }, view: [["down", "enter"]], select: ["B"] },
	{ name: "single by click", params: { questions: [{ question: "Which route?", options: [{ label: "A" }, { label: "B" }, { label: "C" }] }] }, view: [{ click: "C" }], select: ["C"] },
	{ name: "described option", params: { questions: [{ question: "Which database?", header: "Database", options: [{ label: "Postgres (Recommended)", description: "keeps the joins" }, { label: "SQLite" }] }] }, view: [["enter"]], select: ["Postgres (Recommended) — keeps the joins"] },
	{ name: "typed answer", params: { questions: [{ question: "Which cache?", options: [{ label: "Redis" }, { label: "None" }] }] }, view: [["down", "down", "enter"]], select: ["Write my own answer."], input: ["  Valkey  "] },
	{ name: "typed answer left blank", params: { questions: [{ question: "Which cache?", options: [{ label: "Redis" }, { label: "None" }] }] }, view: [["down", "down", "enter"]], select: ["Write my own answer."], input: ["   "] },
	{ name: "free answer", params: { questions: [{ question: "When should it back up?" }] }, input: ["every night"] },
	{ name: "free answer dismissed", params: { questions: [{ question: "When should it back up?" }] }, input: [undefined] },
	{ name: "three questions", params: { questions: [
		{ question: "Which database?", header: "Database", options: [{ label: "Postgres (Recommended)", description: "keeps the joins" }, { label: "SQLite" }] },
		{ question: "Which cache?", options: [{ label: "Redis" }, { label: "None" }] },
		{ question: "When should it back up?" },
	] }, view: [["enter"], ["down", "down", "enter"]], select: ["Postgres (Recommended) — keeps the joins", "Write my own answer."], input: ["Valkey", "every night"] },
	{ name: "multiple choice", params: { questions: [{ question: "Which checks?", multiSelect: true, options: [{ label: "lint" }, { label: "unit", description: "fast" }, { label: "types" }] }] },
		view: [["enter"], ["down", "space"], ["enter"], ["down", "down", "down", "enter"], ["down", "down", "down", "down", "enter"]],
		select: ["[ ] lint", "[ ] unit — fast", "[x] lint", "Write my own answer.", "Done"], input: ["e2e"] },
	{ name: "multiple choice, none chosen", params: { questions: [{ question: "Which checks?", multiSelect: true, options: [{ label: "lint" }, { label: "unit" }] }] }, view: [["down", "down", "down", "enter"]], select: ["Done"] },
	{ name: "multiple choice dismissed", params: { questions: [{ question: "Which checks?", multiSelect: true, options: [{ label: "lint" }, { label: "unit" }] }] }, view: [["enter"], ["esc"]], select: ["[ ] lint", undefined] },
	{ name: "dismissed second", params: { questions: [{ question: "First?", options: [{ label: "a" }, { label: "b" }] }, { question: "Second?", options: [{ label: "c" }, { label: "d" }] }, { question: "Third?" }] }, view: [["enter"], ["esc"]], select: ["a", undefined] },
	{ name: "dismissed first", params: { questions: [{ question: "First?", options: [{ label: "a" }, { label: "b" }] }] }, view: [["esc"]], select: [undefined] },
	{ name: "aborted", params: { questions: [{ question: "First?", options: [{ label: "a" }, { label: "b" }] }] }, view: ["abort"], select: [undefined] },
	{ name: "child", params: { questions: [{ question: "Which?" }] }, child: true },
	{ name: "headless", params: { questions: [{ question: "Which?" }] }, ctx: "headless" },
	{ name: "no ui", params: { questions: [{ question: "Which?" }] }, ctx: "no-ui" },
];

const REFUSED: unknown[] = [
	undefined, [], Array(5).fill({ question: "q" }), [null], [{ question: " " }], [{ question: 3 }],
	[{ question: "q", header: "far too long a header" }], [{ question: "q", header: 7 }],
	[{ question: "q", options: [{ label: "only" }] }], [{ question: "q", options: Array(5).fill(0).map((_, i) => ({ label: `o${i}` })) }], [{ question: "q", options: "a" }],
	[{ question: "q", options: [{ label: "a" }, { label: " " }] }], [{ question: "q", options: [{ label: "a" }, null] }],
	[{ question: "q", options: [{ label: "a", description: 4 }, { label: "b" }] }], [{ question: "q", options: [{ label: "a" }, { label: "a" }] }],
	[{ question: "q", multiSelect: "yes" }], [{ question: "q", multiSelect: true }],
	[{ question: "ok" }, { question: "" }],
];

/** Drives one question view as `step` says. */
function drive(component: any, step: Step, controller: AbortController): void {
	if (step === "abort") return void controller.abort();
	if (Array.isArray(step)) {
		for (const key of step) component.handleInput(KEY[key]);
		return;
	}
	const width = 60;
	const lines: string[] = component.render(width).map(strip);
	const y = lines.findIndex((line) => line.replace(/^[\s❯>]+/, "").trimEnd() === step.click);
	if (y < 0) throw new Error(`capture: no row ${step.click} in ${JSON.stringify(lines)}`);
	const event = (type: "press" | "click") => ({ type, button: "left", x: 4, y, screenX: 4, screenY: y, width, height: lines.length, shift: false, alt: false, ctrl: false });
	component.handleMouse(event("press"));
	component.handleMouse(event("click"));
}

async function play(round: Round, path: "view" | "dialog"): Promise<Record<string, unknown>> {
	const controller = new AbortController();
	const inputs = [...(round.input ?? [])];
	const selects = [...(round.select ?? [])];
	const views = [...(round.view ?? [])];
	const asked: unknown[] = [];
	const ui: Record<string, unknown> = {
		input: async (title: string, placeholder: string) => { asked.push(["input", title, placeholder]); return inputs.shift(); },
		select: async (title: string, options: string[]) => { asked.push(["select", title, options]); return selects.shift(); },
	};
	if (path === "view") {
		ui.custom = (factory: any) => new Promise((resolve) => {
			const component = factory({ requestRender() {} }, plain, undefined, resolve);
			asked.push(["custom"]);
			const step = views.shift();
			if (!step) throw new Error(`capture: ${round.name} has no step left`);
			setImmediate(() => drive(component, step, controller));
		});
	}
	const ctx = round.ctx === "headless" ? { hasUI: false } : round.ctx === "no-ui" ? { hasUI: true } : { hasUI: true, ui };
	const outcome = await askUser(round.params as never, ctx, round.child ?? false, controller.signal);
	return { round: round.name, path, outcome, asked };
}

/** The tool as the main session registers it, and as a child does. */
function definitions(): unknown {
	const dir = mkdtempSync(join(tmpdir(), "jhon-ask-capture-"));
	mkdirSync(join(dir, "agents"));
	const saved = { agents: process.env.PI_CODING_AGENT_DIR, child: process.env.JHON_PI_CHILD };
	const out: Record<string, unknown> = {};
	try {
		for (const child of [undefined, "1"]) {
			process.env.PI_CODING_AGENT_DIR = dir;
			if (child === undefined) delete process.env.JHON_PI_CHILD;
			else process.env.JHON_PI_CHILD = child;
			const tools: any[] = [];
			jhon({ on: () => {}, registerTool: (t: any) => tools.push(t), registerCommand: () => {}, registerShortcut: () => {}, events: { emit: () => {}, on: () => () => {} } } as any);
			const tool = tools.find((t) => t.name === "ask_user");
			out[child ? "child" : "main"] = tool ? { name: tool.name, label: tool.label, description: tool.description, parameters: tool.parameters } : null;
		}
	} finally {
		if (saved.agents === undefined) delete process.env.PI_CODING_AGENT_DIR; else process.env.PI_CODING_AGENT_DIR = saved.agents;
		if (saved.child === undefined) delete process.env.JHON_PI_CHILD; else process.env.JHON_PI_CHILD = saved.child;
		rmSync(dir, { recursive: true, force: true });
	}
	return out;
}

/** The tool's own answers when it runs: a refusal is thrown, an answer is its text content. */
async function executions(): Promise<unknown[]> {
	const dir = mkdtempSync(join(tmpdir(), "jhon-ask-capture-"));
	mkdirSync(join(dir, "agents"));
	const saved = process.env.PI_CODING_AGENT_DIR;
	const out: unknown[] = [];
	try {
		process.env.PI_CODING_AGENT_DIR = dir;
		const tools: any[] = [];
		delete process.env.JHON_PI_CHILD;
		jhon({ on: () => {}, registerTool: (t: any) => tools.push(t), registerCommand: () => {}, registerShortcut: () => {}, events: { emit: () => {}, on: () => () => {} } } as any);
		const tool = tools.find((t) => t.name === "ask_user");
		const runs: Array<[unknown, unknown]> = [
			[{ questions: [] }, { hasUI: true, ui: { input: async () => "x" } }],
			[{ questions: [{ question: "Which?" }] }, { hasUI: false }],
			[{ questions: [{ question: "Which?" }] }, { hasUI: true, ui: { input: async () => "  soon " } }],
			[{ questions: [{ question: "Which?" }] }, { hasUI: true, ui: { input: async () => undefined } }],
		];
		for (const [params, ctx] of runs) {
			try { out.push({ ok: await tool.execute("1", params, undefined, undefined, ctx) }); } catch (error) { out.push({ error: error instanceof Error ? error.message : String(error) }); }
		}
	} finally {
		if (saved === undefined) delete process.env.PI_CODING_AGENT_DIR; else process.env.PI_CODING_AGENT_DIR = saved;
		rmSync(dir, { recursive: true, force: true });
	}
	return out;
}

export async function capture(): Promise<{ definitions: unknown; refused: unknown[]; rounds: unknown[]; executions: unknown[] }> {
	const refused = REFUSED.map((questions) => parseQuestions({ questions }));
	const rounds: unknown[] = [];
	for (const round of ROUNDS) {
		rounds.push(await play(round, "view"));
		rounds.push(await play(round, "dialog"));
	}
	// As the model gets it: JSON, so an undefined field is no field.
	return JSON.parse(JSON.stringify({ definitions: definitions(), refused, rounds, executions: await executions() }));
}

if (process.argv[1] && import.meta.url.endsWith(process.argv[1].split("/").pop()!) && process.argv.includes("--write")) {
	const { writeFileSync } = await import("node:fs");
	writeFileSync(new URL("./fixtures/ask-model-text.json", import.meta.url), `${JSON.stringify(await capture(), null, "\t")}\n`);
}
