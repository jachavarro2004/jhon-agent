/**
 * shell/lib/review-tool.ts — T118c (L726, L727, L730, L731): the native 4R in
 * one tool call. `jhon review start` freezes the candidate once and gives one
 * self-contained, tool-less prompt per lens; each runs as one completion
 * through pi's model registry (a single user message, no tools, no system
 * prompt, a fresh session id), all lenses at once; then `refute` → the
 * refuter, `validate` → the validator, and `record` writes review-4r.md bound
 * to the subject. Every completion uses exactly the model and effort the
 * engine names for its routing key; a role with none, or one the registry
 * cannot resolve, is not run natively: the orchestrator is told to launch its
 * agents instead. The model gets a compact result, never the prompts.
 */

import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";

/** One completion: the prompt as the single user message. */
export interface CompletionRequest {
	model: string;
	effort: string;
	prompt: string;
	routingKey: string;
	sessionId: string;
	signal: AbortSignal;
}

/** What a review's completions spent, as the provider reported it. */
export interface ReviewUsage { calls: number; input: number; output: number; cacheRead: number; cacheWrite: number; cost: number }

/** How completions run: check says why a model cannot ("" when it can); usage is what they spent so far. */
export interface Completer {
	check(model: string): string;
	complete(r: CompletionRequest): Promise<string>;
	usage?(): ReviewUsage;
}

export interface ReviewDeps {
	bin: string;
	cwd: string;
	completer: Completer;
	signal?: AbortSignal;
	/** Why the native review must not run (T118e's agents arm); the lens agents are the fallback. */
	disabled?: string;
}

export interface ReviewOutcome {
	text: string;
	details: { status: "recorded" | "fallback" | "refused"; record?: string; verdict?: string; reviewers?: ReviewUsage };
}

/**
 * JHON_REVIEW_NATIVE=off (or 0, false) turns the native review off, so the
 * 4R runs on its lens agents: the switch the bench's agents arm sets (T118e).
 */
export function nativeReviewDisabled(env: Record<string, string | undefined>): string | undefined {
	return ["off", "0", "false"].includes((env.JHON_REVIEW_NATIVE ?? "").trim().toLowerCase()) ? "the native review is off (JHON_REVIEW_NATIVE=off)" : undefined;
}

/** The result's bound, in characters. */
export const REVIEW_RESULT_MAX = 2_000;
/** A completion's answer past this many bytes is not read. */
export const REVIEW_OUTPUT_MAX = 256 * 1024;
const CLAIM_MAX = 160;
const LISTED_MAX = 8;
const THINKING = new Set(["minimal", "low", "medium", "high", "xhigh", "max"]);

/** A completion's time bound: 3 minutes plus 1.5 s per KiB of prompt, at most 30 minutes. */
export function reviewTimeoutMs(promptBytes: number): number {
	return Math.min(180_000 + Math.ceil(Math.max(0, promptBytes) / 1024) * 1_500, 30 * 60_000);
}

/**
 * The completer over pi's live model registry (ctx.modelRegistry): `find`
 * resolves "provider/id", `streamSimple` completes with the registry's
 * request-time auth, which reaches extension-registered providers too.
 */
export function registryCompleter(registry: any): Completer {
	const split = (model: string) => {
		const at = model.indexOf("/");
		return at > 0 && at < model.length - 1 ? { provider: model.slice(0, at), id: model.slice(at + 1) } : undefined;
	};
	const find = (model: string) => { const s = split(model); return s && registry.find(s.provider, s.id); };
	const spent: ReviewUsage = { calls: 0, input: 0, output: 0, cacheRead: 0, cacheWrite: 0, cost: 0 };
	const n = (v: unknown) => (typeof v === "number" && Number.isFinite(v) && v > 0 ? v : 0);
	return {
		usage: () => ({ ...spent }),
		check(model) {
			if (!registry || typeof registry.find !== "function" || typeof registry.streamSimple !== "function") return "pi's model registry is unavailable";
			if (!split(model)) return `model ${JSON.stringify(model)} is not provider/id`;
			return find(model) ? "" : `model ${model} is not in pi's model registry`;
		},
		async complete(r) {
			const model = find(r.model);
			if (!model) throw new Error(`model ${r.model} is not in pi's model registry`);
			const reasoning = THINKING.has(r.effort) && model.reasoning !== false ? r.effort : undefined;
			const context = { messages: [{ role: "user", content: [{ type: "text", text: r.prompt }], timestamp: Date.now() }] };
			const msg = await registry.streamSimple(model, context, { signal: r.signal, sessionId: r.sessionId, ...(reasoning ? { reasoning } : {}) }).result();
			const u = msg.usage ?? {};
			spent.calls += 1; spent.input += n(u.input); spent.output += n(u.output); spent.cacheRead += n(u.cacheRead); spent.cacheWrite += n(u.cacheWrite);
			spent.cost = Math.round((spent.cost + n(u.cost?.total)) * 1e9) / 1e9;
			if (msg.stopReason === "error" || msg.stopReason === "aborted") throw new Error(msg.errorMessage || `the completion ${msg.stopReason === "error" ? "failed" : "was aborted"}`);
			if ((msg.content ?? []).some((p: any) => p?.type === "toolCall")) throw new Error("the model attempted a tool call");
			const text = (msg.content ?? []).filter((p: any) => p?.type === "text").map((p: any) => p.text).join("");
			if (!text) throw new Error(`no text (stopReason: ${msg.stopReason})`);
			return text;
		},
	};
}

/** Runs jhon with stdin; never throws. */
function jhonCall(bin: string, args: string[], cwd: string, input = ""): Promise<{ code: number; stdout: string; stderr: string }> {
	return new Promise((resolve) => {
		const p = spawn(bin, args, { cwd, stdio: ["pipe", "pipe", "pipe"] });
		let stdout = "", stderr = "";
		p.stdout.on("data", (d) => { if (stdout.length < 64 << 20) stdout += d; });
		p.stderr.on("data", (d) => { if (stderr.length < 64 << 10) stderr += d; });
		p.on("error", (e) => resolve({ code: -1, stdout, stderr: e.message }));
		p.on("close", (code) => resolve({ code: code ?? -1, stdout, stderr }));
		p.stdin.on("error", () => {});
		p.stdin.end(input);
	});
}

/** The last line of text that parses as a JSON object, code fences aside. */
function jsonLine(text: string): any {
	if (Buffer.byteLength(text) > REVIEW_OUTPUT_MAX) return undefined;
	const lines = text.split("\n").map((l) => l.trim()).filter((l) => l.startsWith("{"));
	for (const l of lines.reverse()) {
		try { const v = JSON.parse(l); if (v && typeof v === "object" && !Array.isArray(v)) return v; } catch { /* Not this line. */ }
	}
	return undefined;
}

type Lens = { lens: string; routing_key: string; model: string; effort: string; prompt: string; bytes: number };

const one = (s: string) => s.replace(/\s+/g, " ").trim();
const clip = (s: string, n: number) => (s.length <= n ? s : s.slice(0, n - 1) + "…");
const unavailable = (subject: string, lens: string, reason: string) =>
	({ subject_hash: subject, lens, inspection: { status: "unavailable", paths: [], reason: clip(one(reason), 300) }, findings: [], evidence: [] });
const and = (xs: string[]) => (xs.length < 2 ? xs.join("") : `${xs.slice(0, -1).join(", ")} and ${xs[xs.length - 1]}`);

/** The bounded list of findings, by severity. */
function findingLines(findings: any[], more: string): string[] {
	const order = ["BLOCKER", "CRITICAL", "WARNING", "SUGGESTION"];
	const sorted = [...findings].sort((a, b) => order.indexOf(a.severity) - order.indexOf(b.severity));
	const out = sorted.slice(0, LISTED_MAX).map((f) => `- ${f.severity} ${f.id} ${clip(one(String(f.location)), 80)} — ${clip(one(String(f.claim)), CLAIM_MAX)}`);
	if (sorted.length > LISTED_MAX) out.push(`- … ${sorted.length - LISTED_MAX} more in ${more}`);
	return out;
}

function bounded(lines: string[]): string {
	const text = lines.join("\n");
	return text.length <= REVIEW_RESULT_MAX ? text : text.slice(0, REVIEW_RESULT_MAX - 1) + "…";
}

/** Run the native review; the outcome carries what its completions spent, when any ran. */
export async function runReview(params: { base?: string; change?: string }, deps: ReviewDeps): Promise<ReviewOutcome> {
	const out = await review(params, deps);
	const spent = deps.completer.usage?.();
	if (spent && spent.calls > 0) out.details.reviewers = spent;
	return out;
}

async function review(params: { base?: string; change?: string }, deps: ReviewDeps): Promise<ReviewOutcome> {
	const { bin, cwd, completer } = deps;
	const opts = [...(params.base ? ["--base", params.base] : []), ...(params.change ? ["--change", params.change] : [])];
	const refused = (what: string, r: { stderr: string; stdout: string; code: number }): ReviewOutcome =>
		({ text: bounded([`jhon review ${what} refused (exit ${r.code}): ${clip(one(r.stderr || r.stdout || "no output"), 1_200)}`]), details: { status: "refused" } });

	const started = await jhonCall(bin, ["review", "start", "--json", ...opts], cwd);
	let start: any;
	try { start = JSON.parse(started.stdout); } catch { /* Below. */ }
	if (started.code !== 0 || !start || !Array.isArray(start.lenses)) return refused("start", started);
	const subject: string = start.subject;
	const lenses: Lens[] = start.lenses;
	const lensAgents = lenses.map((l) => l.routing_key);
	const fallback = (reason: string, agents: string, findings: any[] = []): ReviewOutcome => ({
		text: bounded([`jhon_review could not run natively: ${clip(one(reason), 400)}.`, `Run the 4R with its agents instead: launch ${agents}, as the 4R contract's fallback says.`,
			...(findings.length ? ["Findings so far:", ...findingLines(findings, "the lens results")] : [])]),
		details: { status: "fallback" },
	});
	const allAgents = `the ${and(lensAgents)} agents, then the review-refuter and review-validator agents`;
	if (deps.disabled) return fallback(deps.disabled, allAgents);

	// Each role runs on exactly its routing key's model, or not natively at all.
	const why = (key: string, model: string) => (model ? completer.check(model) : `no pi model for ${key} in the active profile`);
	for (const l of lenses) {
		const w = why(l.routing_key, l.model);
		if (w) return fallback(w, allAgents);
	}

	const ask = async (key: string, model: string, effort: string, prompt: string): Promise<string> => {
		let last: unknown;
		for (let attempt = 0; attempt < 2; attempt++) {
			const timeout = AbortSignal.timeout(reviewTimeoutMs(Buffer.byteLength(prompt)));
			const signal = deps.signal ? AbortSignal.any([timeout, deps.signal]) : timeout;
			try {
				return await completer.complete({ model, effort: effort ?? "", prompt, routingKey: key, sessionId: `${key}-${randomUUID()}`, signal });
			} catch (e) {
				last = e;
				if (deps.signal?.aborted) break;
			}
		}
		throw last instanceof Error ? last : new Error(String(last));
	};

	const results = await Promise.all(lenses.map(async (l) => {
		let text: string;
		try { text = await ask(l.routing_key, l.model, l.effort, l.prompt); } catch (e) { return unavailable(subject, l.lens, `the completion failed: ${(e as Error).message}`); }
		const v = jsonLine(text);
		const shaped = v && v.lens === l.lens && String(v.subject_hash ?? "").replace(/^sha256:/, "") === subject && Array.isArray(v.findings)
			&& (v.inspection?.status === "completed" || v.inspection?.status === "unavailable");
		return shaped ? v : unavailable(subject, l.lens, "its answer was not one line of JSON for this lens and subject");
	}));
	// The engine numbers findings <lens>-<n> in this order.
	const findings = results.flatMap((r: any) => r.findings.map((f: any, i: number) => ({ ...f, id: `${r.lens}-${i + 1}` })));

	const role = async (name: "refuter" | "validator", sub: "refute" | "validate", input: unknown): Promise<any | ReviewOutcome> => {
		const stdin = JSON.stringify(input);
		const r = sub === "refute"
			? await jhonCall(bin, ["review", "refute", "--json", "--subject", subject, ...opts], cwd, stdin)
			: await jhonCall(bin, ["review", "validate", "--json", "--subject", subject, ...opts], cwd, stdin);
		let out: any;
		try { out = JSON.parse(r.stdout); } catch { /* Below. */ }
		if (r.code !== 0 || !out || typeof out.prompt !== "string") return refused(sub, r);
		const key = `review-${name}`;
		const agents = name === "refuter" ? "the review-refuter agent, then the review-validator agent, on the findings below" : "the review-validator agent on the findings below";
		const w = why(key, out.model ?? "");
		if (w) return fallback(w, agents, findings);
		let text: string;
		try { text = await ask(key, out.model, out.effort ?? "", out.prompt); } catch (e) { return fallback(`the ${name} completion failed: ${(e as Error).message}`, agents, findings); }
		const v = jsonLine(text);
		if (!v || !Array.isArray(v.results)) return fallback(`the ${name}'s answer was not one line of JSON`, agents, findings);
		return { subject_hash: v.subject_hash, role: name, results: v.results };
	};
	const isOutcome = (x: any): x is ReviewOutcome => x && typeof x.text === "string" && x.details;

	// No finding: nothing to refute or validate.
	let refuter: any = { subject_hash: subject, role: "refuter", results: [] };
	let validator: any = { subject_hash: subject, role: "validator", results: [] };
	if (findings.length) {
		refuter = await role("refuter", "refute", results);
		if (isOutcome(refuter)) return refuter;
		const refutedIds = new Set(refuter.results.filter((v: any) => v?.outcome === "refuted").map((v: any) => v.finding_id));
		if (findings.some((f) => !refutedIds.has(f.id))) {
			validator = await role("validator", "validate", { lenses: results, refuter });
			if (isOutcome(validator)) return validator;
		}
	}

	const rec = await jhonCall(bin, ["review", "record", "--json", "--subject", subject, ...opts], cwd, JSON.stringify({ lenses: results, refuter, validator }));
	let out: any;
	try { out = JSON.parse(rec.stdout); } catch { /* Below. */ }
	if (rec.code !== 0 || !out) return refused("record", rec);
	const n = (k: string) => (Array.isArray(out[k]) ? out[k].length : 0);
	const correction = findings.filter((f) => (out.correction_set ?? []).includes(f.id));
	const lines = [
		`4R native review: verdict ${out.verdict} · lenses ${(out.lenses ?? []).join(", ")} · record ${out.record}`,
		`correction set ${n("correction_set")} · for verify ${n("for_verify")} · follow-ups ${n("follow_ups")} · information ${n("information")} · refuted ${n("refuted")} · unavailable ${n("unavailable")}`,
		...(correction.length ? ["Correction set:", ...findingLines(correction, out.record)] : []),
		...(out.for_verify?.length ? [`For verify: ${clip(out.for_verify.join(", "), 200)}`] : []),
		...(out.unavailable?.length ? [`Unavailable: ${clip(one(out.unavailable.join("; ")), 300)}`] : []),
	];
	return { text: bounded(lines), details: { status: "recorded", record: out.record, verdict: out.verdict } };
}
