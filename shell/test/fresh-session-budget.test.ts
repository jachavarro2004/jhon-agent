/**
 * T115a (L691): the context notice comes at whichever is first, the share of
 * the window (shell.behaviour.fresh_session_at) or an absolute token budget
 * (shell.behaviour.fresh_session_tokens, 150000 by default; 0 turns it off).
 */
import assert from "node:assert/strict";
import test from "node:test";
import { installShellSettings, SETTINGS_DEFAULTS, shellSettings } from "../lib/settings.ts";
import { FreshSessionLatch } from "../lib/fresh-session.ts";
import { memorySettings } from "./fake-jhon-settings.ts";

test("the token budget is shell.behaviour.fresh_session_tokens, 150000 by default", () => {
	assert.equal(SETTINGS_DEFAULTS["shell.behaviour.fresh_session_tokens"], 150000);
	const previous = installShellSettings(memorySettings({ "shell.behaviour.fresh_session_tokens": 90000 }));
	try { assert.equal(shellSettings().freshSessionTokens(), 90000); } finally { installShellSettings(previous); }
	installShellSettings(memorySettings());
	try { assert.equal(shellSettings().freshSessionTokens(), 150000); } finally { installShellSettings(previous); }
});

test("on a large window the budget comes before the share: the notice names the budget, once per session", () => {
	const latch = new FreshSessionLatch(50, 150000);
	assert.equal(latch.context(14.9, 149_000), undefined, "under both");
	assert.equal(latch.context(16, 160_400), "One session per change: the context is at 160K tokens, past its 150K-token budget, and every turn re-reads it. Save a handoff (`jhon context <change> --phase none --handoff`, then mem_session_summary), then start a fresh session (/new in pi; /clear or a restart in Claude Code) and give it the handoff.");
	assert.equal(latch.context(20, 200_000), undefined, "once");
	assert.equal(latch.context(60, 600_000), undefined, "the share later says nothing more");
});

test("on a small window the share comes first and reads as today", () => {
	const latch = new FreshSessionLatch(50, 150000);
	assert.equal(latch.context(49, 62_720), undefined);
	assert.equal(latch.context(52, 66_560), "One session per change: the context is at 52% of its window, and every turn re-reads it. Save a handoff (`jhon context <change> --phase none --handoff`, then mem_session_summary), then start a fresh session (/new in pi; /clear or a restart in Claude Code) and give it the handoff.");
	assert.equal(latch.context(99, 300_000), undefined, "once");
});

test("a budget of 0 leaves only the share; unknown tokens leave only the share", () => {
	const off = new FreshSessionLatch(50, 0);
	assert.equal(off.context(30, 300_000), undefined);
	assert.match(off.context(50, 500_000) ?? "", /^One session per change: the context is at 50% of its window,/);
	const unknown = new FreshSessionLatch(50, 150000);
	assert.equal(unknown.context(30, null), undefined);
	assert.equal(unknown.context(30), undefined);
});

test("the compaction budget is shell.behaviour.compact_tokens, 200000 by default", () => {
	assert.equal(SETTINGS_DEFAULTS["shell.behaviour.compact_tokens"], 200000);
	const previous = installShellSettings(memorySettings({ "shell.behaviour.compact_tokens": 0 }));
	try { assert.equal(shellSettings().compactTokens(), 0); } finally { installShellSettings(previous); }
	installShellSettings(memorySettings());
	try { assert.equal(shellSettings().compactTokens(), 200000); } finally { installShellSettings(previous); }
});
