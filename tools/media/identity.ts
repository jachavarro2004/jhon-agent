/**
 * tools/media/identity.ts — Jhon's visual identity for every asset, in one place.
 *
 * Jhon's own look, not another project's: rose ink's colours (asked of the
 * engine, tools/media/palette.ts), the dashboard's type and its square, flat
 * build (dashboard/app.css: no rounded corners, a hard offset shadow, mono
 * kickers in spaced capitals), the 力 seal and the name as the wordmark, and
 * one motion style ("ink and stamp"). The banner, the feature captures and
 * the presentation video (tools/media/hyperframes/presentation) all draw from
 * here; tools/media/README.md describes it.
 */

/** Font family names (installed fonts; the SVGs name them, never embed them). */
export const FONTS = {
  /** The wordmark and titles: the dashboard's display serif. */
  display: "Fraunces, Georgia, serif",
  /** Captions and body text: the dashboard's UI face. */
  ui: "'Space Grotesk', sans-serif",
  /** Terminal frames and kickers: a Nerd mono, CJK after it for the seal and wide glyphs. */
  mono: "'FiraCode Nerd Font Mono', 'CaskaydiaCove Nerd Font Mono', 'Noto Sans CJK JP', monospace",
  /** The seal's kanji. */
  jp: "'Noto Sans CJK JP', sans-serif",
} as const;

/** The seal: 力, the shell's brand glyph, which reads «jhon». */
export const SEAL = "力";
export const NAME = "Jhon";
/** One line saying what Jhon is. */
export const TAGLINE = "Let the agent run. Deliver only what is proven.";
/** The kicker over the name. */
export const KICKER = "agent framework · ldd";

/**
 * Jhon's voice: the presentation video's narration, spoken by Kokoro-82M (open
 * weights, run locally; tools/media/hyperframes/presentation/narrate.mjs, which
 * keeps these values). One voice, in English:
 * af_heart, Kokoro's warmest American English voice. The Spanish video
 * carries the same English narration with Spanish subtitles. `model` names
 * the weights; a change of model, voice or speed changes every cache key,
 * so the narration is spoken again.
 */
export interface Voice { model: string; voice: string; lang: string; speed: number }
export const VOICE: Voice = { model: "kokoro-v1.0", voice: "af_heart", lang: "en-us", speed: 1 };

/** Spacing: a 4 px grid, square corners, the dashboard's hard 3 px shadow (scaled by the asset). */
export const SPACE = { unit: 4, radius: 0, shadow: 3, rule: 1 } as const;

/** The mono face's advance in em (FiraCode Nerd Font Mono: 1200/1950); a cell is this wide. */
export const ADVANCE = 0.6154;
/** Terminal cell metrics for the captures: a cell is ADVANCE em wide, 1.375 em tall. */
export const CELL = { fontSize: 16, width: 16 * ADVANCE, height: 22 } as const;

/** The colours every asset paints with: rose ink's semantic tokens, by meaning. */
export interface Colors {
  page: string; frame: string; band: string; box: string; boxStrong: string;
  ink: string; soft: string; faint: string; line: string;
  accent: string; accentFill: string; accentInk: string; accentSoft: string; frameSoft: string;
  ok: string; bad: string; warn: string; shadow: string;
}

/** Rose ink's semantic tokens (`jhon palette --json`) as the identity's colours. */
export function colors(semantic: Record<string, string>): Colors {
  const need = (k: string) => {
    const v = semantic[k];
    if (!v) throw new Error(`rose ink has no ${k}`);
    return v;
  };
  return {
    page: need("page"), frame: need("frame"), band: need("band"), box: need("box"), boxStrong: need("boxStrong"),
    ink: need("ink"), soft: need("soft"), faint: need("faint"), line: need("line"),
    accent: need("accent"), accentFill: need("accentFill"), accentInk: need("accentInk"), accentSoft: need("accentSoft"), frameSoft: need("frameSoft"),
    ok: need("ok"), bad: need("bad"), warn: need("warn"),
    // The dashboard's --shadow, as the engine's CSS writes it.
    shadow: "#000000",
  };
}

/**
 * Motion, "ink and stamp": the seal stamps down (scale in, its hard shadow
 * landing), text is inked left to right as a terminal types, and scenes
 * change under a flat rose band that sweeps across. No blur, no gradients,
 * no rounded corners.
 */
export const MOTION = {
  fps: 30,
  /** Frames the seal takes to stamp down. */
  stamp: 9,
  /** Characters typed per second in a terminal reveal. */
  typeRate: 55,
  /** Seconds of the rose band's sweep between scenes. */
  wipe: 0.5,
} as const;

/** Easing: a cubic ease-out for arrivals. */
export const easeOut = (t: number) => 1 - Math.pow(1 - clamp01(t), 3);
export const clamp01 = (t: number) => Math.max(0, Math.min(1, t));

/** Text escaped for SVG. */
export function esc(text: string): string {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

/** A number for SVG: fixed decimals so two builds write the same bytes. */
export const n = (v: number) => (Math.round(v * 100) / 100).toString();

/** The kicker style: mono, spaced capitals, in the accent (the dashboard's `.kick`). */
export function kicker(c: Colors, x: number, y: number, text: string, size = 14, anchor: "start" | "middle" = "start", opacity = 1): string {
  return `<text x="${n(x)}" y="${n(y)}" font-family="${esc(FONTS.mono)}" font-weight="600" font-size="${n(size)}" letter-spacing="${n(size * 0.12)}" fill="${c.accent}" text-anchor="${anchor}" opacity="${n(opacity)}">${esc(text.toUpperCase())}</text>`;
}

/**
 * The seal: a square in the accent fill with 力 in the accent ink and the
 * dashboard's hard offset shadow. `stamp` (0-1) is how far it has landed:
 * at 0 it is lifted (larger, no shadow, transparent), at 1 it sits.
 */
export function seal(c: Colors, x: number, y: number, size: number, stamp = 1): string {
  const t = easeOut(stamp);
  const scale = 1 + 0.35 * (1 - t);
  const shadow = SPACE.shadow * (size / 34) * t;
  const cx = x + size / 2, cy = y + size / 2;
  return `<g transform="translate(${n(cx)} ${n(cy)}) scale(${n(scale)}) translate(${n(-size / 2)} ${n(-size / 2)})" opacity="${n(clamp01(stamp * 3))}">`
    + `<rect x="${n(shadow)}" y="${n(shadow)}" width="${n(size)}" height="${n(size)}" fill="${c.shadow}"/>`
    + `<rect width="${n(size)}" height="${n(size)}" fill="${c.accentFill}"/>`
    + `<text x="${n(size / 2)}" y="${n(size * 0.75)}" font-family="${esc(FONTS.jp)}" font-weight="700" font-size="${n(size * 0.6)}" fill="${c.accentInk}" text-anchor="middle">${SEAL}</text>`
    + `</g>`;
}

/**
 * The wordmark: the seal, then «Jhon» in the display serif, its last letters
 * in the accent's italic as the dashboard's brand sets its <em>. `reveal`
 * (0-1) inks the name left to right.
 */
export function wordmark(c: Colors, x: number, y: number, size: number, stamp = 1, reveal = 1): string {
  const sealSize = size * 1.05;
  const tx = x + sealSize + size * 0.45;
  const baseline = y + sealSize * 0.8;
  const clipW = size * 2.6 * clamp01(reveal);
  return seal(c, x, y, sealSize, stamp)
    + `<clipPath id="wm-${n(x)}-${n(y)}"><rect x="${n(tx - 2)}" y="${n(y - size)}" width="${n(clipW + 4)}" height="${n(size * 3)}"/></clipPath>`
    + `<text clip-path="url(#wm-${n(x)}-${n(y)})" x="${n(tx)}" y="${n(baseline)}" font-family="${esc(FONTS.display)}" font-weight="500" font-size="${n(size)}" fill="${c.ink}">Jh<tspan font-style="italic" fill="${c.accent}">on</tspan></text>`;
}
