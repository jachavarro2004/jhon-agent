import { shellSettings, type AnimationsSetting, type ShellSettings } from "../settings.ts";

// The animation policy is shell.appearance.animations in the owner's
// settings (lib/settings.ts), named full/off there and quality/potato here
// (T15.3: one animation level, or none).
export const ANIMATION_POLICY = { QUALITY: "quality", POTATO: "potato" } as const;
export type AnimationPolicy = (typeof ANIMATION_POLICY)[keyof typeof ANIMATION_POLICY];
const SOURCE = { GLOBAL: "global_file", DEFAULT: "default" } as const;
export const ANIMATION_KEY = "shell.appearance.animations";

const TO_SETTING: Record<AnimationPolicy, AnimationsSetting> = { quality: "full", potato: "off" };
const FROM_SETTING: Record<AnimationsSetting, AnimationPolicy> = { full: "quality", off: "potato" };

interface AnimationOptions { settings?: ShellSettings }
export interface AnimationResolution {
	policy: AnimationPolicy;
	source: (typeof SOURCE)[keyof typeof SOURCE];
	/** The settings could not be read: the policy is the default. */
	malformed: boolean;
	/** Where the setting is kept. */
	globalFile: string;
}

export function resolveAnimationPolicy(options: AnimationOptions = {}): AnimationResolution {
	const settings = options.settings ?? shellSettings();
	const policy = FROM_SETTING[settings.animations()] ?? "quality";
	return { policy, source: settings.source(ANIMATION_KEY) === "default" && !settings.unreadable ? SOURCE.DEFAULT : SOURCE.GLOBAL, malformed: settings.unreadable, globalFile: settings.where };
}

/** Saved with `jhon settings set`; throws when jhon refuses or cannot. */
export function writeAnimationPolicy(policy: AnimationPolicy, options: AnimationOptions = {}): string {
	const settings = options.settings ?? shellSettings();
	const result = settings.set(ANIMATION_KEY, TO_SETTING[policy]);
	if (!result.ok) throw new Error(result.error);
	return settings.path;
}
