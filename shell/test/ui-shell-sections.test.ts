/**
 * The sections style (T32, L96-L97): the minimalist chrome beside the Gentle
 * Shell. A thin identity line on top, the prompt under a rule-line header, one
 * full-width status line on its own band under everything, and the rail as
 * plain sections with one dim line of keys under them. It is the only style
 * (L76: float and neon are gone).
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { BAND_ROLE } from "../lib/ui/primitives/index.ts";
import { renderShellAgentsSection, renderShellJobsSection, renderShellStatusBar, renderShellStatusSections, type ShellBarModel, type ShellBarTheme } from "../lib/ui/shell-bar.ts";
import { setChromeSurface } from "../lib/ui/shell-panel.ts";
import { framePromptLines, resolvePromptLayout, PROMPT_STATE } from "../lib/ui/shell-prompt.ts";
import { narrowStatusOwner, STATUS_OWNER } from "../lib/ui/shell-sidebar-layout.ts";

const plain: ShellBarTheme & { strikethrough(t: string): string } = { fg: (_c, t) => t, bold: (t) => t, strikethrough: (t) => t };
const tagged: ShellBarTheme & { strikethrough(t: string): string } = { fg: (c, t) => `<${c}>${t}</${c}>`, bold: (t) => `<b>${t}</b>`, strikethrough: (t) => t };
const BG_OPEN = "\x1b[48;5;236m";
const BG_CLOSE = "\x1b[49m";

const NOW = 1_788_600_000_000;
function model(overrides: Partial<ShellBarModel> = {}): ShellBarModel {
	return {
		cwd: "/home/owner/code/jhon",
		branch: "main",
		sessionName: undefined,
		modelId: "claude-opus-5-5",
		effort: "high",
		profile: "jhon",
		contextPercent: 45,
		contextWindow: 1_000_000,
		costTotal: 0,
		subscription: true,
		...overrides,
	};
}

// The title row and the status bar are the Gentle Shell's split chrome now:
// test/ui-shell-chrome.test.ts holds them (T15.12d slice 6).

test("the prompt in sections: a rule-line header with the petal and state, then › and the input", () => {
	const layout = resolvePromptLayout(40, undefined, undefined, true);
	assert.equal(layout.panel, true);
	assert.equal(layout.prefixWidth, 2);
	assert.equal(layout.nativeWidth, 38);
	const lines = framePromptLines(["─".repeat(38), "hello", "─".repeat(38)], 40, { state: PROMPT_STATE.IDLE, tick: 0, borderColor: (t) => t, fg: (_c, t) => t }, layout);
	assert.deepEqual(lines.map((l) => visibleWidth(l)), [40, 40, 40]);
	assert.equal(lines[0], `─ 力 ready for you ${"─".repeat(21)}`, "力 is two cells");
	assert.equal(lines[1], `› hello${" ".repeat(33)}`);
	assert.equal(lines[2].trim(), "");
	const working = framePromptLines(["─".repeat(38), "a", "b", "─".repeat(38)], 40, { state: PROMPT_STATE.QUEUED, tick: 1, borderColor: (t) => t, fg: (_c, t) => t }, layout);
	assert.match(working[0], /^─ 力 queued ─+$/, "the brand petal, Jhon's state words");
	assert.match(working[2], /^ {2}b/, "continuation rows keep the input's column");
	assert.equal(resolvePromptLayout(3, undefined, undefined, true).panel, undefined, "under 4 columns the prompt keeps a frame, as G's");
	assert.equal(resolvePromptLayout(40).panel, undefined, "outside the fullscreen chrome the prompt is a card, as G's");
});

test("the rail sections name their state: LDD the route, AGENTS the count; an idle AGENTS is its header and `no agent running` (L165)", () => {
	const status = renderShellStatusSections(model({ ldd: { change: "my-change", route: "verified", slices: ["done", "current"], next: "apply" } as ShellBarModel["ldd"] }), plain, 50);
	assert.match(status[0]!, /^▾ LDD +verified route$/);
	assert.match(status[1]!, /^ {2}◆ my-change *$/, "the route is said once, in the header");
	assert.deepEqual(renderShellAgentsSection([], plain, 50), ["▾ AGENTS" + " ".repeat(50 - 12) + "idle", "  no agent running · /jhon:agents".padEnd(50)], "the idle AGENTS");
	const busy = renderShellAgentsSection([{ id: "a", kind: "agent", label: "x", startedAt: NOW }, { id: "b", kind: "agent", label: "y", startedAt: NOW }], plain, 50);
	assert.match(busy[0]!, /^▾ AGENTS +2 running$/);
});

test("STATUS in sections: one block per change, the header counting them, and no action line (T35, L110)", () => {
	const session = { change: "refactor-layering-by-service", slices: Array(7).fill("done"), route: "verified", next: "verify", tests: { passed: false, failed: 0, at: "2026-11-04T05:00:00Z" }, worktree: "layering", root: "/w/layering" } as NonNullable<ShellBarModel["ldd"]>;
	const lane = { change: "fix-infra-hardening", worktree: "infra-hardening", root: "/w/infra-hardening", done: 10, slices: 10, next: "archive" };
	// The LDD section's rows, read under its header (T174: it is the rail's only Status section).
	const inner = (lines: string[]) => lines.slice(1).map((l) => l.slice(2).trimEnd());
	const twoCard = renderShellStatusSections(model({ ldd: { ...session, lanes: [lane] } }), plain, 50);
	assert.match(twoCard[0]!, /^▾ LDD +2 changes$/);
	const two = inner(twoCard);
	assert.deepEqual(two.slice(0, 5), [
		"◆ refactor-layering-by-service ⎇ layering",
		"  7/7 slices · verified route",
		"  next verify · tests ✗ failed",
		"◆ fix-infra-hardening          ⎇ infra-hardening",
		"  10/10 slices · next archive",
	], two.join("\n"));
	assert.ok(!two.some((l) => /enter detail|t tests|d diff|o tasks\.md/.test(l)), "no action line: its keys work only in the alt+j panel");
	const oneCard = renderShellStatusSections(model({ ldd: session }), plain, 50);
	assert.match(oneCard[0]!, /^▾ LDD +verified route$/);
	assert.deepEqual(inner(oneCard).slice(0, 3), [
		"◆ refactor-layering-by-service" + " ".repeat(8) + "⎇ layering",
		"  7/7 slices",
		"  next verify · tests ✗ failed",
	], "one change: the header says its route, said once");
	const many = Array.from({ length: 6 }, (_, i) => ({ ...lane, change: `c${i}`, worktree: `w${i}` }));
	const sevenCard = renderShellStatusSections(model({ ldd: { ...session, lanes: many } }), plain, 50);
	assert.match(sevenCard[0]!, /^▾ LDD +7 changes$/, "the count includes the lanes past the four shown");
	assert.ok(inner(sevenCard).includes("+2 more"), sevenCard.join("\n"));
	assert.match(renderShellStatusSections(model(), plain, 50)[0]!, /^▾ LDD *$/, "no change: the title alone");
	const narrow = renderShellStatusSections(model({ ldd: { ...session, lanes: [lane] } }), plain, 30);
	assert.ok(narrow.every((l) => visibleWidth(l) <= 30), narrow.join("\n"));
	assert.equal(inner(narrow)[0], "◆ refacto… ⎇ layering", "at a narrow width the name is cut first");
});

test("JOBS hides while idle; AGENTS stays as G's idle row", () => {
	assert.deepEqual(renderShellAgentsSection([], plain, 50), ["▾ AGENTS" + " ".repeat(50 - 12) + "idle", "  no agent running · /jhon:agents".padEnd(50)]);
	assert.deepEqual(renderShellJobsSection([], plain, 50), []);
});

test("the status line owns the status at narrow fullscreen widths, unless Status is hidden", () => {
	assert.equal(narrowStatusOwner({ mode: "fullscreen", columns: 100, statusPlacement: "auto" }), STATUS_OWNER.BOTTOM);
	assert.equal(narrowStatusOwner({ mode: "fullscreen", columns: 100, statusPlacement: "hidden" }), STATUS_OWNER.HEADER);
	assert.equal(narrowStatusOwner({ mode: "fullscreen", columns: 140, statusPlacement: "auto" }), undefined, "the rail carries it when wide");
});

// T34 (L98): the band is darker than the page (rose ink's `band`, Mix(page,
// frame, 0.5)); pi reads it as customMessageBg. Notices are cards in the
// card style now (L153 answer 1, ui-primitives-frame.test.ts).
test("a tinted status bar and rail paint one band role: pi's customMessageBg", () => {
	assert.equal(BAND_ROLE, "customMessageBg");
	const roles: string[] = [];
	setChromeSurface("tinted");
	try {
		renderShellStatusBar(model(), { ...plain, bg: (c: string, t: string) => { roles.push(c); return `${BG_OPEN}${t}${BG_CLOSE}`; } }, 60);
	} finally { setChromeSurface("transparent"); }
	assert.deepEqual([...new Set(roles)], [BAND_ROLE]);
});
