// pi's own offline model (pi-ai's faux provider), as an extension a real pi
// loads with -e (test/print-mode.test.ts): provider "faux", model "faux-1",
// and every turn answers FAUX_ANSWER (default "ok"). No network, no login.
import { fauxAssistantMessage, fauxProvider } from "@earendil-works/pi-ai";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export default function fauxOnly(pi: ExtensionAPI): void {
	const faux = fauxProvider({ provider: "faux", models: [{ id: "faux-1" }] });
	const answer = process.env.FAUX_ANSWER || "ok";
	faux.setResponses(Array.from({ length: 20 }, () => fauxAssistantMessage(answer)));
	pi.registerProvider(faux.provider as never);
}
