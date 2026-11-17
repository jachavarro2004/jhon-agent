/**
 * pi's LDD reminder finds a project's layout as the engine does (L477, A5):
 * openspec/config.yaml, or a native ldd/ by its config or its changes/,
 * never a native ldd/ above the file's git repository.
 */
import { test, after } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const HOOK_BIN = process.env.JHON_TEST_BIN || join(REPO, "build", "jhon");
import { LDD_REMINDER, lddCodeFile, lddReminder, reminderAnswer } from "../lib/ldd-reminder.ts";

const SCRATCH = realpathSync(mkdtempSync(join(tmpdir(), "jhon-ldd-reminder-")));
after(() => rmSync(SCRATCH, { recursive: true, force: true }));

function put(dir: string, files: Record<string, string>): void {
	for (const [rel, body] of Object.entries(files)) {
		mkdirSync(dirname(join(dir, rel)), { recursive: true });
		writeFileSync(join(dir, rel), body);
	}
}

function repo(dir: string): string {
	mkdirSync(dir, { recursive: true });
	assert.equal(spawnSync("git", ["init", "-q", dir]).status, 0);
	return dir;
}

/** The reminder as pi runs it, the engine's `jhon status --json` answering. */
function reminder() {
	return lddReminder(async (cwd) => {
		const r = spawnSync(HOOK_BIN, ["status", "--json"], { cwd, encoding: "utf8" });
		try { return reminderAnswer(JSON.parse(r.stdout)); } catch { return undefined; }
	});
}

test("the reminder fires exactly where the engine reports LDD active (L479)", async () => {
	const cases: [string, Record<string, string>, boolean, boolean][] = [
		["openspec repo", { "openspec/config.yaml": "" }, true, true],
		["native repo", { "ldd/config.yaml": "" }, true, true],
		["only ldd/changes/", { "ldd/changes/c1/tasks.md": "" }, true, false],
		["a folder named ldd", { "ldd/notes.txt": "" }, true, false],
		["a non-repo folder with ldd/config.yaml", { "ldd/config.yaml": "" }, false, true],
		["a non-repo folder with openspec/config.yaml", { "openspec/config.yaml": "" }, false, true],
	];
	for (const [name, files, git, fires] of cases) {
		const dir = join(SCRATCH, name.replace(/[^a-z]+/g, "-"));
		if (git) repo(dir);
		put(dir, { ...files, "src/a.go": "" });
		const note = await reminder()("write", { path: "src/a.go" }, dir);
		assert.equal(note, fires ? LDD_REMINDER : undefined, name);
	}
	const parent = join(SCRATCH, "above");
	put(parent, { "ldd/config.yaml": "", "ldd/changes/c1/tasks.md": "" });
	const inner = repo(join(parent, "inner"));
	put(inner, { "src/a.go": "" });
	assert.equal(await reminder()("write", { path: "src/a.go" }, inner), undefined, "a native ldd/ above the repository");
});

test("the record is no code; a code folder named ldd is", () => {
	const dir = join(SCRATCH, "code");
	put(dir, { "openspec/config.yaml": "", "engine/internal/ldd/route.go": "" });
	assert.equal(lddCodeFile("openspec/changes/c1/x.go", dir), false);
	assert.equal(lddCodeFile("ldd/changes/c1/x.go", dir), false);
	assert.equal(lddCodeFile("engine/internal/ldd/route.go", dir), true);
	assert.equal(lddCodeFile("README.md", dir), false);
});
