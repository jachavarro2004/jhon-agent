import "./isolated-env.ts";
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { recordEvent, telemetryLine } from "../lib/telemetry.ts";
import { subagentAttributes, type AgentDef } from "../lib/agents.ts";
import { OWNER_SETTINGS, useMemorySettings } from "./fake-jhon-settings.ts";

test("an event is one line in the engine's format, in its UTC day's file", (t) => {
	useMemorySettings(t, OWNER_SETTINGS);
	const home = mkdtempSync(join(tmpdir(), "jhon-telemetry-"));
	const at = new Date("2026-10-30T23:59:59.500Z");
	recordEvent("jhon.compaction", { "jhon.context.percent": 61, "jhon.compact_at": 60 }, "INFO", { HOME: home }, at);
	recordEvent("jhon.subagent", { "jhon.outcome": "context-cap" }, "WARN", { HOME: home }, at);
	assert.equal(process.env.JHON_TELEMETRY, "off", "the tests' own runs record nothing");
	const lines = readFileSync(join(home, ".local/share/jhon/telemetry/2026-10-30.jsonl"), "utf8").split("\n");
	assert.deepEqual(lines.slice(0, 2).map((l) => JSON.parse(l)), [
		{ time: "2026-10-30T23:59:59.500Z", name: "jhon.compaction", severity: "INFO", attributes: { "jhon.context.percent": 61, "jhon.compact_at": 60 } },
		{ time: "2026-10-30T23:59:59.500Z", name: "jhon.subagent", severity: "WARN", attributes: { "jhon.outcome": "context-cap" } },
	]);
	assert.equal(lines[2], "");
	assert.equal(telemetryLine("x", {}, "INFO", at), '{"time":"2026-10-30T23:59:59.500Z","name":"x","severity":"INFO","attributes":{}}\n');
});

test("JHON_TELEMETRY=off records nothing, and an unwritable home fails nothing", (t) => {
	useMemorySettings(t, OWNER_SETTINGS);
	const home = mkdtempSync(join(tmpdir(), "jhon-telemetry-"));
	recordEvent("jhon.x", {}, "INFO", { HOME: home, JHON_TELEMETRY: "off" });
	assert.throws(() => readFileSync(join(home, ".local/share/jhon/telemetry", `${new Date().toISOString().slice(0, 10)}.jsonl`)));
	recordEvent("jhon.x", {}, "INFO", { HOME: "/proc/no-such-home" });
});

test("telemetry is opt-in (T55): with integrations.telemetry off, the default, nothing is recorded", (t) => {
	for (const file of [{}, { ...OWNER_SETTINGS, "integrations.telemetry": false }]) {
		useMemorySettings(t, file);
		const home = mkdtempSync(join(tmpdir(), "jhon-telemetry-"));
		recordEvent("jhon.compaction", { "jhon.context.percent": 61 }, "INFO", { HOME: home });
		assert.equal(existsSync(join(home, ".local")), false, JSON.stringify(file));
	}
});

test("a sub-agent's event: its name, how it ended, how long, its turns; no turn cap, whatever its definition carries", () => {
	const agent = { name: "worker", maxTurns: 40 } as unknown as AgentDef;
	assert.deepEqual(subagentAttributes(agent, "completed", 1200, 90, "s1"),
		{ "gen_ai.agent.name": "worker", "jhon.outcome": "completed", "jhon.duration_ms": 1200, "jhon.turns": 90, "session.id": "s1" });
	assert.deepEqual(subagentAttributes({ name: "explore" } as AgentDef, "context-cap", 5), { "gen_ai.agent.name": "explore", "jhon.outcome": "context-cap", "jhon.duration_ms": 5 });
});
