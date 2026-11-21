/**
 * tools/media/raster.ts — SVG frames to PNG (resvg), PNG frames to MP4 and GIF (ffmpeg).
 *
 * Every output is byte-stable: resvg writes no timestamp, and ffmpeg runs
 * with bitexact flags, no metadata, one encoder thread and fixed settings.
 */
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { copyFileSync, writeFileSync } from "node:fs";
import { availableParallelism } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";

const run = promisify(execFile);
export const RESVG = process.env.JHON_MEDIA_RESVG || "resvg";
export const FFMPEG = process.env.JHON_MEDIA_FFMPEG || "ffmpeg";

/** One SVG text to a PNG file; `zoom` scales the raster (2 for sharp captures). */
export async function svgToPng(svg: string, png: string, zoom = 1): Promise<void> {
  const src = `${png}.svg`;
  writeFileSync(src, svg);
  await run(RESVG, ["--zoom", String(zoom), src, png]);
}

/**
 * Many SVG frames to `dir/00000.png`…, a few resvg at a time. A frame whose
 * SVG equals an earlier one (a held scene) is a copy of that one's PNG, so
 * only frames that change are rasterised.
 */
export async function framesToPngs(count: number, frame: (i: number) => string, dir: string): Promise<string> {
  const pool = Math.max(1, Math.min(8, availableParallelism()));
  const done = new Map<string, Promise<string>>();
  let next = 0;
  const worker = async () => {
    while (next < count) {
      const i = next++;
      const png = join(dir, `${String(i).padStart(5, "0")}.png`);
      const svg = frame(i);
      const key = createHash("sha256").update(svg).digest("hex");
      const first = done.get(key);
      if (first) {
        copyFileSync(await first, png);
        continue;
      }
      const made = svgToPng(svg, png).then(() => png);
      done.set(key, made);
      await made;
    }
  };
  await Promise.all(Array.from({ length: pool }, worker));
  return join(dir, "%05d.png");
}

const BITEXACT = ["-fflags", "+bitexact", "-flags:v", "+bitexact", "-map_metadata", "-1", "-map_chapters", "-1"];

/** The audio track's input: raw PCM, signed 16-bit little-endian, mono, 48 kHz. */
export const AUDIO_PCM = ["-f", "s16le", "-ar", "48000", "-ac", "1"];

/**
 * PNG frames to an H.264 MP4 that plays everywhere (yuv420p, faststart);
 * with `audio` (raw PCM, AUDIO_PCM) an AAC track too, mono, 48 kHz, 64 kb/s.
 */
export async function encodeMp4(pattern: string, fps: number, out: string, audio?: string): Promise<void> {
  const sound = audio
    ? { input: [...AUDIO_PCM, "-i", audio], output: ["-map", "0:v", "-map", "1:a", "-c:a", "aac", "-b:a", "64k", "-ar", "48000", "-ac", "1", "-flags:a", "+bitexact"] }
    : { input: [], output: [] };
  await run(FFMPEG, [
    "-hide_banner", "-loglevel", "error", "-y", "-threads", "1", "-framerate", String(fps), "-i", pattern, ...sound.input,
    "-c:v", "libx264", "-preset", "slow", "-crf", "20", "-tune", "animation", "-pix_fmt", "yuv420p",
    "-x264-params", "threads=1:lookahead-threads=1", ...sound.output, "-movflags", "+faststart", ...BITEXACT, out,
  ]);
}

/** PNG frames to a looping GIF: one palette for the whole clip, no dither, only changed rectangles. */
export async function encodeGif(pattern: string, fps: number, out: string): Promise<void> {
  await run(FFMPEG, [
    "-hide_banner", "-loglevel", "error", "-y", "-threads", "1", "-framerate", String(fps), "-i", pattern,
    "-filter_threads", "1", "-filter_complex", "[0:v]split[a][b];[a]palettegen=max_colors=128:stats_mode=full[p];[b][p]paletteuse=dither=none:diff_mode=rectangle",
    "-loop", "0", ...BITEXACT, out,
  ]);
}
