import assert from "node:assert/strict";
import test from "node:test";
import { parseSessionBrief, sessionBriefs } from "../lib/session-brief.ts";

// T31 D: pi's main session gets the binding lead and the project's rules
// `jhon hook session-brief` prints, only in a project that uses LDD;
// elsewhere the engine's line that says LDD is inactive (T64).

test("a brief is read from the engine's JSON; nothing, or no brief, is none", () => {
	assert.deepEqual(parseSessionBrief(JSON.stringify({ lead: "Jhon runs this session.", brief: "## This project's rules" })), { lead: "Jhon runs this session.", brief: "## This project's rules" });
	assert.equal(parseSessionBrief(""), undefined);
	assert.equal(parseSessionBrief("not json"), undefined);
	assert.equal(parseSessionBrief(JSON.stringify({ lead: "x" })), undefined);
	assert.deepEqual(parseSessionBrief(JSON.stringify({ inactive: "LDD is inactive here." })), { inactive: "LDD is inactive here." });
	assert.equal(parseSessionBrief(JSON.stringify({ inactive: "" })), undefined);
});

test("the engine is asked once per session and folder; a failure is no brief", async () => {
	const asked: string[] = [];
	const briefs = sessionBriefs(async (cwd) => { asked.push(cwd); return cwd === "/ldd" ? JSON.stringify({ lead: "L", brief: "B" }) : JSON.stringify({ inactive: "I" }); });
	assert.deepEqual(await briefs("s1", "/ldd"), { lead: "L", brief: "B" });
	assert.deepEqual(await briefs("s1", "/ldd"), { lead: "L", brief: "B" });
	// The engine's line where LDD is inactive, its text, never the shell's.
	assert.deepEqual(await briefs("s1", "/plain"), { inactive: "I" });
	await briefs("s2", "/ldd");
	assert.deepEqual(asked, ["/ldd", "/plain", "/ldd"]);
	const failing = sessionBriefs(async () => { throw new Error("no jhon"); });
	assert.equal(await failing("s", "/ldd"), undefined);
	assert.equal(await sessionBriefs(async () => "not json")("s", "/ldd"), undefined, "unreadable output is a failure");
	assert.equal(await sessionBriefs(async () => "")("s", "/ldd"), undefined, "no output says nothing");
});
