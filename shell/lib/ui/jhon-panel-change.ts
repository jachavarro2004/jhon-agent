/**
 * shell/lib/ui/jhon-panel-change.ts — the change detail's card (alt+j): the
 * whole of `jhon status --json` for the session's change, in the owner's
 * words. STATUS in the rail says where the work is; this card says all of
 * it: the route, its risk and why, every slice and what it delivers, the
 * open slice's tasks, the record's artifacts, the last test run, and the
 * notes, in one vocabulary (S29, L93: standard or verified route, done/total
 * slices, ✓ passed, ✗ N failed). Its keys act on the change (jhon-panel.ts): t runs its tests, d
 * opens its diff, o opens its tasks.md when it has one.
 *
 * Every word the engine names (next, the route's reasons, notes) is read
 * from it, never rephrased here.
 */
import { visibleWidth, wrapTextWithAnsi } from "@earendil-works/pi-tui";
import { ago, ROUTE_WORDS, type Lane } from "./ldd-status.ts";
import { STATES, type StateName } from "./primitives/states.ts";
import { THEME_NAME } from "./primitives/paint.ts";
import { runVerdict } from "./jhon-panel-tests.ts";

export interface ChangePaint {
	fg(role: string, text: string): string;
	bold(text: string): string;
}

/** The record's artifacts in the owner's words, in jhon status's order. */
const ARTIFACT_WORDS: Record<string, string> = {
	"exploration.md": "exploration",
	"research.md": "research",
	"proposal.md": "proposal",
	"specs/": "specs",
	"design.md": "design",
	"tasks.md": "tasks",
	"tdd-evidence.md": "TDD evidence",
	"verify-report.md": "verify report",
};

// Section headers: one style, bold accentSoft, sentence case, no blank row above.
const SECTION = "customMessageLabel";

function sliceName(name: string): string {
	return name.replace(/^Slice \d+:\s*/, "");
}

/** Whether the change's route asks for this missing artifact before delivery. */
function asked(artifact: string, route: any): boolean {
	if (artifact === "verify-report.md") return route?.level === "verified";
	if (artifact === "proposal.md" || artifact === "design.md") return route?.design === true;
	return false;
}

/** The artifact each of jhon status's `needs` names (T31, L252). */
const NEEDS: Record<string, string> = { "tasks.md": "tasks", "specs/": "spec", "proposal.md": "proposal", "design.md": "design" };

/**
 * Whether this change needs the artifact, by status `needs`; the verify
 * report by its route. Without `needs` (an older engine), undefined.
 */
function needed(artifact: string, status: any): boolean | undefined {
	const needs = status.needs;
	if (!needs || typeof needs !== "object") return undefined;
	if (artifact === "verify-report.md") return status.route?.level === "verified";
	return NEEDS[artifact] ? needs[NEEDS[artifact]] === true : false;
}

/** A change with no folder's RED and GREEN, from its runs (tests.red_before_green and the last run). */
function redGreen(tests: any, paint: ChangePaint): string {
	const glyph = (s: StateName) => paint.fg(THEME_NAME[STATES[s].role], STATES[s].glyph);
	if (!tests?.timestamp) return `${glyph("blocked")} RED test ${paint.fg(THEME_NAME[STATES.blocked.role], "missing: no run on record")}`;
	if (tests.red_before_green === true) return `${glyph("done")} RED → GREEN`;
	if (tests.passed !== true) return `${glyph("done")} RED   ${paint.fg("dim", "· GREEN next")}`;
	return `${glyph("blocked")} ${paint.fg(THEME_NAME[STATES.blocked.role], "GREEN without a RED before it")}`;
}

/** jhon risk's levels, highest first. */
const RISK_RANK: Record<string, number> = { high: 3, medium: 2, low: 1, passive: 0 };
const TOP_REASONS = 3;

/**
 * The risk row: the tier and its top reasons, the files `jhon risk --json`
 * (status.risk) names above passive, highest first; without it, the tier
 * the route read.
 */
function riskRow(status: any): string | undefined {
	const risk = status.risk;
	const tier = typeof risk?.tier === "string" ? risk.tier : status.route?.tier;
	if (typeof tier !== "string" || !tier) return undefined;
	const reasons = (Array.isArray(risk?.reasons) ? risk.reasons : [])
		.filter((r: any) => typeof r?.file === "string" && r.file && (RISK_RANK[r.level] ?? 0) > 0)
		.sort((a: any, b: any) => (RISK_RANK[b.level] ?? 0) - (RISK_RANK[a.level] ?? 0));
	const top = reasons.slice(0, TOP_REASONS).map((r: any) => r.why ? `${r.file} (${r.why})` : r.file);
	const more = reasons.length > TOP_REASONS ? [`+${reasons.length - TOP_REASONS} more`] : [];
	return [`risk ${tier}`, ...top, ...more].join(" · ");
}

/** The other worktrees' changes in progress (ldd-status.ts lddLanes), one row each: every lane, not only the one shown. */
function laneLines(lanes: unknown, paint: ChangePaint): string[] {
	if (!Array.isArray(lanes) || !lanes.length) return [];
	return [paint.bold(paint.fg(SECTION, "Also in progress")), ...lanes.map((l: Lane) => {
		// An umbrella is split into changes, never in progress itself (T37).
		if (l.umbrella) return `${paint.fg("dim", "○")} ${paint.bold(l.change)}  ${paint.fg("dim", [l.worktree, `umbrella · ${l.next ?? "start-change"}: split it`].filter(Boolean).join(" · "))}`;
		const facts = [l.slices ? `${l.done}/${l.slices} slices` : "", l.next ? `next ${l.next}` : ""].filter(Boolean).join(" · ");
		return `${paint.fg(THEME_NAME[STATES.working.role], STATES.working.glyph)} ${paint.bold(l.change)}  ${paint.fg("dim", [l.worktree, facts].filter(Boolean).join(" · "))}`;
	})];
}

/** The Cambio card's rows for status (`jhon status --json`), width columns wide. */
export function changeCardLines(status: any, paint: ChangePaint, width: number, now = Date.now()): string[] {
	if (!status || typeof status.change !== "string") {
		const umbrellas: string[] = Array.isArray(status?.umbrellas) ? status.umbrellas : [];
		const active: string[] = (Array.isArray(status?.activeChanges) ? status.activeChanges : []).filter((c: string) => !umbrellas.includes(c));
		// An umbrella alone is no change in progress: jhon status selects none (T37).
		if (umbrellas.length && !active.length) return [paint.fg("dim", `No change in progress; umbrella: ${umbrellas.join(", ")} (start-change: split it into changes)`), ...laneLines(status?.lanes, paint)];
		return [active.length
			? paint.fg("dim", `No change selected: ${active.length} active, and neither the branch nor a worktree names one.`)
			: paint.fg("dim", "No active LDD change in this project."), ...laneLines(status?.lanes, paint)];
	}
	const out: string[] = [];
	const wrap = (text: string, indent = "") => {
		for (const [i, l] of wrapTextWithAnsi(text, Math.max(10, width - visibleWidth(indent))).entries()) out.push(i ? indent + l : l);
	};
	const glyph = (s: StateName) => paint.fg(THEME_NAME[STATES[s].role], STATES[s].glyph);
	const route = status.route;
	out.push(`${paint.fg("accent", "◆")} ${paint.bold(status.change)}`);
	const folderless = status.folder === false;
	if (folderless) out.push(paint.fg("dim", "no folder · branch names it"));
	if (typeof status.worktree === "string") out.push(paint.fg("dim", `in worktree ${status.worktree}`));
	out.push([route?.level ? ROUTE_WORDS[route.level] ?? route.level : "", status.strict_tdd?.value ? "strict TDD" : ""].filter(Boolean).join(" · "));
	const risk = riskRow(status);
	if (risk) wrap(risk, "     ");
	for (const reason of route?.reasons ?? []) wrap(`  ${paint.fg("dim", `· ${reason}`)}`, "    ");
	if (status.next) out.push(`next ${paint.bold(status.next)}`);

	const tasks = status.tasks;
	const slices: any[] = Array.isArray(tasks?.slices) ? tasks.slices : [];
	if (slices.length) {
		const done = slices.filter((s) => s.total > 0 && s.completed >= s.total).length;
		out.push(`${paint.bold(paint.fg(SECTION, "Slices"))}  ${paint.fg("dim", `${done}/${slices.length} done · tasks ${tasks.completed}/${tasks.total}`)}`);
		const current = slices.findIndex((s) => s.name === tasks.slice);
		for (const [i, s] of slices.entries()) {
			const state = s.total > 0 && s.completed >= s.total ? "done" : i === current ? "working" : "pending";
			const name = `${i + 1} ${sliceName(s.name)}`;
			const head = `${glyph(state)} ${i === current ? paint.bold(name) : name}  ${paint.fg("dim", `${s.completed}/${s.total}`)}`;
			wrap(s.delivers ? `${head} ${paint.fg("dim", `· delivers ${s.delivers}`)}` : head, "    ");
			if (i === current) {
				const open = (s.tasks ?? []).findIndex((t: any) => !t.done);
				for (const [j, t] of (s.tasks ?? []).entries()) {
					const mark = t.done ? glyph("done") : j === open ? glyph("working") : glyph("pending");
					wrap(`    ${mark} ${t.done ? paint.fg("dim", t.text) : t.text}`, "      ");
				}
			}
		}
	}

	const artifacts: any[] = Array.isArray(status.artifacts) ? [...status.artifacts] : [];
	// A change with no folder lists no artifacts (T31b): what its needs ask for is still owed.
	if (folderless) {
		for (const artifact of Object.keys(NEEDS)) if (!artifacts.some((a) => a.artifact === artifact)) artifacts.push({ artifact, state: "missing" });
		artifacts.sort((a, b) => Object.keys(ARTIFACT_WORDS).indexOf(a.artifact) - Object.keys(ARTIFACT_WORDS).indexOf(b.artifact));
	}
	if (artifacts.length || folderless) {
		out.push(paint.bold(paint.fg(SECTION, "Record")));
		// What this change needs (T31 B): needed and missing marked, written done, not needed absent.
		const cells = artifacts.flatMap((a) => {
			const name = ARTIFACT_WORDS[a.artifact] ?? a.artifact;
			const need = needed(a.artifact, status);
			if (a.state === "done") return need === false ? [] : [`${glyph("done")} ${name}`];
			if (need ?? asked(a.artifact, route)) return [`${glyph("blocked")} ${name} ${paint.fg(THEME_NAME[STATES.blocked.role], "missing")}`];
			return need === false ? [] : [paint.fg("dim", `· ${name}`)];
		});
		if (folderless) cells.unshift(redGreen(status.tests, paint));
		let line = "";
		for (const cell of cells) {
			const next = line ? `${line}   ${cell}` : cell;
			if (line && visibleWidth(next) > width) {
				out.push(line);
				line = cell;
			} else line = next;
		}
		if (line) out.push(line);
	}

	const t = status.tests;
	out.push(paint.bold(paint.fg(SECTION, "Last run")));
	if (!t?.timestamp) out.push(paint.fg("dim", "No runs on record: jhon executions run records them."));
	else {
		const verdict = runVerdict(t, paint);
		const scope = t.scope === "affected" ? "affected" : t.scope === "full" ? "full" : t.scope;
		out.push([verdict, scope, ago(t.timestamp, now), t.commit ? paint.fg("dim", String(t.commit).slice(0, 7)) : ""].filter(Boolean).join(" · "));
		if (t.complete === false) out.push(paint.fg(THEME_NAME[STATES.blocked.role], "  did not finish: its record is incomplete"));
		if (t.clean === false) out.push(paint.fg("warning", "  ran on uncommitted changes: to deliver, commit and run again"));
		if (t.failed?.length) wrap(`  ${paint.fg(THEME_NAME[STATES.blocked.role], `failed: ${t.failed.join(", ")}`)}`, "    ");
	}

	const notes: string[] = Array.isArray(status.notes) ? status.notes : [];
	if (notes.length) {
		out.push(paint.bold(paint.fg(SECTION, "Notes")));
		for (const n of notes) wrap(`${paint.fg("dim", "·")} ${n}`, "  ");
	}
	// The other changes last, under their own header, so nothing above reads as theirs (L316).
	out.push(...laneLines(status.lanes, paint));
	return out;
}
