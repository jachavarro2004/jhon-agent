import { shellSettings, type ShellSettings } from "../settings.ts";

// ---------------------------------------------------------------------------
// Double-esc-cancel policy — shell.behaviour.double_esc_cancel in the owner's
// settings (lib/settings.ts): default off < settings.toml <
// JHON_SHELL_DOUBLE_ESC_CANCEL, resolved by `jhon settings` (S38).
//
// Deliberately global-only (no project layer): this preference changes what
// a keypress does while the agent is working, and a per-project override for
// a personal habit like this would make the same key do two different things
// depending on which repo happens to be open.
// ---------------------------------------------------------------------------

export type DoubleEscCancelPolicy = "on" | "off";

/** Which of the three sources decided the effective policy. */
export type DoubleEscCancelSource = "global_file" | "environment" | "default";

export interface DoubleEscCancelResolution {
	policy: DoubleEscCancelPolicy;
	source: DoubleEscCancelSource;
	/** The settings could not be read, so the policy is off. */
	malformed: boolean;
	/** The settings file. */
	globalFile: string;
	/** settings.toml holds the key. */
	globalFileExists: boolean;
	/** JHON_SHELL_DOUBLE_ESC_CANCEL's value when it decided ("on" or "off"); the engine ignores any other. */
	envValue: DoubleEscCancelPolicy | undefined;
}

export interface LoadDoubleEscCancelOptions {
	/** The settings to read (tests); the process's by default. */
	settings?: ShellSettings;
}

export const DOUBLE_ESC_CANCEL_KEY = "shell.behaviour.double_esc_cancel";

// Pi's own idle double-Esc (empty editor -> /tree or /fork, interactive-mode.js
// `lastEscapeTime`) uses a 500ms window. Canceling a running turn throws away
// in-flight work and is harder to undo than switching prompts, so this
// confirmation deliberately gets double that time to land the second press.
export const DOUBLE_ESC_CANCEL_WINDOW_MS = 1000;

/** The double-esc-cancel policy AND the source that decided it. */
export function resolveDoubleEscCancelPolicy(
	options: LoadDoubleEscCancelOptions = {},
): DoubleEscCancelResolution {
	const settings = options.settings ?? shellSettings();
	const source = settings.source(DOUBLE_ESC_CANCEL_KEY);
	const policy: DoubleEscCancelPolicy = settings.doubleEscCancel() ? "on" : "off";
	return {
		policy,
		source: source === "env" ? "environment" : source === "file" ? "global_file" : "default",
		malformed: settings.unreadable,
		globalFile: settings.where,
		globalFileExists: source === "file",
		envValue: source === "env" ? policy : undefined,
	};
}

/**
 * The effective policy alone, for callers that do not report a source.
 * It delegates so the loader and the resolver can never disagree.
 */
export function loadDoubleEscCancelPolicy(options: LoadDoubleEscCancelOptions = {}): DoubleEscCancelPolicy {
	return resolveDoubleEscCancelPolicy(options).policy;
}

/**
 * Save the policy with `jhon settings set`.
 * Used by both `/jhon:double-esc-cancel enable` and `... disable`.
 */
export function writeDoubleEscCancelPolicy(
	policy: DoubleEscCancelPolicy,
	options: { settings?: ShellSettings } = {},
): string {
	const settings = options.settings ?? shellSettings();
	const result = settings.set(DOUBLE_ESC_CANCEL_KEY, policy === "on");
	if (!result.ok) throw new Error(result.error);
	return settings.path;
}
