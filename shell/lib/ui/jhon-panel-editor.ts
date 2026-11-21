/**
 * shell/lib/ui/jhon-panel-editor.ts — opening a file in the owner's editor
 * from inside pi, with the TUI stopped around it: the change detail's o (the
 * change's tasks.md) and the changes overlay's o (extensions/ui.ts) share it.
 */
import { spawnSync } from "node:child_process";

/** The TUI as the editor launch needs it: stopped while the editor owns the terminal, started and repainted after. */
export type ExternalEditorHost = { stop: () => void; start: () => void; requestRender: (force?: boolean) => void };

/**
 * Open `path` in $VISUAL or $EDITOR with the TUI stopped around it: why it
 * could not, or undefined once the editor closed cleanly. The command is
 * read by the shell, as git reads it, so quotes and paths with spaces
 * (`code --wait`, `"/opt/my editor/bin/ed"`) work, and the file is passed
 * as one argument.
 */
export function openInExternalEditor(host: ExternalEditorHost, path: string, env: NodeJS.ProcessEnv = process.env, spawn: typeof spawnSync = spawnSync, cwd?: string): string | undefined {
	const command = env.VISUAL || env.EDITOR;
	if (!command?.trim()) return "No editor: set $VISUAL or $EDITOR.";
	host.stop();
	let r: ReturnType<typeof spawnSync>;
	try {
		r = process.platform === "win32"
			? spawn(`${command} "${path}"`, [], { cwd, stdio: "inherit", shell: true })
			: spawn("sh", ["-c", `${command} "$@"`, command, path], { cwd, stdio: "inherit" });
	} finally {
		host.start();
		host.requestRender(true);
	}
	if (r.error) return `Could not run the editor (${command}): ${r.error.message}`;
	if (r.status !== 0) return `The editor (${command}) ended with ${r.status === null ? `signal ${r.signal}` : `code ${r.status}`}.`;
	return undefined;
}

