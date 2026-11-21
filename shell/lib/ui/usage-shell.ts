/**
 * shell/lib/ui/usage-shell.ts — the usage in pi (T174-S5b, L1493-L1495):
 * every reply's windows recorded (after_provider_response, and the Codex
 * WebSocket's provider_stream_event), the sources other extensions register
 * on pi.events, a forced refresh at a session's start and a 5-minute one at
 * each agent end, /jhon:usage and its key (shell.keys.usage, alt+u) opening
 * the overlay. extensions/ui.ts installs it and asks it for the status bar's
 * windows.
 *
 * The overlay's providers are the session's and its sub-agents': a session on
 * a profile of its own (T149, lib/session-profile.ts) runs them on that
 * profile's models, so the scope reads them as extensions/jhon.ts launches
 * them; a refresh waits for the profile's rows.
 */
import { execFile } from "node:child_process";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { TUI } from "@earendil-works/pi-tui";
import { knownAgents, resolveAgent } from "../agents.ts";
import { SessionProfile, type ProfilesJson } from "../session-profile.ts";
import type { ShellSettings } from "../settings.ts";
import { CARD_OVERLAY, closeWithRepaint } from "./overlay.ts";
import { barWindows, usageFromCodexEvent, usageFromHeaders, usageScope, usageTimeoutMs, UsageTracker, USAGE_SOURCE_EVENT, type BarWindow } from "./usage.ts";
import { UsageOverlay } from "./usage-view.ts";

export interface UsageShellDeps {
	settings: ShellSettings;
	now(): number;
	fetch?: typeof fetch;
	/** The session the shell runs now. */
	current(): ExtensionContext | undefined;
	/** A provider settled: the bar and an open overlay draw again. */
	repaint(): void;
	/** The session's own profile (T120), undefined while it follows the default. */
	sessionProfile?(): string | undefined;
	/** `jhon profile json` for cwd; JHON_BIN's by default. */
	profilesJson?(cwd: string): Promise<ProfilesJson>;
}

/** `jhon profile json`, read without blocking pi. */
function readProfilesJson(env: NodeJS.ProcessEnv): (cwd: string) => Promise<ProfilesJson> {
	return (cwd) => new Promise((resolve, reject) =>
		execFile(env.JHON_BIN || "jhon", ["profile", "json"], { cwd, encoding: "utf8", maxBuffer: 8 << 20, timeout: 10_000 }, (error, stdout) => {
			if (error) return reject(error);
			try { resolve(JSON.parse(stdout) as ProfilesJson); } catch (cause) { reject(cause); }
		}));
}

export interface UsageShell {
	/** The status bar's windows for the session's model. */
	bar(ctx: ExtensionContext): BarWindow[];
	tracker: UsageTracker;
}

/** The overlay's size, as Customize's. */
const OVERLAY = CARD_OVERLAY;

export function installUsage(pi: ExtensionAPI, env: NodeJS.ProcessEnv, deps: UsageShellDeps): UsageShell {
	let repaintOverlay: (() => void) | undefined;
	const tracker = new UsageTracker({
		now: deps.now,
		fetch: deps.fetch ?? ((input, init) => globalThis.fetch(input, init)),
		timeoutMs: usageTimeoutMs(env),
		apiKey: async (provider) => deps.current()?.modelRegistry.getApiKeyForProvider(provider),
		onSettled: () => { deps.repaint(); repaintOverlay?.(); },
	});
	// The models Jhon's sub-agents run on (the agent dir `jhon start pi` sets),
	// read when a refresh or the overlay starts, never during a frame.
	const agentDir = env.PI_CODING_AGENT_DIR;
	const routing = new SessionProfile(deps.profilesJson ?? readProfilesJson(env));
	let routed: string | undefined;
	let routingRead: Promise<unknown> = Promise.resolve();
	// The session's profile moved: its rows are read once, and every refresh after waits for them.
	const follow = (ctx: ExtensionContext): Promise<unknown> => {
		const name = deps.sessionProfile?.() || undefined;
		if (name === routed) return routingRead;
		routed = name;
		routing.start(name);
		routingRead = agentDir && name ? routing.refresh(ctx.cwd).catch(() => undefined) : Promise.resolve();
		return routingRead;
	};
	const subAgentModels = (): Array<string | undefined> => agentDir ? knownAgents(agentDir).map((name) => {
		try { return routing.agent(resolveAgent(agentDir, name)).model; } catch { return undefined; }
	}) : [];
	const bare = (ctx: ExtensionContext) => (modelId: string): string | undefined => {
		try {
			const providers = new Set(ctx.modelRegistry.getAll().filter((model) => model.id === modelId).map((model) => model.provider));
			return providers.size === 1 ? [...providers][0] : undefined;
		} catch { return undefined; }
	};
	const scopeOf = (ctx: ExtensionContext) => usageScope(ctx.model?.provider, subAgentModels(), bare(ctx));
	/** The scope once the session's profile rows are in. */
	const freshScope = async (ctx: ExtensionContext) => { await follow(ctx); return scopeOf(ctx); };

	pi.events.on(USAGE_SOURCE_EVENT, (payload) => {
		const source = tracker.register(payload);
		const ctx = deps.current();
		// A source for a provider in scope is fetched at once, and only it.
		if (source && ctx && scopeOf(ctx).includes(source.provider)) void tracker.refresh([source.provider], true);
	});
	pi.on("after_provider_response", (event, ctx) => { tracker.record(usageFromHeaders(event.headers, deps.now(), ctx?.model?.provider)); });
	pi.on("provider_stream_event", (event, ctx) => { tracker.record(usageFromCodexEvent(event.data, deps.now(), event.provider ?? ctx?.model?.provider)); });
	pi.on("session_start", (_event, ctx) => { void freshScope(ctx).then((scope) => tracker.refresh(scope, true)); });
	pi.on("agent_end", (_event, ctx) => { void freshScope(ctx).then((scope) => tracker.refresh(scope, false)); });

	// It opens at once on what the tracker holds and repaints as the forced refresh settles.
	const open = (ctx: ExtensionContext) => ctx.ui.custom<null>((tui: TUI, theme, _keys, done) => {
		let scope = scopeOf(ctx);
		const close = closeWithRepaint(tui, done);
		const view = new UsageOverlay({
			tracker, theme, now: deps.now,
			scope: () => scope,
			active: () => ctx.model?.provider,
			refresh: async () => { scope = await freshScope(ctx); await tracker.refresh(scope, true); },
			close: () => { repaintOverlay = undefined; close(null); },
			requestRender: () => tui.requestRender(),
			ascii: deps.settings.ascii(),
		});
		repaintOverlay = () => tui.requestRender();
		view.refresh();
		return { render: (width: number) => view.render(width), handleInput: (data: string) => view.handleInput(data), handleMouse: (event) => view.handleMouse(event), invalidate: () => view.invalidate() };
	}, OVERLAY);
	pi.registerCommand("jhon:usage", {
		description: "Each provider's subscription windows and resets; r refreshes",
		handler: async (_args, ctx) => { await open(ctx); },
	});
	const key = deps.settings.key("usage");
	if (key) pi.registerShortcut(key as Parameters<ExtensionAPI["registerShortcut"]>[0], { description: "Shows each provider's subscription usage", handler: async (ctx) => { await open(ctx); } });
	return { tracker, bar: (ctx) => barWindows(tracker.get(ctx.model?.provider), ctx.model?.id) };
}
