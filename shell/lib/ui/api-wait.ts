// How long the agent has waited on the Claude API (T45, L147): a provider
// request with no first byte can hold a turn for minutes while Claude Code
// retries, and the prompt only said "working…". While a turn runs and no
// tool does, the wait counts from the last stream event (or the turn's or a
// tool's end); past API_WAIT_MS the working line names it.

export const API_WAIT_MS = 60_000;

export interface ApiWait {
	/** A turn or a provider request began: the wait counts from now. */
	start(): void;
	/** A stream event arrived: the wait counts again from now. */
	event(): void;
	toolStart(): void;
	toolEnd(): void;
	/** The turn ended: nothing waits. */
	end(): void;
	/** `waiting for Claude API · <N>s` past the threshold, else undefined. */
	label(): string | undefined;
	/** Milliseconds waited on the API, or undefined when no turn runs or a tool does. */
	waited(): number | undefined;
}

export function createApiWait(now: () => number, thresholdMs = API_WAIT_MS): ApiWait {
	let since: number | undefined;
	let tools = 0;
	return {
		start() { since = now(); tools = 0; },
		event() { if (since !== undefined) since = now(); },
		toolStart() { if (since !== undefined) tools += 1; },
		toolEnd() {
			if (since === undefined) return;
			tools = Math.max(0, tools - 1);
			since = now();
		},
		end() { since = undefined; tools = 0; },
		label() {
			const waited = this.waited();
			return waited !== undefined && waited >= thresholdMs ? `waiting for Claude API · ${Math.floor(waited / 1000)}s` : undefined;
		},
		waited() {
			return since === undefined || tools > 0 ? undefined : now() - since;
		},
	};
}
