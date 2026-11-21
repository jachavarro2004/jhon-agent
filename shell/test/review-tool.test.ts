/**
 * T118c (L726, L727, L730): the `jhon_review` tool runs the native Prism review in one
 * call: `jhon review start` → one tool-less completion per facet in parallel →
 * `refute` → the challenger → `validate` → the arbiter → `record`. The engine is
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
const facet = (name: string, model = "prov/m-" + name, effort = "high") =>
	({ facet: name, routing_key: `prism-${name}`, model, effort, prompt: `PROMPT ${name}`, bytes: 13, est_tokens: 3 });
const START = { subject: SUBJECT, base: "b0", candidate: "c1", facets: [facet("safety"), facet("clarity")] };

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
const facetAnswer = (name: string, findings: unknown[] = []) =>
	JSON.stringify({ subject_hash: SUBJECT, facet: name, inspection: { status: "completed", paths: ["a.ts"] }, findings, evidence: ["a.ts"] });

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

const REFUTE = { prompt: "REFUTER PROMPT", bytes: 14, est_tokens: 4, model: "prov/challenger", effort: "medium" };
const VALIDATE = { prompt: "VALIDATOR PROMPT", bytes: 16, est_tokens: 5, model: "prov/arbiter", effort: "low" };
const RECORD = { record: "odd/changes/c/review-prism.md", subject: SUBJECT, base: "b0", facets: ["safety", "clarity"], verdict: "fail",
	correction_set: ["safety-1"], for_verify: [], follow_ups: [], information: ["clarity-1"], refuted: ["safety-2"], unavailable: [] };

function happyAnswers(): (r: CompletionRequest) => string {
	return (r) => {
		switch (r.routingKey) {
			case "prism-safety": return "Here is my answer:\n" + facetAnswer("safety", [finding(), finding({ location: "b.ts:9", claim: "a false alarm" })]);
			case "prism-clarity": return "```json\n" + facetAnswer("clarity", [finding({ severity: "SUGGESTION", claim: "rename x", evidence_class: undefined, causal_disposition: undefined, proof_refs: [] })]) + "\n```";
			case "prism-challenger": return JSON.stringify({ subject_hash: SUBJECT, role: "challenger", results: [{ finding_id: "safety-1", outcome: "stands", reason: "a.ts:3" }, { finding_id: "safety-2", outcome: "refuted", reason: "b.ts:9" }, { finding_id: "clarity-1", outcome: "stands", reason: "ok" }] });
			case "prism-arbiter": return JSON.stringify({ subject_hash: SUBJECT, role: "arbiter", results: [{ finding_id: "safety-1", outcome: "confirmed", reason: "seen", proof_refs: ["a.ts:3"] }, { finding_id: "clarity-1", outcome: "unconfirmed", reason: "style" }] });
		}
		throw new Error("unexpected " + r.routingKey);
	};
}

test("the happy path: start, the facets in parallel, the challenger, the arbiter, then record with all three", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, refute: { out: REFUTE }, validate: { out: VALIDATE }, record: { out: RECORD } });
	const c = fakeCompleter(happyAnswers());
	const r = await runReview({ base: "main", change: "c" }, { bin, cwd: dir, completer: c });
	assert.deepEqual(calls(dir), [
		"review start --json --base main --change c",
		`review refute --json --subject ${SUBJECT} --base main --change c`,
		`review validate --json --subject ${SUBJECT} --base main --change c`,
		`review record --json --subject ${SUBJECT} --base main --change c`,
	]);
	assert.deepEqual(c.log.map((x) => x.routingKey), ["prism-safety", "prism-clarity", "prism-challenger", "prism-arbiter"]);
	assert.equal(c.peak, 2, "the two facets ran at once");
	// Each completion: its own prompt, its model and effort, a fresh session id.
	assert.deepEqual(c.log.map((x) => [x.prompt, x.model, x.effort]), [
		["PROMPT safety", "prov/m-safety", "high"], ["PROMPT clarity", "prov/m-clarity", "high"],
		["REFUTER PROMPT", "prov/challenger", "medium"], ["VALIDATOR PROMPT", "prov/arbiter", "low"],
	]);
	assert.equal(new Set(c.log.map((x) => x.sessionId)).size, 4);
	for (const x of c.log) assert.ok(x.signal instanceof AbortSignal);
	// The engine reads the parsed answers.
	const facets = stdin(dir, "refute");
	assert.deepEqual(facets.map((l: any) => l.facet), ["safety", "clarity"]);
	assert.equal(facets[0].findings.length, 2);
	assert.deepEqual(Object.keys(stdin(dir, "validate")), ["facets", "challenger"]);
	const rec = stdin(dir, "record");
	assert.deepEqual(Object.keys(rec), ["facets", "challenger", "arbiter"]);
	assert.equal(rec.challenger.role, "challenger");
	assert.equal(rec.arbiter.results[0].outcome, "confirmed");
	// The compact result.
	assert.equal(r.details.status, "recorded");
	assert.match(r.text, /^Prism native review: verdict fail · facets safety, clarity · record odd\/changes\/c\/review-prism\.md$/m);
	assert.match(r.text, /^correction set 1 · for verify 0 · follow-ups 0 · information 1 · refuted 1 · unavailable 0$/m);
	assert.match(r.text, /^- CRITICAL safety-1 a\.ts:3 — the guard never runs$/m);
	assert.doesNotMatch(r.text, /PROMPT/);
});

test("no finding: the challenger and the arbiter are not asked; record gets their empty answers", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, record: { out: { ...RECORD, verdict: "pass", correction_set: [], information: [], refuted: [] } } });
	const c = fakeCompleter((r) => facetAnswer(r.routingKey.slice(6)));
	const r = await runReview({}, { bin, cwd: dir, completer: c });
	assert.deepEqual(calls(dir), ["review start --json", `review record --json --subject ${SUBJECT}`]);
	assert.equal(c.log.length, 2);
	const rec = stdin(dir, "record");
	assert.deepEqual(rec.challenger, { subject_hash: SUBJECT, role: "challenger", results: [] });
	assert.deepEqual(rec.arbiter, { subject_hash: SUBJECT, role: "arbiter", results: [] });
	assert.match(r.text, /verdict pass/);
});

test("a facet whose completion fails twice is unavailable; the review still records", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, record: { out: { ...RECORD, unavailable: ["clarity: x"] } } });
	const c = fakeCompleter((r) => { if (r.routingKey === "prism-clarity") throw new Error("provider 503"); return facetAnswer("safety"); });
	const r = await runReview({}, { bin, cwd: dir, completer: c });
	assert.equal(c.log.filter((x) => x.routingKey === "prism-clarity").length, 2, "one retry");
	const rl = stdin(dir, "record").facets[1];
	assert.equal(rl.facet, "clarity");
	assert.equal(rl.inspection.status, "unavailable");
	assert.match(rl.inspection.reason, /provider 503/);
	assert.deepEqual(rl.findings, []);
	assert.equal(rl.subject_hash, SUBJECT);
	assert.match(r.text, /unavailable 1/);
});

test("a facet that fails once and answers on the retry counts", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, record: { out: RECORD } });
	const c = fakeCompleter((r, n) => { if (r.routingKey === "prism-safety" && n === 1) throw new Error("reset"); return facetAnswer(r.routingKey.slice(6)); });
	await runReview({}, { bin, cwd: dir, completer: c });
	assert.equal(stdin(dir, "record").facets[0].inspection.status, "completed");
});

test("a malformed facet answer makes that facet unavailable", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, record: { out: RECORD } });
	const c = fakeCompleter((r) => r.routingKey === "prism-safety" ? "I think it is fine." : facetAnswer("clarity"));
	await runReview({}, { bin, cwd: dir, completer: c });
	const rl = stdin(dir, "record").facets[0];
	assert.equal(rl.facet, "safety");
	assert.equal(rl.inspection.status, "unavailable");
	assert.match(rl.inspection.reason, /not one line of JSON/);
	// The wrong facet or subject is malformed too.
	const w = fakeJhon({ start: { out: START }, record: { out: RECORD } });
	await runReview({}, { bin: w.bin, cwd: w.dir, completer: fakeCompleter((r) => r.routingKey === "prism-safety" ? facetAnswer("clarity") : facetAnswer("clarity")) });
	assert.equal(stdin(w.dir, "record").facets[0].inspection.status, "unavailable");
});

test("no model for a facet: no completion runs and the orchestrator is told to launch the facet agents, then the challenger and arbiter agents", async () => {
	const { bin, dir } = fakeJhon({ start: { out: { ...START, facets: [facet("safety"), facet("clarity", "")] } } });
	const c = fakeCompleter(happyAnswers());
	const r = await runReview({}, { bin, cwd: dir, completer: c });
	assert.equal(c.log.length, 0);
	assert.equal(r.details.status, "fallback");
	assert.match(r.text, /no pi model for prism-clarity/);
	assert.match(r.text, /launch the prism-safety and prism-clarity agents, then the prism-challenger and prism-arbiter agents/);
	const u = fakeJhon({ start: { out: START } });
	const r2 = await runReview({}, { bin: u.bin, cwd: u.dir, completer: fakeCompleter(happyAnswers(), (m) => m === "prov/m-safety" ? "model prov/m-safety is not in pi's model registry" : "") });
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

test("the challenger failing twice falls back, and record is not called", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, refute: { out: REFUTE }, record: { out: RECORD } });
	const base = happyAnswers();
	const r = await runReview({}, { bin, cwd: dir, completer: fakeCompleter((x) => { if (x.routingKey === "prism-challenger") throw new Error("down"); return base(x); }) });
	assert.equal(r.details.status, "fallback");
	assert.match(r.text, /challenger.*down/);
	assert.ok(!existsSync(join(dir, "record.stdin")));
});

test("a role with no model in the engine's output, or one the registry cannot resolve, is not run natively: never another model", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, refute: { out: REFUTE }, validate: { out: { ...VALIDATE, model: "" } }, record: { out: RECORD } });
	const c = fakeCompleter(happyAnswers());
	const r = await runReview({}, { bin, cwd: dir, completer: c });
	assert.deepEqual(c.log.map((x) => x.routingKey), ["prism-safety", "prism-clarity", "prism-challenger"]);
	assert.equal(r.details.status, "fallback");
	assert.match(r.text, /no pi model for prism-arbiter/);
	assert.match(r.text, /launch the prism-arbiter agent/);
	assert.match(r.text, /^- CRITICAL safety-1 a\.ts:3 — the guard never runs$/m, "the findings it needs");
	assert.ok(!existsSync(join(dir, "record.stdin")));
	const u = fakeJhon({ start: { out: START }, refute: { out: { ...REFUTE, model: undefined, effort: undefined } } });
	const c2 = fakeCompleter(happyAnswers());
	const r2 = await runReview({}, { bin: u.bin, cwd: u.dir, completer: c2 });
	assert.equal(c2.log.length, 2);
	assert.match(r2.text, /no pi model for prism-challenger/);
	assert.match(r2.text, /launch the prism-challenger agent, then the prism-arbiter agent/);
	const v = fakeJhon({ start: { out: START }, refute: { out: REFUTE } });
	const r3 = await runReview({}, { bin: v.bin, cwd: v.dir, completer: fakeCompleter(happyAnswers(), (m) => m === "prov/challenger" ? "model prov/challenger is not in pi's model registry" : "") });
	assert.match(r3.text, /prov\/challenger is not in pi's model registry/);
	assert.equal(r3.details.status, "fallback");
});

test("a facet prompt refused for size returns the engine's split-the-slice reason, with no completion", async () => {
	const why = "jhon review start: the safety facet prompt is ~120000 tokens, over the 100000-token cap: split the slice and review each part";
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
	const ids = many.map((_, i) => `safety-${i + 1}`);
	const { bin, dir } = fakeJhon({ start: { out: { ...START, facets: [facet("safety")] } }, refute: { out: REFUTE }, validate: { out: VALIDATE },
		record: { out: { ...RECORD, facets: ["safety"], correction_set: ids, information: [], refuted: [] } } });
	const c = fakeCompleter((r) => {
		if (r.routingKey === "prism-safety") return JSON.stringify({ subject_hash: SUBJECT, facet: "safety", inspection: { status: "completed", paths: ["a.ts"] }, findings: many, evidence: [] });
		const role = r.routingKey.slice(7);
		return JSON.stringify({ subject_hash: SUBJECT, role, results: ids.map((id) => ({ finding_id: id, outcome: role === "challenger" ? "stands" : "confirmed", reason: "r", proof_refs: ["a.ts:1"] })) });
	});
	const r = await runReview({}, { bin, cwd: dir, completer: c });
	assert.ok(r.text.length <= REVIEW_RESULT_MAX, `${r.text.length}`);
	assert.match(r.text, /more in odd\/changes\/c\/review-prism\.md/);
});

test("a subject mismatch at record returns the engine's reason", async () => {
	const why = "jhon review record: subject mismatch: the bytes changed since `jhon review start`: start the review again";
	const { bin, dir } = fakeJhon({ start: { out: START }, record: { err: why, code: 1 } });
	const r = await runReview({}, { bin, cwd: dir, completer: fakeCompleter((x) => facetAnswer(x.routingKey.slice(7))) });
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
	assert.equal(await c.complete({ model: "prov/m", effort: "high", prompt: "P", routingKey: "prism-safety", sessionId: "s1", signal }), "ANSWER");
	const { context, options } = seen[0];
	assert.equal(context.messages.length, 1);
	assert.equal(context.messages[0].role, "user");
	assert.equal(context.messages[0].content[0].text, "P");
	assert.equal(context.tools, undefined);
	assert.equal(context.systemPrompt, undefined);
	assert.equal(options.reasoning, "high");
	assert.equal(options.sessionId, "s1");
	assert.equal(options.signal, signal);
	await c.complete({ model: "prov/m", effort: "off", prompt: "P", routingKey: "prism-safety", sessionId: "s2", signal });
	assert.equal(seen[1].options.reasoning, undefined);
	registry.streamSimple = () => ({ result: async () => ({ stopReason: "error", errorMessage: "quota", content: [] }) });
	await assert.rejects(c.complete({ model: "prov/m", effort: "", prompt: "P", routingKey: "prism-safety", sessionId: "s3", signal }), /quota/);
	registry.streamSimple = () => ({ result: async () => ({ stopReason: "stop", content: [{ type: "toolCall" }] }) });
	await assert.rejects(c.complete({ model: "prov/m", effort: "", prompt: "P", routingKey: "prism-safety", sessionId: "s4", signal }), /tool call/);
});

// T118e (L727): the bench's agents arm turns the native review off, so the
// orchestrator falls back to the facet agents; the native arm reports what its
// completions spent, which the session transcript does not hold.
test("JHON_REVIEW_NATIVE=off: start still names the facets, no completion runs, and the facet agents are the fallback", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START } });
	const c = fakeCompleter(() => { throw new Error("no completion expected"); });
	const r = await runReview({}, { bin, cwd: dir, completer: c, disabled: nativeReviewDisabled({ JHON_REVIEW_NATIVE: "off" }) });
	assert.deepEqual(calls(dir), ["review start --json"]);
	assert.equal(c.log.length, 0);
	assert.equal(r.details.status, "fallback");
	assert.match(r.text, /^jhon_review could not run natively: the native review is off \(JHON_REVIEW_NATIVE=off\)\.$/m);
	assert.match(r.text, /launch the prism-safety and prism-clarity agents, then the prism-challenger and prism-arbiter agents/);
});

test("nativeReviewDisabled reads off, 0 and false; anything else keeps the native review", () => {
	for (const v of ["off", "0", "false", "OFF"]) assert.equal(nativeReviewDisabled({ JHON_REVIEW_NATIVE: v }), "the native review is off (JHON_REVIEW_NATIVE=off)");
	for (const v of [undefined, "", "on", "1"]) assert.equal(nativeReviewDisabled({ JHON_REVIEW_NATIVE: v }), undefined);
});

test("the registry completer sums its completions' usage, and the outcome carries it as details.reviewers", async () => {
	const usage = { input: 100, output: 20, cacheRead: 5, cacheWrite: 7, totalTokens: 132, cost: { total: 0.25 } };
	const registry = {
		find: () => ({ provider: "prov", id: "m" }),
		streamSimple: (_m: unknown, ctx: any) => ({ result: async () => ({ stopReason: "stop", usage, content: [{ type: "text", text: facetAnswer(ctx.messages[0].content[0].text.slice(7)) }] }) }),
	};
	const START1 = { ...START, facets: [facet("safety", "prov/m"), facet("clarity", "prov/m")] };
	const { bin, dir } = fakeJhon({ start: { out: START1 }, record: { out: { ...RECORD, verdict: "pass", correction_set: [], information: [], refuted: [] } } });
	const c = registryCompleter(registry as any);
	const r = await runReview({}, { bin, cwd: dir, completer: c });
	assert.equal(r.details.status, "recorded");
	assert.deepEqual(r.details.reviewers, { calls: 2, input: 200, output: 40, cacheRead: 10, cacheWrite: 14, cost: 0.5 });
});

// T174-S7: the review card (lib/ui/review-card.ts) reads what the review found from the result's details.
test("the details carry what the card draws: the change, each facet's state, every finding and its set, the challenger and the arbiter", async () => {
	const { bin, dir } = fakeJhon({ start: { out: START }, refute: { out: REFUTE }, validate: { out: VALIDATE }, record: { out: RECORD } });
	const r = await runReview({ base: "main", change: "c" }, { bin, cwd: dir, completer: fakeCompleter(happyAnswers()) });
	assert.deepEqual(r.details, {
		status: "recorded", record: "odd/changes/c/review-prism.md", verdict: "fail", change: "c",
		facets: [{ facet: "safety", status: "completed" }, { facet: "clarity", status: "completed" }],
		findings: [
			{ id: "safety-1", facet: "safety", severity: "CRITICAL", location: "a.ts:3", claim: "the guard never runs", set: "correction_set" },
			{ id: "safety-2", facet: "safety", severity: "CRITICAL", location: "b.ts:9", claim: "a false alarm", set: "refuted" },
			{ id: "clarity-1", facet: "clarity", severity: "SUGGESTION", location: "a.ts:3", claim: "rename x", set: "information" },
		],
		challenger: { findings: 3, refuted: 1 },
		arbiter: { asked: 2, confirmed: 1 },
	});
	// No change named: the change is the record's folder; no finding: neither role ran; an unavailable facet keeps its reason.
	const quiet = fakeJhon({ start: { out: START }, record: { out: { ...RECORD, verdict: "fail", correction_set: [], information: [], refuted: [], unavailable: ["clarity: the completion failed"] } } });
	const r2 = await runReview({}, { bin: quiet.bin, cwd: quiet.dir, completer: fakeCompleter((q) => {
		if (q.routingKey === "prism-clarity") throw new Error("provider 503");
		return facetAnswer("safety");
	}) });
	assert.equal(r2.details.change, "c");
	assert.deepEqual(r2.details.findings, []);
	assert.equal(r2.details.challenger, undefined);
	assert.equal(r2.details.arbiter, undefined);
	assert.deepEqual(r2.details.facets, [{ facet: "safety", status: "completed" }, { facet: "clarity", status: "unavailable", reason: "the completion failed: provider 503" }]);
	assert.doesNotMatch(JSON.stringify(r.details), /PROMPT/, "never the prompts");
});
