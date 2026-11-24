/**
 * shell/lib/studio-command.ts — pi's /jhon:video (odd/tasks/jhon-studio.md
 * S3): jhon-studio's video skill as a command, registered only while
 * integrations.studio is on (off, nothing about video is registered or
 * shown). In Claude Code the same skill is /jhon-studio:video, from the
 * plugin `jhon start` loads.
 *
 * At each use the command asks `jhon-studio integration --json` where the
 * skill is ({version, plugin_dir, skill}), reads it, and sends its body with
 * the user's arguments as the prompt, as /jhon-ldd builds its prompt
 * (lib/ldd-command.ts): the body after the frontmatter with pi's argument
 * substitution. A skill that names no argument placeholder gets them after
 * it as `ARGUMENTS: <args>`, as Claude Code appends them to a skill's body.
 */
import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { parseFrontmatter } from "@earendil-works/pi-coding-agent";
import { lddPrompt } from "./ldd-command.ts";

/** The setting that turns jhon-studio on (integrations/studio/manifest.yaml). */
export const STUDIO_SETTING = "integrations.studio";
/** jhon-studio's program, found on PATH. */
export const STUDIO_BIN = "jhon-studio";
/** How long `jhon-studio integration --json` may take, as the engine's bound (engine/internal/integrations/cli.go). */
export const STUDIO_TIMEOUT_MS = 10_000;

/** What /jhon:video says it does in pi's `/` list. */
export const VIDEO_DESCRIPTION = "Makes a narrated presentation video of this project with jhon-studio";

/** pi's argument placeholders: $1, $@, $ARGUMENTS, ${N:-default}, ${@:N}. */
const PLACEHOLDER = /\$\{(?:\d+|ARGUMENTS|@)(?::-[^}]*)?\}|\$\{@:\d+(?::\d+)?\}|\$(?:ARGUMENTS|@|\d+)/;

/** The prompt /jhon:video <args> sends: the skill's body, its arguments in place or after it. */
export function videoPrompt(skill: string, args: string): string {
	const prompt = lddPrompt(skill, args);
	const given = args.trim();
	return given && !PLACEHOLDER.test(parseFrontmatter(skill).body) ? `${prompt}\n\nARGUMENTS: ${given}` : prompt;
}

/** One run of a program: what it printed, or why it failed. */
export type Run = (bin: string, args: string[]) => Promise<{ stdout: string; error?: string }>;

const run: Run = (bin, args) => new Promise((resolve) =>
	execFile(bin, args, { encoding: "utf8", timeout: STUDIO_TIMEOUT_MS, killSignal: "SIGKILL", maxBuffer: 1 << 20 }, (error, stdout) => {
		if (!error) return resolve({ stdout });
		const code = (error as NodeJS.ErrnoException).code;
		const why = code === "ENOENT" ? "is not on PATH" : error.killed ? `did not answer in ${STUDIO_TIMEOUT_MS / 1000}s` : `exit ${typeof code === "number" ? code : error.message}`;
		resolve({ stdout: "", error: `${bin} ${why}` });
	}));

/** The video skill's text, read where `jhon-studio integration --json` says it is, or why it cannot be. */
export async function studioSkill(exec: Run = run): Promise<{ text: string } | { error: string }> {
	const r = await exec(STUDIO_BIN, ["integration", "--json"]);
	if (r.error) return { error: r.error };
	let skill: unknown;
	try { skill = (JSON.parse(r.stdout) as { skill?: unknown })?.skill; } catch { skill = undefined; }
	if (typeof skill !== "string" || !skill) return { error: `${STUDIO_BIN} integration --json names no skill` };
	try { return { text: await readFile(skill, "utf8") }; } catch (error) { return { error: `${skill} cannot be read (${(error as Error).message})` }; }
}
