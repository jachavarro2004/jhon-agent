/**
 * The status bar's sandbox segment (T174-S3, design/shell-tui 01): the
 * session pins sandbox.mode once at its start (lib/sandbox.ts) and says so on
 * pi.events (SANDBOX_EVENT), which the shell UI turns into `sandbox on` or
 * `sandbox off`; an unread pin says nothing, and the bar shows no segment.
 * The bars' own rows are test/ui-bars.test.ts's.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-shell-chrome.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test, { type TestContext } from "node:test";
import { chmodSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { wireSandbox } from "../lib/sandbox.ts";
import { SANDBOX_EVENT } from "../lib/events.ts";

type Handler = (event: unknown, ctx: unknown) => unknown;

/** A jhon that answers `sandbox status --json` with `answer`, or fails when it is undefined. */
function fakeJhon(t: TestContext, answer: object | undefined): string {
	const dir = mkdtempSync(join(tmpdir(), "jhon-chrome-sbx-"));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const bin = join(dir, "jhon");
	writeFileSync(bin, answer
		? `#!/bin/sh\nprintf '%s\\n' '${JSON.stringify(answer)}'\n`
		: "#!/bin/sh\necho 'jhon sandbox: settings.toml is not readable' >&2\nexit 1\n");
	chmodSync(bin, 0o755);
	return bin;
}

/** The pin's events at one session start. */
async function said(jhon: string | undefined): Promise<unknown[]> {
	const handlers: Handler[] = [];
	const events: unknown[] = [];
	const pi = {
		on: (name: string, handler: Handler) => { if (name === "session_start") handlers.push(handler); },
		registerTool: () => {},
		events: { emit: (name: string, payload: unknown) => { if (name === SANDBOX_EVENT) events.push(payload); } },
	};
	wireSandbox(pi, { jhon, env: {} });
	for (const handler of handlers) await handler({}, { cwd: tmpdir() });
	return events;
}

test("the session's sandbox pin reaches the status bar: on, off, and nothing when it was not read", async (t) => {
	const status = (mode: "on" | "off") => ({ mode, can_run: true, reason: "", fix: "", project: "" });
	assert.equal(SANDBOX_EVENT, "jhon-shell:sandbox");
	assert.deepEqual(await said(fakeJhon(t, status("on"))), [{ mode: "on" }]);
	assert.deepEqual(await said(fakeJhon(t, status("off"))), [{ mode: "off" }]);
	assert.deepEqual(await said(fakeJhon(t, undefined)), [{ mode: undefined }], "unread: the segment goes");
	assert.deepEqual(await said(undefined), [{ mode: "off" }], "no jhon to ask: the sandbox is off");
});
