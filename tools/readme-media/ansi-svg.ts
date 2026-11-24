/**
 * tools/readme-media/ansi-svg.ts — terminal output (ANSI SGR escapes) to SVG text.
 *
 * The shell's components render rows of text painted with SGR escapes (pi's
 * theme in truecolor). This reads those rows into styled runs placed on a
 * cell grid and writes them as SVG: a background rect per painted run, and
 * text per run of one style, so a monospace font lines them up as the
 * terminal does. Wide glyphs (CJK, the 力 brand) take two cells; any glyph
 * outside ASCII is placed on its own cell, so a fallback font's other advance
 * never shifts the rest of the row.
 */
import { CELL, FONTS, esc, n } from "./identity.ts";

export interface Style {
  fg?: string;
  bg?: string;
  bold?: boolean;
  dim?: boolean;
  italic?: boolean;
  underline?: boolean;
  inverse?: boolean;
}

/** A run of text of one style starting at a cell column. */
export interface Run {
  col: number;
  text: string;
  cells: number;
  style: Style;
}

const BASIC = ["#000000", "#cd3131", "#0dbc79", "#e5e510", "#2472c8", "#bc3fbc", "#11a8cd", "#e5e5e5"];
const BRIGHT = ["#666666", "#f14c4c", "#23d18b", "#f5f543", "#3b8eea", "#d670d6", "#29b8db", "#ffffff"];

const hex2 = (v: number) => Math.max(0, Math.min(255, v | 0)).toString(16).padStart(2, "0");
const rgb = (r: number, g: number, b: number) => `#${hex2(r)}${hex2(g)}${hex2(b)}`;

/** xterm's 256-colour palette entry. */
function xterm256(i: number): string {
  if (i < 8) return BASIC[i]!;
  if (i < 16) return BRIGHT[i - 8]!;
  if (i < 232) {
    const k = i - 16;
    const step = (v: number) => (v === 0 ? 0 : 55 + v * 40);
    return rgb(step(Math.floor(k / 36)), step(Math.floor(k / 6) % 6), step(k % 6));
  }
  const g = 8 + (i - 232) * 10;
  return rgb(g, g, g);
}

/** Cells a grapheme takes: 0 for marks and joiners, 2 for wide East Asian and emoji, else 1. */
export function graphemeCells(g: string): number {
  const cp = g.codePointAt(0) ?? 0;
  if (/^\p{M}+$/u.test(g) || cp === 0x200d || cp === 0xfe0f) return 0;
  if (/\p{Extended_Pictographic}/u.test(g) && (g.includes("️") || cp >= 0x1f000)) return 2;
  if (
    (cp >= 0x1100 && cp <= 0x115f) || (cp >= 0x2e80 && cp <= 0xa4cf && cp !== 0x303f) || (cp >= 0xac00 && cp <= 0xd7a3) ||
    (cp >= 0xf900 && cp <= 0xfaff) || (cp >= 0xfe30 && cp <= 0xfe4f) || (cp >= 0xff00 && cp <= 0xff60) || (cp >= 0xffe0 && cp <= 0xffe6) ||
    (cp >= 0x20000 && cp <= 0x3fffd)
  ) return 2;
  return 1;
}

const segmenter = new Intl.Segmenter();

/** Apply one SGR parameter list to a style. */
function sgr(style: Style, params: number[]): Style {
  const s = { ...style };
  if (params.length === 0) params = [0];
  for (let i = 0; i < params.length; i++) {
    const p = params[i]!;
    if (p === 0) { for (const k of Object.keys(s)) delete (s as Record<string, unknown>)[k]; }
    else if (p === 1) s.bold = true;
    else if (p === 2) s.dim = true;
    else if (p === 3) s.italic = true;
    else if (p === 4) s.underline = true;
    else if (p === 7) s.inverse = true;
    else if (p === 22) { s.bold = false; s.dim = false; }
    else if (p === 23) s.italic = false;
    else if (p === 24) s.underline = false;
    else if (p === 27) s.inverse = false;
    else if (p >= 30 && p <= 37) s.fg = BASIC[p - 30];
    else if (p >= 90 && p <= 97) s.fg = BRIGHT[p - 90];
    else if (p >= 40 && p <= 47) s.bg = BASIC[p - 40];
    else if (p >= 100 && p <= 107) s.bg = BRIGHT[p - 100];
    else if (p === 39) delete s.fg;
    else if (p === 49) delete s.bg;
    else if (p === 38 || p === 48) {
      const key = p === 38 ? "fg" : "bg";
      if (params[i + 1] === 2) { s[key] = rgb(params[i + 2] ?? 0, params[i + 3] ?? 0, params[i + 4] ?? 0); i += 4; }
      else if (params[i + 1] === 5) { s[key] = xterm256(params[i + 2] ?? 0); i += 2; }
    }
  }
  return s;
}

const sameStyle = (a: Style, b: Style) => !!a.bold === !!b.bold && !!a.dim === !!b.dim && !!a.italic === !!b.italic && !!a.underline === !!b.underline && !!a.inverse === !!b.inverse && a.fg === b.fg && a.bg === b.bg;

/**
 * One painted row into runs of one style. SGR escapes style the text; any
 * other escape (cursor moves, OSC links, pi-tui's APC markers) is dropped.
 */
export function parseAnsiLine(line: string): Run[] {
  const runs: Run[] = [];
  let style: Style = {};
  let col = 0;
  const push = (text: string) => {
    for (const { segment } of segmenter.segment(text)) {
      if (segment === "\t") { push("  "); continue; }
      const cells = graphemeCells(segment);
      const last = runs[runs.length - 1];
      if (last && sameStyle(last.style, style) && last.col + last.cells === col) { last.text += segment; last.cells += cells; }
      else runs.push({ col, text: segment, cells, style: { ...style } });
      col += cells;
    }
  };
  // CSI (ESC [ … final), OSC (ESC ] … BEL|ST), APC/DCS/PM (ESC _ P ^ … BEL|ST), other two-byte escapes.
  const re = /\x1b\[([0-9;:?]*)([@-~])|\x1b[\]_P^][\s\S]*?(?:\x07|\x1b\\)|\x1b[@-Z\\-_]/g;
  let at = 0;
  for (let m = re.exec(line); m; m = re.exec(line)) {
    if (m.index > at) push(line.slice(at, m.index));
    if (m[2] === "m") style = sgr(style, (m[1] ?? "").split(/[;:]/).filter((x) => x !== "").map(Number));
    at = re.lastIndex;
  }
  if (at < line.length) push(line.slice(at));
  return runs;
}

/** Rows cut to a reveal point: rows before `row` whole, row `row` to `col` cells, the rest hidden. */
export function revealRuns(rows: Run[][], row: number, col: number): Run[][] {
  return rows.map((runs, i) => {
    if (i < row) return runs;
    if (i > row) return [];
    const out: Run[] = [];
    for (const r of runs) {
      if (r.col >= col) break;
      if (r.col + r.cells <= col) { out.push(r); continue; }
      let text = "", cells = 0;
      for (const { segment } of segmenter.segment(r.text)) {
        const w = graphemeCells(segment);
        if (r.col + cells + w > col) break;
        text += segment; cells += w;
      }
      if (text) out.push({ ...r, text, cells });
    }
    return out;
  });
}

export interface GridOptions {
  /** The default foreground and background (pi's text and the page). */
  fg: string;
  bg: string;
  /** Faint text (SGR 2) is mixed toward the background by this much. */
  dimMix?: number;
  fontSize?: number;
  cellWidth?: number;
  cellHeight?: number;
}

function mix(a: string, b: string, t: number): string {
  const p = (c: string, i: number) => parseInt(c.slice(1 + 2 * i, 3 + 2 * i), 16);
  return rgb(...([0, 1, 2].map((i) => Math.round(p(a, i) + (p(b, i) - p(a, i)) * t)) as [number, number, number]));
}

const ASCII = /^[\x20-\x7e]+$/;

/**
 * Styled rows as an SVG group at (x, y): background rects, then text. Each
 * row sits on its cell line; each run's text starts at its column.
 */
export function runsToSvg(rows: Run[][], x: number, y: number, o: GridOptions): string {
  const size = o.fontSize ?? CELL.fontSize;
  const cw = o.cellWidth ?? CELL.width;
  const ch = o.cellHeight ?? CELL.height;
  const backs: string[] = [];
  const texts: string[] = [];
  rows.forEach((runs, r) => {
    const top = y + r * ch;
    const base = top + ch * 0.72;
    for (const run of runs) {
      let fg = run.style.fg ?? o.fg;
      let bg = run.style.bg;
      if (run.style.inverse) { const f = fg; fg = bg ?? o.bg; bg = f; }
      if (run.style.dim) fg = mix(fg, bg ?? o.bg, o.dimMix ?? 0.4);
      const left = x + run.col * cw;
      if (bg) backs.push(`<rect x="${n(left)}" y="${n(top)}" width="${n(run.cells * cw)}" height="${n(ch)}" fill="${bg}"/>`);
      const attrs = `fill="${fg}"${run.style.bold ? ` font-weight="700"` : ""}${run.style.italic ? ` font-style="italic"` : ""}${run.style.underline ? ` text-decoration="underline"` : ""}`;
      // ASCII stretches are one text with a position per character: each
      // sits on its cell whatever the font's advance, and no ligature forms
      // (FiraCode would join := and != where the terminal shows them apart).
      // Any other glyph sits alone, centred on its cells.
      let col = run.col;
      let pending = "";
      let xs: string[] = [];
      const flush = () => {
        if (pending.trim()) texts.push(`<text x="${xs.join(" ")}" y="${n(base)}" ${attrs}>${esc(pending)}</text>`);
        pending = "";
        xs = [];
      };
      for (const { segment } of segmenter.segment(run.text)) {
        const w = graphemeCells(segment);
        if (ASCII.test(segment)) {
          pending += segment;
          xs.push(n(x + col * cw));
        } else {
          flush();
          if (w > 0 && segment.trim()) texts.push(`<text x="${n(x + col * cw + (w * cw) / 2)}" y="${n(base)}" text-anchor="middle" ${attrs}>${esc(segment)}</text>`);
        }
        col += w;
      }
      flush();
    }
  });
  return `<g font-family="${esc(FONTS.mono)}" font-size="${n(size)}" xml:space="preserve">${backs.join("")}${texts.join("")}</g>`;
}

/** Rows' width in cells. */
export function rowsCells(rows: Run[][]): number {
  return Math.max(0, ...rows.map((runs) => runs.reduce((w, r) => Math.max(w, r.col + r.cells), 0)));
}

/** Painted rows straight to an SVG group (parse, then place). */
export function ansiToSvg(lines: readonly string[], x: number, y: number, o: GridOptions): string {
  return runsToSvg(lines.map(parseAnsiLine), x, y, o);
}
