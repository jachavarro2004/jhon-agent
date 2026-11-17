/**
 * shell/lib/telemetry.ts — the jhon shell's own events, beside the engine's:
 * one JSON line each, appended to ~/.local/share/jhon/telemetry/<UTC day>.jsonl
 * in the engine's format (engine/internal/telemetry): an OpenTelemetry log
 * record, `{time, name, severity, attributes}`. It records what only the
 * shell sees: a sub-agent's run and how it ended, and the compaction it
 * starts. Telemetry is an opt-in integration (T55): it records only when
 * the settings turn integrations.telemetry on (JHON_TELEMETRY overrides it,
 * through `jhon settings`). Recording never fails its caller;
 * JHON_TELEMETRY=off records nothing.
 */
import { appendFileSync, mkdirSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { shellSettings } from "./settings.ts";

// Under node's test runner the runs are the tests', not the owner's: they
// record nothing, nor does any jhon they start, unless a test turns it on.
if (process.env.NODE_TEST_CONTEXT && process.env.JHON_TELEMETRY === undefined) process.env.JHON_TELEMETRY = "off";

export type Attributes = Record<string, string | number | boolean | string[]>;

/** Where the records are. */
export function telemetryDir(env: NodeJS.ProcessEnv = process.env): string {
	return join(env.HOME || homedir(), ".local", "share", "jhon", "telemetry");
}

/** One event as its file holds it, newline-terminated. */
export function telemetryLine(name: string, attributes: Attributes, severity: "INFO" | "WARN" = "INFO", at: Date = new Date()): string {
	return `${JSON.stringify({ time: at.toISOString(), name, severity, attributes })}\n`;
}

/** Append one event to its day's file. */
export function recordEvent(name: string, attributes: Attributes, severity: "INFO" | "WARN" = "INFO", env: NodeJS.ProcessEnv = process.env, at: Date = new Date()): void {
	if (env.JHON_TELEMETRY === "off" || !shellSettings().bool("integrations.telemetry")) return;
	try {
		const dir = telemetryDir(env);
		mkdirSync(dir, { recursive: true, mode: 0o700 });
		appendFileSync(join(dir, `${at.toISOString().slice(0, 10)}.jsonl`), telemetryLine(name, attributes, severity, at), { mode: 0o600 });
	} catch { /* recording never fails its caller */ }
}
