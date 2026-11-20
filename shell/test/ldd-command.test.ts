/**
 * pi's /jhon-ldd (L194): the shell's command in place of the prompt
 * template the render wrote to <agent dir>/prompts/jhon-ldd.md. It lists
 * each action with one line from the template's own table, and sends the
 * agent exactly what pi made of the template.
 *
 *   jhon test
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
// pi's own template loader and expansion, the code that ran the template.
import { expandPromptTemplate, loadPromptTemplates } from "../node_modules/@earendil-works/pi-coding-agent/dist/core/prompt-templates.js";
import { LDD_SKILL, lddActions, lddCompletions, lddPrompt } from "../lib/ldd-command.ts";

const skill = readFileSync(LDD_SKILL, "utf8");

/** The template as the render wrote it (engine/internal/render, piPromptTemplate): the skill's description, then its body. */
function renderedTemplate(text: string): string {
	const m = /^---\n([\s\S]*?)\n---\n([\s\S]*)$/.exec(text)!;
	const description = /^description:\s*(.+)$/m.exec(m[1]!)![1]!.trim().replace(/^"(.*)"$/, "$1");
	return `---\ndescription: ${JSON.stringify(description)}\n---\n${m[2]!.replace(/^\n+/, "")}`;
}

test("/jhon-ldd sends byte for byte what pi made of the template, for every argument shape", (t) => {
	const dir = mkdtempSync(join(tmpdir(), "jhon-ldd-template-"));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	mkdirSync(join(dir, "prompts"));
	writeFileSync(join(dir, "prompts", "jhon-ldd.md"), renderedTemplate(skill));
	const { templates } = loadPromptTemplates({ cwd: dir, agentDir: dir, promptPaths: [], includeDefaults: true });
	assert.deepEqual(templates.map((tpl: { name: string }) => tpl.name), ["jhon-ldd"]);
	for (const args of ["", "new abc-process", "  apply   abc-process  ", 'init "npm test" --x', "continue $1 ${@:2}"]) {
		const typed = args ? `/jhon-ldd ${args}` : "/jhon-ldd";
		assert.equal(lddPrompt(skill, args), expandPromptTemplate(typed, templates), JSON.stringify(args));
	}
	assert.match(lddPrompt(skill, "new x"), /^Arguments: `new x`\./);
});

test("/jhon-ldd's actions are the template's, in its order, each with the first clause of its own line", () => {
	assert.deepEqual(lddActions(skill), [
		{ value: "new", label: "new", description: "Write the change's intent and acceptance criteria in tasks.md ## Intent and outline every slice, tasking the first" },
		{ value: "ff", label: "ff", description: "Run propose, then the first slice's spec and tasks, skipping any whose artifact is already done in jhon status" },
		{ value: "continue", label: "continue", description: "Run jhon status <change> --brief and run the suggested next phase for that change, unless the owner says otherwise" },
		{ value: "status", label: "status", description: "Run jhon status <change> from the project root and present its result" },
		{ value: "explore", label: "explore", description: "Explore it (inline or in the jhon-scout agent)" },
		{ value: "research", label: "research", description: "Collect source-backed evidence with the jhon-scout agent" },
		{ value: "apply", label: "apply", description: "Run the apply phase (inline for a small task, or in the jhon-builder agent)" },
		{ value: "verify", label: "verify", description: "Run the verify phase in the jhon-checker agent, independent of the writer" },
		{ value: "archive", label: "archive", description: "Run the archive phase inline" },
		{ value: "init", label: "init", description: "Run the init phase in this project inline" },
	]);
});

test("/jhon-ldd completes the action, then the change after it; nothing once both are typed", () => {
	const changes = [{ value: "abc-process", label: "abc-process", description: "The ABC process." }];
	assert.equal(lddCompletions(skill, "", changes)!.length, 10);
	assert.deepEqual(lddCompletions(skill, "a", changes)!.map((item) => item.value), ["apply", "archive"]);
	assert.deepEqual(lddCompletions(skill, "apply ", changes), [{ value: "apply abc-process", label: "abc-process", description: "The ABC process." }]);
	assert.deepEqual(lddCompletions(skill, "apply a", changes)!.map((item) => item.value), ["apply abc-process"]);
	assert.equal(lddCompletions(skill, "apply x", changes), null);
	assert.equal(lddCompletions(skill, "zz", changes), null);
	assert.equal(lddCompletions(skill, "apply abc-process ", changes), null);
});
