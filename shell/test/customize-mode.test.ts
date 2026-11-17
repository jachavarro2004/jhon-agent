/**
 * L175 (6): the permission mode is a row of Customize, as G keeps its
 * permissions among its Customization rows, the only place the mode changes
 * (L214), through changeMode (lib/ui/jhon-panel-grants.ts): `jhon mode
 * <mode> --root <root>` on the owner's step, super-yolo only after the
 * owner's Yes; its note names /jhon:grants, where the grants are.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { changeMode, SUPER_YOLO_QUESTION, type ModeData } from "../lib/ui/jhon-panel-grants.ts";
import { customizeRows as settingsRows } from "../lib/ui/customize/registry.ts";
import { memorySettings } from "./fake-jhon-settings.ts";

const root = "/p/acme";
const at = (mode: string): ModeData => ({ mode, root, since: null });

test("changeMode: the mode on already runs nothing; yolo and normal at once; super-yolo only after the owner's Yes, a refused or failed question runs nothing", async () => {
	const ran: string[] = [];
	const asked: string[] = [];
	let answer: boolean | Error = false;
	const deps = {
		setMode: async (mode: string, where: string) => { ran.push(`${mode} ${where}`); },
		confirmMode: async (question: string) => { asked.push(question); if (answer instanceof Error) throw answer; return answer; },
	};
	assert.equal(await changeMode("normal", deps, at("normal")), "same");
	assert.equal(await changeMode("yolo", deps, at("normal")), "set");
	assert.equal(await changeMode("super-yolo", deps, at("yolo")), "declined");
	answer = new Error("no UI");
	assert.equal(await changeMode("super-yolo", deps, at("yolo")), "declined");
	assert.equal(await changeMode("super-yolo", { setMode: deps.setMode }, at("yolo")), "declined", "no one to ask: nothing runs");
	answer = true;
	assert.equal(await changeMode("super-yolo", deps, at("yolo")), "set");
	assert.equal(await changeMode("normal", deps, at("super-yolo")), "set");
	assert.deepEqual(ran, [`yolo ${root}`, `super-yolo ${root}`, `normal ${root}`]);
	assert.deepEqual(asked, [SUPER_YOLO_QUESTION, SUPER_YOLO_QUESTION, SUPER_YOLO_QUESTION]);
	await assert.rejects(changeMode("yolo", { setMode: async () => { throw new Error("refused"); } }, at("normal")), /refused/);
	await assert.rejects(changeMode("yolo", deps, at("normal") && { mode: "normal", root: "", since: null }), /not in a project/);
});

test("Customize has a Permissions row for the mode when the shell can read it: radios normal, yolo, super yolo, its note naming /jhon:grants", async () => {
	const noop = () => {};
	const hooks = { settings: memorySettings(), animations: noop, doubleEsc: noop, status: noop, redraw: noop };
	assert.ok(!settingsRows(hooks).some((row) => row.group === "Permissions"), "without the mode's hook, no row");
	const set: string[] = [];
	const rows = settingsRows({ ...hooks, mode: { value: () => "yolo", set: async (mode) => { set.push(mode); return undefined; } } });
	const row = rows.find((candidate) => candidate.group === "Permissions")!;
	assert.equal(row.label, "Permission mode");
	assert.deepEqual(row.options, ["normal", "yolo", "super-yolo"]);
	assert.deepEqual(row.options!.map((option) => row.display!(option)), ["normal", "yolo", "super yolo"]);
	assert.equal(row.value(), "yolo");
	assert.match(row.note!, /\/jhon:grants/);
	assert.match(row.preview!("super-yolo", false).lines.join(" "), /nothing asks, destroy included/);
	assert.equal(await row.apply!("super-yolo"), undefined);
	assert.deepEqual(set, ["super-yolo"]);
});
