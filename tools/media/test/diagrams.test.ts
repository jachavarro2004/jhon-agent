/**
 * The diagrams' tests (tools/media/diagrams.py):
 *
 *   node --experimental-strip-types --test tools/media/test/*.test.ts
 */
import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ASSETS, MEDIA } from "../paths.ts";
import { roseInk } from "../palette.ts";
import { colors } from "../identity.ts";

const SCRIPT = join(MEDIA, "diagrams.py");
const py = (...args: string[]) => execFileSync("python3", ["-I", SCRIPT, ...args], { encoding: "utf8" });

test("the diagrams paint with identity.ts's colours only, each the rose ink token of its name", (t) => {
  const scratch = mkdtempSync(join(tmpdir(), "jhon-diagrams-ink-"));
  t.after(() => rmSync(scratch, { recursive: true, force: true }));
  const c = colors(roseInk(scratch).semantic) as unknown as Record<string, string>;
  const tokens = JSON.parse(py("--tokens")) as Record<string, string>;
  for (const [k, v] of Object.entries(tokens)) assert.equal(v, c[k], `diagrams.py ${k}`);
  const allowed = new Set(Object.values(tokens));
  for (const f of readdirSync(join(ASSETS, "diagrams"))) {
    for (const hex of readFileSync(join(ASSETS, "diagrams", f), "utf8").match(/#[0-9a-fA-F]{6}\b/g) ?? []) {
      assert.ok(allowed.has(hex.toLowerCase()), `${f}: ${hex} is not one of identity's colours`);
    }
  }
});

test("the diagrams build deterministically and the committed ones are the script's; no text under 14 px", (t) => {
  const a = mkdtempSync(join(tmpdir(), "jhon-diagrams-a-"));
  const b = mkdtempSync(join(tmpdir(), "jhon-diagrams-b-"));
  t.after(() => { rmSync(a, { recursive: true, force: true }); rmSync(b, { recursive: true, force: true }); });
  py(a);
  py(b);
  const names = readdirSync(a).sort();
  assert.deepEqual(names, ["agents-launch-context.svg", "guards-tool-call.svg", "ldd-flow.svg", "runtimes-engine.svg"]);
  for (const f of names) {
    const svg = readFileSync(join(a, f));
    assert.ok(svg.equals(readFileSync(join(b, f))), `${f} differs between two runs`);
    assert.ok(svg.equals(readFileSync(join(ASSETS, "diagrams", f))), `docs/assets/diagrams/${f} is not what diagrams.py draws`);
    assert.match(svg.toString(), /width="1280" height="720"/, `${f} is 16:9`);
    for (const m of svg.toString().matchAll(/font-size="([\d.]+)"/g)) assert.ok(Number(m[1]) >= 14, `${f}: text at ${m[1]} px`);
  }
});
