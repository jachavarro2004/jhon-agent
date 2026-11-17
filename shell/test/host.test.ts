import "./isolated-env.ts";
import { test } from "node:test";
import assert from "node:assert/strict";
import { HOST_EVENT, HOST_QUERY_EVENT, askHosts, forgetHosts, hostAlerts, hostShowsPath, listenForHosts } from "../lib/host.ts";

function bus() {
  const listeners = new Map<string, Set<(data: unknown) => void>>();
  const sent: [string, unknown][] = [];
  return {
    sent,
    on(name: string, fn: (data: unknown) => void) {
      if (!listeners.has(name)) listeners.set(name, new Set());
      listeners.get(name)!.add(fn);
      return () => { listeners.get(name)!.delete(fn); };
    },
    emit(name: string, data: unknown) { sent.push([name, data]); for (const fn of [...listeners.get(name) ?? []]) fn(data); },
  };
}

test("the neutral names are the contract's", () => {
  assert.equal(HOST_EVENT, "jhon:host");
  assert.equal(HOST_QUERY_EVENT, "jhon:host?");
});

test("without a host the shell keeps its own sound and its path", () => {
  forgetHosts();
  listenForHosts(bus());
  assert.equal(hostAlerts(), false);
  assert.equal(hostShowsPath(), false);
});

test("a host announced at any time is heard: alerts and showsPath as it says", () => {
  forgetHosts();
  const events = bus();
  listenForHosts(events);
  events.emit(HOST_EVENT, { name: "plain" });
  assert.equal(hostAlerts(), false, "a host that says nothing changes nothing");
  assert.equal(hostShowsPath(), false);
  events.emit(HOST_EVENT, { name: "mux", alerts: true, showsPath: true });
  assert.equal(hostAlerts(), true);
  assert.equal(hostShowsPath(), true);
  events.emit(HOST_EVENT, { name: "mux", alerts: false });
  assert.equal(hostAlerts(), false, "the same host announcing again replaces what it said");
  assert.equal(hostShowsPath(), false);
});

test("a malformed announcement is ignored", () => {
  forgetHosts();
  const events = bus();
  listenForHosts(events);
  for (const bad of [undefined, null, "mux", { alerts: true }, { name: "", alerts: true }, { name: 3, showsPath: true }, { name: "m", alerts: "yes" }]) events.emit(HOST_EVENT, bad);
  assert.equal(hostAlerts(), false);
  assert.equal(hostShowsPath(), false);
});

test("askHosts emits the query with an empty payload", () => {
  const events = bus();
  askHosts(events);
  assert.deepEqual(events.sent, [[HOST_QUERY_EVENT, {}]]);
});
