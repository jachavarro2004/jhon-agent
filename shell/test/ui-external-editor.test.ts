import "./isolated-env.ts";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { openInExternalEditor } from "../lib/ui/jhon-panel-editor.ts";

// Opening a file in the owner's editor (lib/ui/jhon-panel-editor.ts): the
// TUI steps aside while it runs and comes back whole.

type Spawn = typeof spawnSync;
const host = (log: string[] = []) => ({ stop: () => { log.push("stop"); }, start: () => { log.push("start"); }, requestRender: (force?: boolean) => { log.push(`render ${force}`); } });

test("the TUI stops around the editor and is drawn again whole; $VISUAL wins over $EDITOR", () => {
	const log: string[] = [];
	const spawn = ((command: string, args: string[]) => { log.push(`run ${command} ${args.join(" ")}`); return { status: 0 }; }) as unknown as Spawn;
	assert.equal(openInExternalEditor(host(log), "lib/a.ts", { VISUAL: "nvim -u none", EDITOR: "vi" }, spawn), undefined);
	assert.deepEqual(log, ["stop", 'run sh -c nvim -u none "$@" nvim -u none lib/a.ts', "start", "render true"]);
	assert.equal(openInExternalEditor(host(), "lib/a.ts", {}, spawn), "No editor: set $VISUAL or $EDITOR.");
});

test("the shell reads the editor command, so quotes and spaces hold, and a failing editor is reported", { skip: process.platform === "win32" }, (t) => {
	const dir = mkdtempSync(join(tmpdir(), "jhon-editor-"));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const editor = join(dir, "my editor");
	writeFileSync(editor, `#!/bin/sh\nprintf '%s|' "$@" > "${join(dir, "args")}"\n`);
	chmodSync(editor, 0o755);
	assert.equal(openInExternalEditor(host(), "a file.ts", { EDITOR: `"${editor}" --wait` }, spawnSync, dir), undefined);
	assert.equal(readFileSync(join(dir, "args"), "utf8"), "--wait|a file.ts|");
	assert.match(openInExternalEditor(host(), "a.ts", { EDITOR: "false" }, spawnSync, dir)!, /The editor \(false\) ended with code 1/);
});

test("the editor runs in the worktree it was given", () => {
	let cwd: string | undefined;
	const spawn = ((_command: string, _args: string[], options: { cwd?: string }) => { cwd = options.cwd; return { status: 0 }; }) as unknown as Spawn;
	openInExternalEditor(host(), "same.ts", { EDITOR: "vi" }, spawn, "/linked");
	assert.equal(cwd, "/linked");
});
