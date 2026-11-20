/**
 * shell/lib/events.ts — the pi.events channels jhon-shell's extensions share.
 */

/**
 * A session's running background tasks (lib/background-tasks-tools.ts), for
 * the shell UI's bar and sidebar: `{ sessionId, tasks: [{ id, label, startedAt }] }`.
 */
export const TASKS_EVENT = "jhon-shell:tasks";

/**
 * Open the Agents view (/jhon:agents), or the Jobs view with `view: "jobs"`, for `{ ctx }`: the narrow work
 * row's tap while sub-agents lead it (T15.12d, as the Gentle Shell's opens its
 * agents view), asked on pi.events rather than sent as a message.
 */
export const OPEN_TASKS_EVENT = "jhon-shell:open-tasks";

/**
 * The session's sandbox pin (lib/sandbox.ts), for the status bar:
 * `{ mode: "on" | "off" }` once `jhon sandbox status --json` answered,
 * `{ mode: undefined }` when it did not.
 */
export const SANDBOX_EVENT = "jhon-shell:sandbox";
