import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, writeFile, rm, realpath } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import * as capture from "../lib/ui/session-change-capture.ts";
import * as sessionChanges from "../lib/ui/session-changes.ts";
import { installSessionChangeCapture } from "../lib/ui/session-change-capture.ts";
import { SessionChanges, SESSION_CHANGE_ENTRY } from "../lib/ui/session-changes.ts";

async function fixture(run: (f: any) => Promise<void>, child = false) {
	const root = await realpath(await mkdtemp(join(tmpdir(), "change-capture-")));
	const handlers = new Map<string, Function>();
	const entries: any[] = [];
	const listeners = new Map<string, Function>();
	const pi = { on: (key, fn) => handlers.set(key, fn), appendEntry: (customType, data) => entries.push({type:"custom",customType,data}),
		events: { on: (key, fn) => { listeners.set(key, fn); return () => listeners.delete(key); }, emit: (key, data) => listeners.get(key)?.(data) } };
	let id = "session";
	const ctx = { cwd:root, sessionManager: { getSessionId: () => id, getEntries: () => entries } };
	installSessionChangeCapture(pi as never, child ? {JHON_PI_CHILD:"1"} : {}, () => ({root,commonDir:root}));
	const fire = (key, event = {}) => handlers.get(key)?.(event, ctx);
	try { await fire("session_start"); await run({root, entries, ctx, fire, pi, handlers, listeners, switchSession: () => id = "other"}); }
	finally { await rm(root, {recursive:true,force:true}); }
}

// T132 (L932): the child-to-parent evidence relay had no sender and no reader
// in production. A child's worktree is one of the session's roots instead
// (the `subagent` tool registers it), so the relay is gone: no listener on
// the event bus, no publisher for another process's evidence.
test("the session records only its own tool outcomes: nothing listens for a child's evidence and nothing publishes another process's", async () => fixture(async ({ listeners, entries, pi }) => {
	assert.deepEqual([...listeners.keys()], [], "no relay listener on the event bus");
	assert.equal("publishForeignSessionChange" in capture, false);
	assert.equal("SESSION_CHANGE_RELAY" in sessionChanges, false);
	pi.events.emit("jhon-shell:child-session-change", { sessionId: "session", evidence: { id: "task:write", root: "/elsewhere", path: "file.txt", before: { kind: "absent" }, after: { kind: "text", text: "child output\n" } } });
	assert.deepEqual(entries, []);
}));

test("capture reads no inventory at startup and ignores read-only tools", async () => fixture(async ({fire, entries}) => {
	await fire("tool_call", {toolCallId:"r",toolName:"read",input:{path:"missing"}});
	await fire("tool_result", {toolCallId:"r",toolName:"read",input:{path:"missing"},isError:false});
	await fire("tool_execution_end", {toolCallId:"r",toolName:"read",isError:false});
	assert.deepEqual(entries, []);
}));

test("successful writes capture only the exact tool target and persist for reload", async () => fixture(async ({root,fire,entries}) => {
	await writeFile(join(root,"file"),"human baseline\n");
	const event = {toolCallId:"w",toolName:"write",input:{path:"file",content:"agent output\n"}};
	await fire("tool_call",event);
	await writeFile(join(root,"file"),event.input.content);
	await fire("tool_result",{...event,isError:false});
	assert.equal(entries.length,0);
	await fire("tool_execution_end",{toolCallId:"w",toolName:"write",isError:false});
	assert.equal(entries[0].customType,SESSION_CHANGE_ENTRY);
	await writeFile(join(root,"file"),"later human output\n");
	const changes = new SessionChanges("session",entries);
	assert.match(entries[0].data.evidence.after.text,/agent output/);
	assert.doesNotMatch(entries[0].data.evidence.after.text,/later human/);
	assert.equal(changes.model.files.length,1);
}));

test("failed and stale-session tool outcomes never add session changes", async () => fixture(async ({root,fire,entries,switchSession}) => {
	const event={toolCallId:"w",toolName:"write",input:{path:"new",content:"agent\n"}};
	await fire("tool_call",event);
	await writeFile(join(root,"new"),event.input.content);
	await fire("tool_result",{...event,isError:false});
	await fire("tool_execution_end",{toolCallId:"w",toolName:"write",isError:true});
	assert.equal(entries.length,0);
	await fire("tool_call",{...event,toolCallId:"x"});
	switchSession();
	await fire("tool_result",{...event,toolCallId:"x",isError:false});
	await fire("tool_execution_end",{toolCallId:"x",toolName:"write",isError:false});
	assert.equal(entries.length,0);
}));

// Converted (T132): the child used to attach `details.jhonSessionChange` to
// its write and edit results, which no parent read.
test("a child leaves its tool results as they are and records nothing: its worktree is the session's root", async () => fixture(async ({root,fire,entries,handlers}) => {
	const event={toolCallId:"w",toolName:"write",input:{path:"new",content:"agent\n"}};
	await fire("tool_call",event); await writeFile(join(root,"new"),event.input.content);
	const result=await fire("tool_result",{...event,isError:false,details:{original:"preserved"}});
	assert.equal(result,undefined,"the result's details are not rewritten");
	await fire("tool_execution_end",{toolCallId:"w",toolName:"write",isError:false});
	assert.deepEqual(entries,[]);
	assert.deepEqual([...handlers.keys()],[],"a child observes no tool call at all");
},true));

test("capture caps bound session entries without a Changes notice", async () => fixture(async ({root,entries,fire,pi}) => {
	const events: unknown[] = [];
	pi.events.on("jhon-shell:session-change", (data: unknown) => events.push(data));
	for (let i = 0; i < 80; i++) {
		const path = `f${i}`;
		const content = "x".repeat(60 * 1024) + "\n";
		const event = {toolCallId:`w${i}`,toolName:"write",input:{path,content}};
		await fire("tool_call",event);
		await writeFile(join(root,path),content);
		await fire("tool_result",{...event,isError:false});
		await fire("tool_execution_end",{toolCallId:`w${i}`,toolName:"write",isError:false});
	}
	const persisted = entries.filter(entry => entry.customType === SESSION_CHANGE_ENTRY).length;
	assert.ok(persisted > 0 && persisted < 80, `the session byte cap stops persisting evidence (${persisted})`);
	assert.deepEqual(events, [], "Changes reads Git; capture emits no refresh or limit notice");
}));
