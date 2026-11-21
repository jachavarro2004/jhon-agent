/**
 * tools/media/palette.ts — rose ink for the media, asked of its one source.
 *
 * Rose ink is made by the engine (engine/internal/palette); no surface keeps
 * a copy of it. The media build compiles the engine from this checkout and
 * asks `jhon palette --pi` (pi's theme, what the shell paints with) and
 * `jhon palette --json` (the semantic tokens: page, frame, ink, accent…) in
 * a sandbox: HOME and the settings point into the build's scratch folder, so
 * the owner's settings (a desktop palette, say) never leak into an asset and
 * two builds read the same fixed values.
 */
import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { REPO } from "./paths.ts";

export interface RoseInk {
  /** pi's theme file (rose-ink.json) written into the scratch folder. */
  themePath: string;
  /** pi's theme document. */
  theme: { vars: Record<string, string>; colors: Record<string, string> };
  /** The semantic tokens (`jhon palette --json`), colors only. */
  semantic: Record<string, string>;
}

/** Build the engine into `scratch` and read rose ink's fixed values from it. */
export function roseInk(scratch: string): RoseInk {
  const bin = join(scratch, "jhon");
  execFileSync("go", ["build", "-o", bin, "./engine/cmd/jhon"], { cwd: REPO, stdio: ["ignore", "ignore", "inherit"] });
  const home = join(scratch, "home");
  mkdirSync(home, { recursive: true });
  const env = {
    PATH: process.env.PATH ?? "/usr/bin:/bin",
    HOME: home,
    XDG_CONFIG_HOME: join(home, ".config"),
    XDG_DATA_HOME: join(home, ".local", "share"),
    XDG_CACHE_HOME: join(home, ".cache"),
    JHON_SETTINGS: join(home, "settings.toml"),
    TMPDIR: scratch,
  };
  const run = (flag: string) => execFileSync(bin, ["palette", flag], { cwd: home, env, encoding: "utf8" });
  const text = run("--pi");
  const themePath = join(scratch, "rose-ink.json");
  writeFileSync(themePath, text);
  const raw = JSON.parse(run("--json")) as Record<string, unknown>;
  const semantic = Object.fromEntries(Object.entries(raw).filter((e): e is [string, string] => typeof e[1] === "string"));
  return { themePath, theme: JSON.parse(text), semantic };
}
