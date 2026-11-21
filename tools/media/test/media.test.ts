/**
 * The media pipeline's tests (tools/media):
 *
 *   node --experimental-strip-types --test tools/media/test/*.test.ts
 */
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { parseAnsiLine, runsToSvg } from "../ansi-svg.ts";
import { build } from "../build.ts";
import { SESSION, VIEWPORT, chromePath } from "../dashboard.ts";

const ROSE = "\x1b[38;2;231;187;200m";
const INK = "\x1b[38;2;233;224;225m";
const SEL = "\x1b[48;2;83;52;63m";

test("a styled line reads into runs on the cell grid: colour, bold, background, a wide glyph's two cells", () => {
  const line = `${ROSE}力${"\x1b[39m"} ${"\x1b[1m"}${INK}lumen${"\x1b[39m\x1b[22m"} ${SEL}a:=b${"\x1b[49m"}\x1b_pi:c\x07`;
  assert.deepEqual(parseAnsiLine(line), [
    { col: 0, text: "力", cells: 2, style: { fg: "#e7bbc8" } },
    { col: 2, text: " ", cells: 1, style: {} },
    { col: 3, text: "lumen", cells: 5, style: { bold: true, fg: "#e9e0e1" } },
    { col: 8, text: " ", cells: 1, style: { bold: false, dim: false } },
    { col: 9, text: "a:=b", cells: 4, style: { bold: false, dim: false, bg: "#53343f" } },
  ], "pi-tui's APC cursor marker is dropped, not drawn");
});

test("the runs become SVG: a rect per background, each ASCII character on its cell, a wide glyph centred on its two", () => {
  const svg = runsToSvg([parseAnsiLine(`${ROSE}力\x1b[39m \x1b[1m${INK}ok\x1b[22m ${SEL}x\x1b[49m`)], 10, 20, { fg: "#ffffff", bg: "#000000", fontSize: 10, cellWidth: 6, cellHeight: 14 });
  assert.equal(svg,
    `<g font-family="'FiraCode Nerd Font Mono', 'CaskaydiaCove Nerd Font Mono', 'Noto Sans CJK JP', monospace" font-size="10" xml:space="preserve">`
    + `<rect x="46" y="20" width="6" height="14" fill="#53343f"/>`
    + `<text x="16" y="30.08" text-anchor="middle" fill="#e7bbc8">力</text>`
    + `<text x="28 34" y="30.08" fill="#e9e0e1" font-weight="700">ok</text>`
    + `<text x="46" y="30.08" fill="#e9e0e1">x</text>`
    + `</g>`);
});

test("the dashboard capture is the engine's audit of the invented session in the dashboard's page, and two builds draw the same bytes", async (t) => {
  if (!chromePath()) {
    t.skip("no chrome-headless-shell (JHON_MEDIA_CHROME, or npx hyperframes browser ensure)");
    return;
  }
  const dirs = [0, 1].map((i) => mkdtempSync(join(tmpdir(), `jhon-media-dash-${i}-`)));
  t.after(() => { for (const d of dirs) rmSync(d, { recursive: true, force: true }); });
  for (const d of dirs) await build({ out: join(d, "out"), only: ["dashboard"], scratch: d });
  const page = readFileSync(join(dirs[0]!, "dashboard.html"), "utf8");
  assert.match(page, new RegExp(SESSION), "the page holds the session");
  assert.match(page, /delivered-untested/, "and its audit's finding");
  assert.match(page, /"modified":"2026-11-16T09:42:20/, "the transcript's fixed time, not the build's");
  assert.ok(!page.includes(dirs[0]!), "no scratch path on the page");
  const [a, b] = dirs.map((d) => readFileSync(join(d, "out", "features", "dashboard-audit.png")));
  assert.equal(a!.readUInt32BE(16), VIEWPORT.width * VIEWPORT.scale, "the PNG's width");
  assert.equal(a!.readUInt32BE(20), VIEWPORT.height * VIEWPORT.scale, "the PNG's height");
  assert.ok(a!.equals(b!), "dashboard-audit.png differs between two builds");
});

test("the build is deterministic: the feature captures and the banner rendered twice are the same bytes", async (t) => {
  const a = mkdtempSync(join(tmpdir(), "jhon-media-a-"));
  const b = mkdtempSync(join(tmpdir(), "jhon-media-b-"));
  t.after(() => { rmSync(a, { recursive: true, force: true }); rmSync(b, { recursive: true, force: true }); });
  await build({ out: a, only: ["features", "banner"] });
  await build({ out: b, only: ["features", "banner"] });
  const features = readdirSync(join(a, "features")).sort();
  assert.ok(features.length >= 4, `at least four feature captures, got ${features.join(", ")}`);
  assert.deepEqual(readdirSync(join(b, "features")).sort(), features);
  for (const f of [...features.map((x) => join("features", x)), join("brand", "jhon-banner.gif")]) {
    assert.ok(readFileSync(join(a, f)).equals(readFileSync(join(b, f))), `${f} differs between two builds`);
  }
});
