/**
 * shell/lib/ui/pi-layout-hook.ts — the one place the shell reaches into a
 * pi-tui internal (T174 D2, the owner's L1469 answer).
 *
 * pi lays a full-screen terminal out from its layout root: the component
 * pi-tui keeps as `layoutRoot` (TuiAltScreen), whose layout node is a method
 * keyed by `Symbol.for("@earendil-works/pi-tui/layout-node")`. Neither is
 * exported, and the extension API has no way to put a pane beside the
 * conversation. The hook replaces that one method on pi's root with one that
 * returns the shell's own tree (built from pi-tui's public VStack and HStack),
 * with pi's own layout nested inside it, and puts the method back on dispose.
 *
 * Nothing else in the shell names the symbol or `layoutRoot`. When the
 * internal is not there at run time (a pi-tui that moved it), the hook says so
 * once through `note` and stays out: the shell keeps its narrow form, drawn
 * through pi's public slots. test/ui-pane.test.ts checks the internal against
 * the pinned pi-tui in shell/node_modules.
 */
import type { Component, TUI } from "@earendil-works/pi-tui";

/** The pi-tui layout symbol (layout-node.ts in pi-tui 1.5.0), not exported by the package. */
export const PI_LAYOUT_NODE: unique symbol = Symbol.for("@earendil-works/pi-tui/layout-node") as never;

/** The note the hook gives, once per process, when the internal is missing. */
export const LAYOUT_HOOK_MISSING_NOTE = "This pi-tui has no layout hook the right pane can use; the shell shows its narrow form instead.";

type Rooted = Component & { [PI_LAYOUT_NODE]?: () => unknown };
type Host = TUI & { mode?: string; layoutRoot?: Rooted };

/**
 * What the TUI offers: `regular` (not full-screen: no layout root by design),
 * `pending` (full-screen, pi has not set its root yet), `ready`, or `missing`
 * (full-screen, and the internal is not where pi-tui 1.5.0 keeps it).
 */
export type LayoutHookState = "regular" | "pending" | "ready" | "missing";

export function layoutHookState(tui: TUI): LayoutHookState {
	const host = tui as Host;
	if (host.mode !== "fullscreen") return "regular";
	if (!("layoutRoot" in host)) return "missing";
	const root = host.layoutRoot;
	if (root === undefined) return "pending";
	return typeof root[PI_LAYOUT_NODE] === "function" ? "ready" : "missing";
}

const NOTED = Symbol.for("jhon-shell.layout-hook.noted");
const noted = globalThis as typeof globalThis & { [NOTED]?: boolean };

/** Says the missing internal once per process, whatever the number of terminals or sessions. */
function noteMissing(note: (text: string) => void): void {
	if (noted[NOTED]) return;
	noted[NOTED] = true;
	try { note(LAYOUT_HOOK_MISSING_NOTE); } catch { /* A note never breaks the shell. */ }
}

/** Forget the note, for tests that check it is given once. */
export function resetLayoutHookNote(): void {
	noted[NOTED] = false;
}

export interface LayoutHook {
	/** The shell's tree is in place on pi's current root. */
	attached(): boolean;
	/** The internal was missing: the shell keeps its narrow form. */
	missing(): boolean;
	dispose(): void;
}

/**
 * Wraps pi's layout root: `build(native)` gets a component that lays out
 * pi's own tree and returns the component whose layout replaces the root's.
 * Each new root pi sets (a renderer switch) is wrapped again; `wanted()` false
 * leaves pi's own layout for that frame.
 */
export function hookLayout(tui: TUI, build: (native: Component) => Component, wanted: () => boolean, note: (text: string) => void, poll = 100): LayoutHook {
	const host = tui as Host;
	const wrapped = new WeakSet<object>();
	const restore: Array<() => void> = [];
	let stopped = false;
	let missing = false;
	let current: Rooted | undefined;
	const attach = () => {
		if (stopped || missing) return;
		const state = layoutHookState(tui);
		if (state === "missing") { missing = true; noteMissing(note); return; }
		if (state !== "ready") return;
		const root = host.layoutRoot!;
		if (wrapped.has(root)) { current = root; return; }
		try {
			const original = root[PI_LAYOUT_NODE]!;
			const own = Object.getOwnPropertyDescriptor(root, PI_LAYOUT_NODE);
			const native: Rooted = { render: () => [], invalidate() {}, [PI_LAYOUT_NODE]: () => original.call(root) };
			const tree = build(native) as Rooted;
			if (typeof tree[PI_LAYOUT_NODE] !== "function") { missing = true; noteMissing(note); return; }
			const replacement = () => (!stopped && wanted() ? tree[PI_LAYOUT_NODE]!() : original.call(root));
			root[PI_LAYOUT_NODE] = replacement;
			wrapped.add(root);
			current = root;
			restore.push(() => {
				if (root[PI_LAYOUT_NODE] !== replacement) return;
				if (own) Object.defineProperty(root, PI_LAYOUT_NODE, own);
				else Reflect.deleteProperty(root, PI_LAYOUT_NODE);
			});
			tui.requestRender();
		} catch {
			missing = true;
			noteMissing(note);
		}
	};
	attach();
	// pi sets its root when it mounts a renderer, without an event the footer sees.
	const timer = setInterval(attach, poll);
	timer.unref?.();
	return {
		attached: () => !stopped && !missing && current !== undefined && host.mode === "fullscreen" && host.layoutRoot === current,
		missing: () => missing,
		dispose() {
			stopped = true;
			clearInterval(timer);
			for (const undo of restore.reverse()) undo();
			restore.length = 0;
			current = undefined;
			tui.requestRender();
		},
	};
}
