/**
 * shell/lib/ui/review-card.ts — the Prism review card (design/shell-tui 01, T174-S7).
 *
 *   ◇ Prism review  add-invoice-export                 ✗ changes requested
 *     facets      ✓ safety   ✓ clarity   ✓ behavior   ✗ failure 1 finding
 *     challenger  kept 1 of 2 findings · arbiter agreed
 *     ✗ critical  billing/api/export.py:88  an empty range answers 200 …
 *     record      odd/changes/add-invoice-export/review-prism.md
 *
 * Drawn from jhon_review's result details (lib/review-tool.ts): the change,
 * each facet's state, every finding with the set the engine put it in, the
 * challenger's and the arbiter's counts and the record. Collapsed, the
 * findings that block (the correction set, ✗) or wait for verify (◐), each
 * cut to one row; ctrl+o shows every finding whole, the refuted ones and an
 * unavailable facet's reason too. shell.appearance.density "minimal" keeps
 * the facets and those findings. A fallback to the facet agents, an engine
 * refusal or an error shows the tool's own words.
 */
import type { ToolRenderers } from "@earendil-works/pi-coding-agent";
import { visibleWidth } from "@earendil-works/pi-tui";
import { shellSettings } from "../settings.ts";
import { asciiText } from "./primitives/glyphs.ts";
import { stateGlyph } from "./primitives/states.ts";
import { TOOL_FAMILY, toolGlyph } from "./primitives/tool-icons.ts";
import { sanitizeTerminalText } from "./terminal-theme.ts";
import { SplitBody, SplitCall, wrapRows, type CardLook, type SplitContext, type SplitResult } from "./tool-card.ts";
import { ownsIcon } from "./tool-icons.ts";

export interface ReviewFacet { facet: string; status: string; reason?: string }

/** One finding and the set the engine's reduction put it in (correction_set, for_verify, follow_ups, information, refuted). */
export interface ReviewFinding { id: string; facet: string; severity: string; location: string; claim: string; set: string }

/** What jhon_review's details carry for the card. */
export interface ReviewCardDetails {
	status?: string;
	record?: string;
	verdict?: string;
	change?: string;
	facets?: ReviewFacet[];
	findings?: ReviewFinding[];
	/** The challenger's pass: how many findings it read and how many it refuted. */
	challenger?: { findings: number; refuted: number };
	/** The arbiter's pass: how many findings it answered and how many it confirmed. */
	arbiter?: { asked: number; confirmed: number };
}

type Paint = { fg(role: string, text: string): string; bold(text: string): string };

/** The label column: as wide as `challenger  `, wider when a label needs it. */
const COLUMN = 12;
/** Collapsed, at most this many findings, then how many more. */
const COLLAPSED_FINDINGS = 3;
/** The body of a result without the review's details: its first lines. */
const PREVIEW = 3;
const SEVERITY = ["BLOCKER", "CRITICAL", "WARNING", "SUGGESTION"];
const SETS = ["correction_set", "for_verify", "follow_ups", "information", "refuted"];
/** The reviewed change, as the result tells the call's header in the row state pi shares between them. */
const CHANGE = Symbol.for("jhon-shell.review-card-change");

const clean = (value: unknown) => sanitizeTerminalText(String(value ?? "")).replace(/\s+/g, " ").trim();
const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

function detailsOf(value: unknown): ReviewCardDetails {
	return typeof value === "object" && value !== null && !Array.isArray(value) ? (value as ReviewCardDetails) : {};
}

function rank(f: ReviewFinding): number {
	const set = SETS.indexOf(f.set);
	const severity = SEVERITY.indexOf(f.severity);
	return (set < 0 ? SETS.length : set) * 10 + (severity < 0 ? SEVERITY.length : severity);
}

/** A row's label and its text, the text wrapped under the text column when expanded. */
interface Line { label: string; text: string }

function textOf(result: { content?: unknown }): string {
	const content = Array.isArray(result?.content) ? result.content : [];
	return sanitizeTerminalText(content.flatMap((part: any) => (part?.type === "text" && typeof part.text === "string" ? [part.text] : [])).join("\n")).trim();
}

/** The review's rows, painted, at cells text cells. */
function reviewRows(d: ReviewCardDetails, t: Paint, look: CardLook, cells: number, expanded: boolean, minimal: boolean): string[] {
	const a = look.ascii;
	const words = (text: string) => (a ? asciiText(text) : text);
	const facets = d.facets ?? [];
	const findings = [...(d.findings ?? [])].sort((x, y) => rank(x) - rank(y));
	const kept = findings.filter((f) => f.set !== "refuted");
	const lines: Line[] = [];

	const states = facets.map((f) => {
		const name = clean(f.facet);
		if (f.status === "unavailable") return `${t.fg("error", stateGlyph("failed", a))} ${t.fg("text", name)} ${t.fg("error", "unavailable")}`;
		// ✗ while one of its kept findings blocks or waits for verify; the count says every kept one.
		const own = kept.filter((k) => k.facet === f.facet);
		const blocks = own.some((k) => k.set === "correction_set" || k.set === "for_verify");
		const count = own.length ? ` ${t.fg(blocks ? "error" : "muted", plural(own.length, "finding"))}` : "";
		return `${blocks ? t.fg("error", stateGlyph("failed", a)) : t.fg("success", stateGlyph("done", a))} ${t.fg("text", name)}${count}`;
	});
	if (states.length) lines.push({ label: "facets", text: states.join("   ") });

	if (!minimal && d.challenger && d.challenger.findings > 0) {
		const n = d.challenger.findings;
		const arbiter = d.arbiter && d.arbiter.asked > 0
			? `${t.fg("muted", words(" · arbiter"))} ${t.fg("text", d.arbiter.confirmed === d.arbiter.asked ? "agreed" : `confirmed ${d.arbiter.confirmed} of ${d.arbiter.asked}`)}`
			: "";
		lines.push({ label: "challenger", text: `${t.fg("text", `kept ${n - d.challenger.refuted} of ${plural(n, "finding")}`)}${arbiter}` });
	}

	const finding = (f: ReviewFinding): Line & { role: string } => {
		const severity = clean(f.severity).toLowerCase();
		const where = t.fg("mdCode", clean(f.location));
		if (f.set === "refuted") return { label: "refuted", text: `${t.fg("dim", clean(f.location))}  ${t.fg("dim", clean(f.claim))}`, role: "muted" };
		const [glyph, role] = f.set === "correction_set" ? [stateGlyph("failed", a), "error"] : f.set === "for_verify" ? [stateGlyph("working", a), "warning"] : [a ? "." : "·", "dim"];
		return { label: `${glyph} ${severity}`, text: `${where}  ${t.fg(role === "dim" ? "muted" : "text", clean(f.claim))}`, role };
	};
	const shown = expanded ? findings : kept.filter((f) => f.set === "correction_set" || f.set === "for_verify");
	const listed = expanded ? shown : shown.slice(0, COLLAPSED_FINDINGS);
	const findingLines = listed.map(finding);
	lines.push(...findingLines);
	if (shown.length > listed.length) lines.push({ label: "", text: t.fg("muted", `${a ? "~" : "…"} ${plural(shown.length - listed.length, "more finding")}`) });

	if (expanded) for (const f of facets) if (f.status === "unavailable" && f.reason) lines.push({ label: "unavailable", text: t.fg("muted", `${clean(f.facet)}: ${clean(f.reason)}`) });
	if (!minimal && d.record) lines.push({ label: "record", text: t.fg("mdCode", clean(d.record)) });

	const column = Math.max(COLUMN, ...lines.map((line) => visibleWidth(line.label) + 2));
	const roles = new Map<Line, string>(findingLines.map((line) => [line, line.role]));
	return lines.flatMap((line) => {
		const role = roles.get(line) ?? "muted";
		const label = t.fg(role, line.label) + " ".repeat(column - visibleWidth(line.label));
		const text = words(line.text);
		if (!expanded) return [`${label}${text}`];
		const [first = "", ...rest] = wrapRows(text, Math.max(1, cells - column));
		return [`${label}${first}`, ...rest.map((row) => `${" ".repeat(column)}${row}`)];
	});
}

/** The card for a result: the verdict and the review's rows, or the tool's own words. */
function reviewResult(result: { content?: unknown; details?: unknown }, isError: boolean, isPartial: boolean, t: Paint): SplitResult {
	const d = detailsOf(result.details);
	const sign = (name: "done" | "failed" | "working", ascii: boolean) => stateGlyph(name, ascii);
	let minimal = false;
	try { minimal = shellSettings().density() === "minimal"; } catch { /* Not minimal. */ }
	if (!isError && !isPartial && d.status === "recorded") {
		const fail = d.verdict === "fail";
		const pass = d.verdict === "pass";
		return {
			get right() {
				let ascii = false;
				try { ascii = shellSettings().ascii(); } catch { /* Unicode. */ }
				if (fail) return t.fg("error", `${sign("failed", ascii)} ${t.bold("changes requested")}`);
				if (pass) return t.fg("success", `${sign("done", ascii)} approved`);
				return t.fg("muted", clean(d.verdict));
			},
			tone: "normal",
			rows: (cells, look, expanded) => reviewRows(d, t, look, cells, expanded, minimal),
		};
	}
	const text = textOf(result);
	const failed = isError || d.status === "refused";
	return {
		get right() {
			let ascii = false;
			try { ascii = shellSettings().ascii(); } catch { /* Unicode. */ }
			if (isPartial) return t.fg("warning", `${sign("working", ascii)} running`);
			if (failed) return t.fg("error", `${sign("failed", ascii)} failed`);
			if (d.status === "fallback") return t.fg("warning", `${sign("working", ascii)} agents instead`);
			return undefined;
		},
		tone: failed ? "error" : "normal",
		rows: (cells, _look, expanded) => {
			const lines = text ? text.split("\n") : [];
			const role = failed ? "error" : "muted";
			return expanded ? lines.flatMap((line) => wrapRows(t.fg(role, line), cells)) : lines.slice(0, PREVIEW).map((line) => t.fg(role, line));
		},
	};
}

/** jhon_review's renderers: the Prism review card. */
export function reviewRenderers(): Pick<ToolRenderers, "renderCall" | "renderResult"> {
	return {
		renderCall: ownsIcon((_args, theme, context) => {
			const t = theme as unknown as Paint;
			const row = (context ?? {}) as SplitContext & { state?: unknown };
			return new SplitCall((look) => {
				let style: "geometric" | "emoji" = "geometric";
				try { style = shellSettings().icons(); } catch { /* Geometric. */ }
				const told = row.state && typeof row.state === "object" ? (row.state as Record<symbol, { change?: string } | undefined>)[CHANGE] : undefined;
				const change = clean(told?.change);
				return {
					icon: toolGlyph(TOOL_FAMILY.AGENTS, { ascii: look.ascii, style }),
					title: `${t.fg("toolTitle", t.bold("Prism review"))}${change ? t.fg("muted", `  ${change}`) : ""}`,
				};
			}, t as never, row);
		}),
		renderResult(result, options, theme, context) {
			const t = theme as unknown as Paint;
			const ctx = (context ?? {}) as SplitContext;
			const change = detailsOf(result?.details).change;
			if (ctx.state && typeof ctx.state === "object") (ctx.state as Record<symbol, { change?: string }>)[CHANGE] = { change };
			const isError = ctx.isError ?? ("isError" in options && (options as { isError?: boolean }).isError === true);
			return new SplitBody(reviewResult(result as never, isError === true, options.isPartial === true, t), { ...ctx, expanded: options.expanded }, t as never);
		},
	};
}
