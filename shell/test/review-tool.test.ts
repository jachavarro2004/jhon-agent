/**
 * T118c (L726, L727, L730): the `jhon_review` tool runs the native 4R in one
 * call: `jhon review start` → one tool-less completion per lens in parallel →
 * `refute` → the refuter → `validate` → the validator → `record`. The engine is
 * a fake `jhon` script; the completions are a fake completer.
 *
 *   jhon test
 */

import "./isolated-env.ts";
import { test, after } from "node:test";
import assert from "node:assert/strict";
import { chmodSync, existsSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { REVIEW_RESULT_MAX, nativeReviewDisabled, registryCompleter, reviewTimeoutMs, runReview, type Completer, type CompletionRequest } from "../lib/review-tool.ts";

const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
const tempDir = () => { const d = realpathSync(mkdtempSync(join(tmpdir(), "jhon-review-test-"))); temps.push(d); return d; };

const SUBJECT = "abc123";
const lens = (name: string, model = "prov/m-" + name, effort = "high") =>
	({ lens: name, routing_key: `review-${name}`, model, effort, prompt: `PROMPT ${name}`, bytes: 13, est_tokens: 3 });
const START = { subject: SUBJECT, base: "b0", candidate: "c1", lenses: [lens("risk"), lens("readability")] };

/** A fake jhon: logs each call's argv and stdin under its dir, prints the canned answer for its subcommand. */
function fakeJhon(answers: Record<string, { out?: unknown; err?: string; code?: number }>): { bin: string; dir: string } {
	const dir = tempDir();
	for (const [sub, a] of Object.entries(answers)) {
		writeFileSync(join(dir, `${sub}.out`), a.out === undefined ? "" : JSON.stringify(a.out) + "\n");
		writeFileSync(join(dir, `${sub}.err`), a.err ?? "");
		writeFileSync(join(dir, `${sub}.code`), String(a.code ?? 0));
	}
	const bin = join(dir, "jhon");
	writeFileSync(bin, `#!/bin/sh\nD=${dir}\nprintf '%s\\n' "$*" >> $D/calls\ncat > $D/$2.stdin\ncat $D/$2.out\ncat $D/$2.err >&2\nexit $(cat $D/$2.code)\n`);
	chmodSync(bin, 0o755);
	return { bin, dir };
}
const calls = (dir: string) => readFileSync(join(dir, "calls"), "utf8").trim().split("\n");
const stdin = (dir: string, sub: string) => JSON.parse(readFileSync(join(dir, `${sub}.stdin`), "utf8"));

const finding = (over: Record<string, unknown> = {}) => ({ location: "a.ts:3", severity: "CRITICAL", claim: "the guard never runs", evidence_class: "deterministic", causal_disposition: "introduced", proof_refs: ["a.ts:3"], ...over });
const lensAnswer = (name: string, findings: unknown[] = []) =>
	JSON.stringify({ subject_hash: SUBJECT, lens: name, inspection: { status: "completed", paths: ["a.ts"] }, findings, evidence: ["a.ts"] });

/** A fake completer: answers by routing key, records every request, and counts how many run at once. */
function fakeCompleter(answer: (r: CompletionRequest, n: number) => string | Promise<string>, check: (model: string) => string = () => ""): Completer & { log: CompletionRequest[]; peak: number } {
	let running = 0;
	const seen = new Map<string, number>();
	const c = {
		log: [] as CompletionRequest[], peak: 0,
		check,
		async complete(r: CompletionRequest) {
			c.log.push(r);
			const n = (seen.get(r.routingKey) ?? 0) + 1;
			seen.set(r.routingKey, n);
			running += 1; c.peak = Math.max(c.peak, running);
			await new Promise((res) => setTimeout(res, 20));
			try { return await answer(r, n); } finally { running -= 1; }
		},
	};
	return c;
}

const REFUTE = { prompt: "REFUTER PROMPT", bytes: 14, est_tokens: 4, model: "prov/refuter", effort: "medium" };
const VALIDATE = { prompt: "VALIDATOR PROMPT", bytes: 16, est_tokens: 5, model: "prov/validator", effort: "low" };
const RECORD = { record: "odd/changes/c/review-4r.md", subject: SUBJECT, base: "b0", lenses: ["risk", "readability"], verdict: "fail",
	correction_set: ["risk-1"], for_verify: [], follow_ups: [], information: ["readability-1"], refuted: ["risk-2"], unavailable: [] };

function happyAnswers(): (r: CompletionRequest) => string {
	return (r) => {
		switch (r.routingKey) {
			case "review-risk": return "Here is my answer:\n" + lensAnswer("risk", [finding(), finding({ location: "b.ts:9", claim: "a false alarm" })]);
			case "review-readability": return "```json\n" + lensAnswer("readability", [finding({ severity: "SUGGESTION", claim: "rename x", evidence_class: undefined, causal_disposition: undefined, proof_refs: [] })]) + "\n```";
			case "review-refuter": return JSON.stringify({ subject_hash: SUBJECT, role: "refuter", results: [{ finding_id: "risk-1", outcome: "stands", reason: "a.ts:3" }, { finding_id: "risk-2", outcome: "refuted", reason: "b.ts:9" }, { finding_id: "readability-1", outcome: "stands", reason: "ok" }] });
			case "review-validator": return JSON.stringify({ subject_hash: SUBJECT, role: "validator", results: [{ finding_id: "risk-1", outcome: "confirmed", reason: "seen", proof_refs: ["a.ts:3"] }, { finding_id: "readability-1", outcome: "unconfirmed", reason: "style" }] });
		}
		throw new Error("unexpected " + r.routingKey);
	};
}

test("the happy path: start, the lenses in parallel, the refuter, the validator, then record with all three", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, refute: { out: REFUTE }, validate: { out: VALIDATE }, record: { out: RECORD } });
	const c = fakeCompleter(happyAnswers());
	const r = await runReview({ base: "main", change: "c" }, { bin, cwd: dir, completer: c });
	assert.deepEqual(calls(dir), [
		"review start --json --base main --change c",
		`review refute --json --subject ${SUBJECT} --base main --change c`,
		`review validate --json --subject ${SUBJECT} --base main --change c`,
		`review record --json --subject ${SUBJECT} --base main --change c`,
	]);
	assert.deepEqual(c.log.map((x) => x.routingKey), ["review-risk", "review-readability", "review-refuter", "review-validator"]);
	assert.equal(c.peak, 2, "the two lenses ran at once");
	// Each completion: its own prompt, its model and effort, a fresh session id.
	assert.deepEqual(c.log.map((x) => [x.prompt, x.model, x.effort]), [
		["PROMPT risk", "prov/m-risk", "high"], ["PROMPT readability", "prov/m-readability", "high"],
		["REFUTER PROMPT", "prov/refuter", "medium"], ["VALIDATOR PROMPT", "prov/validator", "low"],
	]);
	assert.equal(new Set(c.log.map((x) => x.sessionId)).size, 4);
	for (const x of c.log) assert.ok(x.signal instanceof AbortSignal);
	// The engine reads the parsed answers.
	const lenses = stdin(dir, "refute");
	assert.deepEqual(lenses.map((l: any) => l.lens), ["risk", "readability"]);
	assert.equal(lenses[0].findings.length, 2);
	assert.deepEqual(Object.keys(stdin(dir, "validate")), ["lenses", "refuter"]);
	const rec = stdin(dir, "record");
	assert.deepEqual(Object.keys(rec), ["lenses", "refuter", "validator"]);
	assert.equal(rec.refuter.role, "refuter");
	assert.equal(rec.validator.results[0].outcome, "confirmed");
	// The compact result.
	assert.equal(r.details.status, "recorded");
	assert.match(r.text, /^4R native review: verdict fail · lenses risk, readability · record odd\/changes\/c\/review-4r\.md$/m);
	assert.match(r.text, /^correction set 1 · for verify 0 · follow-ups 0 · information 1 · refuted 1 · unavailable 0$/m);
	assert.match(r.text, /^- CRITICAL risk-1 a\.ts:3 — the guard never runs$/m);
	assert.doesNotMatch(r.text, /PROMPT/);
});

test("no finding: the refuter and the validator are not asked; record gets their empty answers", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, record: { out: { ...RECORD, verdict: "pass", correction_set: [], information: [], refuted: [] } } });
	const c = fakeCompleter((r) => lensAnswer(r.routingKey.slice(7)));
	const r = await runReview({}, { bin, cwd: dir, completer: c });
	assert.deepEqual(calls(dir), ["review start --json", `review record --json --subject ${SUBJECT}`]);
	assert.equal(c.log.length, 2);
	const rec = stdin(dir, "record");
	assert.deepEqual(rec.refuter, { subject_hash: SUBJECT, role: "refuter", results: [] });
	assert.deepEqual(rec.validator, { subject_hash: SUBJECT, role: "validator", results: [] });
	assert.match(r.text, /verdict pass/);
});

test("a lens whose completion fails twice is unavailable; the review still records", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, record: { out: { ...RECORD, unavailable: ["readability: x"] } } });
	const c = fakeCompleter((r) => { if (r.routingKey === "review-readability") throw new Error("provider 503"); return lensAnswer("risk"); });
	const r = await runReview({}, { bin, cwd: dir, completer: c });
	assert.equal(c.log.filter((x) => x.routingKey === "review-readability").length, 2, "one retry");
	const rl = stdin(dir, "record").lenses[1];
	assert.equal(rl.lens, "readability");
	assert.equal(rl.inspection.status, "unavailable");
	assert.match(rl.inspection.reason, /provider 503/);
	assert.deepEqual(rl.findings, []);
	assert.equal(rl.subject_hash, SUBJECT);
	assert.match(r.text, /unavailable 1/);
});

test("a lens that fails once and answers on the retry counts", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, record: { out: RECORD } });
	const c = fakeCompleter((r, n) => { if (r.routingKey === "review-risk" && n === 1) throw new Error("reset"); return lensAnswer(r.routingKey.slice(7)); });
	await runReview({}, { bin, cwd: dir, completer: c });
	assert.equal(stdin(dir, "record").lenses[0].inspection.status, "completed");
});

test("a malformed lens answer makes that lens unavailable", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, record: { out: RECORD } });
	const c = fakeCompleter((r) => r.routingKey === "review-risk" ? "I think it is fine." : lensAnswer("readability"));
	await runReview({}, { bin, cwd: dir, completer: c });
	const rl = stdin(dir, "record").lenses[0];
	assert.equal(rl.lens, "risk");
	assert.equal(rl.inspection.status, "unavailable");
	assert.match(rl.inspection.reason, /not one line of JSON/);
	// The wrong lens or subject is malformed too.
	const w = fakeJhon({ start: { out: START }, record: { out: RECORD } });
	await runReview({}, { bin: w.bin, cwd: w.dir, completer: fakeCompleter((r) => r.routingKey === "review-risk" ? lensAnswer("readability") : lensAnswer("readability")) });
	assert.equal(stdin(w.dir, "record").lenses[0].inspection.status, "unavailable");
});

test("no model for a lens: no completion runs and the orchestrator is told to launch the lens agents, then the refuter and validator agents", async () => {
	const { bin, dir } = fakeJhon({ start: { out: { ...START, lenses: [lens("risk"), lens("readability", "")] } } });
	const c = fakeCompleter(happyAnswers());
	const r = await runReview({}, { bin, cwd: dir, completer: c });
	assert.equal(c.log.length, 0);
	assert.equal(r.details.status, "fallback");
	assert.match(r.text, /no pi model for review-readability/);
	assert.match(r.text, /launch the review-risk and review-readability agents, then the review-refuter and review-validator agents/);
	const u = fakeJhon({ start: { out: START } });
	const r2 = await runReview({}, { bin: u.bin, cwd: u.dir, completer: fakeCompleter(happyAnswers(), (m) => m === "prov/m-risk" ? "model prov/m-risk is not in pi's model registry" : "") });
	assert.match(r2.text, /not in pi's model registry/);
	assert.equal(r2.details.status, "fallback");
});

test("pi's registry unavailable: the fallback", async () => {
	const c = registryCompleter(undefined);
	assert.match(c.check("prov/m"), /model registry is unavailable/);
	const { bin, dir } = fakeJhon({ start: { out: START } });
	const r = await runReview({}, { bin, cwd: dir, completer: c });
	assert.equal(r.details.status, "fallback");
});

test("the refuter failing twice falls back, and record is not called", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, refute: { out: REFUTE }, record: { out: RECORD } });
	const base = happyAnswers();
	const r = await runReview({}, { bin, cwd: dir, completer: fakeCompleter((x) => { if (x.routingKey === "review-refuter") throw new Error("down"); return base(x); }) });
	assert.equal(r.details.status, "fallback");
	assert.match(r.text, /refuter.*down/);
	assert.ok(!existsSync(join(dir, "record.stdin")));
});

test("a role with no model in the engine's output, or one the registry cannot resolve, is not run natively: never another model", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, refute: { out: REFUTE }, validate: { out: { ...VALIDATE, model: "" } }, record: { out: RECORD } });
	const c = fakeCompleter(happyAnswers());
	const r = await runReview({}, { bin, cwd: dir, completer: c });
	assert.deepEqual(c.log.map((x) => x.routingKey), ["review-risk", "review-readability", "review-refuter"]);
	assert.equal(r.details.status, "fallback");
	assert.match(r.text, /no pi model for review-validator/);
	assert.match(r.text, /launch the review-validator agent/);
	assert.match(r.text, /^- CRITICAL risk-1 a\.ts:3 — the guard never runs$/m, "the findings it needs");
	assert.ok(!existsSync(join(dir, "record.stdin")));
	const u = fakeJhon({ start: { out: START }, refute: { out: { ...REFUTE, model: undefined, effort: undefined } } });
	const c2 = fakeCompleter(happyAnswers());
	const r2 = await runReview({}, { bin: u.bin, cwd: u.dir, completer: c2 });
	assert.equal(c2.log.length, 2);
	assert.match(r2.text, /no pi model for review-refuter/);
	assert.match(r2.text, /launch the review-refuter agent, then the review-validator agent/);
	const v = fakeJhon({ start: { out: START }, refute: { out: REFUTE } });
	const r3 = await runReview({}, { bin: v.bin, cwd: v.dir, completer: fakeCompleter(happyAnswers(), (m) => m === "prov/refuter" ? "model prov/refuter is not in pi's model registry" : "") });
	assert.match(r3.text, /prov\/refuter is not in pi's model registry/);
	assert.equal(r3.details.status, "fallback");
});

test("a lens prompt refused for size returns the engine's split-the-slice reason, with no completion", async () => {
	const why = "jhon review start: the risk lens prompt is ~120000 tokens, over the 100000-token cap: split the slice and review each part";
	const { bin, dir } = fakeJhon({ start: { err: why, code: 1 } });
	const c = fakeCompleter(happyAnswers());
	const r = await runReview({}, { bin, cwd: dir, completer: c });
	assert.equal(c.log.length, 0);
	assert.equal(r.details.status, "refused");
	assert.ok(r.text.includes(why));
	assert.match(r.text, /split the slice/);
});

test("the result stays bounded however many findings stand", async () => {
	const many = Array.from({ length: 60 }, (_, i) => finding({ location: `a.ts:${i + 1}`, claim: "x".repeat(900) }));
	const ids = many.map((_, i) => `risk-${i + 1}`);
	const { bin, dir } = fakeJhon({ start: { out: { ...START, lenses: [lens("risk")] } }, refute: { out: REFUTE }, validate: { out: VALIDATE },
		record: { out: { ...RECORD, lenses: ["risk"], correction_set: ids, information: [], refuted: [] } } });
	const c = fakeCompleter((r) => {
		if (r.routingKey === "review-risk") return JSON.stringify({ subject_hash: SUBJECT, lens: "risk", inspection: { status: "completed", paths: ["a.ts"] }, findings: many, evidence: [] });
		const role = r.routingKey.slice(7);
		return JSON.stringify({ subject_hash: SUBJECT, role, results: ids.map((id) => ({ finding_id: id, outcome: role === "refuter" ? "stands" : "confirmed", reason: "r", proof_refs: ["a.ts:1"] })) });
	});
	const r = await runReview({}, { bin, cwd: dir, completer: c });
	assert.ok(r.text.length <= REVIEW_RESULT_MAX, `${r.text.length}`);
	assert.match(r.text, /more in odd\/changes\/c\/review-4r\.md/);
});

test("a subject mismatch at record returns the engine's reason", async () => {
	const why = "jhon review record: subject mismatch: the bytes changed since `jhon review start`: start the review again";
	const { bin, dir } = fakeJhon({ start: { out: START }, record: { err: why, code: 1 } });
	const r = await runReview({}, { bin, cwd: dir, completer: fakeCompleter((x) => lensAnswer(x.routingKey.slice(7))) });
	assert.equal(r.details.status, "refused");
	assert.ok(r.text.includes(why));
});

test("the timeout grows with the prompt and is capped", () => {
	assert.ok(reviewTimeoutMs(350_000) > reviewTimeoutMs(1_000));
	assert.ok(reviewTimeoutMs(100_000_000) <= 30 * 60_000);
});

test("registryCompleter resolves provider/id through find and completes through streamSimple with no tools", async () => {
	const seen: any[] = [];
	const model = { provider: "prov", id: "m", reasoning: true };
	const registry = {
		find: (p: string, id: string) => (p === "prov" && id === "m" ? model : undefined),
		streamSimple: (m: unknown, context: any, options: any) => { seen.push({ m, context, options }); return { result: async () => ({ stopReason: "stop", content: [{ type: "thinking", thinking: "hm" }, { type: "text", text: "ANSWER" }] }) }; },
	};
	const c = registryCompleter(registry as any);
	assert.equal(c.check("prov/m"), "");
	assert.match(c.check("prov/zz"), /not in pi's model registry/);
	assert.match(c.check("nope"), /provider\/id/);
	const signal = new AbortController().signal;
	assert.equal(await c.complete({ model: "prov/m", effort: "high", prompt: "P", routingKey: "review-risk", sessionId: "s1", signal }), "ANSWER");
	const { context, options } = seen[0];
	assert.equal(context.messages.length, 1);
	assert.equal(context.messages[0].role, "user");
	assert.equal(context.messages[0].content[0].text, "P");
	assert.equal(context.tools, undefined);
	assert.equal(context.systemPrompt, undefined);
	assert.equal(options.reasoning, "high");
	assert.equal(options.sessionId, "s1");
	assert.equal(options.signal, signal);
	await c.complete({ model: "prov/m", effort: "off", prompt: "P", routingKey: "review-risk", sessionId: "s2", signal });
	assert.equal(seen[1].options.reasoning, undefined);
	registry.streamSimple = () => ({ result: async () => ({ stopReason: "error", errorMessage: "quota", content: [] }) });
	await assert.rejects(c.complete({ model: "prov/m", effort: "", prompt: "P", routingKey: "review-risk", sessionId: "s3", signal }), /quota/);
	registry.streamSimple = () => ({ result: async () => ({ stopReason: "stop", content: [{ type: "toolCall" }] }) });
	await assert.rejects(c.complete({ model: "prov/m", effort: "", prompt: "P", routingKey: "review-risk", sessionId: "s4", signal }), /tool call/);
});

// T118e (L727): the bench's agents arm turns the native review off, so the
// orchestrator falls back to the lens agents; the native arm reports what its
// completions spent, which the session transcript does not hold.
test("JHON_REVIEW_NATIVE=off: start still names the lenses, no completion runs, and the lens agents are the fallback", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START } });
	const c = fakeCompleter(() => { throw new Error("no completion expected"); });
	const r = await runReview({}, { bin, cwd: dir, completer: c, disabled: nativeReviewDisabled({ JHON_REVIEW_NATIVE: "off" }) });
	assert.deepEqual(calls(dir), ["review start --json"]);
	assert.equal(c.log.length, 0);
	assert.equal(r.details.status, "fallback");
	assert.match(r.text, /^jhon_review could not run natively: the native review is off \(JHON_REVIEW_NATIVE=off\)\.$/m);
	assert.match(r.text, /launch the review-risk and review-readability agents, then the review-refuter and review-validator agents/);
});

test("nativeReviewDisabled reads off, 0 and false; anything else keeps the native review", () => {
	for (const v of ["off", "0", "false", "OFF"]) assert.equal(nativeReviewDisabled({ JHON_REVIEW_NATIVE: v }), "the native review is off (JHON_REVIEW_NATIVE=off)");
	for (const v of [undefined, "", "on", "1"]) assert.equal(nativeReviewDisabled({ JHON_REVIEW_NATIVE: v }), undefined);
});

test("the registry completer sums its completions' usage, and the outcome carries it as details.reviewers", async () => {
	const usage = { input: 100, output: 20, cacheRead: 5, cacheWrite: 7, totalTokens: 132, cost: { total: 0.25 } };
	const registry = {
		find: () => ({ provider: "prov", id: "m" }),
		streamSimple: (_m: unknown, ctx: any) => ({ result: async () => ({ stopReason: "stop", usage, content: [{ type: "text", text: lensAnswer(ctx.messages[0].content[0].text.slice(7)) }] }) }),
	};
	const START1 = { ...START, lenses: [lens("risk", "prov/m"), lens("readability", "prov/m")] };
	const { bin, dir } = fakeJhon({ start: { out: START1 }, record: { out: { ...RECORD, verdict: "pass", correction_set: [], information: [], refuted: [] } } });
	const c = registryCompleter(registry as any);
	const r = await runReview({}, { bin, cwd: dir, completer: c });
	assert.equal(r.details.status, "recorded");
	assert.deepEqual(r.details.reviewers, { calls: 2, input: 200, output: 40, cacheRead: 10, cacheWrite: 14, cost: 0.5 });
});
