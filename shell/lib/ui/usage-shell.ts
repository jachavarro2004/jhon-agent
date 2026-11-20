/**
 * shell/lib/ui/usage-shell.ts — the usage in pi (T174-S5b, L1493-L1495):
 * every reply's windows recorded (after_provider_response, and the Codex
 * WebSocket's provider_stream_event), the sources other extensions register
 * on pi.events, a forced refresh at a session's start and a 5-minute one at
 * each agent end, /jhon:usage and its key (shell.keys.usage, alt+u) opening
 * the overlay. extensions/ui.ts installs it and asks it for the status bar's
 * windows.
 */
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { TUI } from "@earendil-works/pi-tui";
import { knownAgents, resolveAgent } from "../agents.ts";
import type { ShellSettings } from "../settings.ts";
import { withOverlayRepaint } from "./overlay-repaint.ts";
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
}

export interface UsageShell {
	/** The status bar's windows for the session's model. */
	bar(ctx: ExtensionContext): BarWindow[];
	tracker: UsageTracker;
}

/** The overlay's size, as Customize's. */
const OVERLAY = { overlay: true, overlayOptions: { anchor: "center", width: "70%", minWidth: 60, maxHeight: "85%" } } as const;

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
	const subAgentModels = (): Array<string | undefined> => agentDir ? knownAgents(agentDir).map((name) => {
		try { return resolveAgent(agentDir, name).model; } catch { return undefined; }
	}) : [];
	const bare = (ctx: ExtensionContext) => (modelId: string): string | undefined => {
		try {
			const providers = new Set(ctx.modelRegistry.getAll().filter((model) => model.id === modelId).map((model) => model.provider));
			return providers.size === 1 ? [...providers][0] : undefined;
		} catch { return undefined; }
	};
	const scopeOf = (ctx: ExtensionContext) => usageScope(ctx.model?.provider, subAgentModels(), bare(ctx));

	pi.events.on(USAGE_SOURCE_EVENT, (payload) => {
		const source = tracker.register(payload);
		const ctx = deps.current();
		// A source for a provider in scope is fetched at once, and only it.
		if (source && ctx && scopeOf(ctx).includes(source.provider)) void tracker.refresh([source.provider], true);
	});
	pi.on("after_provider_response", (event, ctx) => { tracker.record(usageFromHeaders(event.headers, deps.now(), ctx?.model?.provider)); });
	pi.on("provider_stream_event", (event, ctx) => { tracker.record(usageFromCodexEvent(event.data, deps.now(), event.provider ?? ctx?.model?.provider)); });
	pi.on("session_start", (_event, ctx) => { void tracker.refresh(scopeOf(ctx), true); });
	pi.on("agent_end", (_event, ctx) => { void tracker.refresh(scopeOf(ctx), false); });

	// It opens at once on what the tracker holds and repaints as the forced refresh settles.
	const open = (ctx: ExtensionContext) => ctx.ui.custom<null>((tui: TUI, theme, _keys, done) => {
		let scope = scopeOf(ctx);
		const close = withOverlayRepaint(tui, done);
		const view = new UsageOverlay({
			tracker, theme, now: deps.now,
			scope: () => scope,
			active: () => ctx.model?.provider,
			refresh: async () => { scope = scopeOf(ctx); await tracker.refresh(scope, true); },
			close: () => { repaintOverlay = undefined; close(null); },
			requestRender: () => tui.requestRender(),
			ascii: deps.settings.ascii(),
		});
		repaintOverlay = () => tui.requestRender();
		view.refresh();
		return { render: (width: number) => view.render(width), handleInput: (data: string) => view.handleInput(data), invalidate: () => view.invalidate() };
	}, OVERLAY);
	pi.registerCommand("jhon:usage", {
		description: "Each provider's subscription windows and resets; r refreshes",
		handler: async (_args, ctx) => { await open(ctx); },
	});
	const key = deps.settings.key("usage");
	if (key) pi.registerShortcut(key as Parameters<ExtensionAPI["registerShortcut"]>[0], { description: "Shows each provider's subscription usage", handler: async (ctx) => { await open(ctx); } });
	return { tracker, bar: (ctx) => barWindows(tracker.get(ctx.model?.provider), ctx.model?.id) };
}
