# The dashboard contract

What the dashboard component consumes from the framework. The component is
the page only: `index.html`, `app.css`, and `app.js`, with no build step and
nothing fetched from the network. It reaches Jhon only through the surface
below and never reads transcripts, `prose/`, or the engine's files. A change
to anything below changes the contract; `engine/internal/dashboard` serves
it, and its tests hold both sides.

## What `jhon dashboard` serves (127.0.0.1, GET only)

Every answer carries `X-Jhon-Dashboard: 1`, so the shell's `/jhon:dashboard`
knows a Jhon dashboard from any server on the port. Every `/api/` route
answers only a request whose Host is `127.0.0.1`, `localhost` or `[::1]`
on the server's own port, else 403 `the dashboard answers only at
127.0.0.1, localhost or [::1] on its own port`; and, when it carries an
Origin, only `http://` plus that same Host, else 403 `a request from another
origin is refused` (a page of another site reaching the port through DNS
rebinding names its own host). The page's own files load whatever the Host.

| Path | Shape |
|---|---|
| `/`, `/app.css`, `/app.js` | This component's files, from the active release's `dashboard/`. A missing file answers 500, naming `jhon sync`. |
| `/palette.css` | Rose ink (`jhon palette`): the colors as CSS variables, read on every request, after `app.css` so they win over its fallback; with the roles `--accent-soft` (section headers) and `--frame-soft` (overlay frames). |
| `/api/sessions` | The recent Claude Code sessions, newest first: `[{id, project, name, title, modified, bytes, runtime, summary?}]`, Claude Code's sessions and Jhon's pi sessions (`~/.pi/jhon/sessions`) alike; `runtime` is `claude-code` or `pi`; `entrypoint`, for a Claude Code session, is how it was started as its transcript's head records it (`cli` the owner at its prompt, `sdk-ts` an SDK such as pi's model bridge, `sdk-cli` claude -p), absent when the head names none. `name` is the project's readable name; `title`, its first request or `null`; `summary`, the session's audit in a few numbers once one matches its transcript (`status`, `violation`, `suspect`, `look`, `relaunches`, `verify_fail`, `record_failed`, `processed`, `outside`, `active_ms`, `elapsed_ms`, `checks` (each `{check, title, violation, suspect}`, `title` in View's words), `notes`, `errors`, `change`, the change the session worked on most, `changes`, every change it worked on, and `usage`, what its transcripts record of the model's usage), for the list's states, Home and the Usage page. |
| `/api/session/<id>` | One session, by its id or a prefix of 8 or more: `{session: {id, project, modified, runtime, title?}, view, usage}`: what the session did. `usage` is `{rows}`, one row per day and model of what each assistant message of the main transcript and its sub-agents' (`<session>/subagents/*.jsonl`) records in `message.usage`: `{day, model, input, output, cache_read, cache_write, total, cost, messages}`, `day` the machine's calendar day, `cost` the dollars pi recorded (`cost.total`), `null` where no message recorded one (every Claude Code session): never an estimate. A Claude Code response streamed as several entries counts once. Its changes' state today is the Changes pages' only (L138). `view` is `engine/internal/dashboard.View` of `jhon audit --json --context`: `status`, `version`, `session`, `summary`, `changes`, `unattributed`, `agentTypes`, `agents`, `findings`, `reads`, `errors`, `notes`, `authorizations` (the owner's yeses to a guard's question the session records: `{classes, scope, at, ask}`, scope `once` or `grant`), `worked` (what the session did right, each with its evidence: `{kind, change, what, evidence}`, kind `delivery`, `tests`, `verify`, `agents`, `records`, or `guards`; change null for the session), `timing` (how the session ran, or null: `{main, agents, compactions}`; each thread `{agent, agent_type, change, turns, model_ms, tool_ms, first_response_ms, processed, cache_read, cache_hit}`, first_response_ms null for the main thread and cache_hit null with no usage; each compaction `{at, trigger, tokens_before, duration_ms}`, trigger `auto`, `manual`, `extension`, or null). |
| `/api/changes` | Every change the listed sessions' summaries name, one row per project and change, the newest session's change first: `[{name, project, sessions, ldd}]`; `project` is the project's readable name, `sessions` the change's sessions newest first (`{id, title, modified, runtime}`), and `ldd` the change's state as its project holds it today, read from the newest session's working directory (what `jhon status <change> --json` reads: `{total, completed, slices, slice, unchecked, artifacts, next, notes, tests, route, current, slicesDone}`, `current` the first slice not done (an outline one included, null when every slice is done), `route` the route's level, `standard` or `verified`, null when status could not read it, each slice with its `delivers` when tasks.md says it), or `null` when the project no longer holds it as active. A session not audited yet names no change; a summary that predates `changes` names its main `change` only. The page shows it at `#/changes`. |
| `/api/usage?from=<day>&to=<day>&project=<name>` | The usage history (L138, answers 2 and 3): the listed sessions' summaries' `usage` from `from` (absent: all history) to `to` (absent: today), both `YYYY-MM-DD` days of the machine, in `project` (a readable `name`; absent: all). Only the owner's own sessions count, as on Home: Claude Code at its prompt and pi, not the pi model bridge's Claude Code sessions (they repeat pi's turns), `claude -p` runs or scratch runs. `{from, listed, total, capped, to, project, sessions, pending, totals, cost_covers, days, models, active_days, days_in_range, longest_streak, current_streak, most_active_day, longest_session}`: `listed` the sessions the server lists (the 80 most recent), `total` how many there are, `capped` whether some were cut; `pending` the sessions not audited yet; `totals` `{input, output, cache_read, cache_write, total, cost, messages}`; `cost_covers` what a cost sums, `all`, `pi only` or `none` (then `cost` is null); each active day `{day, total, cost, messages}`, oldest first; each model `{model, share, total, cost, messages}`, most tokens first; a streak counts days in a row with usage, the current one back from `to` (from the day before when `to` is idle); `most_active_day` a day or null; `longest_session` `{id, title, project, active_ms}` by its audit's active time, or null. A day that is not one, or `from` after `to`, answers 400. |
| `/api/coverage` | What the listing covers: `{listed, total, capped}`, `listed` the sessions `/api/sessions` lists (the 80 most recent), `total` every session on disk, `capped` whether some were cut. Home and Memory read it. |
| `/api/integrations?probe=1` | Each integration of the manifests (`integrations/<name>/manifest.yaml`), by their order: `[{name, display, required, state, reason, numbers}]`. `state` is `on`, `off` (its setting is false: `reason` is `<setting> = false`, or `the settings cannot be read, so it is off`) or `unavailable` (on, but `<binary> is not on PATH`, or a remote one answered `<url>: <why>`, or Engram's local API `Engram does not answer: <why>`); `reason` is `""` when it is plainly on. `numbers` is null unless it is on and answers: Engram's is `{observations}`, its total (`/stats`); an optional one's is null today. A remote integration (`doctor.probe: remote`, context7) is asked over the network only with `probe=1`, else it reads `on` with `not asked: ?probe=1 asks <url>`. Always 200: whatever is off or unreachable is a row saying so (T58b). Home shows it as its Integrations block. |
| `/api/version` | Which build answers: `{version, release, binary, pid}`, `release` the release id under `releases/` (or empty for a checkout build). `jhon sync`, `jhon update` and `jhon dashboard --port` read it, with the server's own start record, to prove a dashboard is Jhon's before restarting one from an older release. |
| `/api/memory/files?project=<dir>` | A project's file memory (L138, answer 1), the one Claude Code keeps and pi shares, `~/.claude/projects/<dir>/memory/`: `{project, dir, index, files}`, `index` whether MEMORY.md exists, each file `{title, file, hook}` from its index lines (`- [Title](file.md) — hook`), in its order. `<dir>` is a project directory as `/api/sessions`' `project` names it, letters, digits, `.`, `_` and `-` only, never `.` or `..` (else 400), and one a listed session names (else 404). |
| `/api/memory/file?project=<dir>&name=<file>` | One memory file: `{name, text}`. `name` is a bare `*.md` name of that directory, not starting with a dot, with no `/`, `\` or NUL (else 400, `a memory file is a bare *.md name of the project's memory`); a link or anything but a regular file is 404, and so is every file when the project's directory or its `memory` is a link or resolves out of the real projects directory (then `/api/memory/files` lists nothing), so nothing out of the directory is read. |
| `/api/memory/engram?project=<dir>&q=<text>` | The project's Engram observations, through Engram's local HTTP API (`ENGRAM_URL`, else `http://127.0.0.1:7437`), GET only, each request cut at 3 s: the 40 newest (`/observations/recent`), or the 30 a search finds (`/search`, with `q`), and the total (`/stats`): `{project, total, query, observations}`, each `{id, type, title, created_at}`. `project` is Engram's name for it: the git origin's name of the project's newest session's working directory (`git remote get-url origin`, read-only, cut at 2 s), else its folder's, else the readable name. Engram not answering, or answering an error, is 502 `Engram does not answer: <why>`; an answer that is not JSON is 502 `Engram answered something that is not JSON`. |
| `/api/memory/engram/<id>` | One observation whole (`/observations/<id>`), as Engram gives it: `{id, type, title, content, created_at, …}`. `<id>` is 1 to 19 digits, else 400; Engram not answering is 502 as above. |
| `/api/session/<id>?fresh=1` | The same, audited again whatever the cache holds: the page's Re-auditar. |
| `/api/telemetry?days=<n>` | What Jhon recorded of its own decisions and runs (`jhon telemetry --json`, engine/internal/telemetry) over the last n days, 7 when n is not a number, clamped to 1..30: `{days, events, skipped, hooks, denials, commands, tests, deliveries, subagents, compactions, per_day}`; each hook `{name, count, allow, deny, ask, context, error, p50_ms, p95_ms}`, each denial `{time, hook, agent, tool, reason}` (the latest 20, newest first), each command `{command, verb, count, failed, p50_ms}`, `tests` `{runs, green}`, `deliveries` `{checks, ready}`, each sub-agent `{agent, count, completed, turn_cap, timeout, cancelled, failed, p50_ms, p50_turns}`, each compaction `{time, percent}`, each day `{day, events, denies}`; a duration or a median with nothing recorded is null. The page shows it at `#/telemetry`. |

## The served page

The dashboard holds the history (S30, one fact, one place): sessions, their
audits, and changes over time. A top bar leads to six pages, every one
full width: Home (`#/`, where to go: the sessions, the telemetry, the
changes; where the owner left off; what needs a look; the range at a
glance; its Integrations block, each integration's state, reason and
numbers from `/api/integrations`, where one off or unavailable reads so
with its reason and `—` for numbers, and a failed read is a line in the
block, never an error panel), Sessions (`#/sessions`, every session filtered by origin, state,
project, runtime, and text; the owner's own by default, the bridge's and
headless runs under the hood, a run in a temporary directory only when
asked; the text search redraws the list alone, 150 ms after the last key),
Changes (`#/changes`, every change across the sessions: its project, last
session, and its slices, last run and route today), Usage (`#/usage`, the
owner's usage per day from `/api/usage`, in Home's range and one project
or all, chosen as on the Sessions page: tokens with their split, money,
active days and streaks, the most active day, the longest session, the
favourite model, each day newest first with its tokens and money, and the
models with their share, tokens, money and messages), Memory (`#/memory`,
a project's memory, read only: a project picker over the owner's own
projects, the most recent session's first and chosen; Engram's
observations, the newest or a search's (`/`), each with its title, type,
id and date, and the project's total; the file memory's MEMORY.md index;
`#/memory/o/<id>` opens an observation whole, `#/memory/f/<file>` a file;
Engram not answering reads `✗ Engram does not answer: <why>` with Try
again, in the Engram card only; empty, `Nothing saved yet.` and `No file
memory for this project.`), and Telemetry (`#/telemetry`). Money is what pi recorded, `$1.20`, and a sum says what
it covers (`$1.20 · pi only`); where nothing recorded it, `—`; never an
estimate. A range says what it covers: Home's and Usage's All reads `all
history` only when no session was cut, else `the 80 most recent sessions`,
and another range adds `(of the 80 most recent sessions)` when cut; Memory's
picker then says `Projects of the 80 most recent sessions.` A
session opens at `#/s/<id>`; a change at `#/c/<project>/<change>`, with its
state in the project today when known, labelled Today, and its sessions,
newest first. Only the Changes pages show a change's state today; a
session's page shows what the session did (L138); its Usage tab, the
model, money, messages and token breakdown its transcripts record, then
who spent the tokens, by change and by agent type.

While a page's data loads it shows a skeleton. A failed read of
`/api/sessions`, `/api/changes`, `/api/usage`, `/api/memory/*`, `/api/telemetry` or `/api/session/<id>`
shows what went wrong and a Try again button, never an empty list; a
background refresh that fails keeps the list it had. `/api/integrations`
is the exception: Home's block says `could not read them: <why>`.

Every state reads in one vocabulary (design/system.md), a glyph and a
word, never upper case: a run is `✓ passed` or `✗ N failed`, a change
`slice x/n` with `◐` on the slice in progress, a route `standard` or
`verified`. A session is `✗ broke a rule`, `◉ needs a look`, `✓ clean`,
`? unreadable` or `· not audited`, its kanji seal beside the words; a
finding `✗ broken` or `◉ to review`. `◉` takes the bad role, as in
the table, and no glyph a second colour. The list's state reads the
page's rule: a suspect, a change with something to look at, most tokens
outside every change, an audit error or note make a session `◉ needs a
look`.

### Keys

| Key | Does |
|---|---|
| `j` / `k` (or ↓ / ↑) | move through a list's rows (sessions, changes, needs a look) |
| `enter` | open the selected row |
| `esc` | close the keys or the bar, leave a field, else go back: a session to the list it came from, a change to Changes, an open memory to Memory, a page Home |
| `g h`, `g s`, `g c`, `g u`, `g m`, `g t` | Home, Sessions, Changes, Usage, Memory, Telemetry |
| `ctrl k` | the command bar (design 03): finds sessions and changes by text, and offers Open, Sessions, Changes, Usage, Memory, Telemetry; ↑ ↓ move, enter goes, esc closes |
| `/` | search the sessions; on the Memory pages, search Engram (enter runs it) |
| `1` … `5` | a session's tab |
| `r` | audit the session again |
| `?` | show or hide the keys |

The page `jhon audit --html` writes answers `1` … `5`, `?` and `esc` only.

## The page `jhon audit --html` writes

The same `index.html` with `app.css` inlined as `<style>`, `app.js` inlined as
`<script type="module">`, and the session embedded as JSON in
`<script type="application/json" id="session">` (the `/api/session/<id>`
shape) beside the report in `id="report"`. With `#session` present, the
script shows that session alone and fetches nothing.

`jhon audit <session> --view` prints that same session JSON alone.

## What else imports `app.js`

Only its tests (`dashboard/test/`): importing it outside a browser runs
nothing and touches no storage, since `start()` runs only with a
`document`. The shell links here (`/jhon:dashboard` opens the current
session's page); it imports nothing. The page `jhon audit --html` writes is
for reading one session where no dashboard runs.

## What the page keeps

Only, in this browser's `localStorage`: the findings the owner marks as
reviewed (`jhon-dashboard:reviewed:<session id>:<where>`), and the
owner's view choices (`jhon-dashboard:` then the Sessions page's `group`,
`sort` and `scratch`, Home's `range` (the Usage page's too), and Telemetry's `tdays`). The type
size is the browser's own zoom.
Nothing is written to the sessions or to any project. `jhon dashboard` keeps each session's summary under `$XDG_CACHE_HOME/jhon/dashboard` (else `~/.cache/jhon/dashboard`), keyed by the transcript's size and mtime, and audits the 80 newest sessions without one in the background, one at a time, every 2 minutes.

## Look

Rose ink, Jhon's own palette (`/palette.css`, which follows an optional
desktop palette file, `integrations.theme.palette = "desktop"`, when one stands;
`app.css` keeps today's palette as the fallback), in the rashin
language: sharp corners, hairlines, hard offset shadows, kanji seals beside
the English names. Section headers take `--accent-soft`; the command
bar, the keys and the agent drawer are framed in `--frame-soft`; the token
split paints the rose ladder, no hue outside rose ink's roles. Every color is a palette variable, a shade of one is a
`color-mix` of them; `app.css` holds no hex value outside its fallback
`:root`. Every visible word is English.
