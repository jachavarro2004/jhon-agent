/**
 * jhon-shell's UI use is measured before any of it is pruned: every command
 * and shortcut an extension registers through counted(pi) appends a
 * `jhon-ui-use` entry naming it when it runs, outside the model's context,
 * which `jhon audit --all` counts.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { counted, UI_USE_ENTRY } from "../lib/ui-use.ts";

function fakePi() {
  const entries: Array<[string, unknown]> = [];
  const commands = new Map<string, any>();
  const shortcuts = new Map<string, any>();
  const pi = {
    appendEntry: (type: string, data: unknown) => entries.push([type, data]),
    registerCommand: (name: string, def: any) => commands.set(name, def),
    registerShortcut: (key: string, def: any) => shortcuts.set(key, def),
    registerTool: (_t: unknown) => {},
  };
  return { pi, entries, commands, shortcuts };
}

test("a command's or a shortcut's run is counted, and its handler still runs with its arguments", async () => {
  const { pi, entries, commands, shortcuts } = fakePi();
  const ran: unknown[] = [];
  const c = counted(pi);
  c.registerCommand("jhon:agents", { description: "d", handler: async (args: string, ctx: unknown) => { ran.push(args, ctx); return "r"; } });
  c.registerShortcut("ctrl+s", { description: "Show the stats", handler: async (ctx: unknown) => { ran.push(ctx); } });
  assert.equal(await commands.get("jhon:agents").handler("x", "ctx"), "r");
  await shortcuts.get("ctrl+s").handler("ctx2");
  assert.deepEqual(ran, ["x", "ctx", "ctx2"]);
  assert.deepEqual(entries, [[UI_USE_ENTRY, { feature: "/jhon:agents" }], [UI_USE_ENTRY, { feature: "key ctrl+s" }]]);
});

test("a failing appendEntry never breaks the feature", async () => {
  const { pi, commands } = fakePi();
  pi.appendEntry = () => { throw new Error("no session"); };
  let ran = false;
  counted(pi).registerCommand("jhon:status", { handler: async () => { ran = true; } });
  await commands.get("jhon:status").handler("", {});
  assert.equal(ran, true);
});

test("everything else is pi's own, run on pi itself", () => {
  class Api {
    #tools: string[] = [];
    registerTool(t: { name: string }) { this.#tools.push(t.name); }
    tools() { return this.#tools; }
  }
  const api = new Api();
  const c = counted(api as any);
  c.registerTool({ name: "inspect" });
  assert.deepEqual(api.tools(), ["inspect"], "a method keeping private state works through counted");
});
