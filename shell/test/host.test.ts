import "./isolated-env.ts";
import { test } from "node:test";
import assert from "node:assert/strict";
import * as host from "../lib/host.ts";
import { HOST_EVENT, HOST_QUERY_EVENT, askHosts, forgetHosts, hostShowsPath, isHostAnnouncement, listenForHosts } from "../lib/host.ts";

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

test("without a host the shell keeps its path", () => {
  forgetHosts();
  listenForHosts(bus());
  assert.equal(hostShowsPath(), false);
});

// T169 (L1438): the shell plays no sound, so nothing asks whether a host
// alerts; CONTRACT.md still names the field, so an announcement with it is
// well formed.
test("alerts is part of a well-formed announcement, and nothing reads it (T169)", () => {
  assert.equal("hostAlerts" in host, false);
  assert.equal(isHostAnnouncement({ name: "mux", alerts: true }), true);
  assert.equal(isHostAnnouncement({ name: "mux", alerts: "yes" }), false);
});

test("a host announced at any time is heard: showsPath as it says", () => {
  forgetHosts();
  const events = bus();
  listenForHosts(events);
  events.emit(HOST_EVENT, { name: "plain" });
  assert.equal(hostShowsPath(), false, "a host that says nothing changes nothing");
  events.emit(HOST_EVENT, { name: "mux", alerts: true, showsPath: true });
  assert.equal(hostShowsPath(), true);
  events.emit(HOST_EVENT, { name: "mux", alerts: false });
  assert.equal(hostShowsPath(), false, "the same host announcing again replaces what it said");
});

test("a malformed announcement is ignored", () => {
  forgetHosts();
  const events = bus();
  listenForHosts(events);
  for (const bad of [undefined, null, "mux", { alerts: true }, { name: "", alerts: true }, { name: 3, showsPath: true }, { name: "m", alerts: "yes", showsPath: true }]) events.emit(HOST_EVENT, bad);
  assert.equal(hostShowsPath(), false);
});

test("askHosts emits the query with an empty payload", () => {
  const events = bus();
  askHosts(events);
  assert.deepEqual(events.sent, [[HOST_QUERY_EVENT, {}]]);
});
