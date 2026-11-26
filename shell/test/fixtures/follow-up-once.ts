// An extension that, at the first settle, sends the agent "again" as a
// follow-up user message (test/print-mode.test.ts): pi defers a prompt sent
// from agent_settled and runs it before the session counts as idle.
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export default function followUpOnce(pi: ExtensionAPI): void {
	let sent = false;
	pi.on("agent_settled", () => {
		if (sent) return;
		sent = true;
		pi.sendUserMessage("again");
	});
}
