/**
 * shell/lib/ui/jhon-panel.ts — Jhon's views of what the jhon CLI configures
 * and reports, each its own overlay on the overlay pattern (T15.10, L113),
 * never tabs:
 *
 *   change    alt+j, /jhon:status [<name>]: the LDD detail, the rail's
 *             STATUS expanded (jhon-panel-change.ts): its route, risk and
 *             why, slices, the open slice's tasks, notes and last run. t
 *             runs its tests in the background and shows their end here
 *             (jhon-panel-tests.ts), o opens its tasks.md.
 *   grants    /jhon:grants: the owner's grants for the project, given,
 *             revoked and timed (jhon-panel-grants.ts); the permission
 *             mode is Customize's (L214).
 *   profiles  /jhon:profile: the model profiles as tabs, each one's
 *             delegation mode and models as a draft saved with s, used in
 *             this session only (u, T120), made the default for new
 *             sessions (d), created and removed (jhon-panel-profiles.ts,
 *             the L694 mockup).
 *
 * The audit and the memory are the dashboard's (design/responsibilities.md).
 */
import { Key, matchesKey, truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";
import { shellSettings, type ShellSettings } from "../settings.ts";
import { ProfilesCard, type ProfilesData } from "./jhon-panel-profiles.ts";
import { changeCardLines } from "./jhon-panel-change.ts";
import { GrantsCard, type GrantsData } from "./jhon-panel-grants.ts";
import { TestsCard, type TestsData } from "./jhon-panel-tests.ts";
import { asciiText, renderBox, renderStateBlock, themePainter, type KeyHint } from "./primitives/index.ts";
import { fullscreenBodyRows, renderFullscreenView } from "./fullscreen-view.ts";

/** A key hint as G's footers write it, `<Key> <action>`: `esc` → `Esc`, `or` kept. */
function hintText(hint: KeyHint): string {
	const [k, action] = Array.isArray(hint) ? hint : [(hint as unknown as Record<string, string>)["key"]!, (hint as unknown as Record<string, string>)["action"]!];
	return `${k.replace(/\b[a-z][a-z]+\b/g, (word: string) => (word === "or" ? word : `${word[0]!.toUpperCase()}${word.slice(1)}`))} ${action}`;
}

export { modelText, type ProfileRow, type ProfilesData } from "./jhon-panel-profiles.ts";

export interface JhonPanelTheme {
	fg(role: string, text: string): string;
	bold(text: string): string;
	bg?(role: string, text: string): string;
	underline?(text: string): string;
}

export interface JhonPanelDeps {
	theme: JhonPanelTheme;
	/** The runtime this shell is: its rows come first. */
	runtime: string;
	load(): Promise<ProfilesData>;
	/** Make a profile the default for new sessions (`jhon profile use`); the error's text when it failed. */
	use(name: string): Promise<string | undefined>;
	/** d in the profiles: make the profile the default; without it, use. */
	makeDefault?(name: string): Promise<string | undefined>;
	/** u in the profiles: use the profile in this session only (T120; T117c, the prompt rebuilt once); without it, use. */
	useNow?(name: string): Promise<string | undefined>;
	/** The profile this session uses, when it has one of its own (T120); undefined while it follows the default. */
	sessionProfile?(): string | undefined;
	/** Create, edit, and remove a profile (jhon profile new | set | mode | rm), and the models a runtime can take. */
	createProfile?(name: string, from: string): Promise<void>;
	setModel?(profile: string, agent: string, runtime: string, value: string): Promise<void>;
	/** s in the profiles, when the draft's mode changed: `jhon profile mode <profile> <inline|writer>`. */
	setMode?(profile: string, mode: "inline" | "writer"): Promise<void>;
	removeProfile?(name: string): Promise<void>;
	models?(runtime: string, data: ProfilesData): string[];
	/** Whether this session's pi lists the model (`provider/id`) as available now; without it, nothing is said on a save. */
	inSession?(model: string): boolean;
	/** `jhon status --json` where the session's change is, with `root` (that folder) and `risk` (`jhon risk --json`) when read. */
	loadChange(): Promise<unknown>;
	/** Open path in the owner's editor, the view closed around it; why it could not (no editor), with the view left open. */
	openFile?(path: string): string | undefined;
	/** `jhon grant --list --json`, and giving or revoking one class. */
	loadGrants(): Promise<GrantsData>;
	grant(cls: string, duration: string): Promise<void>;
	revoke(cls: string): Promise<void>;
	/** The change's test plan (a jhon executions dry run) and last run; running it, and stopping it. */
	loadTests(): Promise<TestsData>;
	startTests(data: TestsData, onEnd: (exitCode: number | null) => void): { id: string; startedAt: number };
	stopTests(id: string): void;
	/** The rows the card may take, frame included (G's rowsAvailable); the card keeps to 24, as G's detail card. */
	height(): number;
	/** The terminal's rows: the LDD detail and the profiles take them all, as G's profiles view (L174); without it, height(). */
	rows?(): number;
	/** The view it shows: the change's detail unless named. */
	view?: View;
	/** shell.appearance.ascii: frames and glyphs in ASCII. */
	ascii?: boolean;
	onClose(): void;
	requestRender(): void;
}

/** The LDD detail's key, shell.keys.panel (alt+j; JHON_SHELL_JHON_KEY overrides it); "off" turns it off. */
export function jhonPanelKey(settings: ShellSettings = shellSettings()): string | undefined {
	return settings.key("panel");
}

export type View = "change" | "grants" | "profiles";

/** Painted text cut to width with `…` (ANSI kept). */
function cut(text: string, width: number, ascii: boolean): string {
	return visibleWidth(text) <= width ? text : truncateToWidth(text, Math.max(0, width), ascii ? "~" : "…");
}

const MAX_HEIGHT = 24;

/** Each view's title; the change view is LDD's (L174), never a "change" that could be git's. */
const TITLES: Record<View, string> = { change: "LDD", grants: "Grants", profiles: "Profiles" };
/** The full-screen views' title rows under their tab (G's profiles view): the change's name, or what the view holds. */
const HEADINGS: Partial<Record<View, string>> = { profiles: "Profiles and their models" };

export class JhonPanel {
	private readonly deps: JhonPanelDeps;
	private readonly view: View;
	private readonly profiles: ProfilesCard;
	private change: { status?: unknown; error?: string; loading: boolean; at: number; note?: string } = { loading: false, at: 0 };
	private scroll = 0;
	private readonly grants: GrantsCard;
	private readonly tests: TestsCard;

	constructor(deps: JhonPanelDeps) {
		this.deps = deps;
		this.view = deps.view ?? "change";
		const missing = async () => { throw new Error("this session cannot edit profiles"); };
		this.profiles = new ProfilesCard({
			runtime: deps.runtime, load: deps.load, requestRender: deps.requestRender,
			useNow: deps.useNow ?? deps.use, makeDefault: deps.makeDefault ?? deps.use,
			create: deps.createProfile ?? missing, set: deps.setModel ?? missing, mode: deps.setMode ?? missing, remove: deps.removeProfile ?? missing,
			models: deps.models ?? ((runtime, data) => modelsSeen(runtime, data)),
			inSession: deps.inSession,
			sessionProfile: deps.sessionProfile,
		});
		this.grants = new GrantsCard({ load: deps.loadGrants, grant: deps.grant, revoke: deps.revoke, requestRender: deps.requestRender });
		// A run's end reads the change again: its Last run is the record the run wrote.
		this.tests = new TestsCard({
			load: deps.loadTests, stop: deps.stopTests, requestRender: deps.requestRender,
			start: (data, onEnd) => deps.startTests(data, (code) => { onEnd(code); this.reloadChange(); }),
		});
	}

	/** A closed view leaves no timer behind; a running test run keeps going in BACKGROUND. */
	dispose(): void {
		this.tests.dispose();
	}

	/** Read the session's change; the detail draws «Reading the change…» until it arrives. */
	reloadChange(): void {
		this.change = { ...this.change, loading: true };
		this.deps.requestRender();
		const settle = (status?: unknown, error?: string) => {
			this.change = { status, error, loading: false, at: Date.now() };
			this.deps.requestRender();
		};
		try {
			this.deps.loadChange().then((s) => settle(s), (e) => settle(undefined, e instanceof Error ? e.message : String(e)));
		} catch (e) {
			settle(undefined, e instanceof Error ? e.message : String(e));
		}
	}

	/** Load what the view shows. */
	open(): void {
		if (this.view === "change") this.reloadChange();
		else if (this.view === "grants") this.grants.reload();
		else this.profiles.reload();
	}

	/** The change the detail shows, when it shows one: its name, folder, and record. */
	private changeShown(): { change: string; root?: string; directory?: string } | undefined {
		const s = this.change.status as any;
		if (this.change.error || typeof s?.change !== "string") return undefined;
		// o opens tasks.md: only a folder whose record says it holds one (an older engine's says nothing).
		const tasks = Array.isArray(s.artifacts) ? s.artifacts.find((a: any) => a?.artifact === "tasks.md") : undefined;
		const directory = typeof s.directory === "string" && (!tasks || tasks.state === "done") ? s.directory : undefined;
		return { change: s.change, root: typeof s.root === "string" ? s.root : undefined, directory };
	}

	/** The detail's actions on its change: the keys and what they do. */
	private changeAction(data: string): boolean {
		const shown = this.changeShown();
		if (!shown) return false;
		if (data === "t") {
			// The owner's run: its time and its end show here, under the change.
			this.tests.runWhenReady();
			return true;
		}
		if (data === "s" && this.tests.running()) return this.tests.handleInput("s");
		if (data === "o" && this.deps.openFile && shown.directory) {
			const path = [shown.root, shown.directory, "tasks.md"].filter(Boolean).join("/");
			const failed = this.deps.openFile(path);
			this.change = { ...this.change, note: failed ? `tasks.md is ${path}: ${failed}` : undefined };
			return true;
		}
		return false;
	}

	private changeKeys(): KeyHint[] {
		const shown = this.changeShown();
		if (!shown) return [];
		return [
			this.tests.running() ? ["s", "stop tests"] : ["t", "tests"],
			...(this.deps.openFile && shown.directory ? [["o", "tasks.md"] as KeyHint] : []),
		];
	}

	handleInput(data: string): void {
		const up = matchesKey(data, Key.up) || data === "k";
		const down = matchesKey(data, Key.down) || data === "j";
		if (this.view === "profiles") {
			// An open profile edit takes every key, esc included.
			if (this.profiles.captures()) return void this.profiles.handleInput(data);
			if (matchesKey(data, Key.escape) || data === "q") return this.deps.onClose();
			if (this.profiles.handleInput(data)) return;
			return;
		}
		if (this.view === "grants") {
			if (matchesKey(data, Key.escape) && this.grants.cancelPending()) return;
			// A waiting confirmation takes the next key, whatever it is.
			if (this.grants.keys()[0]?.[1] === "confirm") return void this.grants.handleInput(data);
			if (matchesKey(data, Key.escape) || data === "q") return this.deps.onClose();
			return void this.grants.handleInput(data);
		}
		if (matchesKey(data, Key.escape) || data === "q") return this.deps.onClose();
		if (up) this.scroll = Math.max(0, this.scroll - 1);
		else if (down) this.scroll += 1;
		else if (data === "r") return this.reloadChange();
		else if (!this.changeAction(data)) return;
		this.deps.requestRender();
	}

	render(width: number): string[] {
		const t = this.deps.theme;
		const paint = themePainter(t);
		const ascii = this.deps.ascii ?? false;
		// The LDD detail and the profiles are work views, near full screen as G's profiles view (L174); the grants are the small card.
		const full = this.view !== "grants";
		const rows = Math.floor(this.deps.rows?.() ?? this.deps.height());
		const inner = Math.max(10, width - 4);
		// G's detailSize: min(24, the rows available).
		const height = Math.max(8, Math.min(MAX_HEIGHT, Math.floor(this.deps.height())));
		// The card: the header row and a gap above the body; a gap and the footer below.
		const room = full ? fullscreenBodyRows(rows) : Math.max(3, height - 2 - 4);
		// One row is one line: a newline in a message (jhon's JSON in an error) would draw past the frame.
		// The cards' rows are words with glyphs among them (· ◆ ✓ ◐): in ASCII they fold as the frame does.
		const fitRow = (text: string) => cut((ascii ? asciiText(text) : text).replace(/\s*[\r\n]+\s*/g, " "), inner, ascii);
		const shown = this.changeShown();
		const name = this.view === "change" && shown ? `${TITLES.change} · ${shown.change}` : HEADINGS[this.view] ?? TITLES[this.view];
		const title = ascii ? asciiText(name) : name;
		// G's detail card (its Customization profiles card): the Commands card, `<title> … esc` in the
		// accent, a blank row, the body, a blank row and the keys muted, joined by ` · `.
		// Full screen, G's profiles view: tab, rule, title, the body, G's hints `<key> <action>` dim.
		const frame = (body: string[], keys: KeyHint[], feedback?: string) => {
			const fitted = body.map(fitRow).slice(0, room);
			if (full) {
				return renderFullscreenView({
					theme: t, width, rows, tab: TITLES[this.view], title: name, body: fitted,
					hints: keys.map((hint) => "key" in hint ? `${hint.key} ${hint.action}` : `${hint[0]} ${hint[1]}`), ...(feedback ? { feedback: [feedback.replace(/\s*[\r\n]+\s*/g, " ")] } : {}), ascii,
				});
			}
			// As G's detail card: as tall as its body, never past the height.
			const head = cut(title, Math.max(1, inner - 4), ascii);
			const footer = keys.map(hintText).join(" · ");
			return renderBox([t.fg("accent", head + " ".repeat(Math.max(1, inner - visibleWidth(head) - 3)) + "esc"), "", ...fitted, "", t.fg("muted", fitRow(footer))], width, t, ascii);
		};
		const close: KeyHint = ["esc", "close"];
		const move = ascii ? "up/down" : "↑/↓";
		/** Rows scrolled to keep the row at `at` (and `after` more) in view. */
		const window = (lines: string[], at?: number, after = 0) => {
			if (at !== undefined) this.scroll = Math.max(Math.min(this.scroll, at), at + after + 1 - room, 0);
			this.scroll = Math.min(this.scroll, Math.max(0, lines.length - room));
			return lines.slice(this.scroll, this.scroll + room);
		};
		if (this.view === "grants") {
			const lines = this.grants.lines(t, inner, Date.now(), ascii);
			const keys = this.grants.keys().map(([key, action]) => [key === "↑↓" ? move : key ?? "", action ?? ""] as KeyHint);
			const confirming = this.grants.keys()[0]?.[1] === "confirm";
			return frame(window(lines, this.grants.cursorRow(), 1), confirming ? keys : [...keys, close]);
		}
		if (this.view === "profiles") {
			// The mockup (L694): the card's own tabs at the top of the frame, its body, the keys dim below.
			const body = Math.max(1, rows - 4);
			const lines = this.profiles.lines(t, inner, ascii);
			const editing = this.profiles.captures();
			const keys = this.profiles.keys().map(([key, action]) => `${key} ${action}`);
			const hints = (editing ? keys : [...keys, "esc close"]).join("  ");
			const at = this.profiles.cursorRow();
			this.scroll = Math.max(Math.min(this.scroll, at), at + 1 - body, 0);
			this.scroll = Math.min(this.scroll, Math.max(0, lines.length - body));
			const shown = [...lines.slice(this.scroll, this.scroll + body).map(fitRow), ...Array<string>(Math.max(0, body - lines.length)).fill(""), "", t.fg("dim", fitRow(hints))];
			const h = ascii ? "-" : "─", v = t.fg("border", ascii ? "|" : "│");
			const pad = (line: string) => line + " ".repeat(Math.max(0, inner - visibleWidth(line)));
			return [
				t.fg("border", `${ascii ? "+" : "╭"}${h.repeat(width - 2)}${ascii ? "+" : "╮"}`),
				...shown.map((line) => `${v} ${pad(line)} ${v}`),
				t.fg("border", `${ascii ? "+" : "╰"}${h.repeat(width - 2)}${ascii ? "+" : "╯"}`),
			];
		}
		// Loading and a failed read are state blocks: the word once, and only the keys that act (P7).
		if (this.change.error || !this.change.at) {
			const block = this.change.error
				? renderStateBlock({ width: inner, kind: "error", text: "Could not read the change", cause: this.change.error.replace(/\s*[\r\n]+\s*/g, " "), ascii, paint })
				: renderStateBlock({ width: inner, kind: "loading", text: "Reading the change…", ascii, paint });
			return frame(block.lines, this.change.error ? [["r", "retry"], close] : [close]);
		}
		// The header names the change: the card's own ◆ line would say it twice (L119).
		const lines = changeCardLines(this.change.status, t, inner).slice(shown ? 1 : 0);
		const run = shown ? this.tests.runLines(t) : [];
		if (run.length) lines.push(t.bold(t.fg("customMessageLabel", "Tests")), ...run);
		// Full screen, a note is G's feedback row; in a card, the body's last row.
		if (this.change.note && !full) lines.push("", t.fg("warning", this.change.note));
		const body = window(lines);
		const more: KeyHint[] = lines.length > room ? [[move, "scroll"]] : [];
		return frame(body, [...more, ...this.changeKeys(), ["r", "refresh"], close], full ? this.change.note : undefined);
	}

	invalidate(): void {}
}

/** A runtime's models seen in the profiles (agents.yaml's and the profiles' own), when the shell knows no others. */
export function modelsSeen(runtime: string, data: ProfilesData): string[] {
	const seen = data.profiles.flatMap((p) => p.agents.filter((a) => a.runtime === runtime).map((a) => a.model.replace(/@[^@]*$/, "")));
	return [...new Set(seen)].sort();
}

/**
 * pi's available models as `provider/id`, read from its model registry
 * (ctx.modelRegistry, through `read`, since a ctx may be stale when it is
 * asked); undefined when the registry cannot be read.
 */
export function registryModels(read: () => unknown): string[] | undefined {
	try {
		const available = (read() as { getAvailable?: () => unknown } | undefined)?.getAvailable?.();
		if (!Array.isArray(available)) return undefined;
		return available.flatMap((m) => typeof m?.provider === "string" && typeof m?.id === "string" ? [`${m.provider}/${m.id}`] : []);
	} catch {
		return undefined;
	}
}

/**
 * The models the panel offers for a runtime (T130): for pi, every model its
 * registry holds and every model a profile names (one from another machine,
 * or of a provider this session did not load, stays selectable), sorted, none
 * twice; for another runtime, or when the registry cannot be read, the
 * models its profiles name.
 */
export function modelsOffered(runtime: string, data: ProfilesData, read: () => unknown): string[] {
	if (runtime !== "pi") return modelsSeen(runtime, data);
	return [...new Set([...(registryModels(read) ?? []), ...modelsSeen(runtime, data)])].sort();
}
