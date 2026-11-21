/** tools/media/paths.ts — where the media pipeline reads and writes. */
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const MEDIA = dirname(fileURLToPath(import.meta.url));
export const REPO = resolve(MEDIA, "..", "..");
export const SHELL = join(REPO, "shell");
/** The committed assets the README links. */
export const ASSETS = join(REPO, "docs", "assets");
