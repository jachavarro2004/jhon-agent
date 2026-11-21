/**
 * shell.appearance.ascii reaches every overlay (T15.11, L121): Settings, the
 * change detail, /jhon:grants, /jhon:profile, the ask view and /jhon:agents
 * draw their frame, header, footer and rows in plain ASCII, nothing outside
 * 0x20-0x7e.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-ascii-overlays.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { AskView } from "../lib/ui/ask-view.ts";
import { JhonPanel, type ProfilesData } from "../lib/ui/jhon-panel.ts";
import { customizeRows as settingsRows } from "../lib/ui/customize/registry.ts";
import { SettingsView } from "../lib/ui/settings-view.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { TasksView } from "../lib/ui/tasks-view.ts";
import type { TaskRecord } from "../lib/background-tasks.ts";
import { memorySettings, useMemorySettings } from "./fake-jhon-settings.ts";

const plain = { fg: (_r: string, t: string) => t, bold: (t: string) => t };
const flush = () => new Promise((r) => setImmediate(r));
/** A fixed clock: 5 September 2026, 09:20 UTC. */
const NOW = Date.UTC(2026, 8, 5, 9, 20);

function pureAscii(name: string, lines: string[]): void {
	assert.ok(lines.length > 0, `${name} drew nothing`);
	const bad = lines.map(stripAnsi).filter((line) => !/^[\x20-\x7e]*$/.test(line));
	assert.deepEqual(bad, [], `${name} leaks non-ASCII:\n${lines.map(stripAnsi).join("\n")}`);
}

const profiles: ProfilesData = { active: "default", file: "~/.config/jhon/profiles.json", profiles: [
	{ name: "default", provider: "claude-bridge", delegation: "inline", agents: [{ agent: "worker", runtime: "pi", model: "claude-bridge/claude-opus-5-5@high", source: "agents.yaml tiers.x.strong" }] },
	{ name: "jhon", delegation: "writer", agents: [{ agent: "worker", runtime: "pi", model: "claude-bridge/claude-fable-5-1@medium", source: "profile agent worker.pi" }] },
] };
const status = {
	change: "abc-process-exercises", next: "apply", strict_tdd: { value: true },
	route: { level: "verified", design: false, reasons: ["high risk: reaches the level that asks for verification (high)"] },
	tasks: { total: 8, completed: 5, slice: "Slice 2: deadline extension", slices: [
		{ name: "Slice 1: launch", total: 4, completed: 4, delivers: "CA-09" },
		{ name: "Slice 2: deadline extension", total: 3, completed: 1, delivers: "CA-13", tasks: [{ text: "2.1 a", done: true }, { text: "2.2 extend the deadline", done: false }, { text: "2.3 c", done: false }] },
		{ name: "Slice 3: panel", total: 1, completed: 0 }] },
	artifacts: [{ artifact: "exploration.md", state: "done" }, { artifact: "design.md", state: "missing" }, { artifact: "tasks.md", state: "done" }, { artifact: "verify-report.md", state: "missing" }],
	tests: { timestamp: "2026-10-29T05:00:00Z", passed: false, failed: ["a"], scope: "affected", complete: true, commit: "94ce20d8a7", clean: false },
	notes: ["scenarios named by a test: 49/50"],
	lanes: [{ change: "fix-infra", worktree: "infra", done: 2, slices: 5, next: "verify" }],
	root: "/p/acme", directory: "openspec/changes/abc-process-exercises",
	risk: { tier: "high", reasons: [{ level: "medium", file: "backend/abc/views.py", why: "code" }, { level: "high", file: "backend/svc/policy.py", why: "credentials" }] },
};

function panel(view: "change" | "grants" | "profiles") {
	return new JhonPanel({
		theme: plain, runtime: "pi", view, ascii: true, height: () => 60,
		load: async () => profiles, use: async () => undefined, loadChange: async () => status,
		loadGrants: async () => ({ root: "/p/acme", classes: ["push", "destroy"], grants: [{ class: "push", root: "/p/acme", until: new Date(Date.now() + 600_000).toISOString() }] }),
		grant: async () => {}, revoke: async () => {},
		loadTests: async () => ({}), startTests: () => ({ id: "job-1", startedAt: 0 }), stopTests: () => {},
		openFile: () => undefined,
		onClose() {}, requestRender() {},
	});
}

test("Settings draws in ASCII", (t) => {
	useMemorySettings(t, { "shell.appearance.ascii": true });
	const noop = () => {};
	const rows = settingsRows({ settings: memorySettings({ "shell.appearance.ascii": true }), animations: noop, doubleEsc: noop, status: noop, redraw: noop });
	for (const width of [160, 120, 100, 80, 50]) pureAscii(`Settings at ${width}`, new SettingsView({ rows, theme: plain, height: () => 30, ascii: () => true, onClose() {}, requestRender() {} }).render(width));
});

for (const view of ["change", "grants", "profiles"] as const) {
	test(`the ${view} overlay draws in ASCII`, async (t) => {
		useMemorySettings(t, { "shell.appearance.ascii": true });
		const p = panel(view);
		p.open();
		for (let i = 0; i < 4; i++) await flush();
		for (const width of [160, 100, 80]) pureAscii(`${view} ${width}`, p.render(width));
		p.dispose();
	});
}

test("the ask view draws in ASCII", () => {
	const ask = new AskView({ step: 1, total: 2, title: "Route", question: "Which route?", multi: false, theme: plain, done() {}, ascii: true,
		items: [{ value: "A", label: "A", description: "the first" }, { value: "B", label: "B" }] });
	for (const width of [160, 100, 80, 48]) pureAscii(`ask ${width}`, ask.render(width));
});

test("/jhon:jobs draws in ASCII", () => {
	const tasks: TaskRecord[] = [
		{ kind: "command", id: "job-1", label: "CI for PR 1834", command: "gh run watch 42", cwd: "/repo", ownerSessionId: "s1", outputPath: "/tmp/jobs/job-1.log", startedAt: 0, status: "running", tail: ["check lint: pass"] },
		{ kind: "command", id: "job-2", label: "build", command: "make", cwd: "/repo", ownerSessionId: "s1", outputPath: "/tmp/jobs/job-2.log", startedAt: 0, endedAt: 4_000, status: "exited", exitCode: 1, tail: [] },
		{ kind: "command", id: "job-3", label: "watch", command: "w", cwd: "/repo", ownerSessionId: "s1", outputPath: "/tmp/jobs/job-3.log", startedAt: 0, endedAt: 4_000, status: "stopped", tail: [] },
	] as TaskRecord[];
	const view = new TasksView({ theme: plain, rows: () => 14, tasks: () => tasks, now: () => 125_000, onStop() {}, onClose() {}, requestRender() {}, ascii: true });
	try {
		for (const width of [160, 100, 80]) pureAscii(`tasks ${width}`, view.render(width));
		assert.match(view.render(100)[1]!, /^\| Jobs {2}1 running - 2 finished +esc \|$/, "the title row, its dots as dashes");
		for (const width of [160, 100, 80, 60]) pureAscii(`tasks narrow ${width}`, view.render(width));
		view.handleInput("s");
		for (const width of [160, 100, 80]) pureAscii(`tasks confirming ${width}`, view.render(width));
	} finally {
		view.dispose();
	}
});

