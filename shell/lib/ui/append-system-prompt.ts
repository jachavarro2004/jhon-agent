/**
 * How the shell's extensions add their text to the system prompt. They never
 * return a replacement prompt from before_agent_start: a provider bridge
 * (pi-claude-bridge) passes on only the structured systemPromptOptions parts,
 * so each extension writes into those options instead.
 *
 * Both helpers add a block once: a handler that runs again on the same
 * options object leaves it as it is. A block loses its leading newlines and
 * follows what is already there after one blank line.
 */

/** The part of pi's options this module touches: the text appended to the system prompt. */
export type AppendableSystemPromptOptions = { appendSystemPrompt: string };

/** The block as it is added: without its leading newlines. */
const asBlock = (text: string) => text.replace(/^\n+/, "");

/** `current` with `block` after it, or undefined when it already holds the block. */
function withBlock(current: string, block: string): string | undefined {
	if (current.includes(block)) return undefined;
	return current === "" ? block : `${current}\n\n${block}`;
}

/** Adds `text` to the appended prompt, the part every provider receives. */
export function appendSystemPromptOnce(options: AppendableSystemPromptOptions | null | undefined, text: string): void {
	const block = asBlock(text);
	if (!options || block === "") return;
	const next = withBlock(options.appendSystemPrompt ?? "", block);
	if (next !== undefined) options.appendSystemPrompt = next;
}

/**
 * The prompt section that changes per run or per owner message (T45b). pi
 * draws custom sections after the appended prompt, and pi-claude-bridge
 * passes them on after it, so text that varies goes here and the appended
 * prompt, with the cached prefix that ends at it, never changes.
 */
export const SESSION_SECTION = "jhon_session";

export interface SectionedSystemPromptOptions {
	sections?: Record<string, string>;
}

/** Adds `text` to the session section, once, as appendSystemPromptOnce does to the appended prompt. */
export function appendSessionOnce(options: SectionedSystemPromptOptions | null | undefined, text: string): void {
	const block = asBlock(text);
	if (!options || block === "") return;
	const sections = (options.sections ??= {});
	const next = withBlock(sections[SESSION_SECTION] ?? "", block);
	if (next !== undefined) sections[SESSION_SECTION] = next;
}
