/**
 * shell/lib/ldd-command.ts — pi's /jhon-ldd (L194): the release plugin's
 * jhon-ldd skill (runtimes/claude-code/plugin/skills/jhon-ldd/SKILL.md),
 * which the render used to copy to <agent dir>/prompts/jhon-ldd.md as a
 * prompt template. A template cannot list its actions; a command can. Its
 * prompt is what pi made of that template, byte for byte: the body after
 * the frontmatter, trimmed, with pi's argument substitution
 * (dist/core/prompt-templates.js, not exported, so its two functions are
 * carried here; test/ldd-command.test.ts holds them equal to pi's).
 */
import { fileURLToPath } from "node:url";
import { parseFrontmatter } from "@earendil-works/pi-coding-agent";
import { completeWord, type Completion } from "./ui/jhon-commands.ts";

/** The skill in this release; the shell sits beside it (shell/ and runtimes/ in one tree). */
export const LDD_SKILL = fileURLToPath(new URL("../../runtimes/claude-code/plugin/skills/jhon-ldd/SKILL.md", import.meta.url));

/**
 * pi's parseCommandArgs (1.5.1, #11861): bash-style words, quotes kept
 * together. A quote opens only at the start of a word (`doesn't` stays one
 * word), an unclosed one is kept as text, and `\"` inside `"..."` (or `\'`
 * inside `'...'`) is a literal quote.
 */
function parseArgs(text: string): string[] {
	const args: string[] = [];
	let current = "";
	let started = false;
	let quote: string | null = null;
	let quoteAt = 0;
	for (let i = 0; i < text.length; i++) {
		const char = text[i]!;
		if (quote) {
			if (char === "\\" && text[i + 1] === quote) { current += quote; i++; }
			else if (char === quote) quote = null;
			else current += char;
		} else if ((char === '"' || char === "'") && !started) {
			quote = char;
			quoteAt = i;
			started = true;
		} else if (/\s/.test(char)) {
			if (started) { args.push(current); current = ""; started = false; }
		} else {
			current += char;
			started = true;
		}
		// Unclosed: the quote is text, and the rest is read again after it.
		if (quote && i === text.length - 1) { quote = null; current = text[quoteAt]!; i = quoteAt; }
	}
	if (started) args.push(current);
	return args;
}

/** pi's substituteArgs: $1, $@, $ARGUMENTS, ${N:-default}, ${@:N[:L]}, never recursively. */
function substitute(content: string, args: string[]): string {
	const all = args.join(" ");
	return content.replace(/\$\{(\d+|ARGUMENTS|@):-([^}]*)\}|\$\{@:(\d+)(?::(\d+))?\}|\$(ARGUMENTS|@|\d+)/g, (_m, target, fallback, start, length, simple) => {
		if (target) {
			const value = target === "@" || target === "ARGUMENTS" ? all : args[parseInt(target, 10) - 1];
			return value ? value : fallback;
		}
		if (start) {
			const from = Math.max(0, parseInt(start, 10) - 1);
			return (length ? args.slice(from, from + parseInt(length, 10)) : args.slice(from)).join(" ");
		}
		if (simple === "ARGUMENTS" || simple === "@") return all;
		return args[parseInt(simple, 10) - 1] ?? "";
	});
}

/** The prompt /jhon-ldd <args> sends: the template's body with its arguments in place. */
export function lddPrompt(skill: string, args: string): string {
	return substitute(parseFrontmatter(skill).body, parseArgs(args));
}

/** Each action of the skill's table, in its order, with its line's first clause. */
export function lddActions(skill: string): Completion[] {
	const out: Completion[] = [];
	for (const [, action, does] of parseFrontmatter(skill).body.matchAll(/^\| `([a-z]+)` \| (.+) \|$/gm)) {
		const line = does!.replace(/`/g, "").split(/; |\. |, following /)[0]!.trim().replace(/\.$/, "");
		out.push({ value: action!, label: action!, description: line });
	}
	return out;
}

/** The action first, then the change after it (value the whole argument, as pi replaces it); nothing past both. */
export function lddCompletions(skill: string, prefix: string, changes: readonly Completion[]): Completion[] | null {
	const text = prefix.trimStart();
	const space = text.search(/\s/);
	if (space < 0) return completeWord(lddActions(skill), text);
	const action = text.slice(0, space);
	if (!lddActions(skill).some((a) => a.value === action)) return null;
	const named = completeWord(changes, text.slice(space).trimStart());
	return named ? named.map((c) => ({ ...c, value: `${action} ${c.value}` })) : null;
}
