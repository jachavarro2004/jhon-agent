/**
 * The teaser's composition (docs/media/hyperframes/teaser, T141): its nine lines are the
 * owner's accepted narration, verbatim; its captions are its script's; its narration cache
 * covers every line; it runs on Jhon's motion engine alone and shows only existing screens.
 *
 *   node --experimental-strip-types --test docs/media/test/*.test.ts
 */
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { ASSETS, MEDIA } from "../paths.ts";

const DIR = join(MEDIA, "hyperframes", "teaser");
const { SHOTS, lines } = await import(join(DIR, "script.mjs"));
const { key } = await import(join(MEDIA, "hyperframes", "presentation", "narrate.mjs"));
const html = readFileSync(join(DIR, "index.html"), "utf8");
type Shot = { id: string; kind: string; en: string; es: string; src?: string; capEn?: boolean };
const shots = SHOTS as Shot[];

/** odd/proposals/t141-plan.md, "Teaser narration (owner accepted, L1459)": en, es. */
const ACCEPTED: [string, string][] = [
  ["Coding agents write fast. The question is what you can trust.", "Los agentes de código escriben rápido. La pregunta es en qué confiar."],
  ["Jhon lets the agent run, and asks one thing of every change: evidence.", "Jhon deja correr al agente y le pide una sola cosa a cada cambio: evidencia."],
  ["A test that failed first, then passed. Recorded, not claimed.", "Una prueba que primero falló y después pasó. Registrada, no declarada."],
  ["Only the tree that was tested is delivered.", "Solo se entrega el árbol que se probó."],
  ["Guards stop only what can't be undone: a destructive command, a write outside the project.", "Los guards frenan solo lo que no se puede deshacer: un comando destructivo, una escritura fuera del proyecto."],
  ["Everything else runs free, inside a sandbox.", "Todo lo demás corre libre, dentro de un sandbox."],
  ["Risky changes get the Prism review: four facets, a challenger, an arbiter.", "Los cambios riesgosos pasan por la revisión Prism: cuatro facetas, un retador, un árbitro."],
  ["Claude Code or pi. Your model. And an audit of every session.", "Claude Code o pi. Tu modelo. Y una auditoría de cada sesión."],
  ["Jhon. Let the agent run. Deliver only what is proven.", "Jhon. Deja correr al agente. Entrega solo lo probado."],
];

test("the teaser says the nine accepted lines, verbatim and in order, one shot each, opening on the intro and ending on the close", () => {
  assert.deepEqual(shots.map((s) => [s.en, s.es]), ACCEPTED);
  assert.equal(shots[0]!.kind, "intro");
  assert.equal(shots.at(-1)!.kind, "close");
});

test("the narration cache holds every line, spoken as written, and nothing else; the composition plays each", () => {
  const want = (lines() as { id: string; text: string }[]).map((l) => ({ ...l, key: key(l.text) as string }));
  assert.equal(want.length, ACCEPTED.length);
  for (const l of want) {
    for (const ext of ["opus", "json"]) assert.ok(existsSync(join(DIR, "narration", `${l.key}.${ext}`)), `${l.id}: narration/${l.key}.${ext} is missing (run narrate.mjs)`);
    const meta = JSON.parse(readFileSync(join(DIR, "narration", `${l.key}.json`), "utf8"));
    assert.equal(meta.text, l.text, `${l.id}: the cached audio says another text`);
    assert.ok(meta.seconds > 0.5 && meta.seconds < 15, `${l.id}: ${meta.seconds} s`);
    assert.ok(html.includes(`src="assets/narration/${l.key}.opus"`), `${l.id}: index.html does not play ${l.key}.opus (run compose.mjs)`);
  }
  const keep = new Set(want.flatMap((l) => [`${l.key}.opus`, `${l.key}.json`]));
  assert.deepEqual(readdirSync(join(DIR, "narration")).filter((f) => !keep.has(f)), [], "cache files no line uses");
});

test("the captions are the script's: every line in English by default, its Spanish subtitle in captions.es.json", () => {
  const vars: { id: string; default: string }[] = JSON.parse(html.match(/data-composition-variables='([^']*)'/)![1]!.replace(/&#39;/g, "'"));
  const es: Record<string, string> = JSON.parse(readFileSync(join(DIR, "captions.es.json"), "utf8"));
  assert.deepEqual(vars.map((v) => v.id), shots.map((s) => `c_${s.id}`), "one caption variable per shot, in order");
  assert.deepEqual(Object.keys(es), shots.map((s) => `c_${s.id}`), "one Spanish subtitle per shot, in order");
  for (const s of shots) {
    assert.ok(s.capEn, `${s.id}: every line is captioned in English`);
    assert.equal(vars.find((v) => v.id === `c_${s.id}`)!.default, s.en, `${s.id}: the English caption is not script.mjs's (run compose.mjs)`);
    assert.equal(es[`c_${s.id}`], s.es, `${s.id}: captions.es.json is not script.mjs's (run compose.mjs)`);
    assert.ok(html.includes(`data-var-text="c_${s.id}"`), `${s.id}: no caption clip shows c_${s.id}`);
  }
});

test("the teaser runs on Jhon's motion engine alone, shows only screens in docs/assets, lasts 60 to 90 s, and each proof's marks land in order while held", () => {
  const motion = readFileSync(join(MEDIA, "hyperframes", "motion", "motion.js"), "utf8");
  const timeline = readFileSync(join(MEDIA, "hyperframes", "presentation", "timeline.js"), "utf8");
  const at = html.indexOf(motion);
  assert.ok(at > 0, "index.html does not inline motion.js as it is (run compose.mjs)");
  assert.ok(html.indexOf(timeline) > at, "the presentation's timeline is inlined after the engine it uses");
  assert.ok(!/gsap/i.test(html), "index.html names GSAP");
  for (const s of shots.filter((x) => x.src)) assert.ok(existsSync(join(ASSETS, s.src!)), `${s.id}: docs/assets/${s.src} does not exist`);
  const plan = JSON.parse(html.match(/window\.__plan = (.*);/)![1]!);
  assert.ok(plan.total >= 60 && plan.total <= 90, `${plan.total} s`);
  for (const s of plan.shots.filter((x: { kind: string }) => x.kind === "proof")) {
    const t = s.marks.map((m: [string, number]) => m[1]);
    for (let i = 0; i < t.length; i++) {
      assert.ok(t[i] >= s.start && t[i] < s.end - 0.5, `${s.id}: mark ${i} at ${t[i]} s is outside ${s.start}-${s.end} s`);
      if (i) assert.ok(t[i] > t[i - 1] + 0.15, `${s.id}: mark ${i} (${t[i]} s) does not follow mark ${i - 1} (${t[i - 1]} s)`);
    }
  }
});
