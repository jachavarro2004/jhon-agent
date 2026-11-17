/**
 * docs/media/banner.ts — the README's animated banner (docs/assets/brand/jhon-banner.gif).
 *
 * Left, the wordmark stamps down and its name is inked, the kicker and the
 * one line saying what Jhon is follow; right, the shell's LDD status (its
 * real rows, captures.ts) types itself into a panel. It holds, then loops.
 */
import { FONTS, MOTION, TAGLINE, KICKER, clamp01, easeOut, esc, kicker, n, wordmark, type Colors } from "./identity.ts";
import { panel, svgDoc, wrap } from "./frames.ts";

export const BANNER = { width: 1280, height: 400, fps: 15, seconds: 6 } as const;

/** The banner at time t (seconds). */
export function bannerSvg(c: Colors, lines: readonly string[], t: number): string {
  const stamp = clamp01((t * BANNER.fps) / (MOTION.stamp / 2));
  const ink = clamp01((t - 0.35) / 0.6);
  const rest = easeOut((t - 0.9) / 0.5);
  const left = 72;
  const body = kicker(c, left + 2, 118, KICKER, 15, "start", rest)
    + wordmark(c, left, 140, 76, stamp, ink)
    + `<rect x="${left}" y="262" width="${n(80 * rest)}" height="3" fill="${c.accent}"/>`
    + wrap(TAGLINE, 38).map((row, i) => `<text x="${left}" y="${306 + i * 32}" font-family="${esc(FONTS.ui)}" font-size="24" fill="${c.soft}" opacity="${n(clamp01((t - 1.1 - i * 0.2) / 0.4))}">${esc(row)}</text>`).join("");
  const typing = clamp01((t - 1.3) / 2.6);
  const p = panel(c, lines, 640, 92, { title: "ldd status", fontSize: 14, reveal: typing, cursor: t < 5.2 });
  // The panel slides up a little as it lands.
  const lift = (1 - easeOut((t - 1.0) / 0.4)) * 16;
  const shown = clamp01((t - 1.0) / 0.25);
  return svgDoc(c, BANNER.width, BANNER.height,
    body + `<g opacity="${n(shown)}" transform="translate(0 ${n(lift)})">${p.svg}</g>`
    + `<rect x="0" y="${BANNER.height - 6}" width="${BANNER.width}" height="6" fill="${c.accentFill}"/>`);
}
