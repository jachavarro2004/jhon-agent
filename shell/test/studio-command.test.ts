/**
 * pi's /jhon:video (odd/tasks/jhon-studio.md S3): jhon-studio's video skill
 * as a command, while integrations.studio is on. It sends the skill's body
 * with the user's arguments, as /jhon-ldd builds its prompt, reading the
 * skill where `jhon-studio integration --json` says it is, at each use.
 *
 *   jhon test
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { lddPrompt } from "../lib/ldd-command.ts";
import { STUDIO_BIN, studioSkill, videoPrompt, type Run } from "../lib/studio-command.ts";

const SKILL = "---\nname: video\ndescription: Make a video.\n---\n\n# Make a video\n\nIf the user named a source, use it.\n";

test("/jhon:video's prompt is the skill's body as /jhon-ldd builds it, the arguments after it as Claude Code appends them", () => {
	assert.equal(videoPrompt(SKILL, ""), lddPrompt(SKILL, ""));
	assert.equal(videoPrompt(SKILL, ""), "# Make a video\n\nIf the user named a source, use it.");
	assert.equal(videoPrompt(SKILL, "  release:v1.2.0  "), "# Make a video\n\nIf the user named a source, use it.\n\nARGUMENTS: release:v1.2.0");
	// A skill that places its arguments gets them in place, and nothing after.
	const placed = "---\nname: video\n---\nSource: $ARGUMENTS.\n";
	assert.equal(videoPrompt(placed, "readme"), "Source: readme.");
	assert.equal(videoPrompt(placed, "readme"), lddPrompt(placed, "readme"));
});

test("/jhon:video reads the skill where `jhon-studio integration --json` says it is, at each use", async (t) => {
	const dir = mkdtempSync(join(tmpdir(), "jhon-studio-skill-"));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const skill = join(dir, "SKILL.md");
	writeFileSync(skill, SKILL);
	const calls: string[][] = [];
	const answer = (stdout: string, error?: string): Run => async (bin, args) => { calls.push([bin, ...args]); return error ? { stdout: "", error } : { stdout }; };
	assert.deepEqual(await studioSkill(answer(JSON.stringify({ version: "0.1.0", plugin_dir: dir, skill }))), { text: SKILL });
	assert.deepEqual(calls, [[STUDIO_BIN, "integration", "--json"]]);
	writeFileSync(skill, SKILL + "Changed.\n");
	assert.deepEqual(await studioSkill(answer(JSON.stringify({ skill }))), { text: SKILL + "Changed.\n" }, "read again at each use");
	assert.deepEqual(await studioSkill(answer("", "jhon-studio is not on PATH")), { error: "jhon-studio is not on PATH" });
	assert.deepEqual(await studioSkill(answer("nope")), { error: "jhon-studio integration --json names no skill" });
	const gone = join(dir, "gone.md");
	const r = await studioSkill(answer(JSON.stringify({ skill: gone })));
	assert.ok("error" in r && r.error.startsWith(`${gone} cannot be read (`), JSON.stringify(r));
});

test("/jhon:video runs the real program on PATH: missing, it says so", async () => {
	const saved = process.env.PATH;
	process.env.PATH = mkdtempSync(join(tmpdir(), "jhon-studio-empty-path-"));
	try { assert.deepEqual(await studioSkill(), { error: "jhon-studio is not on PATH" }); }
	finally { rmSync(process.env.PATH, { recursive: true, force: true }); process.env.PATH = saved; }
});
