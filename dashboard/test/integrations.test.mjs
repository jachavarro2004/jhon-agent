// Unit tests for Home's Integrations block (T58b O3, L1167): each
// integration of /api/integrations as a row; one that is off, or on but
// unreachable, reads its state and reason with empty numbers, never an
// error panel.
//
//   timeout 120 node --test dashboard/test/integrations.test.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import { integrationLine, integrationsCard } from "../app.js";

const row = (name, display, state, reason, numbers = null, required = false) => ({ name, display, required, state, reason, numbers });

test("an integration that is off reads off, its reason, and no numbers", () => {
  assert.deepEqual(integrationLine(row("context7", "Context7", "off", "integrations.context7 = false")),
    { name: "Context7", state: "off", reason: "integrations.context7 = false", numbers: "—" });
  const html = integrationsCard([row("context7", "Context7", "off", "integrations.context7 = false")]);
  assert.match(html, /Context7/);
  assert.match(html, /data-state="off"/);
  assert.match(html, /integrations\.context7 = false/);
  assert.doesNotMatch(html, /role="alert"/);
});

test("an integration on but unreachable reads unavailable, its reason, and no numbers", () => {
  assert.deepEqual(integrationLine(row("context7", "Context7", "unavailable", "https://mcp.context7.com/mcp: HTTP 503")),
    { name: "Context7", state: "unavailable", reason: "https://mcp.context7.com/mcp: HTTP 503", numbers: "—" });
  const html = integrationsCard([row("engram", "Engram", "unavailable", "Engram does not answer: /stats answered 500", null, true)]);
  assert.match(html, /data-state="unavailable"/);
  assert.match(html, /Engram does not answer: \/stats answered 500/);
  assert.doesNotMatch(html, /role="alert"/);
});

test("an integration on with numbers reads them", () => {
  assert.deepEqual(integrationLine(row("engram", "Engram", "on", "", { observations: 12 }, true)),
    { name: "Engram", state: "on", reason: "", numbers: "12 observations" });
});

test("the block never breaks: not loaded yet, or the route failing, is a line, not an error panel", () => {
  assert.match(integrationsCard(undefined), /Reading the integrations/);
  const failed = integrationsCard(undefined, "502: boom");
  assert.match(failed, /could not read them: 502: boom/);
  assert.doesNotMatch(failed, /role="alert"/);
  assert.match(integrationsCard([]), /No integration/);
  // A reason is text, never markup.
  assert.match(integrationsCard([row("x", "<b>X</b>", "off", "<i>why</i>")]), /&#60;i&#62;why&#60;\/i&#62;/);
});
