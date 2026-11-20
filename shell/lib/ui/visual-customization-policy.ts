import { shellSettings, type ShellSettings } from "../settings.ts";

// The layout's choices: the status line and the rail's Status shown where
// they fit (auto), or hidden (T15.3, shell.appearance.status), and the chrome
// surface (T15.12d, shell.appearance.surface), and G's density (L195,
// shell.appearance.density). The header position (split, L153 answer 3) and
// the section toggles are not options.
export const STATUS_PLACEMENT = { AUTO: "auto", HIDDEN: "hidden" } as const;
export type StatusPlacement = (typeof STATUS_PLACEMENT)[keyof typeof STATUS_PLACEMENT];

/** The Gentle Shell's chrome surface (L153 answer 2), shell.appearance.surface: transparent by default, as G's. */
export const CHROME_SURFACE = { TRANSPARENT: "transparent", TINTED: "tinted" } as const;
export type ChromeSurface = (typeof CHROME_SURFACE)[keyof typeof CHROME_SURFACE];

/** The pane's density (L195): compact by default, the owner's choice (L1498). */
export const DENSITY = { COMFORTABLE: "comfortable", COMPACT: "compact", MINIMAL: "minimal" } as const;
export type Density = (typeof DENSITY)[keyof typeof DENSITY];

export interface VisualSettings {
	statusPlacement: StatusPlacement;
	surface: ChromeSurface;
	density: Density;
}
export const DEFAULT_VISUAL_SETTINGS: VisualSettings = { statusPlacement: STATUS_PLACEMENT.AUTO, surface: CHROME_SURFACE.TRANSPARENT, density: DENSITY.COMPACT };
interface VisualOptions { settings?: ShellSettings }
export interface VisualResolution {
	settings: VisualSettings;
	source: "global_file" | "default";
	malformed: boolean;
	readError: boolean;
	globalFile: string;
}

export function resolveVisualSettings(options: VisualOptions = {}): VisualResolution {
	const store = options.settings ?? shellSettings();
	return {
		settings: { statusPlacement: store.status(), surface: store.surface(), density: store.density() },
		source: store.source("shell.appearance.status") === "default" ? "default" : "global_file",
		malformed: store.unreadable,
		readError: false,
		globalFile: store.where,
	};
}

/** Saved with `jhon settings set`; throws when jhon refuses or cannot. */
export function writeVisualSettings(settings: Pick<VisualSettings, "statusPlacement">, options: VisualOptions = {}): string {
	const store = options.settings ?? shellSettings();
	const result = store.set("shell.appearance.status", settings.statusPlacement === "hidden" ? "hidden" : "auto");
	if (!result.ok) throw new Error(result.error);
	return store.path;
}
