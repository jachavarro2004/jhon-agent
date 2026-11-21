/**
 * shell/lib/ui/appearance-policy.ts — the appearance settings the shell reads
 * when it starts and Customize writes (lib/settings.ts holds them; `jhon
 * settings set` saves them):
 *
 *   shell.appearance.animations  full | off, here quality | potato (T15.3)
 *   shell.appearance.status      auto (where it fits) | hidden (T15.3)
 *   shell.appearance.surface     transparent | tinted (T15.12d)
 *   shell.appearance.density     comfortable | compact | minimal; compact by
 *                                default, the owner's choice (L195, L1498)
 *
 * Unreadable settings give the defaults and say so (`malformed`); a write
 * that jhon refuses throws its reason and leaves the file as it was.
 */
import { shellSettings, type AnimationsSetting, type ShellSettings } from "../settings.ts";

export const ANIMATION_KEY = "shell.appearance.animations";
const STATUS_KEY = "shell.appearance.status";

/** How the prompt moves: quality animates the working line, potato keeps it still. */
export type AnimationPolicy = "quality" | "potato";
export const ANIMATION_POLICY = { QUALITY: "quality", POTATO: "potato" } as const satisfies Record<string, AnimationPolicy>;

/** Whether the status pane shows where it fits, or never. */
export type StatusPlacement = "auto" | "hidden";
export const STATUS_PLACEMENT = { AUTO: "auto", HIDDEN: "hidden" } as const satisfies Record<string, StatusPlacement>;

/** The right pane on the terminal's own background, or on rose ink's band. */
export type ChromeSurface = "transparent" | "tinted";
export const CHROME_SURFACE = { TRANSPARENT: "transparent", TINTED: "tinted" } as const satisfies Record<string, ChromeSurface>;

/** How much room the pane's sections take: a box each, a blank row between them, or nothing between. */
export type Density = "comfortable" | "compact" | "minimal";
export const DENSITY = { COMFORTABLE: "comfortable", COMPACT: "compact", MINIMAL: "minimal" } as const satisfies Record<string, Density>;

/** The three layout choices Customize writes. */
export type VisualSettings = { statusPlacement: StatusPlacement; surface: ChromeSurface; density: Density };

export const DEFAULT_VISUAL_SETTINGS: VisualSettings = { statusPlacement: "auto", surface: "transparent", density: "compact" };

/** Where a value came from: the owner's settings, or the default. */
type Source = "global_file" | "default";

interface Options {
	/** The settings to read and write; the shell's own when absent. */
	settings?: ShellSettings;
}

/** What every resolution says besides its value: its source, whether the settings could not be read (the value is then the default), and where the setting is kept or why it could not be read. */
type Resolved = { source: Source; malformed: boolean; globalFile: string };

export type AnimationResolution = Resolved & { policy: AnimationPolicy };
export type VisualResolution = Resolved & { settings: VisualSettings; readError: boolean };

const POLICY_OF: Record<AnimationsSetting, AnimationPolicy> = { full: "quality", off: "potato" };
const SETTING_OF: Record<AnimationPolicy, AnimationsSetting> = { quality: "full", potato: "off" };

/** Saves one key through `jhon settings set`; the settings file's path, or jhon's refusal thrown. */
function save(store: ShellSettings, key: string, value: string): string {
	const result = store.set(key, value);
	if (!result.ok) throw new Error(result.error);
	return store.path;
}

export function resolveAnimationPolicy(options: Options = {}): AnimationResolution {
	const store = options.settings ?? shellSettings();
	const unread = store.unreadable;
	return {
		policy: POLICY_OF[store.animations()] ?? "quality",
		source: !unread && store.source(ANIMATION_KEY) === "default" ? "default" : "global_file",
		malformed: unread,
		globalFile: store.where,
	};
}

/** Saves the animation level; throws when jhon refuses or cannot. */
export function writeAnimationPolicy(policy: AnimationPolicy, options: Options = {}): string {
	return save(options.settings ?? shellSettings(), ANIMATION_KEY, SETTING_OF[policy]);
}

export function resolveVisualSettings(options: Options = {}): VisualResolution {
	const store = options.settings ?? shellSettings();
	return {
		settings: { statusPlacement: store.status(), surface: store.surface(), density: store.density() },
		source: store.source(STATUS_KEY) === "default" ? "default" : "global_file",
		malformed: store.unreadable,
		readError: false,
		globalFile: store.where,
	};
}

/** Saves the status line's placement; throws when jhon refuses or cannot. */
export function writeVisualSettings(settings: Pick<VisualSettings, "statusPlacement">, options: Options = {}): string {
	return save(options.settings ?? shellSettings(), STATUS_KEY, settings.statusPlacement === "hidden" ? "hidden" : "auto");
}
