/**
 * shell/lib/desktop-palette.ts — the desktop palette's poster in Jhon's pi header.
 *
 * The colors are rose ink, written by `jhon palette` (lib/rose-ink.ts). A
 * desktop palette file is an optional source (integrations.theme.palette =
 * "desktop").
 * Nothing here writes to the desktop or to pi's own files; it reads what the
 * desktop already publishes:
 *
 * - The palette file (JHON_DESKTOP_COLORS, by default
 *   ~/.cache/ryoku/colors.json), only as the sign of a
 *   desktop palette: where it stands, extensions/jhon.ts draws the poster.
 * - The wordmark, from the dashboard spec the desktop ships into Neovim
 *   (Ryoku's lua/plugins/ryoku-dashboard.lua), read rather than copied, as
 *   Neovim's poster reads it: an update that changes the banner changes this one.
 *
 * poster() is Ryoku's lua/config/ryoku_poster.lua in pi's header: the rail with the
 * disc at the top and 羅針盤 down its middle, the framed hero, and the dashed
 * metarow. Widths are measured in terminal cells; a kanji takes two.
 */

import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

// JHON_DESKTOP_COLORS and JHON_DESKTOP_NVIM_SPEC override the paths.
export const PALETTE_PATH = (env: NodeJS.ProcessEnv = process.env) =>
  env.JHON_DESKTOP_COLORS || join(homedir(), ".cache", "ryoku", "colors.json");
export const NVIM_SPEC_PATH = (env: NodeJS.ProcessEnv = process.env) =>
  env.JHON_DESKTOP_NVIM_SPEC || join(homedir(), ".config", "nvim", "lua", "plugins", "ryoku-dashboard.lua");

/**
 * The roles rose ink reads, with its values without a desktop palette: generated from
 * engine/internal/palette's Fallback, never edited here (its
 * TestShellFallbackIsTheEngines fails on drift and prints this line).
 */
export const FALLBACK: Record<string, string> = { surface: "#161313", surfaceContainerLowest: "#100d0e", surfaceContainerLow: "#1e1b1b", surfaceContainer: "#221f1f", surfaceContainerHigh: "#2d292a", surfaceContainerHighest: "#383434", onSurface: "#e9e0e1", onSurfaceVariant: "#d2c3c6", outline: "#9b8d90", outlineVariant: "#4f4447", primary: "#e7bbc8", primaryContainer: "#53343f", onPrimaryContainer: "#f2c5d2", secondary: "#d8c1c7", secondaryContainer: "#534247", tertiary: "#b6cdae", tertiaryContainer: "#2f422b", error: "#ffb4ab", errorContainer: "#93000a" };

export type Palette = Record<keyof typeof FALLBACK, string>;

const hex = (v: unknown): string | null => {
  const m = typeof v === "string" ? v.trim().match(/^#([0-9a-fA-F]{6})(?:[0-9a-fA-F]{2})?$/) : null;
  return m ? `#${m[1].toLowerCase()}` : null;
};

/** The palette at `path`, each role normalised or fallen back; null when there is none (no desktop palette). */
export function readPalette(path = PALETTE_PATH()): Palette | null {
  let raw: Record<string, unknown>;
  try { raw = JSON.parse(readFileSync(path, "utf8")); } catch { return null; }
  if (!raw || typeof raw !== "object") return null;
  return Object.fromEntries(Object.entries(FALLBACK).map(([k, d]) => [k, hex(raw[k]) ?? d])) as Palette;
}

/** Every color pi's theme schema requires (the full set a complete theme declares): fixtures/kanagawa.json, the reference theme (not registered with pi), declares each. */
export const THEME_COLORS = [
  "accent", "bashMode", "border", "borderAccent", "borderMuted", "customMessageBg", "customMessageLabel", "customMessageText", "dim",
  "error", "mdCode", "mdCodeBlock", "mdCodeBlockBorder", "mdHeading", "mdHr", "mdLink", "mdLinkUrl", "mdListBullet", "mdQuote",
  "mdQuoteBorder", "muted", "selectedBg", "success", "syntaxComment", "syntaxFunction", "syntaxKeyword", "syntaxNumber",
  "syntaxOperator", "syntaxPunctuation", "syntaxString", "syntaxType", "syntaxVariable", "text", "thinkingHigh", "thinkingLow",
  "thinkingMedium", "thinkingMinimal", "thinkingOff", "thinkingText", "thinkingXhigh", "toolDiffAdded", "toolDiffContext",
  "toolDiffRemoved", "toolErrorBg", "toolOutput", "toolPendingBg", "toolSuccessBg", "toolTitle", "userMessageBg", "userMessageText", "warning",
] as const;

/** The desktop's wordmark from its shipped Neovim dashboard spec: the `header = [[ ]]` rows, dedented, blank rows dropped. */
export function wordmark(spec: string): string[] | null {
  const m = spec.match(/header\s*=\s*\[\[([\s\S]*?)\]\]/);
  if (!m) return null;
  const rows = m[1].split("\n").map((l) => l.replace(/\s+$/, "")).filter((l) => /\S/.test(l));
  if (!rows.length) return null;
  const indent = Math.min(...rows.map((l) => l.length - l.replace(/^ +/, "").length));
  return rows.map((l) => l.slice(indent));
}

export function readWordmark(path = NVIM_SPEC_PATH()): string[] | null {
  try { return wordmark(readFileSync(path, "utf8")); } catch { return null; }
}

/** A string's width in terminal cells: color codes take none, East Asian wide characters two. */
export function cells(s: string): number {
  let n = 0;
  for (const ch of s.replace(/\x1b\[[0-9;]*m/g, "")) {
    const c = ch.codePointAt(0)!;
    n += (c >= 0x1100 && c <= 0x115f) || (c >= 0x2e80 && c <= 0xa4cf) || (c >= 0xac00 && c <= 0xd7a3) || (c >= 0xf900 && c <= 0xfaff) || (c >= 0xfe30 && c <= 0xfe4f) || (c >= 0xff00 && c <= 0xff60) || (c >= 0xffe0 && c <= 0xffe6) ? 2 : 1;
  }
  return n;
}

const pad = (s: string, n: number) => s + " ".repeat(Math.max(0, n - cells(s)));
const track = (s: string) => [...s].join(" ");
/** `s` cut to `n` cells, an ellipsis marking the cut. */
const fit = (s: string, n: number) => (cells(s) <= n ? s : [...s].slice(0, Math.max(0, n - 1)).join("") + "…");

/** A pi theme color name and the text it paints. */
export type Paint = (role: string, text: string) => string;
export type Facts = { user: string; project: string; branch: string; profile: string };

/**
 * The rashin poster as header lines. The roles are pi theme colors: `border`
 * (nvim's RyokuRule), `text` (Title), `dim` (Eyebrow), `muted` (Value and
 * Kanji), `error` (Disc).
 */
export function poster(o: { width: number; art: string[] | null; facts: Facts; paint: Paint }): string[] {
  const { paint } = o;
  const RAIL = 8; // a space, a two-cell mark, a space, the border, three cells of gutter
  const body = Math.max(16, Math.min(96, o.width - RAIL - 1));
  const inner = body - 2;
  const indent = "   ";
  const art = o.art && Math.max(...o.art.map(cells)) + 2 * indent.length <= inner ? o.art : null;

  const frame = (l: string, r: string) => paint("border", l + "─".repeat(inner) + r);
  const paintAll = (xs: [string, string][]) => xs.map(([role, t]) => (role ? paint(role, t) : t)).join("");
  const width = (xs: [string, string][]) => xs.reduce((n, [, t]) => n + cells(t), 0);
  const row = (left: [string, string][], right: [string, string][] = []) =>
    paint("border", "│") + paintAll(left) + " ".repeat(Math.max(1, inner - width(left) - width(right))) + paintAll(right) + paint("border", "│");
  /** A row whose content sits in the middle of the frame, as a centred banner sits. */
  const centred = (mid: [string, string][]) => {
    const left = Math.max(0, Math.floor((inner - width(mid)) / 2));
    return paint("border", "│") + " ".repeat(left) + paintAll(mid) + " ".repeat(Math.max(0, inner - left - width(mid))) + paint("border", "│");
  };
  // Tracked when it fits, as rashin's eyebrow; plainer as the frame narrows.
  const room = inner - 2 * indent.length;
  const subtitle = [track("JHON · THE AGENT NEEDLE"), "JHON · THE AGENT NEEDLE", "JHON"].find((t) => cells(t) <= room) ?? "";

  const rows: string[] = [frame("┌", "┐"), row([["", "  "]], [["dim", "◌"], ["", " "]]), row([])];
  if (art) {
    const w = Math.max(...art.map(cells));
    for (const line of art) rows.push(centred([["text", pad(line, w)]]));
  } else {
    rows.push(centred([["text", "JHON"]]));
  }
  rows.push(row([]), centred([["error", "●"], ["", "  "], ["dim", subtitle]]), row([]), frame("└", "┘"), "");

  const cellsOf: [string, string][] = [["USER", o.facts.user], ["PROJECT", o.facts.project], ["BRANCH", o.facts.branch], ["PROFILE", o.facts.profile]];
  const col = Math.min(24, Math.floor(body / cellsOf.length));
  const tracked = cellsOf.every(([l]) => cells(track(l)) < col);
  rows.push(
    paint("border", "╌".repeat(body)),
    cellsOf.map(([l]) => paint("dim", pad(fit(tracked ? track(l) : l, col - 1), col))).join("").trimEnd(),
    cellsOf.map(([, v]) => paint("muted", pad(fit(v, col - 1), col))).join("").trimEnd(),
    paint("border", "╌".repeat(body)),
  );

  const kanji = ["羅", "針", "盤"];
  const first = Math.max(2, Math.floor((rows.length - kanji.length) / 2));
  // The whole poster sits in the middle of the terminal.
  const margin = " ".repeat(Math.max(0, Math.floor((o.width - RAIL - body) / 2)));
  return rows.map((r, i) => {
    const mark = i === 0 ? paint("error", "●") + " " : i >= first && i < first + kanji.length ? paint("muted", kanji[i - first]) : "  ";
    return margin + " " + mark + " " + paint("border", "│") + "   " + r;
  });
}
