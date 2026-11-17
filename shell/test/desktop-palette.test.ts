/**
 * shell/lib/desktop-palette.ts and fixtures/kanagawa.json: the colors are the
 * owner's Kanagawa, the full-schema reference theme (not registered with pi); where a desktop palette
 * stands (Ryoku's ~/.cache/ryoku/colors.json, the one optional source today)
 * the header is the rashin poster Neovim draws there
 * (~/.config/nvim/lua/config/ryoku_poster.lua): the rail with the disc and the
 * vertical kanji, the framed hero with the desktop's wordmark, and the dashed
 * metarow.
 *
 *   jhon test
 */

import { test, after } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { FALLBACK, NVIM_SPEC_PATH, PALETTE_PATH, THEME_COLORS, cells, poster, readPalette, wordmark } from "../lib/desktop-palette.ts";

const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
const tempDir = () => { const d = mkdtempSync(join(tmpdir(), "jhon-desktop-palette-")); temps.push(d); return d; };

const PALETTE = { ...FALLBACK, primary: "#aabbcc", error: "#ff0000ff", tertiary: "#d2c5ab", surface: "#101010" };

test("the palette is read with its alpha dropped, and every missing or malformed role falls back", () => {
  const dir = tempDir();
  writeFileSync(join(dir, "colors.json"), JSON.stringify({ primary: "#AABBCC", error: "#ff0000ff", tertiary: "not a colour" }));
  const p = readPalette(join(dir, "colors.json"))!;
  assert.equal(p.primary, "#aabbcc");
  assert.equal(p.error, "#ff0000");
  assert.equal(p.tertiary, FALLBACK.tertiary);
  assert.equal(p.surface, FALLBACK.surface);
  assert.equal(readPalette(join(dir, "absent.json")), null, "no palette: no desktop palette");
  writeFileSync(join(dir, "bad.json"), "{ nope");
  assert.equal(readPalette(join(dir, "bad.json")), null);
});

test("the reference theme is Kanagawa: every color pi requires, each a var, sakura the accent and peachRed only the errors", () => {
  const t = JSON.parse(readFileSync(new URL("../fixtures/kanagawa.json", import.meta.url), "utf8"));
  assert.equal(t.name, "kanagawa");
  assert.equal(t.appearance, "dark");
  assert.deepEqual(Object.keys(t.colors).sort(), [...THEME_COLORS].sort());
  for (const [k, v] of Object.entries(t.colors)) assert.ok(typeof v === "string" && v in t.vars, `${k} names a var`);
  const color = (k: string) => t.vars[t.colors[k]];
  assert.equal(color("accent"), "#d27e99", "sakura pink, never a neon red");
  assert.equal(color("borderAccent"), "#d27e99");
  assert.equal(color("error"), "#ff5d62");
  assert.deepEqual(Object.keys(t.colors).filter((k) => t.colors[k] === "peachRed"), ["error"], "peachRed is only for failures");
  assert.equal(color("selectedBg"), "#43242b", "winterRed is the selection");
});

const pkg = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));
test("the package ships its themes to pi", () => {
  assert.deepEqual(pkg.pi.themes, ["./themes/*.json"]);
});

/** A palette object as readPalette returns it, from roles already normalised. */
function readPaletteFrom(roles: Record<string, string>) {
  const dir = tempDir();
  writeFileSync(join(dir, "c.json"), JSON.stringify(roles));
  return readPalette(join(dir, "c.json"))!;
}

test("the wordmark is read from the desktop's shipped nvim dashboard spec, dedented, without its blank rows", () => {
  const spec = 'return { { "folke/snacks.nvim", opts = { dashboard = { preset = { header = [[\n\n          ██████╗ ██╗\n          ██╔══██╗╚██╗\n\n          ]], keys = {} } } } } }';
  assert.deepEqual(wordmark(spec), ["██████╗ ██╗", "██╔══██╗╚██╗"]);
  assert.equal(wordmark("return {}"), null);
});

test("a kanji is two cells, a box-drawing glyph one, and color codes none", () => {
  assert.equal(cells("羅針盤"), 6);
  assert.equal(cells("─│●"), 3);
  assert.equal(cells("\x1b[31mab\x1b[0m"), 2);
});

const paint = (role: string, s: string) => `<${role}>${s}</>`;
const plain = (lines: string[]) => lines.map((l) => l.replace(/<\/?[a-z]*>/g, ""));
const FACTS = { user: "jhon", project: "acme", branch: "main", profile: "default" };

test("the poster carries the rail (disc, then the kanji), the framed hero with the wordmark, and the metarow, within the width", () => {
  const art = ["██████╗", "██╔══██╗"];
  const lines = poster({ width: 90, art, facts: FACTS, paint });
  const text = plain(lines);
  for (const l of text) assert.ok(cells(l) <= 90, `${cells(l)}: ${l}`);
  assert.match(text[0], /^ ●  │/);
  const rail = text.map((l) => l.slice(1, 2)).join("");
  assert.ok(rail.includes("羅針盤"), rail);
  assert.ok(text.some((l) => l.includes("██╔══██╗")), "the wordmark");
  assert.ok(text.some((l) => l.includes("J H O N")), "the subtitle");
  assert.ok(text.some((l) => /U S E R +P R O J E C T +B R A N C H +P R O F I L E/.test(l)), "the metarow labels");
  assert.ok(text.some((l) => /jhon +acme +main +default/.test(l)), "the metarow values");
  assert.ok(lines.some((l) => l.includes("<error>●</>")), "the disc takes the error role");
});

test("a narrow terminal drops the wordmark for the title rather than breaking the frame", () => {
  const text = plain(poster({ width: 40, art: ["█".repeat(60)], facts: FACTS, paint }));
  for (const l of text) assert.ok(cells(l) <= 40, `${cells(l)}: ${l}`);
  assert.ok(text.some((l) => /│ +JHON +│/.test(l)));
  assert.ok(!text.some((l) => l.includes("█".repeat(60))));
});

test("a wide terminal centres the poster and the wordmark within its frame, as a centred banner", () => {
  const art = ["██████╗", "██╔══██╗"];
  const text = plain(poster({ width: 160, art, facts: FACTS, paint }));
  for (const l of text) assert.ok(cells(l) <= 160, `${cells(l)}: ${l}`);
  const left = text[0].indexOf("●"), right = 160 - cells(text[0]);
  assert.ok(left > 10 && Math.abs(left - right) <= 2, `margins ${left} and ${right}`);
  const hero = text.find((l) => l.includes("██╔══██╗"))!;
  const inside = hero.slice(hero.indexOf("│", hero.indexOf("│") + 1) + 1, hero.lastIndexOf("│"));
  const pre = inside.length - inside.trimStart().length, post = inside.length - inside.trimEnd().length;
  assert.ok(Math.abs(pre - post) <= 2, `wordmark margins ${pre} and ${post}`);
});

test("the palette and wordmark paths are JHON_DESKTOP_*, else Ryoku's files; the JHON_RYOKU_* names before T59 are no longer read (T29 E6)", () => {
  const home = (p: string) => p.startsWith("/") && !p.startsWith("/new") && !p.startsWith("/old");
  assert.ok(PALETTE_PATH({}).endsWith(join(".cache", "ryoku", "colors.json")) && home(PALETTE_PATH({})));
  assert.equal(PALETTE_PATH({ JHON_RYOKU_COLORS: "/old.json" }), PALETTE_PATH({}));
  assert.equal(PALETTE_PATH({ JHON_DESKTOP_COLORS: "/new.json", JHON_RYOKU_COLORS: "/old.json" }), "/new.json");
  assert.ok(NVIM_SPEC_PATH({}).endsWith(join("nvim", "lua", "plugins", "ryoku-dashboard.lua")));
  assert.equal(NVIM_SPEC_PATH({ JHON_RYOKU_NVIM_SPEC: "/old.lua" }), NVIM_SPEC_PATH({}));
  assert.equal(NVIM_SPEC_PATH({ JHON_DESKTOP_NVIM_SPEC: "/new.lua", JHON_RYOKU_NVIM_SPEC: "/old.lua" }), "/new.lua");
});
