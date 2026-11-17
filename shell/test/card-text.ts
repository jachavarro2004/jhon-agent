import { stripAnsi } from "../lib/ui/terminal-theme.ts";

// Test-only readers for shell cards: the title text in the top rule and
// the body between the rules with the frame removed. They accept both plain and tagged output.

const TAG = /<\/?[a-zA-Z]+>/g;
const EDGE_LEFT = /^(?:<[a-zA-Z]+>)?│(?:<\/[a-zA-Z]+>)? ?/;
const EDGE_RIGHT = / ?(?:<[a-zA-Z]+>)?│(?:<\/[a-zA-Z]+>)?$/;

function plain(line: string): string {
	return stripAnsi(line).replace(TAG, "");
}

export function cardTitle(rendered: string): string {
	const first = rendered.split("\n")[0] ?? "";
	return plain(first).replace(/^╭─ /, "").replace(/ ─+(?: .* )?╮$/, "").trim();
}

export function cardBody(rendered: string): string {
	return rendered
		.split("\n")
		.filter((line) => !/^[╭╰]/.test(plain(line)))
		.map((line) => line.replace(EDGE_LEFT, "").replace(EDGE_RIGHT, "").trimEnd())
		.join("\n");
}

export function cardHint(rendered: string): string | undefined {
	const first = rendered.split("\n")[0] ?? "";
	return plain(first).match(/ ─+ (.+) ╮$/)?.[1];
}
