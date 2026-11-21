/**
 * Text from outside the shell (tool output, file names, a model's words) made
 * safe to draw: escape sequences out, and with sanitizeTerminalText the
 * control characters too. Two passes, in this order: first every string
 * command (OSC `ESC ]`, APC `ESC _` and their 8-bit forms 0x9D and 0x9F, pi-tui
 * marks its cursor with APC) up to its terminator, BEL, `ESC \` or 0x9C, or to
 * the end when it has none; then, on what is left, every CSI sequence
 * (`ESC [` or 0x9B, parameters, intermediates, a final byte) and every
 * two-byte escape. An incomplete CSI is left as it is.
 */

const ESC = "\x1b";
const BEL = "\x07";
const ST_8BIT = "\x9c";
const CSI_8BIT = "\x9b";

const inRange = (code: number, low: number, high: number) => code >= low && code <= high;

/** Where a string command opening at `at` begins its payload, or -1 when none opens there. */
function stringCommandStart(text: string, at: number): number {
	const char = text[at];
	if (char === "\x9d" || char === "\x9f") return at + 1;
	if (char === ESC && (text[at + 1] === "]" || text[at + 1] === "_")) return at + 2;
	return -1;
}

function withoutStringCommands(text: string): string {
	let out = "";
	let at = 0;
	while (at < text.length) {
		const payload = stringCommandStart(text, at);
		if (payload < 0) { out += text[at]; at++; continue; }
		let end = payload;
		while (end < text.length) {
			const char = text[end];
			if (char === BEL || char === ST_8BIT) { end += 1; break; }
			if (char === ESC && text[end + 1] === "\\") { end += 2; break; }
			end++;
		}
		at = end;
	}
	return out;
}

/** The end of a CSI body (parameters, intermediates, final) that starts at `from`, or -1 when it is incomplete. */
function csiEnd(text: string, from: number): number {
	let at = from;
	while (at < text.length && inRange(text.charCodeAt(at), 0x30, 0x3f)) at++;
	while (at < text.length && inRange(text.charCodeAt(at), 0x20, 0x2f)) at++;
	return at < text.length && inRange(text.charCodeAt(at), 0x40, 0x7e) ? at + 1 : -1;
}

/** The length of the escape sequence at `at`, or 0 when none starts there. */
function sequenceLength(text: string, at: number): number {
	const char = text[at];
	if (char === CSI_8BIT) {
		const end = csiEnd(text, at + 1);
		return end < 0 ? 0 : end - at;
	}
	if (char !== ESC || at + 1 >= text.length) return 0;
	if (text[at + 1] === "[") {
		const end = csiEnd(text, at + 2);
		return end < 0 ? 0 : end - at;
	}
	const next = text.charCodeAt(at + 1);
	// A two-byte escape: ESC and one of @ to Z or \ ] ^ _ (never `[`, which opens a CSI).
	return inRange(next, 0x40, 0x5a) || inRange(next, 0x5c, 0x5f) ? 2 : 0;
}

export function stripAnsi(value: string): string {
	const text = withoutStringCommands(value);
	let out = "";
	let at = 0;
	while (at < text.length) {
		const length = sequenceLength(text, at);
		if (length > 0) { at += length; continue; }
		out += text[at];
		at++;
	}
	return out;
}

/** C0 and C1 controls go, DEL too; a tab and a newline stay. */
function isControl(code: number): boolean {
	if (code === 0x09 || code === 0x0a) return false;
	return code <= 0x1f || inRange(code, 0x7f, 0x9f);
}

export function sanitizeTerminalText(value: string): string {
	let out = "";
	for (const char of stripAnsi(value)) if (!isControl(char.charCodeAt(0))) out += char;
	return out;
}
