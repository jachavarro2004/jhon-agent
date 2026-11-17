// T50e (F1): every `jhon` call the shell makes is a row of CONTRACT.md's
// call table, and every row is a call the shell makes. The calls are read
// from the source: a call whose first argument is the jhon binary (`bin`,
// `config.jhon`, `o.jhon`) and whose second is an argv literal, and the
// callers of the wrappers that prepend part of the argv. A call is named by
// its leading literal words; a row by the words before its first
// placeholder (`<…>`, `[…]`).
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const SHELL = join(dirname(fileURLToPath(import.meta.url)), "..");

/** The shell's own sources: extensions/ and lib/, tests excluded. */
function sources(): string[] {
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, e.name);
      if (e.isDirectory()) walk(p);
      else if (e.name.endsWith(".ts")) out.push(p);
    }
  };
  walk(join(SHELL, "extensions"));
  walk(join(SHELL, "lib"));
  return out.sort();
}

/** The leading string literals of an argv literal's text, up to the first other element. */
function leading(text: string): string[] {
  const words: string[] = [];
  let rest = text;
  for (let m = /^\s*"([^"]*)"\s*(?:,|$)/.exec(rest); m; m = /^\s*"([^"]*)"\s*(?:,|$)/.exec(rest)) {
    words.push(m[1]);
    rest = rest.slice(m[0].length);
  }
  return words;
}

const BIN = String.raw`(?:bin|config\.jhon|o\.jhon)`;
// Wrappers that take the argv without the binary, with the words they prepend.
const WRAPPERS: [RegExp, string[]][] = [
  [/\bjhonRun\(\[([^\]]*)\]/g, []],
  [/\bjhonText\(\[([^\]]*)\]/g, []],
  [/\bjhonMode\(\[([^\]]*)\]/g, ["mode"]],
];

/** Every jhon call site: its name (leading literal words) and where it is. */
function callSites(): { name: string; at: string }[] {
  const out: { name: string; at: string }[] = [];
  for (const file of sources()) {
    const src = readFileSync(file, "utf8");
    const rel = relative(SHELL, file);
    const lineOf = (i: number) => `${rel}:${src.slice(0, i).split("\n").length}`;
    const add = (words: string[], i: number) => out.push({ name: ["jhon", ...words].join(" "), at: lineOf(i) });
    // A wrapper's own call (its argv ends in `...args`) is named by its callers below.
    for (const m of src.matchAll(new RegExp(String.raw`\(${BIN},\s*\[([^\]]*)\]`, "g"))) if (!/\.\.\.args\s*$/.test(m[1])) add(leading(m[1]), m.index!);
    for (const [re, prefix] of WRAPPERS) for (const m of src.matchAll(re)) add([...prefix, ...leading(m[1])], m.index!);
    if (rel === join("lib", "settings.ts")) for (const m of src.matchAll(/\brun\(\[([^\]]*)\]/g)) add(["settings", ...leading(m[1])], m.index!);
    for (const m of src.matchAll(/\brunGuard\(config,\s*"([\w-]+)",\s*(\[[^\]]*\])?/g)) add(["hook", m[1], ...(m[2] ? leading(m[2].slice(1, -1)) : [])], m.index!);
    // The binary with an argv that is not a literal: a wrapper's own
    // definition (`args`), or the dashboard's, read from its function.
    for (const m of src.matchAll(new RegExp(String.raw`\(${BIN},\s*(?!\[)([\w.]+)`, "g"))) {
      if (m[1] === "args") continue;
      if (m[1] === "dashboardStartArgs") {
        const body = /function dashboardStartArgs[\s\S]*?return \[([^\]]*)\]/.exec(src);
        assert.ok(body, "dashboardStartArgs returns an argv literal");
        add(leading(body[1]), m.index!);
        continue;
      }
      assert.fail(`${lineOf(m.index!)}: a jhon call whose argv this test cannot read (${m[1]}); give it a literal argv or teach the test its wrapper`);
    }
  }
  return out;
}

/** The rows of CONTRACT.md's call table, by the words before their first placeholder. */
function contractCalls(): string[] {
  const text = readFileSync(join(SHELL, "CONTRACT.md"), "utf8");
  const section = /## Every `jhon` call the shell makes\n([\s\S]*?)(?=\n## |$)/.exec(text);
  assert.ok(section, "CONTRACT.md has a section \"Every `jhon` call the shell makes\"");
  const rows = [...section[1].matchAll(/^\| `(jhon [^`]+)` \|/gm)].map((m) => m[1]);
  return rows.map((row) => {
    const words = row.split(/\s+/);
    const at = words.findIndex((w) => /[<[…]/.test(w));
    return (at < 0 ? words : words.slice(0, at)).join(" ");
  });
}

test("every jhon call the shell makes is in CONTRACT.md's call table, and every row is a call it makes", () => {
  const sites = callSites();
  const rows = contractCalls();
  assert.ok(sites.length > 20, `the test finds the shell's calls (${sites.length})`);
  const missing = sites.filter((s) => !rows.includes(s.name)).map((s) => `${s.name} (${s.at})`);
  assert.deepEqual(missing, [], "calls the table does not list");
  const unused = rows.filter((r) => !sites.some((s) => s.name === r));
  assert.deepEqual(unused, [], "rows no call makes");
  assert.equal(new Set(rows).size, rows.length, "each call is one row");
});

// T117a: the profile json shape carries each profile's delegation mode, in
// the contract and in the shell's type of it.
test("jhon profile json's row and the shell's ProfilesData carry each profile's delegation", () => {
  const contract = readFileSync(join(SHELL, "CONTRACT.md"), "utf8");
  const row = /^\| `jhon profile json` \|(.*)$/m.exec(contract);
  assert.ok(row, "CONTRACT.md has the profile json row");
  assert.match(row[1], /"delegation": "inline" or "writer"/);
  const type = readFileSync(join(SHELL, "lib", "ui", "jhon-panel-profiles.ts"), "utf8");
  assert.match(type, /profiles: Array<\{ name: string; provider\?: string; delegation: "inline" \| "writer"; agents: ProfileRow\[\] \}>/);
});
