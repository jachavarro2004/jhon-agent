/**
 * shell/lib/profiles.ts — the owner's active Jhon model profile, which the
 * header poster and the status bar show.
 */
import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { shellSettings } from "./settings.ts";

/**
 * The owner's active profile, as the engine settles it (T56b): settings'
 * pi.profile when the owner set it, else ~/.config/jhon/profiles.json's
 * `active` (JHON_PROFILES overrides the path), or `default` when the file is
 * absent, unreadable, or names none. A settings file changed since the last
 * read is read again first.
 */
export function activeProfileName(file = process.env.JHON_PROFILES || join(homedir(), ".config", "jhon", "profiles.json")): string {
  const settings = shellSettings();
  // `jhon profile use` from a terminal or another session writes the file.
  settings.refreshIfChanged();
  if (settings.source("pi.profile") !== "default") return settings.string("pi.profile");
  try {
    const raw = JSON.parse(readFileSync(file, "utf8"));
    const ok = raw && typeof raw === "object" && !Array.isArray(raw)
      && (raw.profiles === undefined || (raw.profiles && typeof raw.profiles === "object" && !Array.isArray(raw.profiles)));
    return ok && typeof raw.active === "string" ? raw.active : "default";
  } catch {
    return "default";
  }
}

/**
 * T117c (L720): a profile's delegation mode as profiles.json says it,
 * "writer" or else "inline" (the default the engine omits); an absent or
 * unreadable file, or an unknown profile, is inline.
 */
export function profileDelegation(name: string, file = process.env.JHON_PROFILES || join(homedir(), ".config", "jhon", "profiles.json")): "inline" | "writer" {
  try {
    const raw = JSON.parse(readFileSync(file, "utf8"));
    return raw?.profiles?.[name]?.delegation === "writer" ? "writer" : "inline";
  } catch {
    return "inline";
  }
}
