/**
 * T15.18 (L197, L198): Customize → Appearance has a Theme row, as G's: the
 * pi themes Jhon registers (rose ink, written in the agent dir by `jhon
 * palette`, and every JSON in shell/themes/, which package.json's pi.themes
 * hands to pi), the current one marked "(current)", a preview of its accent
 * and text read from the theme's own file, applied with pi's setTheme (pi
 * saves it). Adding a theme is one JSON file in shell/themes/.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { customizeRows } from "../lib/ui/customize/registry.ts";
import { jhonThemes, PACKAGED_THEMES, themePreview } from "../lib/ui/customize/themes.ts";
import { memorySettings } from "./fake-jhon-settings.ts";

const noop = () => {};
const hooks = { settings: memorySettings(), animations: noop, doubleEsc: noop, status: noop, redraw: noop };
const shell = join(dirname(fileURLToPath(import.meta.url)), "..");

test("the packaged themes are shell/themes, the directory package.json's pi.themes registers", () => {
	assert.equal(PACKAGED_THEMES, join(shell, "themes"));
	const pkg = JSON.parse(readFileSync(join(shell, "package.json"), "utf8"));
	assert.deepEqual(pkg.pi.themes, ["./themes/*.json"]);
});

test("today rose ink is the only Jhon theme (L197): Kanagawa is the full-schema reference in shell/fixtures/, not a registered theme", () => {
	const registered = existsSync(PACKAGED_THEMES) ? readdirSync(PACKAGED_THEMES).filter((f) => f.endsWith(".json")) : [];
	assert.deepEqual(registered, []);
	assert.equal(JSON.parse(readFileSync(join(shell, "fixtures", "kanagawa.json"), "utf8")).name, "kanagawa");
});

test("jhonThemes keeps rose ink and the packaged themes, rose ink first, and drops pi's own and anyone else's", () => {
	const all = [
		{ name: "dark", path: undefined },
		{ name: "zen", path: join(PACKAGED_THEMES, "zen.json") },
		{ name: "other", path: "/elsewhere/themes/other.json" },
		{ name: "rose-ink", path: "/home/u/.pi/jhon/themes/rose-ink.json" },
		{ name: "kanagawa", path: join(PACKAGED_THEMES, "kanagawa.json") },
		{ name: "kanagawa", path: join(PACKAGED_THEMES, "kanagawa.json") },
	];
	assert.deepEqual(jhonThemes(all), ["rose-ink", "kanagawa", "zen"]);
	assert.deepEqual(jhonThemes([{ name: "dark", path: undefined }]), []);
});

test("themePreview reads the theme's own file: its accent as a swatch and its text, through vars", (t) => {
	const dir = mkdtempSync(join(tmpdir(), "jhon-theme-preview-"));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const path = join(dir, "zen.json");
	writeFileSync(path, JSON.stringify({ name: "zen", vars: { pink: "#e7bbc8" }, colors: { accent: "pink", text: "#ffffff" } }));
	const sample = themePreview(path);
	assert.equal(sample.caption, "accent and text");
	assert.equal(sample.lines[0], "\x1b[48;2;231;187;200m  \x1b[0m \x1b[38;2;255;255;255mAa  sample text\x1b[0m");
	assert.equal(sample.lines[1], "accent #e7bbc8 · text #ffffff");
	assert.throws(() => themePreview(join(dir, "missing.json")));
	assert.throws(() => themePreview(undefined), /no file/);
});

test("Appearance's Theme row: Jhon's themes, the current one marked, set through pi's setTheme", (t) => {
	const dir = mkdtempSync(join(tmpdir(), "jhon-theme-row-"));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const roseInk = join(dir, "rose-ink.json");
	writeFileSync(roseInk, JSON.stringify({ name: "rose-ink", vars: { pink: "#e7bbc8" }, colors: { accent: "pink", text: "#ffffff" } }));
	const set: string[] = [];
	let current = "rose-ink";
	const themes = {
		list: () => [{ name: "dark", path: undefined }, { name: "rose-ink", path: roseInk }, { name: "kanagawa", path: join(PACKAGED_THEMES, "kanagawa.json") }],
		current: () => current,
		set: (name: string) => { if (name === "bad") return "no such theme"; set.push(name); current = name; return undefined; },
	};
	const rows = customizeRows({ ...hooks, themes });
	const row = rows.find((r) => r.label === "Theme")!;
	assert.equal(row.group, "Appearance");
	assert.equal(rows.filter((r) => r.group === "Appearance")[0], row, "first in Appearance");
	assert.deepEqual(row.options, ["rose-ink", "kanagawa"]);
	assert.deepEqual(row.options!.map((o) => row.display!(o)), ["rose-ink (current)", "kanagawa"]);
	assert.equal(row.value(), "rose-ink");
	assert.equal(row.apply!("kanagawa"), undefined);
	assert.deepEqual(set, ["kanagawa"]);
	assert.equal(row.display!("kanagawa"), "kanagawa (current)");
	assert.equal(row.apply!("bad"), "no such theme");
	const preview = row.preview!("rose-ink", false);
	assert.equal(preview.caption, "accent and text");
	assert.equal(preview.lines[1], "accent #e7bbc8 · text #ffffff");
});

test("rose ink alone is the Theme row's single value; no themes hook or no Jhon theme, no row", () => {
	const only = customizeRows({ ...hooks, themes: { list: () => [{ name: "rose-ink", path: "/a/rose-ink.json" }], current: () => "rose-ink", set: () => undefined } });
	assert.deepEqual(only.find((r) => r.label === "Theme")!.options, ["rose-ink"]);
	assert.ok(!customizeRows(hooks).some((r) => r.label === "Theme"));
	assert.ok(!customizeRows({ ...hooks, themes: { list: () => [{ name: "dark", path: undefined }], current: () => "dark", set: () => undefined } }).some((r) => r.label === "Theme"));
});
