/**
 * T135 (L1055): pi's announce-stop. A main-session run whose final reply
 * announces an action of the agent's own and makes no tool call is sent back
 * once per owner turn, as one follow-up user message carrying the engine's
 * reason (`jhon hook announce-stop`, the rule Claude Code's Stop hook runs);
 * never in a child, after an abort, with the setting off, or when the check
 * fails. Every text is synthetic.
 */
import { test, after, type TestContext } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { AnnounceSendBack, engineStopRunner, finalReply } from "../lib/announce-stop.ts";
import jhon, { CONFIG_FILE } from "../extensions/jhon.ts";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const BIN = process.env.JHON_TEST_BIN || join(REPO, "build", "jhon");
const scratch = mkdtempSync(join(tmpdir(), "jhon-announce-stop-"));
after(() => rmSync(scratch, { recursive: true, force: true }));
Object.assign(process.env, { PI_CODING_AGENT_DIR: join(scratch, "agent"), JHON_PROFILES: join(scratch, "profiles.json"), CLAUDE_CONFIG_DIR: join(scratch, "claude") });

const ANNOUNCE = "Slice 2 is green. I'll now run the full suite.";
const EMPTY = "Jhon: your turn ended with an empty reply. Continue the work, or say in one line why you stopped.";
const REASON = `Jhon: you announced "I'll now run the full suite." and ended the turn without doing it. Do it now, or say in one line why not.`;
const reply = (text: string, extra: Record<string, unknown> = {}) => [{ role: "user", content: "go" }, { role: "assistant", content: [{ type: "text", text }], stopReason: "stop", ...extra }];

/** The engine reads the owner's settings from this file for the test. */
function settingsFile(t: TestContext, text = "schema = 1\n"): void {
	const file = join(mkdtempSync(join(scratch, "settings-")), "settings.toml");
	writeFileSync(file, text);
	const saved = process.env.JHON_SETTINGS;
	process.env.JHON_SETTINGS = file;
	t.after(() => { if (saved === undefined) delete process.env.JHON_SETTINGS; else process.env.JHON_SETTINGS = saved; });
}

test("the final reply is the last assistant message's text, unless it called a tool, failed, or was aborted", () => {
	assert.equal(finalReply(reply("Sigo con el slice 3.")), "Sigo con el slice 3.");
	assert.equal(finalReply([{ role: "assistant", content: [{ type: "thinking", thinking: "x" }, { type: "text", text: "a" }, { type: "text", text: "b" }] }]), "a\nb");
	assert.equal(finalReply(reply("Sigo.", { content: [{ type: "text", text: "Sigo." }, { type: "toolCall", id: "1", name: "bash", arguments: {} }] })), undefined);
	assert.equal(finalReply(reply("Sigo.", { stopReason: "error" })), undefined);
	assert.equal(finalReply(reply("Sigo.", { stopReason: "aborted" })), undefined);
	assert.equal(finalReply([{ role: "user", content: "hi" }]), undefined);
	assert.equal(finalReply(undefined), undefined);
});

test("one send-back per owner turn; the owner's next message re-arms it; an abort, a failed check or no block sends nothing", async () => {
	const payloads: any[] = [];
	let answer: () => Promise<string> = async () => JSON.stringify({ decision: "block", reason: "R" });
	const s = new AnnounceSendBack(async (payload) => { payloads.push(JSON.parse(payload)); return answer(); });
	s.ended(reply(ANNOUNCE));
	assert.equal(await s.settled(false, "/p"), "R");
	assert.deepEqual(payloads[0], { hook_event_name: "Stop", stop_hook_active: false, last_assistant_message: ANNOUNCE, cwd: "/p" });
	s.ended(reply(ANNOUNCE));
	assert.equal(await s.settled(false, "/p"), undefined, "a second announce-and-stop in the same owner turn ends normally");
	assert.equal(payloads.length, 1, "and is not even checked");
	s.ownerTurn();
	s.ended(reply(ANNOUNCE));
	assert.equal(await s.settled(true, "/p"), undefined, "an aborted run");
	assert.equal(await s.settled(false, "/p"), undefined, "a settle is judged once");
	answer = async () => { throw new Error("no engine"); };
	s.ended(reply(ANNOUNCE));
	assert.equal(await s.settled(false, "/p"), undefined, "a failed check lets the turn end");
	answer = async () => "not json";
	s.ended(reply(ANNOUNCE));
	assert.equal(await s.settled(false, "/p"), undefined);
	answer = async () => "";
	s.ended(reply(ANNOUNCE));
	assert.equal(await s.settled(false, "/p"), undefined, "no block");
	answer = async () => JSON.stringify({ decision: "block", reason: "R2" });
	s.ended(reply(ANNOUNCE));
	assert.equal(await s.settled(false, "/p"), "R2", "still armed after the turns that sent nothing");
});

test("the engine's hook decides: the same reason Claude Code's Stop hook gives, nothing for an outcome or with the setting off", async (t) => {
	settingsFile(t);
	const run = engineStopRunner(BIN);
	const s = new AnnounceSendBack(run);
	s.ended(reply(ANNOUNCE));
	assert.equal(await s.settled(false, scratch), REASON);
	s.ownerTurn();
	s.ended(reply("Done: the suite passes."));
	assert.equal(await s.settled(false, scratch), undefined);
	s.ended(reply("When job-4 finishes I'll merge the branch."));
	assert.equal(await s.settled(false, scratch), undefined);
	settingsFile(t, "schema = 1\n\n[agents]\nact_on_announce = false\n");
	s.ended(reply(ANNOUNCE));
	assert.equal(await s.settled(false, scratch), undefined, "agents.act_on_announce = false");
	const missing = new AnnounceSendBack(engineStopRunner(join(scratch, "no-such-jhon")));
	missing.ended(reply(ANNOUNCE));
	assert.equal(await missing.settled(false, scratch), undefined, "no engine: the turn ends");
});

/** The extension loaded as pi loads it, with handlers fired in order. */
function load(env: Record<string, string> = {}) {
	const agent = mkdtempSync(join(scratch, "agent-"));
	writeFileSync(join(agent, "orchestrator.md"), "# Orchestrator\n");
	writeFileSync(join(agent, CONFIG_FILE), JSON.stringify({ jhon: BIN, orchestrator: join(agent, "orchestrator.md"), vcsDirs: [".git"], protected: [], inspectAllow: [] }));
	const all = new Map<string, Array<(e: any, c: any) => any>>();
	const sent: Array<{ text: string; options: unknown }> = [];
	const saved = { ...process.env };
	Object.assign(process.env, { PI_CODING_AGENT_DIR: agent, ...env });
	try {
		jhon({
			on: (n: string, h: any) => { all.set(n, [...(all.get(n) ?? []), h]); },
			registerTool: () => {}, registerCommand: () => {}, sendMessage: () => {},
			sendUserMessage: (text: string, options: unknown) => sent.push({ text, options }),
		} as any);
	} finally {
		for (const k of Object.keys(process.env)) if (!(k in saved)) delete process.env[k];
		Object.assign(process.env, saved);
	}
	const fire = async (name: string, event: any) => { for (const h of all.get(name) ?? []) await h(event, ctx); };
	const ctx = { cwd: scratch, hasUI: false, isIdle: () => true, ui: { notify: () => {} }, getContextUsage: () => ({ percent: 1, tokens: 1000, contextWindow: 1_000_000 }) };
	return { fire, sent };
}

test("pi: the main session sends one follow-up user message with the engine's reason; the shell's own message does not re-arm it, the owner's does", async (t) => {
	settingsFile(t);
	const { fire, sent } = load();
	const turn = async () => { await fire("agent_end", { messages: reply(ANNOUNCE) }); await fire("agent_settled", { aborted: false }); };
	await fire("input", { source: "interactive", text: "go" });
	await turn();
	assert.deepEqual(sent, [{ text: REASON, options: undefined }]);
	await fire("input", { source: "extension", text: REASON });
	await turn();
	assert.equal(sent.length, 1, "at most one send-back per owner turn");
	await fire("input", { source: "interactive", text: "go on" });
	await fire("agent_end", { messages: reply(ANNOUNCE) });
	await fire("agent_settled", { aborted: true });
	assert.equal(sent.length, 1, "an aborted run is the owner's stop");
	await turn();
	assert.equal(sent.length, 2, "the owner's message re-armed it");
});

test("pi: a child is never sent back", async (t) => {
	settingsFile(t);
	const { fire, sent } = load({ JHON_PI_CHILD: "1" });
	await fire("agent_end", { messages: reply(ANNOUNCE) });
	await fire("agent_settled", { aborted: false });
	assert.deepEqual(sent, []);
});

test("L1063 KA2: an empty final reply (no text, or only thinking, and no tool call) is the empty string, unless it failed or was aborted", () => {
	assert.equal(finalReply(reply("")), "");
	assert.equal(finalReply(reply(" \n")), "");
	assert.equal(finalReply([{ role: "user", content: "go" }, { role: "assistant", content: [{ type: "thinking", thinking: "x" }], stopReason: "stop" }]), "");
	assert.equal(finalReply([{ role: "user", content: "go" }, { role: "assistant", content: [], stopReason: "stop" }]), "");
	assert.equal(finalReply([{ role: "assistant", content: [{ type: "toolCall", id: "1", name: "bash", arguments: {} }] }]), undefined, "a tool call");
	assert.equal(finalReply(reply("", { stopReason: "error" })), undefined, "an error outcome");
	assert.equal(finalReply(reply("", { stopReason: "aborted" })), undefined, "an abort");
	assert.equal(finalReply(reply("", { errorMessage: "overloaded" })), undefined, "an error message");
});

test("L1063 KA2: pi hands the engine the empty reply and sends back its reason once per owner turn; never after an abort or an error, in a child, or with the setting off", async (t) => {
	settingsFile(t);
	const payloads: any[] = [];
	const s = new AnnounceSendBack(async (payload) => { payloads.push(JSON.parse(payload)); return engineStopRunner(BIN)(payload, scratch); });
	s.ended(reply(""));
	assert.equal(await s.settled(false, scratch), EMPTY);
	assert.deepEqual(payloads[0], { hook_event_name: "Stop", stop_hook_active: false, last_assistant_message: "", cwd: scratch });
	s.ended(reply(""));
	assert.equal(await s.settled(false, scratch), undefined, "one send-back per owner turn");
	s.ownerTurn();
	s.ended(reply(""));
	assert.equal(await s.settled(true, scratch), undefined, "an aborted run");
	s.ended(reply("", { stopReason: "error" }));
	assert.equal(await s.settled(false, scratch), undefined, "an error outcome");
	assert.equal(payloads.length, 1, "neither is even checked");

	const { fire, sent } = load();
	await fire("input", { source: "interactive", text: "go" });
	await fire("agent_end", { messages: reply("") });
	await fire("agent_settled", { aborted: false });
	assert.deepEqual(sent, [{ text: EMPTY, options: undefined }]);
	await fire("input", { source: "extension", text: EMPTY });
	await fire("agent_end", { messages: reply("") });
	await fire("agent_settled", { aborted: false });
	assert.equal(sent.length, 1, "the shell's own message does not re-arm it");

	const child = load({ JHON_PI_CHILD: "1" });
	await child.fire("agent_end", { messages: reply("") });
	await child.fire("agent_settled", { aborted: false });
	assert.deepEqual(child.sent, [], "a child");

	settingsFile(t, "schema = 1\n\n[agents]\nact_on_announce = false\n");
	const off = new AnnounceSendBack(engineStopRunner(BIN));
	off.ended(reply(""));
	assert.equal(await off.settled(false, scratch), undefined, "agents.act_on_announce = false");
});
