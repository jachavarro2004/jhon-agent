// An extension whose input handler takes every message another extension
// sends (source "extension"), so that prompt starts no run
// (test/print-mode.test.ts); the owner's own prompts pass.
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export default function takeExtensionInput(pi: ExtensionAPI): void {
	pi.on("input", (event) => (event.source === "extension" ? { action: "handled" } : undefined));
}
