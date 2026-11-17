// The sandbox wiring alone, as an extension a real pi session loads
// (test/sandbox.test.ts): the binary is the test's fake jhon.
import { wireSandbox } from "../../lib/sandbox.ts";

export default function sandboxOnly(pi: any): void {
	wireSandbox(pi, { jhon: process.env.JHON_TEST_FAKE_JHON });
}
