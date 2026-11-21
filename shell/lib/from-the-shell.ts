/**
 * shell/lib/from-the-shell.ts — T194 (L1608): who speaks in the shell's own messages.
 *
 * pi hands a custom message (a task notice, the full context, a steer, a
 * route line) and a send-back to the model as user text, so an English
 * notice read as the owner's latest message switched the reply to English.
 * Every message the shell writes for the model opens with this mark: it is
 * not the owner and does not set the reply language. A card or note the
 * owner reads shows the text without it.
 */

/** The mark a message the shell writes for the model opens with. */
export const NOT_THE_OWNER = "[From the Jhon shell, not the owner; it does not set the reply language]";

/** text as the model reads it: the mark, a space, the text. */
export function fromTheShell(text: string): string {
	return `${NOT_THE_OWNER} ${text}`;
}

/** text as the owner reads it: without the mark, when it opens with it. */
export function withoutTheMark(text: string): string {
	return text.startsWith(`${NOT_THE_OWNER} `) ? text.slice(NOT_THE_OWNER.length + 1) : text;
}
