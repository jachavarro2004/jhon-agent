/**
 * shell/lib/m365.ts — the `m365_read` tool: pi reads Microsoft 365 (the
 * HUs in SharePoint) through Claude Code, read-only.
 *
 * pi-claude-bridge always blocks claude.ai's cloud MCP, and Microsoft 365 is
 * one; a headless Claude Code (`claude -p`) loads it under the owner's
 * claude.ai login. That connector also sends mail, deletes, and uploads, and
 * Jhon's guards cannot see inside that process, so the read-only limit is
 * the command line this file builds, never the prompt:
 *
 * - `--permission-mode dontAsk` runs only the tools an allow rule names
 *   (it overrides a bypassPermissions default in the settings), and
 *   `--allowedTools` names only M365_READ;
 * - `--disallowedTools` denies every other Microsoft 365 tool known
 *   (M365_DENIED), so a settings allow rule can never admit one;
 * - `--tools ""` removes every built-in tool (Bash, Edit, Write, Read);
 * - it runs in an empty temporary directory, with no session saved.
 *
 * The user settings stay loaded: `--setting-sources ""` and
 * `--strict-mcp-config` both drop the claude.ai connectors.
 */

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export const M365_PREFIX = "mcp__claude_ai_Microsoft_365__";

/** Every Microsoft 365 tool the connector offers, as of 2026-10-28. */
export const M365_TOOLS = [
	"chat_message_search", "find_meeting_availability", "get_granted_scopes", "get_me",
	"outlook_batch_delete_messages", "outlook_batch_modify_labels", "outlook_calendar_search", "outlook_create_draft",
	"outlook_create_event", "outlook_create_filter", "outlook_create_label", "outlook_create_reply_all_draft",
	"outlook_create_reply_draft", "outlook_delete_draft", "outlook_delete_event", "outlook_delete_filter",
	"outlook_delete_label", "outlook_email_search", "outlook_find_available_time", "outlook_forward_mail",
	"outlook_modify_labels", "outlook_modify_thread_labels", "outlook_respond_to_event", "outlook_send_draft",
	"outlook_send_mail", "outlook_set_vacation", "outlook_trash_thread", "outlook_untrash_thread",
	"outlook_update_draft", "outlook_update_event", "outlook_update_label", "read_resource",
	"search_people", "sharepoint_copy_item", "sharepoint_create_folder", "sharepoint_delete_item",
	"sharepoint_folder_search", "sharepoint_move_item", "sharepoint_rename_item", "sharepoint_search",
	"sharepoint_update_file", "sharepoint_upload_file", "teams_create_chat", "teams_list_channel_messages",
	"teams_list_channels", "teams_list_chats", "teams_list_teams", "teams_reply_channel_message",
	"teams_send_channel_message", "teams_send_chat_message",
].map((t) => M365_PREFIX + t);

/** The tools m365_read may run: finding and reading SharePoint and OneDrive documents. */
export const M365_READ = ["sharepoint_search", "sharepoint_folder_search", "read_resource"].map((t) => M365_PREFIX + t);

/** Every other Microsoft 365 tool, denied outright. */
export const M365_DENIED = M365_TOOLS.filter((t) => !M365_READ.includes(t));

/** What the reading Claude is told, beside the owner's request. */
export const M365_PROMPT = "You read Microsoft 365 for an agent that cannot: find the document the request names in SharePoint or OneDrive and read it. Return the parts the request asks for verbatim (a user story's criteria and rules word for word), then the document's name, its path, and its last-modified date. You only read: never create, move, send, or change anything.";

/** The claude arguments of one m365_read request. */
export function m365Args(request: string, model: string): string[] {
	return [
		"-p", request,
		"--model", model,
		"--permission-mode", "dontAsk",
		"--tools", "",
		"--allowedTools", M365_READ.join(","),
		"--disallowedTools", M365_DENIED.join(","),
		"--append-system-prompt", M365_PROMPT,
		"--no-session-persistence",
		"--disable-slash-commands",
	];
}

export interface M365Options {
	bin?: string;
	model?: string;
	timeoutMs?: number;
	signal?: AbortSignal;
}

/** The reading Claude's answer to request, or why there is none. */
export function m365Read(request: string, options: M365Options = {}): Promise<{ ok: boolean; text: string }> {
	const cwd = mkdtempSync(join(tmpdir(), "jhon-m365-"));
	const child = spawn(options.bin ?? "claude", m365Args(request, options.model ?? "sonnet"), { cwd, stdio: ["ignore", "pipe", "pipe"] });
	let out = "", err = "";
	child.stdout.on("data", (d) => { out += d; });
	child.stderr.on("data", (d) => { err += d; });
	const stop = () => child.kill("SIGTERM");
	const timer = setTimeout(stop, options.timeoutMs ?? 300_000);
	options.signal?.addEventListener("abort", stop, { once: true });
	return new Promise((resolve) => {
		const done = (ok: boolean, text: string) => {
			clearTimeout(timer);
			options.signal?.removeEventListener("abort", stop);
			rmSync(cwd, { recursive: true, force: true });
			resolve({ ok, text });
		};
		child.on("error", (e) => done(false, `m365_read: could not run ${options.bin ?? "claude"}: ${e.message}`));
		child.on("close", (code, signal) => {
			if (code === 0 && out.trim()) done(true, out.trim());
			else done(false, `m365_read: claude ${signal ? `stopped (${signal})` : `exited ${code}`}${err.trim() ? `: ${err.trim()}` : ""}${out.trim() ? `\n${out.trim()}` : ""}`);
		});
	});
}
