// tools/media/hyperframes/presentation/narrate.mjs — speak the presentation's narration with local Kokoro.
//
//   node tools/media/hyperframes/presentation/narrate.mjs <scratch-dir> [composition-dir]
//
// The composition is this folder (the presentation) or another one with its own script.mjs
// and narration/ (tools/media/hyperframes/teaser). Each distinct line of its script.mjs is spoken on its own by tools/media/kokoro.py (Kokoro-82M,
// the voice tools/media/identity.ts names: af_heart, en-us, speed 1) with the venv under
// ~/.cache/jhon-media/kokoro (or $JHON_MEDIA_KOKORO), then cached here as
// narration/<key>.opus (mono Opus 32 kb/s, bit-exact flags) and narration/<key>.json (its
// text and seconds). The key hashes everything that shapes the sound, so a changed line is
// spoken again and a cached line never twice (a line another composition's cache already
// holds, such as the tagline, is copied from it); files no line uses any more are removed.
// Prints each line's key and seconds; compose.mjs reads the cache to time the shots.
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const MEDIA = join(HERE, "..", "..");
export const DIR = join(HERE, "narration");
/** The narration cache of a composition folder. */
export const narrationDir = (comp) => join(comp, "narration");
/** The compositions whose caches a line may be copied from. */
const SHARED = [DIR, narrationDir(join(HERE, "..", "teaser"))];
const KOKORO = process.env.JHON_MEDIA_KOKORO || join(homedir(), ".cache", "jhon-media", "kokoro");
export const VOICE = { model: "kokoro-v1.0", voice: "af_heart", lang: "en-us", speed: 1 };

/** The cache key of one spoken line. */
export const key = (text) =>
  createHash("sha256").update(JSON.stringify({ ...VOICE, gap: 0, cues: [text] })).digest("hex").slice(0, 16);

const cached = (dir, k) => existsSync(join(dir, `${k}.opus`)) && existsSync(join(dir, `${k}.json`));

async function main(scratch, comp = HERE) {
  if (!scratch) throw new Error("usage: narrate.mjs <scratch-dir> [composition-dir]");
  comp = resolve(comp);
  const DIR = narrationDir(comp);
  const { lines } = await import(pathToFileURL(join(comp, "script.mjs")).href);
  mkdirSync(DIR, { recursive: true });
  const plan = lines().map((l) => ({ ...l, key: key(l.text) }));
  for (const p of plan) {
    const from = !cached(DIR, p.key) && SHARED.find((d) => resolve(d) !== DIR && cached(d, p.key));
    if (from) for (const ext of ["opus", "json"]) copyFileSync(join(from, `${p.key}.${ext}`), join(DIR, `${p.key}.${ext}`));
  }
  const missing = plan.filter((p) => !cached(DIR, p.key));
  if (missing.length) {
    const py = join(KOKORO, "venv", "bin", "python");
    const model = join(KOKORO, "model", "kokoro-v1.0.onnx"), voices = join(KOKORO, "model", "voices-v1.0.bin");
    for (const f of [py, model, voices]) if (!existsSync(f)) throw new Error(`Kokoro is not set up: ${f} is missing`);
    const items = missing.map((p) => ({ voice: VOICE.voice, lang: VOICE.lang, speed: VOICE.speed, gap: 0, cues: [p.text], wav: join(scratch, `${p.key}.wav`) }));
    const job = join(scratch, "job.json");
    writeFileSync(job, JSON.stringify({ model, voices, items }));
    const out = execFileSync(py, ["-I", join(MEDIA, "kokoro.py"), job], { encoding: "utf8", maxBuffer: 1 << 24 });
    const made = JSON.parse(out.trim().split("\n").pop());
    missing.forEach((p, i) => {
      execFileSync("ffmpeg", ["-hide_banner", "-loglevel", "error", "-y", "-threads", "1", "-i", items[i].wav, "-c:a", "libopus", "-b:a", "32k", "-ac", "1",
        "-fflags", "+bitexact", "-flags:a", "+bitexact", "-map_metadata", "-1", join(DIR, `${p.key}.opus`)]);
      writeFileSync(join(DIR, `${p.key}.json`), JSON.stringify({ text: p.text, seconds: Math.round(made[i].seconds * 1000) / 1000 }) + "\n");
    });
  }
  const keep = new Set(plan.flatMap((p) => [`${p.key}.opus`, `${p.key}.json`]));
  for (const f of readdirSync(DIR)) if (!keep.has(f)) rmSync(join(DIR, f));
  const report = Object.fromEntries(plan.map((p) => [p.id, { key: p.key, seconds: JSON.parse(readFileSync(join(DIR, `${p.key}.json`), "utf8")).seconds }]));
  console.log(JSON.stringify(report, null, 2));
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) await main(process.argv[2], process.argv[3]);
