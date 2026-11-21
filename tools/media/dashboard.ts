/**
 * tools/media/dashboard.ts — the dashboard's audit of one session, captured for the media.
 *
 * The page is the dashboard component (dashboard/) as `jhon audit <session> --html`
 * builds it: the engine, compiled from this checkout, audits an invented Claude Code
 * session and inlines its view into the component's page, painted in rose ink. Nothing
 * is recorded by hand: the session, its project and the clock are fixed here, the
 * engine runs in a sandbox (HOME, XDG_* and the settings in the build's scratch
 * folder, JHON_ROOT this checkout so the page is this checkout's component), and a
 * headless Chrome draws the page. The sandbox's path is written as /home/dev, as the
 * terminal captures write it.
 *
 * The session (5e7c1a90…, in the invented project ~/src/lumen of captures.ts): asked to
 * add the amount to each CSV row and ship it, the agent edits export/csv.go, commits,
 * pushes to main with no test run, then reads the change's tasks. Its audit is the one
 * `audit.png` shows in the terminal.
 *
 * Chrome is chrome-headless-shell: $JHON_MEDIA_CHROME, else the one the HyperFrames CLI
 * keeps (`npx hyperframes browser ensure`, ~/.cache/hyperframes/chrome).
 */
import { execFile, execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readdirSync, utimesSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { REPO } from "./paths.ts";

const run = promisify(execFile);

/** The session's id, its project as the transcript names it, and when its transcript was last written. */
export const SESSION = "5e7c1a90-0000-4000-8000-00000000c5a1";
const HOME = "/home/dev";
const MODIFIED = new Date("2026-11-16T09:42:20Z");
/** The page's viewport in CSS pixels (the head, the figures, the tabs and the Summary's first row), drawn at 2x. */
export const VIEWPORT = { width: 1600, height: 760, scale: 2 };

/** chrome-headless-shell: $JHON_MEDIA_CHROME, else the newest the HyperFrames CLI keeps; "" when there is none. */
export function chromePath(): string {
  if (process.env.JHON_MEDIA_CHROME) return process.env.JHON_MEDIA_CHROME;
  const root = join(homedir(), ".cache", "hyperframes", "chrome", "chrome-headless-shell");
  if (!existsSync(root)) return "";
  for (const v of readdirSync(root).sort().reverse()) {
    const bin = join(root, v, "chrome-headless-shell-linux64", "chrome-headless-shell");
    if (existsSync(bin)) return bin;
  }
  return "";
}

/** The invented session's transcript, one JSON entry a line, its working directory `cwd`. */
export function sessionTranscript(cwd: string): string {
  const id = (i: number) => `00000000-0000-4000-8000-${String(i).padStart(12, "0")}`;
  const usage = (input: number, output: number) => ({ input_tokens: input, output_tokens: output, cache_read_input_tokens: 18000, cache_creation_input_tokens: 2400 });
  const tool = (use: string, name: string, input: Record<string, unknown>) => ({ role: "assistant", model: "claude-opus-5-5", content: [{ type: "tool_use", id: use, name, input }], usage: usage(1200, 180) });
  const result = (use: string, content: string) => ({ role: "user", content: [{ type: "tool_result", tool_use_id: use, content }] });
  const push = ["git", "push", "origin", "HEAD:main"].join(" ");
  const entries: Array<[string, Record<string, unknown>]> = [
    ["09:40:00", { type: "user", message: { role: "user", content: "Add the amount to each CSV row and ship it." }, origin: { kind: "human" } }],
    ["09:40:20", { type: "assistant", message: tool("toolu_01", "Read", { file_path: `${cwd}/export/csv.go` }) }],
    ["09:40:21", { type: "user", message: result("toolu_01", "package export\n\nfunc Row(i Invoice) string { return i.ID + \",\" + i.Customer }\n") }],
    ["09:40:50", { type: "assistant", message: tool("toolu_02", "Edit", { file_path: `${cwd}/export/csv.go`, old_string: "i.ID + \",\" + i.Customer", new_string: "i.ID + \",\" + i.Customer + \",\" + i.Amount" }) }],
    ["09:40:51", { type: "user", message: result("toolu_02", `The file ${cwd}/export/csv.go has been updated.`) }],
    ["09:41:30", { type: "assistant", message: tool("toolu_03", "Bash", { command: "git commit -am \"export: the amount in each row\"" }) }],
    ["09:41:31", { type: "user", message: result("toolu_03", "[main 50e3ff2] export: the amount in each row\n 1 file changed, 1 insertion(+), 1 deletion(-)") }],
    ["09:42:10", { type: "assistant", message: tool("toolu_04", "Bash", { command: push }) }],
    ["09:42:12", { type: "user", message: result("toolu_04", "To example.com:lumen.git\n   9a1b2c3..50e3ff2  HEAD -> main") }],
    ["09:42:15", { type: "assistant", message: tool("toolu_05", "Read", { file_path: `${cwd}/openspec/changes/invoice-csv-export/tasks.md` }) }],
    ["09:42:16", { type: "user", message: result("toolu_05", "# Tasks\n\n## Slice 1: the CSV writer\n\n- [x] 1.1 the row\n") }],
    ["09:42:20", { type: "assistant", message: { role: "assistant", model: "claude-opus-5-5", content: [{ type: "text", text: "Done: each row ends with its amount, on main." }], usage: usage(900, 40) } }],
  ];
  return entries.map(([at, e], i) => JSON.stringify({
    parentUuid: i ? id(i) : null, isSidechain: false, ...e, uuid: id(i + 1), timestamp: `2026-11-16T${at}.000Z`,
    userType: "external", entrypoint: "cli", cwd, sessionId: SESSION, version: "2.1.304", gitBranch: "main",
  })).join("\n") + "\n";
}

/** The invented project: the CSV writer, and the change invoice-csv-export under `delivery: change`, slice 2 open. */
const PROJECT: Record<string, string> = {
  "go.mod": "module example.com/lumen\n\ngo 1.22\n",
  "export/csv.go": "package export\n\n// Invoice is one invoice row.\ntype Invoice struct {\n\tID, Customer, Amount string\n}\n\n// Row is the invoice as one CSV row.\nfunc Row(i Invoice) string { return i.ID + \",\" + i.Customer + \",\" + i.Amount }\n",
  "openspec/config.yaml": "delivery: change\n",
  "openspec/changes/invoice-csv-export/proposal.md": "# Invoice CSV export\n\nExport the invoices as CSV, streamed for large exports.\n",
  "openspec/changes/invoice-csv-export/tasks.md": "# Tasks\n\n## Slice 1: the CSV writer\n\n- [x] 1.1 the row\n\n## Slice 2: stream large exports\n\n- [ ] 2.1 chunked reader\n",
};

/** The page `jhon audit <session> --html` writes for the invented session, the sandbox's path written as /home/dev. */
export function auditPage(bin: string, scratch: string): string {
  const home = join(scratch, "dashboard-home");
  const project = join(home, "src", "lumen");
  for (const [file, text] of Object.entries(PROJECT)) {
    mkdirSync(join(project, file, ".."), { recursive: true });
    writeFileSync(join(project, file), text);
  }
  const dir = join(home, ".claude", "projects", "-home-dev-src-lumen");
  mkdirSync(dir, { recursive: true });
  const transcript = join(dir, `${SESSION}.jsonl`);
  writeFileSync(transcript, sessionTranscript(project));
  utimesSync(transcript, MODIFIED, MODIFIED);
  const env = {
    PATH: process.env.PATH ?? "/usr/bin:/bin",
    HOME: home,
    XDG_CONFIG_HOME: join(home, ".config"),
    XDG_DATA_HOME: join(home, ".local", "share"),
    XDG_CACHE_HOME: join(home, ".cache"),
    JHON_SETTINGS: join(home, "settings.toml"),
    JHON_ENGRAM: "/dev/null/engram",
    JHON_ROOT: REPO,
    TMPDIR: scratch,
    TZ: "UTC",
  };
  const html = execFileSync(bin, ["audit", transcript, "--html"], { cwd: home, env, encoding: "utf8", maxBuffer: 64 << 20 });
  return html.replaceAll(home, HOME);
}

/** The audit page drawn by headless Chrome into `png`, at VIEWPORT. */
export async function screenshot(html: string, scratch: string, png: string): Promise<void> {
  const chrome = chromePath();
  if (!chrome) throw new Error("no chrome-headless-shell: set JHON_MEDIA_CHROME, or run `npx --yes hyperframes@0.9.58 browser ensure` (tools/media/hyperframes/README.md)");
  const page = join(scratch, "dashboard.html");
  writeFileSync(page, html);
  const profile = join(scratch, "chrome-profile");
  mkdirSync(profile, { recursive: true });
  await run(chrome, [
    "--headless", "--no-sandbox", "--disable-gpu", "--hide-scrollbars", "--no-first-run", "--no-default-browser-check",
    "--disable-extensions", "--disable-background-networking", "--disable-sync", "--font-render-hinting=none",
    `--user-data-dir=${profile}`, `--window-size=${VIEWPORT.width},${VIEWPORT.height}`, `--force-device-scale-factor=${VIEWPORT.scale}`,
    "--virtual-time-budget=5000", `--screenshot=${png}`, pathToFileURL(page).href,
  ], { env: { ...process.env, TZ: "UTC" } });
}
