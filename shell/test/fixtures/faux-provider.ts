// pi's own offline model (pi-ai's faux provider), as an extension a real pi
// loads with -e (test/print-mode.test.ts): provider "faux", model "faux-1".
// Its turns answer, in order, FAUX_ANSWERS (comma-separated; the last one
// repeats), else FAUX_ANSWER every time (default "ok"); FAUX_DELAY_MS, when
// set, holds each answer back that long before its first token. No network,
// no login.
import { fauxAssistantMessage, fauxProvider } from "@earendil-works/pi-ai";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export default function fauxOnly(pi: ExtensionAPI): void {
	const faux = fauxProvider({ provider: "faux", models: [{ id: "faux-1" }] });
	const listed = process.env.FAUX_ANSWERS ? process.env.FAUX_ANSWERS.split(",") : [];
	const answers = listed.length ? listed : [process.env.FAUX_ANSWER || "ok"];
	const delay = Number(process.env.FAUX_DELAY_MS) || 0;
	const answer = (text: string) => async () => {
		if (delay > 0) await new Promise((resolve) => setTimeout(resolve, delay));
		return fauxAssistantMessage(text);
	};
	const last = answers[answers.length - 1]!;
	faux.setResponses([...answers, ...Array.from({ length: 20 }, () => last)].map(answer));
	pi.registerProvider(faux.provider as never);
}
