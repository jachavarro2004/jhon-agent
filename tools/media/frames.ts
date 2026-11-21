/**
 * tools/media/frames.ts — the identity's terminal panel and the feature capture frame.
 *
 * A panel is Jhon's, not a desktop window's: a square page-coloured sheet
 * with a one-pixel rose rule (frameSoft) and the dashboard's hard offset
 * shadow, headed by a band carrying the small seal and a mono kicker naming
 * the view. No traffic lights, no rounded corners, no gradients.
 */
import { parseAnsiLine, revealRuns, rowsCells, runsToSvg, type Run } from "./ansi-svg.ts";
import { ADVANCE, CELL, FONTS, SPACE, esc, kicker, n, seal, type Colors } from "./identity.ts";

export interface PanelOptions {
  /** Width in cells (defaults to the widest row). */
  cols?: number;
  fontSize?: number;
  /** The view's name in the panel's head. */
  title?: string;
  /** How much is shown: 0-1 across the rows' content, typed left to right; 1 is all. */
  reveal?: number;
  /** Draw the typing cursor at the reveal point. */
  cursor?: boolean;
}

export interface Panel {
  svg: string;
  width: number;
  height: number;
}

const HEAD = 34;

/** Last cell with content in a row (text or a painted background). */
function contentCells(runs: Run[]): number {
  let end = 0;
  for (const r of runs) {
    if (r.style.bg || r.text.trim()) end = r.col + (r.style.bg ? r.cells : r.text.replace(/\s+$/, "").length);
  }
  return end;
}

/** Where a 0-1 reveal lands: the row and column it has typed to. */
export function revealPoint(rows: Run[][], reveal: number): { row: number; col: number } {
  const widths = rows.map(contentCells);
  const total = widths.reduce((a, b) => a + Math.max(1, b), 0);
  let left = Math.round(total * Math.max(0, Math.min(1, reveal)));
  for (let i = 0; i < rows.length; i++) {
    const w = Math.max(1, widths[i]!);
    if (left < w) return { row: i, col: left };
    left -= w;
  }
  return { row: rows.length, col: 0 };
}

/** Painted rows in the identity's panel, top-left at (x, y). */
export function panel(c: Colors, lines: readonly string[], x: number, y: number, o: PanelOptions = {}): Panel {
  const size = o.fontSize ?? CELL.fontSize;
  const cw = size * ADVANCE;
  const ch = size * 1.375;
  const parsed = lines.map(parseAnsiLine);
  const cols = o.cols ?? rowsCells(parsed);
  const padX = size * 1.25, padY = size;
  const head = o.title ? HEAD * (size / 16) : 0;
  const width = cols * cw + padX * 2;
  const height = head + padY * 2 + lines.length * ch;
  const shadow = SPACE.shadow * 2 * (size / 16);
  const reveal = o.reveal ?? 1;
  const point = reveal >= 1 ? { row: lines.length, col: 0 } : revealPoint(parsed, reveal);
  const rows = reveal >= 1 ? parsed : revealRuns(parsed, point.row, point.col);
  const top = y + head + padY;
  let svg = `<rect x="${n(x + shadow)}" y="${n(y + shadow)}" width="${n(width)}" height="${n(height)}" fill="${c.shadow}"/>`
    + `<rect x="${n(x)}" y="${n(y)}" width="${n(width)}" height="${n(height)}" fill="${c.page}"/>`;
  if (o.title) {
    const s = head * 0.52;
    svg += `<rect x="${n(x)}" y="${n(y)}" width="${n(width)}" height="${n(head)}" fill="${c.frame}"/>`
      + seal(c, x + (head - s) / 2 + 2, y + (head - s) / 2, s)
      + kicker(c, x + head + 6, y + head * 0.64, `jhon · ${o.title}`, size * 0.72)
      + `<rect x="${n(x)}" y="${n(y + head - 1)}" width="${n(width)}" height="1" fill="${c.line}"/>`;
  }
  svg += runsToSvg(rows, x + padX, top, { fg: c.ink, bg: c.page, fontSize: size, cellWidth: cw, cellHeight: ch });
  if (o.cursor && reveal < 1 && point.row < lines.length) {
    svg += `<rect x="${n(x + padX + point.col * cw)}" y="${n(top + point.row * ch + ch * 0.12)}" width="${n(cw)}" height="${n(ch * 0.76)}" fill="${c.accent}"/>`;
  }
  svg += `<rect x="${n(x + 0.5)}" y="${n(y + 0.5)}" width="${n(width - 1)}" height="${n(height - 1)}" fill="none" stroke="${c.frameSoft}" stroke-width="1"/>`;
  return { svg, width, height };
}

/** An SVG document of a given size on the identity's background. */
export function svgDoc(c: Colors, width: number, height: number, body: string, background = c.frame): string {
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${n(width)}" height="${n(height)}" viewBox="0 0 ${n(width)} ${n(height)}">`
    + `<rect width="${n(width)}" height="${n(height)}" fill="${background}"/>${body}</svg>\n`;
}

/** A feature capture: the view's panel on the frame gray, with a caption line under it. */
export function featureSvg(c: Colors, lines: readonly string[], title: string, caption: string, cols?: number): string {
  const margin = 40;
  const p = panel(c, lines, margin, margin, { title, cols });
  const capY = margin + p.height + 40;
  const width = p.width + margin * 2 + SPACE.shadow * 2;
  // Space Grotesk averages about 0.46 em a character in running text.
  const rows = wrap(caption, Math.floor(p.width / (16 * 0.46)));
  const height = capY + 24 * (rows.length - 1) + 28;
  const text = rows.map((row, i) => `<text x="${margin}" y="${n(capY + i * 24)}" font-family="${esc(FONTS.ui)}" font-size="16" fill="${c.soft}">${esc(row)}</text>`).join("");
  return svgDoc(c, width, height, p.svg + text);
}

/** Words wrapped to at most `width` characters a row. */
export function wrap(text: string, width: number): string[] {
  const rows: string[] = [];
  for (const w of text.split(" ").filter(Boolean)) {
    const last = rows[rows.length - 1];
    if (last !== undefined && `${last} ${w}`.length <= width) rows[rows.length - 1] = `${last} ${w}`;
    else rows.push(w);
  }
  return rows;
}
