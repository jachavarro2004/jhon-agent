/**
 * Whether a path is the home or a broad user folder (Documents, Desktop,
 * Downloads, OneDrive): never a project root to scan for changes.
 */
import { realpathSync } from "node:fs";
import { homedir } from "node:os";
import { basename, dirname, isAbsolute, relative } from "node:path";

const BROAD_HOME_FOLDER = /^(?:documents|desktop|downloads|onedrive(?: - .+)?)$/i;
const BROAD_ONEDRIVE_FOLDER = /^(?:documents|desktop)$/i;

export interface BootstrapDirectoryOptions {
	/** Defaults to the canonical home directory. */
	readonly home?: string;
	/** Defaults to `process.platform`; Windows paths compare case-insensitively. */
	readonly platform?: NodeJS.Platform;
}

/** True when `path` is HOME, an ancestor of it, or a broad folder directly under it. */
export function isHomeOrBroadUserDirectory(path: string, options: BootstrapDirectoryOptions = {}): boolean {
	const platform = options.platform ?? process.platform;
	const fold = (value: string) => platform === "win32" ? value.toLowerCase() : value;
	let home: string;
	try { home = options.home ?? realpathSync.native(homedir()); } catch { return true; }
	const target = fold(path), canonicalHome = fold(home);
	const towardHome = relative(target, canonicalHome);
	if (towardHome === "" || (!isAbsolute(towardHome) && towardHome !== ".." && !towardHome.startsWith("..\\") && !towardHome.startsWith("../"))) return true;
	const parent = dirname(target);
	if (parent === canonicalHome) return BROAD_HOME_FOLDER.test(basename(path));
	return dirname(parent) === canonicalHome && /^onedrive(?: - .+)?$/i.test(basename(parent)) && BROAD_ONEDRIVE_FOLDER.test(basename(path));
}
