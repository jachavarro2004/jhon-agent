/**
 * T115f: a sub-agent child starts from a lean base. Through the public
 * surface (childArgs, and runAgent spawning a fake pi that records its argv):
 * no skills catalog, no raw context files, no orchestrator-only prose, no
 * memory protocol for a child without memory tools, the parent's exact skill
 * paths, and a base under a pinned byte budget, fixed for the whole run.
 *
 *   node --experimental-strip-types --test shell/test/child-context.test.ts
 */

import { test, after } from "node:test";
import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { childArgs, parseAgent, runAgent } from "../lib/agents.ts";

const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
function tempDir(prefix: string): string {
  const d = realpathSync(mkdtempSync(join(tmpdir(), prefix)));
  temps.push(d);
  return d;
}

/** The always-on prose a rendered child body carries (shape of ~/.pi/jhon/agents/verify.md). */
const PROSE = `# Jhon

Agent framework built on LDD.

## Identity and conversation

You report to the agent that launched you.

## Engram

Engram memory is active. **Saving is not replying.** Save before writing the final answer.

## Strict TDD

RED first, then GREEN.

## Routing

Investigation requests are read-only unless the user asks. Ask the owner every open point.
Delegate when it pays. The owner's approval of a PR approves its merge too.

### Delivery

Push only when the owner says so.

## Runtime guarantees

5 enforced.`;

const agent = (tools: string[]) => parseAgent(`---
name: verify
description: "Independent verifier"
model: a/b
tools:
${tools.map((t) => `  - ${t}`).join("\n")}
---
You are the independent verifier.

\`\`\`md
## Routing
(a fenced example stays)
\`\`\`

${PROSE}
`, "verify");

const NO_MEMORY = agent(["read", "grep", "bash"]);
const WITH_MEMORY = agent(["read", "bash", "mcp__engram__mem_search"]);

/** Every --append-system-prompt value, joined: what the child's system prompt gets from Jhon. */
const appended = (args: string[]) => args.flatMap((a, i) => (args[i - 1] === "--append-system-prompt" ? [a] : [])).join("\n\n");
const before = (args: string[]) => args.slice(0, args.indexOf("--"));

test("a child runs with no skills catalog and no raw context files", () => {
  const args = before(childArgs(NO_MEMORY, "check it"));
  assert.ok(args.includes("--no-skills"), "pi's skills catalog is off");
  assert.ok(args.includes("--no-context-files"), "pi's own AGENTS.md/CLAUDE.md loading is off");
  assert.ok(!args.includes("--skill"), "no skill path the parent did not pass");
});

test("a child's prompt carries no orchestrator-only section; a fenced example and the shared rules stay", () => {
  const prompt = appended(childArgs(WITH_MEMORY, "check it"));
  assert.doesNotMatch(prompt, /## Routing\n\nInvestigation/);
  assert.doesNotMatch(prompt, /Ask the owner every open point|approves its merge|Push only when the owner/);
  assert.match(prompt, /```md\n## Routing\n\(a fenced example stays\)\n```/);
  assert.match(prompt, /## Strict TDD\n\nRED first, then GREEN\./);
  assert.match(prompt, /## Runtime guarantees\n\n5 enforced\./);
});

test("a child without memory tools gets no memory protocol; one with them keeps it", () => {
  assert.doesNotMatch(appended(childArgs(NO_MEMORY, "check it")), /Engram|Saving is not replying/);
  assert.match(appended(childArgs(WITH_MEMORY, "check it")), /## Engram\n\nEngram memory is active/);
});

test("the skill paths the parent resolved reach the child, each as --skill, in order", () => {
  const args = before(childArgs(NO_MEMORY, "check it", null, "print", { skills: ["/s/ldd/SKILL.md", "/s/go-testing"] }));
  const skills = args.flatMap((a, i) => (args[i - 1] === "--skill" ? [a] : []));
  assert.deepEqual(skills, ["/s/ldd/SKILL.md", "/s/go-testing"]);
  assert.ok(args.includes("--no-skills"), "explicit paths load even with discovery off");
});

/** A fake pi that records its argv. */
function fakePi(): { bin: string; log: string } {
  const dir = tempDir("jhon-pi-fake-");
  const bin = join(dir, "pi"), log = join(dir, "log.json");
  writeFileSync(bin, `#!/usr/bin/env node
require("node:fs").writeFileSync(${JSON.stringify(log)}, JSON.stringify(process.argv.slice(2)));
process.stdout.write("ok\\n");
`);
  chmodSync(bin, 0o755);
  return { bin, log };
}

async function spawnArgs(cwd: string, agentDir: string, skills?: string[]): Promise<string[]> {
  const { bin, log } = fakePi();
  const saved = { bin: process.env.JHON_PI_BIN, dir: process.env.PI_CODING_AGENT_DIR };
  process.env.JHON_PI_BIN = bin;
  process.env.PI_CODING_AGENT_DIR = agentDir;
  try {
    const r = await runAgent(NO_MEMORY, "check it", cwd, { skills });
    assert.equal(r.ok, true, r.text);
  } finally {
    if (saved.bin === undefined) delete process.env.JHON_PI_BIN; else process.env.JHON_PI_BIN = saved.bin;
    if (saved.dir === undefined) delete process.env.PI_CODING_AGENT_DIR; else process.env.PI_CODING_AGENT_DIR = saved.dir;
  }
  return JSON.parse(readFileSync(log, "utf8"));
}

const AGENTS_MD = `# Project

Build with \`go build\`.

<!-- orchestrator-only -->
Plan the next slice with the owner before delegating it.
<!-- /orchestrator-only -->

## Routing

Hand each worker a brief.

## Engram Persistent Memory

Call mem_save immediately after any decision.

## Testing

Run \`go test ./...\`.
`;

test("a spawned child gets the project's context files filtered, the no-re-read rule, and the parent's skill paths", async () => {
  const cwd = tempDir("jhon-child-cwd-"), agentDir = tempDir("jhon-child-agent-");
  writeFileSync(join(cwd, "AGENTS.md"), AGENTS_MD);
  const args = await spawnArgs(cwd, agentDir, ["/s/ldd/SKILL.md"]);
  const prompt = appended(args);
  assert.match(prompt, new RegExp(`<project_instructions path="${join(cwd, "AGENTS.md")}">`));
  assert.match(prompt, /Build with `go build`\./);
  assert.match(prompt, /Run `go test \.\/\.\.\.`\./);
  assert.doesNotMatch(prompt, /Plan the next slice|Hand each worker|mem_save immediately/);
  assert.match(prompt, /Do not re-read a skill file, spec section, or other file you already read in this session/);
  assert.deepEqual(before(args).filter((a, i, all) => all[i - 1] === "--skill"), ["/s/ldd/SKILL.md"]);
  assert.ok(before(args).includes("--no-context-files"));
});

test("the child's base is byte-stable across spawns and under its pinned budget", async () => {
  const cwd = tempDir("jhon-child-cwd-"), agentDir = tempDir("jhon-child-agent-");
  writeFileSync(join(cwd, "AGENTS.md"), AGENTS_MD);
  const one = await spawnArgs(cwd, agentDir), two = await spawnArgs(cwd, agentDir);
  assert.equal(appended(one), appended(two), "same inputs, same system prompt bytes");
  // What the child's system prompt carries from Jhon and the project: the
  // appended text, plus the context files pi loads itself unless told not to.
  const piLoaded = before(one).includes("--no-context-files") ? 0 : Buffer.byteLength(AGENTS_MD);
  const lean = Buffer.byteLength(appended(one)) + piLoaded;
  const full = Buffer.byteLength(NO_MEMORY.body) + Buffer.byteLength(AGENTS_MD);
  console.log(`child base: ${lean} bytes (unfiltered ${full})`);
  assert.ok(lean <= 700, `the child's base is ${lean} bytes, over its 700-byte budget`);
});
