/**
 * A Jhon session never calls a model nobody chose (T130, L920 B1), by the rule
 * the owner reduced (L944): lib/model-choice.ts judges with what the shell
 * knows for certain at session start and in the process, and never reads a
 * session's history.
 *
 *   jhon test
 */
import assert from "node:assert/strict";
import test from "node:test";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ModelChoice, settingsDefaults, type ChoiceCtx } from "../lib/model-choice.ts";
import { mkdirSync } from "node:fs";

const NAMED = { provider: "anthropic", id: "claude-opus-4-5" };
const PROFILE = { ...NAMED, from: "profile" as const };
const FALLBACK = { provider: "claude-bridge", id: "claude-fable-5-2" };
const LINE = "jhon: the profile names anthropic/claude-opus-4-5 for this session, but its provider anthropic has no credential; this session is on claude-bridge/claude-fable-5-2, which nobody chose in this run.";
/** A registry that knows `known` and has a credential for the providers in `auth`. */
const registry = (auth: string[], known = ["anthropic/claude-opus-4-5", "claude-bridge/claude-fable-5-2"]) => ({
	find: (provider: string, id: string) => known.includes(`${provider}/${id}`) ? { provider, id } : undefined,
	hasConfiguredAuth: (m: any) => auth.includes(m.provider),
});
const ctx = (over: Partial<ChoiceCtx> = {}): ChoiceCtx => ({ model: FALLBACK, modelRegistry: registry(["claude-bridge"]), scopedModels: [], ...over });
const choice = () => { ModelChoice.forget(); return new ModelChoice(() => [PROFILE]); };

test("pi's fallback is named: the profile's model, its provider without a credential, the model this session is on (B1)", () => {
	const c = choice();
	assert.equal(c.start(ctx(), ["--print", "hi"]), LINE);
	assert.equal(c.refused(), LINE, "and no prompt runs while it stands");
});

test("a profile model pi does not have at all says so", () => {
	assert.equal(choice().start(ctx({ modelRegistry: registry(["claude-bridge"], ["claude-bridge/claude-fable-5-2"]) }), []),
		"jhon: the profile names anthropic/claude-opus-4-5 for this session, but pi has no such model; this session is on claude-bridge/claude-fable-5-2, which nobody chose in this run.");
});

test("a session on the model the profile names is untouched, credential or not", () => {
	for (const auth of [["anthropic"], []]) {
		const c = choice();
		assert.equal(c.start(ctx({ model: NAMED, modelRegistry: registry(auth) }), []), undefined);
		assert.equal(c.refused(), undefined);
	}
});

test("a session the owner started with an explicit model is untouched", () => {
	for (const argv of [["--model", "claude-bridge/claude-fable-5-2"], ["--model=fable"], ["--provider", "claude-bridge", "--model", "fable"]]) {
		assert.equal(choice().start(ctx(), argv), undefined, argv.join(" "));
	}
	assert.equal(choice().start(ctx(), ["--print", "--no-session", "-e", "x.ts", "say --model here"]), LINE, "a prompt that mentions --model names none");
	assert.equal(choice().start(ctx({ scopedModels: [{ model: FALLBACK }] }), []), undefined, "scoped models (enabledModels) are the owner's");
});

test("a model the owner picked with pi's picker is theirs: prompts run again, and a later /new or reload keeps it", () => {
	const c = choice();
	assert.equal(c.start(ctx(), []), LINE);
	c.picked({ source: "restore", model: FALLBACK });
	assert.equal(c.refused(), LINE, "a restore is not a pick");
	c.picked({ source: "set", model: { provider: "claude-bridge", id: "claude-opus-5-5" } });
	assert.equal(c.refused(), undefined);
	const picked = { provider: "claude-bridge", id: "claude-opus-5-5" };
	assert.equal(new ModelChoice(() => [PROFILE]).start(ctx({ model: picked }), []), undefined, "a reloaded shell remembers the pick");
	assert.equal(c.start(ctx(), []), LINE, "but another fallback is still one");
	c.picked({ source: "cycle", model: FALLBACK });
	assert.equal(c.start(ctx(), []), undefined, "cycling to it is choosing it");
});

// Was "a saved session that restored its own model is untouched" (L920): the shell no longer reads the
// session's history (L944), so a resumed session is judged as any other.
test("a resumed session on a model of an earlier process is held while a named default cannot run, --continue or not; its history is not read", () => {
	const earlier = { provider: "claude-bridge", id: "claude-opus-5-5" };
	const read = () => { throw new Error("the session's history was read"); };
	const resumed = { ...ctx({ model: earlier }), sessionManager: { getBranch: read, getEntries: read } };
	const line = LINE.replace("claude-bridge/claude-fable-5-2", "claude-bridge/claude-opus-5-5");
	for (const argv of [["--continue"], ["--continue", "-p", "hi"], ["--session", "x.jsonl"], ["--resume"]]) assert.equal(choice().start(resumed, argv), line, argv.join(" "));
	assert.equal(choice().start(resumed, ["--continue", "--model", "claude-bridge/claude-opus-5-5"]), undefined, "named on the command line, it is the owner's");
	assert.equal(choice().start({ ...resumed, modelRegistry: registry(["anthropic", "claude-bridge"]) }, ["--continue"]), undefined, "no named default is broken: nothing is said, whatever the model");
});

test("a default that can run, and whatever cannot be read, say nothing", () => {
	assert.equal(choice().start(ctx({ modelRegistry: registry(["anthropic", "claude-bridge"]) }), []), undefined, "the default can run: another model is a project's setting or a carried pick");
	assert.equal(choice().start(ctx({ modelRegistry: undefined }), []), undefined);
	assert.equal(choice().start(ctx({ modelRegistry: { find: () => { throw new Error("down"); }, hasConfiguredAuth: () => false } }), []), undefined);
	assert.equal(choice().start(ctx({ model: undefined }), []), undefined);
	assert.equal(choice().start(undefined, []), undefined);
	ModelChoice.forget();
	assert.equal(new ModelChoice(() => []).start(ctx(), []), undefined, "settings that name no default");
});

// L926: the re-verification's breaks, gaps and surviving mutants.
test("--models is a choice only when a pattern matched (scoped models); one that matched nothing is none (BREAK 1)", () => {
	assert.equal(choice().start(ctx(), ["--models", "anthropic/*", "-p", "hi"]), LINE, "nothing matched: pi warned and fell back");
	assert.equal(choice().start(ctx(), ["--models=anthropic/*"]), LINE);
	assert.equal(choice().start(ctx({ scopedModels: [{ model: FALLBACK }] }), ["--models", "claude-*"]), undefined, "a pattern that matched scopes the models: the owner's");
});

test("--provider alone is the owner's choice; --model-x, x--model and a prompt that contains --model are not", () => {
	for (const argv of [["--provider", "claude-bridge"], ["--provider=claude-bridge"]]) {
		assert.equal(choice().start(ctx(), argv), undefined, argv.join(" "));
	}
	for (const argv of [["--model-x", "fable"], ["--modelx"], ["x--model"], ["--providers"], ["-p", "use --model fable"], ["-p", "--model"].slice(0, 1), ["say --provider=x"]]) {
		assert.equal(choice().start(ctx(), argv), LINE, argv.join(" "));
	}
});

// Removed with restored() (L944): "a saved session's own model is its last model change or its last answer" tested the
// order of a history scan that no longer exists; the resumed-session test above states the rule for that case.

test("a held shell is released by a later session start on a chosen model, and by one it cannot judge", () => {
	const c = choice();
	assert.equal(c.start(ctx(), []), LINE);
	assert.equal(c.start(ctx({ model: NAMED }), []), undefined);
	assert.equal(c.refused(), undefined, "on the profile's model again");
	assert.equal(c.start(ctx(), []), LINE);
	assert.equal(c.start(ctx({ modelRegistry: { find: () => { throw new Error("down"); }, hasConfiguredAuth: () => false } }), []), undefined);
	assert.equal(c.refused(), undefined, "a registry that throws judges nothing, and holds nothing");
	assert.equal(c.start(ctx(), []), LINE);
	assert.equal(c.start(ctx({ model: undefined }), []), undefined);
	assert.equal(c.refused(), undefined, "no model at all is pi's own error");
});

test("the named defaults are the agent folder's and the project's: a session on either is chosen; on neither, the one that cannot run is named (BREAK 3, GAP 1)", () => {
	const profile = { ...NAMED, from: "profile" as const };
	const project = (provider: string, id: string) => ({ provider, id, from: "project" as const });
	const named = (...list: Array<{ provider: string; id: string; from: "profile" | "project" }>) => { ModelChoice.forget(); return new ModelChoice(() => list); };
	const runnable = { provider: "claude-bridge", id: "claude-opus-5-5" };
	const known = ["anthropic/claude-opus-4-5", "claude-bridge/claude-fable-5-2", "claude-bridge/claude-opus-5-5", "nan/alpha"];
	const on = (model: { provider: string; id: string }, auth: string[]) => ctx({ model, modelRegistry: registry(auth, known) });
	// BREAK 3: the profile's default has no credential, the project's runs and is in use.
	assert.equal(named(project("claude-bridge", "claude-opus-5-5"), profile).start(on(runnable, ["claude-bridge"]), ["--approve", "-p", "hi"]), undefined);
	// GAP 1: the project's default has no credential, the profile's runs, and pi took a third model.
	assert.equal(named(project("anthropic", "claude-opus-4-5"), { provider: "claude-bridge", id: "claude-opus-5-5", from: "profile" }).start(on(FALLBACK, ["claude-bridge"]), []),
		"jhon: the project's .pi/settings.json names anthropic/claude-opus-4-5 for this session, but its provider anthropic has no credential; this session is on claude-bridge/claude-fable-5-2, which nobody chose in this run.");
	// An untrusted project's file is ignored by pi: the session is on the profile's default, a named one.
	assert.equal(named(project("anthropic", "claude-opus-4-5"), { provider: "claude-bridge", id: "claude-opus-5-5", from: "profile" }).start(on(runnable, ["claude-bridge"]), []), undefined);
	// Both named defaults can run and the session is on a third model: carried or set elsewhere, not a fallback.
	assert.equal(named(project("nan", "alpha"), { provider: "claude-bridge", id: "claude-opus-5-5", from: "profile" }).start(on(FALLBACK, ["claude-bridge", "nan"]), []), undefined);
	// Neither can run: the shell cannot tell which one pi read (the project's trust), so the line names both (L930).
	assert.equal(named(project("nan", "alpha"), profile).start(on(FALLBACK, ["claude-bridge"]), []),
		"jhon: the project's .pi/settings.json names nan/alpha (its provider nan has no credential) and the profile names anthropic/claude-opus-4-5 (its provider anthropic has no credential) for this session, and neither can run; this session is on claude-bridge/claude-fable-5-2, which nobody chose in this run.");
	// The project's file repeats the profile's model: one default, the profile's, true whether or not pi read the project's file (L942).
	assert.equal(named(project("anthropic", "claude-opus-4-5"), profile).start(on(FALLBACK, ["claude-bridge"]), []), LINE);
	assert.equal(named(profile, project("anthropic", "claude-opus-4-5")).start(on(FALLBACK, ["claude-bridge"]), []), LINE, "in either order");
	// Another model of the same provider is another default: both are named.
	assert.equal(named(project("anthropic", "claude-haiku-4-5"), profile).start(on(FALLBACK, ["claude-bridge"]), []),
		"jhon: the project's .pi/settings.json names anthropic/claude-haiku-4-5 (pi has no such model) and the profile names anthropic/claude-opus-4-5 (its provider anthropic has no credential) for this session, and neither can run; this session is on claude-bridge/claude-fable-5-2, which nobody chose in this run.");
	// A model pi does not have, beside one without a credential.
	assert.match(named(project("ghost", "none"), profile).start(on(FALLBACK, ["claude-bridge"]), [])!, /^jhon: the project's \.pi\/settings\.json names ghost\/none \(pi has no such model\) and the profile names anthropic\/claude-opus-4-5 \(its provider anthropic has no credential\) for this session, and neither can run; /);
	// Only the profile's cannot run, the project's can but pi did not read it (untrusted): the profile's is named.
	assert.equal(named(project("claude-bridge", "claude-opus-5-5"), profile).start(on(FALLBACK, ["claude-bridge"]), []), LINE);
});

test("settingsDefaults reads the project's .pi/settings.json over the agent folder's, key by key as pi merges them", () => {
	const agent = mkdtempSync(join(tmpdir(), "jhon-model-choice-agent-")), cwd = mkdtempSync(join(tmpdir(), "jhon-model-choice-cwd-"));
	try {
		writeFileSync(join(agent, "settings.json"), JSON.stringify({ defaultProvider: "anthropic", defaultModel: "claude-opus-4-5" }));
		assert.deepEqual(settingsDefaults(agent, cwd), [{ ...NAMED, from: "profile" }], "no project file");
		mkdirSync(join(cwd, ".pi"));
		writeFileSync(join(cwd, ".pi", "settings.json"), JSON.stringify({ theme: "x" }));
		assert.deepEqual(settingsDefaults(agent, cwd), [{ ...NAMED, from: "profile" }], "a project file that names no model");
		writeFileSync(join(cwd, ".pi", "settings.json"), JSON.stringify({ defaultProvider: "nan", defaultModel: "alpha" }));
		assert.deepEqual(settingsDefaults(agent, cwd), [{ provider: "nan", id: "alpha", from: "project" }, { ...NAMED, from: "profile" }]);
		writeFileSync(join(cwd, ".pi", "settings.json"), JSON.stringify({ defaultModel: "claude-haiku-4-5" }));
		assert.deepEqual(settingsDefaults(agent, cwd), [{ provider: "anthropic", id: "claude-haiku-4-5", from: "project" }, { ...NAMED, from: "profile" }], "the model alone: the provider is the agent folder's");
		writeFileSync(join(cwd, ".pi", "settings.json"), "{");
		assert.deepEqual(settingsDefaults(agent, cwd), [{ ...NAMED, from: "profile" }], "a broken project file names none");
		assert.deepEqual(settingsDefaults(agent, undefined), [{ ...NAMED, from: "profile" }]);
		assert.deepEqual(settingsDefaults(join(agent, "none"), cwd), [], "no agent settings and a broken project file");
		writeFileSync(join(agent, "settings.json"), JSON.stringify({ defaultProvider: "anthropic" }));
		assert.deepEqual(settingsDefaults(agent, undefined), [], "a provider with no model names no default");
		writeFileSync(join(agent, "settings.json"), JSON.stringify({ defaultModel: "claude-opus-4-5" }));
		assert.deepEqual(settingsDefaults(agent, undefined), [], "nor a model with no provider");
	} finally {
		rmSync(agent, { recursive: true, force: true });
		rmSync(cwd, { recursive: true, force: true });
	}
});

test("the owner's pick of this process is known to a later session start, to be put back (GAP 3)", () => {
	const c = choice();
	assert.equal(ModelChoice.pick(), undefined);
	c.picked({ source: "restore", model: FALLBACK });
	assert.equal(ModelChoice.pick(), undefined);
	c.picked({ source: "set", model: { provider: "claude-bridge", id: "claude-opus-5-5" } });
	assert.deepEqual(ModelChoice.pick(), { provider: "claude-bridge", id: "claude-opus-5-5" });
	ModelChoice.forget();
	assert.equal(ModelChoice.pick(), undefined);
});

// Was N1 (L930), "an aborted or failed answer is not the session's own model": with no history read (L944) the case
// needs no rule of its own. What it protected is stated here: nothing a held session's own aborted run leaves behind
// can lift the hold at the next start.
test("a held session stays held at its next start, whatever its own aborted run left in the session (N1)", () => {
	const c = choice();
	assert.equal(c.start(ctx(), []), LINE);
	const after = { ...ctx(), sessionManager: { getBranch: () => [{ type: "model_change", provider: "claude-bridge", modelId: "claude-fable-5-2" }, { type: "custom_message" }, { type: "message", message: { role: "assistant", provider: "claude-bridge", model: "claude-fable-5-2", stopReason: "aborted" } }, { type: "message", message: { role: "user" } }] } };
	assert.equal(c.start(after, ["--continue", "-p", "hi"]), LINE, "a new process with --continue");
	assert.equal(c.start(after, []), LINE, "a reload");
});

test("the pick keeps a model id that contains a slash whole", () => {
	const c = choice();
	c.picked({ source: "set", model: { provider: "openrouter", id: "anthropic/claude-sonnet-4.5" } });
	assert.deepEqual(ModelChoice.pick(), { provider: "openrouter", id: "anthropic/claude-sonnet-4.5" });
	const model = { provider: "openrouter", id: "anthropic/claude-sonnet-4.5" };
	assert.equal(c.start(ctx({ model }), []), undefined, "and a session on it is the owner's");
	ModelChoice.forget();
});

test("the held session's fallback is what the owner may take as theirs; taken, it is a pick (N2)", () => {
	const c = choice();
	assert.equal(c.fallback(), undefined);
	assert.equal(c.start(ctx(), []), LINE);
	assert.deepEqual(c.fallback(), FALLBACK);
	c.picked({ source: "set", model: c.fallback() });
	assert.equal(c.refused(), undefined);
	assert.equal(c.fallback(), undefined);
	assert.deepEqual(ModelChoice.pick(), FALLBACK, "remembered for this process");
	assert.equal(c.start(ctx(), []), undefined, "a later /new on it is not held");
	ModelChoice.forget();
});

// L942: a /new on a held session says the line once; any other start says it; a pick forgets it.
test("the line is announced at every start but a /new that repeats it; a pick and a start that holds nothing forget it", () => {
	ModelChoice.forget();
	assert.equal(ModelChoice.announce(LINE), true, "a startup");
	assert.equal(ModelChoice.announce(LINE, true), false, "/new, the same line");
	assert.equal(ModelChoice.announce(LINE), true, "a reload or a resume says it again");
	assert.equal(ModelChoice.announce(`${LINE} other`, true), true, "/new, another line");
	assert.equal(ModelChoice.announce(undefined, true), false, "nothing held, nothing said");
	assert.equal(ModelChoice.announce(LINE, true), true, "and the line is news again");
	const c = choice();
	assert.equal(ModelChoice.announce(LINE), true);
	c.picked({ source: "restore", model: FALLBACK });
	assert.equal(ModelChoice.announce(LINE, true), false, "a restore is not a pick: nothing forgotten");
	c.picked({ source: "set", model: FALLBACK });
	assert.equal(ModelChoice.announce(LINE, true), true, "a pick forgets the line said");
	ModelChoice.forget();
});
