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

export const ANIMATION_POLICY = { QUALITY: "quality", POTATO: "potato" } as const;
export type AnimationPolicy = (typeof ANIMATION_POLICY)[keyof typeof ANIMATION_POLICY];

export const STATUS_PLACEMENT = { AUTO: "auto", HIDDEN: "hidden" } as const;
export type StatusPlacement = (typeof STATUS_PLACEMENT)[keyof typeof STATUS_PLACEMENT];

export const CHROME_SURFACE = { TRANSPARENT: "transparent", TINTED: "tinted" } as const;
export type ChromeSurface = (typeof CHROME_SURFACE)[keyof typeof CHROME_SURFACE];

export const DENSITY = { COMFORTABLE: "comfortable", COMPACT: "compact", MINIMAL: "minimal" } as const;
export type Density = (typeof DENSITY)[keyof typeof DENSITY];

export interface VisualSettings {
	statusPlacement: StatusPlacement;
	surface: ChromeSurface;
	density: Density;
}

export const DEFAULT_VISUAL_SETTINGS: VisualSettings = { statusPlacement: "auto", surface: "transparent", density: "compact" };

/** Where a value came from: the owner's settings, or the default. */
type Source = "global_file" | "default";

interface Options {
	/** The settings to read and write; the shell's own when absent. */
	settings?: ShellSettings;
}

export interface AnimationResolution {
	policy: AnimationPolicy;
	source: Source;
	/** The settings could not be read: the policy is the default. */
	malformed: boolean;
	/** Where the setting is kept, or why it could not be read. */
	globalFile: string;
}

export interface VisualResolution {
	settings: VisualSettings;
	source: Source;
	malformed: boolean;
	readError: boolean;
	globalFile: string;
}

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
