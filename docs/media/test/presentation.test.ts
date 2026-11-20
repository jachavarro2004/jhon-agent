/**
 * The presentation video's composition (docs/media/hyperframes/presentation): its narration
 * cache covers every line, and its Spanish subtitles cover every cue.
 *
 *   node --experimental-strip-types --test docs/media/test/*.test.ts
 */
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { MEDIA } from "../paths.ts";

const DIR = join(MEDIA, "hyperframes", "presentation");
const { SHOTS, lines, spoken } = await import(join(DIR, "script.mjs"));
const { key } = await import(join(DIR, "narrate.mjs"));
const html = readFileSync(join(DIR, "index.html"), "utf8");

test("the narration cache holds every line, spoken as written, and nothing else; the composition plays each", () => {
  const want = (lines() as { id: string; text: string }[]).map((l) => ({ ...l, key: key(l.text) as string }));
  assert.ok(want.length >= 20, `a full film's lines, got ${want.length}`);
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

test("the composition runs on Jhon's motion engine alone: motion.js inlined before the timeline, no GSAP", () => {
  const motion = readFileSync(join(MEDIA, "hyperframes", "motion", "motion.js"), "utf8");
  const timeline = readFileSync(join(DIR, "timeline.js"), "utf8");
  const at = html.indexOf(motion);
  assert.ok(at > 0, "index.html does not inline motion.js as it is (run compose.mjs)");
  assert.ok(html.indexOf(timeline) > at, "the timeline is inlined after the engine it uses");
  assert.ok(!/gsap/i.test(html), "index.html still names GSAP");
  assert.ok(!existsSync(join(MEDIA, "hyperframes", "vendor", "gsap-3.15.1")), "vendor/gsap-3.15.1 is still there");
});

test("each proof's marks land in order while its screen is held, and the film runs 2.5 to 4 minutes", () => {
  const plan = JSON.parse(html.match(/window\.__plan = (.*);/)![1]!);
  assert.ok(plan.total >= 150 && plan.total <= 240, `${plan.total} s`);
  for (const s of plan.shots.filter((x: { kind: string }) => x.kind === "proof")) {
    const at = s.marks.map((m: [string, number]) => m[1]);
    for (let i = 0; i < at.length; i++) {
      assert.ok(at[i] >= s.start && at[i] < s.end - 0.5, `${s.id}: mark ${i} at ${at[i]} s is outside ${s.start}-${s.end} s`);
      if (i) assert.ok(at[i] > at[i - 1] + 0.15, `${s.id}: mark ${i} (${at[i]} s) does not follow mark ${i - 1} (${at[i - 1]} s)`);
    }
  }
});

test("every cue has its Spanish subtitle: declared, translated, in the tú form, and two rows at most", () => {
  const es: Record<string, string> = JSON.parse(readFileSync(join(DIR, "captions.es.json"), "utf8"));
  const ids = (SHOTS as { id: string; en: string; es: string }[]).map((s) => `c_${s.id}`);
  assert.deepEqual(Object.keys(es).sort(), [...ids].sort(), "one subtitle per shot's line, no other");
  for (const s of SHOTS as { id: string; en: string; es: string }[]) {
    const v = es[`c_${s.id}`]!;
    assert.equal(v, s.es.replace(/`/g, ""), `${s.id}: captions.es.json is not script.mjs's (run compose.mjs)`);
    assert.ok(!v.includes("`"), `${s.id}: a subtitle is plain text, with no backticks`);
    assert.ok(v.trim().length > 0, `${s.id}: empty subtitle`);
    assert.notEqual(v.replace(/`/g, ""), s.en.replace(/`/g, ""), `${s.id}: the subtitle repeats the English`);
    assert.ok(!/\b(vos|tenés|podés|querés|sabés|hacé|mirá)\b/i.test(v), `${s.id}: voseo in "${v}"`);
    assert.ok(v.length <= 168, `${s.id}: ${v.length} characters do not fit two rows`);
    assert.ok(html.includes(`"id":"c_${s.id}"`), `${s.id}: index.html does not declare the variable c_${s.id}`);
    assert.ok(html.includes(`data-var-text="c_${s.id}"`), `${s.id}: no subtitle clip shows c_${s.id}`);
  }
  assert.ok(typeof spoken("jhon start pi") === "string" && spoken("jhon start pi") === "John start pie");
});
