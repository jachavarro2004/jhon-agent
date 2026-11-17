// pi-claude-bridge only forwards the structured
// systemPromptOptions parts of before_agent_start, dropping any
// handler-returned replacement systemPrompt. Extensions mutate
// options.appendSystemPrompt instead so the harness reaches every provider.
export interface AppendableSystemPromptOptions {
	appendSystemPrompt: string;
}

// Safe to call more than once with the same options object and the same
// text: a text already present is a no-op, so a handler that runs more than
// once against the same systemPromptOptions never duplicates its own block.
export function appendSystemPromptOnce(
	options: AppendableSystemPromptOptions | null | undefined,
	text: string,
): void {
	const normalized = text.replace(/^\n+/, "");
	if (!options || !normalized) return;
	const current = options.appendSystemPrompt ?? "";
	if (current.includes(normalized)) return;
	options.appendSystemPrompt = current ? `${current}\n\n${normalized}` : normalized;
}

// T45b: the custom section pi renders after the appended prompt (pi's builder
// puts custom sections last; pi-claude-bridge forwards them after the append
// too). Text that changes per run or per owner message goes here, so the
// appended prompt, and the cached prefix up to it, stays byte-identical.
export const SESSION_SECTION = "jhon_session";

export interface SectionedSystemPromptOptions {
	sections?: Record<string, string>;
}

/** text appended once to the session section, as appendSystemPromptOnce does to the appended prompt. */
export function appendSessionOnce(
	options: SectionedSystemPromptOptions | null | undefined,
	text: string,
): void {
	const normalized = text.replace(/^\n+/, "");
	if (!options || !normalized) return;
	options.sections ??= {};
	const current = options.sections[SESSION_SECTION] ?? "";
	if (current.includes(normalized)) return;
	options.sections[SESSION_SECTION] = current ? `${current}\n\n${normalized}` : normalized;
}
