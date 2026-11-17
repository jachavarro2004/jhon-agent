/**
 * The command palette's Jhon entries (lib/ui/command-palette-catalog.ts,
 * S30, L113): the LDD status, the grants, and the dashboard.
 *
 *   jhon test
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { buildCommandPaletteGroups } from "../lib/ui/command-palette-catalog.ts";
import { CommandPalette, type CommandPaletteResult } from "../lib/ui/command-palette.ts";

test("the palette's Jhon rows sit in G's groups: the grants in Session, the change's detail and the dashboard in Diagnostics; the old tabs have no rows (S30, L113, T15.12e)", () => {
	const registered = [
		{ name: "jhon:status", description: "The LDD detail" },
		{ name: "jhon:grants", description: "The permission mode and grants" },
		{ name: "jhon:dashboard", description: "Start jhon dashboard" },
	];
	const groups = buildCommandPaletteGroups(registered, {});
	const rows = (title: string) => groups.find((group) => group.title === title)!.items.map((item) => [item.label, item.command, item.shortcut]);
	assert.deepEqual(rows("Session"), [["Grants", "jhon:grants", undefined]]);
	assert.deepEqual(rows("Diagnostics"), [
		["LDD status", "jhon:status", undefined],
		["Dashboard", "jhon:dashboard", undefined],
	]);

	// Choosing a row runs it as typed: /jhon:grants opens the grants.
	const results: CommandPaletteResult[] = [];
	const palette = new CommandPalette(groups, (result) => results.push(result));
	for (const ch of "Grants") palette.handleInput(ch);
	palette.handleInput("\r");
	assert.deepEqual(results, [{ type: "run", name: "jhon:grants" }]);

	assert.deepEqual(buildCommandPaletteGroups([{ name: "jhon:dashboard" }], {}).map((group) => group.title), ["Diagnostics"], "without the views (a child), only the dashboard");
});
