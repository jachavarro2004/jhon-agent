/**
 * tools/readme-media/build.ts — rebuild the README's images from code.
 *
 *   node --experimental-strip-types tools/readme-media/build.ts [--out <dir>] [--only features,banner,dashboard]
 *
 * Writes docs/assets/features/*.png (the dashboard's audit page among them) and
 * docs/assets/brand/jhon-banner.gif (or the same tree under --out). Jhon's videos are not
 * made here: jhon-studio makes them (github.com/jachavarro2004/jhon-studio). Frames and
 * the engine binary live in a fresh temp
 * folder removed at the end. Two runs give the same bytes.
 */
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { parseArgs } from "node:util";
import { ASSETS } from "./paths.ts";
import { roseInk } from "./palette.ts";
import { colors, type Colors } from "./identity.ts";
import { captures, loadTheme, type Capture } from "./captures.ts";
import { featureSvg } from "./frames.ts";
import { encodeGif, framesToPngs, svgToPng } from "./raster.ts";
import { BANNER, bannerSvg } from "./banner.ts";
import { auditPage, screenshot } from "./dashboard.ts";

/** What each feature capture shows, under it. */
export const FEATURE_CAPTIONS: Record<string, string> = {
  "shell-bar": "The top row and the status bar: the project and its path; branch, sandbox, model and effort, context, cost.",
  "ldd-status": "The right pane's LDD section: the change and its route, the next step, the slices with the open one's tasks, the last test run.",
  "agents-jobs": "Sub-agents and background jobs, live in the right pane while they run: what each does and for how long.",
  "ldd-question": "A repo with no LDD yet: at the first code change, the session asks once how to deliver and test.",
  "guard-refusal": "A writer's edit outside its edit surfaces, refused by scope-guard: the shell's tool card, then the engine's whole reason.",
  "route-refusal": "Under delivery: change, route-guard refuses the pull request while a task is unchecked: the card, then the reason.",
  "prism-review": "The Prism review of a risky change: four facets, a challenger and an arbiter, the verdict and its record.",
  "executions-red-green": "jhon executions records the run: the test fails first (RED), then passes (GREEN), on record.",
  "mode-grant": "The mode and the grants are yours: jhon mode reads it, jhon grant widens one class for a time.",
  audit: "jhon audit reads a session afterwards: here, a push to main with no test run on record.",
};

export type Part = "features" | "banner" | "dashboard";
export const PARTS: readonly Part[] = ["features", "banner", "dashboard"];

export interface Built { files: string[] }

/** The feature PNGs: each capture in its panel, at 2× for sharp text. */
export async function buildFeatures(c: Colors, caps: Capture[], out: string): Promise<string[]> {
  mkdirSync(join(out, "features"), { recursive: true });
  const files: string[] = [];
  for (const cap of caps) {
    const png = join(out, "features", `${cap.name}.png`);
    await svgToPng(featureSvg(c, cap.lines, cap.title, FEATURE_CAPTIONS[cap.name] ?? "", cap.cols), png, 2);
    rmSync(`${png}.svg`);
    files.push(png);
  }
  return files;
}

/** The banner GIF from its frames. */
export async function buildBanner(c: Colors, caps: Capture[], out: string, scratch: string): Promise<string> {
  mkdirSync(join(out, "brand"), { recursive: true });
  // The LDD section's head and its slices (up to its second blank row): the banner's frame holds twelve rows.
  const all = caps.find((x) => x.name === "ldd-status")!.lines;
  const second = all.indexOf("", all.indexOf("") + 1);
  const lines = second > 0 ? all.slice(0, second) : all;
  const dir = join(scratch, "frames-banner");
  mkdirSync(dir, { recursive: true });
  const count = BANNER.seconds * BANNER.fps;
  const pattern = await framesToPngs(count, (i) => bannerSvg(c, lines, i / BANNER.fps), dir);
  const gif = join(out, "brand", "jhon-banner.gif");
  await encodeGif(pattern, BANNER.fps, gif);
  return gif;
}

/** The dashboard's audit of the invented session (dashboard.ts), drawn by headless Chrome. */
export async function buildDashboard(bin: string, out: string, scratch: string): Promise<string> {
  mkdirSync(join(out, "features"), { recursive: true });
  const png = join(out, "features", "dashboard-audit.png");
  await screenshot(auditPage(bin, scratch), scratch, png);
  return png;
}

/** Rebuild the chosen parts into `out` (docs/assets by default). */
export async function build(o: { out?: string; only?: readonly Part[]; scratch?: string } = {}): Promise<Built> {
  const out = o.out ?? ASSETS;
  const only = new Set(o.only ?? PARTS);
  const scratch = o.scratch ?? mkdtempSync(join(tmpdir(), "jhon-media-"));
  try {
    const ink = roseInk(scratch);
    const c = colors(ink.semantic);
    const caps = await captures(await loadTheme(ink.themePath));
    const files: string[] = [];
    if (only.has("features")) files.push(...(await buildFeatures(c, caps, out)));
    if (only.has("banner")) files.push(await buildBanner(c, caps, out, scratch));
    if (only.has("dashboard")) files.push(await buildDashboard(ink.bin, out, scratch));
    return { files };
  } finally {
    if (!o.scratch) rmSync(scratch, { recursive: true, force: true });
  }
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const { values } = parseArgs({ options: { out: { type: "string" }, only: { type: "string" } } });
  const only = values.only ? (values.only.split(",") as Part[]) : undefined;
  for (const p of only ?? []) if (!PARTS.includes(p)) throw new Error(`unknown part ${p}; one of ${PARTS.join(", ")}`);
  const { files } = await build({ out: values.out, only });
  for (const f of files) console.log(f);
}
