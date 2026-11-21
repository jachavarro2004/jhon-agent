# tools/media/hyperframes — the presentation video, made with HyperFrames

Jhon's presentation video (`docs/assets/video/jhon-explainer.mp4`, and
`jhon-explainer.es.mp4` with Spanish subtitles) is made by code with
[HyperFrames](https://github.com/heygen-com/hyperframes): HTML rendered to
video by a headless Chrome that seeks one paused timeline frame by frame.
Jhon's own motion engine (`motion/motion.js`, below) draws the motion;
Three.js draws the one 3D shot, the 力 seal of the intro. It replaced the
SVG-frame explainer (`odd/tasks/v0-1-release.md`, T63f, L1099–L1114) in the style the owner approved on pilot 3 (L1112).

The pilots (pilot 1, `intro/`; pilots 2 and 3, `pilot2/`) are in git history
only, from commit 445e6dec on `feat/t63a-media`; this README's history has
their write-ups.

## The style (pilot 3's, approved)

- **Jhon leads.** The seal and Jhon's own words are the protagonists: one
  idea per full-frame shot, big type (Fraunces, the accent's italic for the
  key words), on a dark stage under a rose light pool that drifts with the
  cuts. No web-page layout, no card grid, no caption paragraph under a screen.
- **Claim → proof.** Each chapter says a claim in big type, then holds its
  proof: one of Jhon's real screens (a feature capture from
  `docs/assets/features/`, cropped to its terminal panel without its footer
  caption) or one of the four diagrams (`docs/assets/diagrams/`). A proof
  screen is the subject of its shot: close (up to 1700×790 px), square-on,
  sharp, held its line plus 1.3 s (at least 4.8 s), rose-lit from behind,
  the camera pushing in toward what is said (scale 0.93 → 1, so it **ends**
  with the whole screen in frame), and a rose mark stamping onto each part
  the line names, as it is said.
- **Video grammar.** Cuts are cut-the-curve to the left (the film's one
  current: exit `power4.in` over 230 px, the fade dying at the cut, entry
  `power4.out` from 0.35 opacity); each chapter opens under identity's flat
  rose band sweeping right to left. Words arrive as a waterfall (binary
  opacity, gaps shrinking ×0.84); body lines are inked left to right as they
  are spoken; marks and seals stamp (scale down to rest, their hard shadow
  landing). Over everything a light vignette and a film grain (six seeded
  noise frames, each held two video frames). Rose ink only from identity's
  tokens (accent `#e7bbc8`, accent fill `#53343f`, accent ink `#f2c5d2`,
  frame-soft `#7b6772`); no glow on text, no neon, none of the reference
  video's chrome (no corner brackets, timecode or chapter counter, L1078).

## How it is made

```
presentation/
├── script.mjs        # THE SOURCE: every shot in order, its kind, what it shows, its line `en` and Spanish subtitle `es`
├── narrate.mjs       # speaks each distinct line with local Kokoro (tools/media/kokoro.py), caches narration/<key>.{opus,json}; [composition-dir]
├── compose.mjs       # writes index.html and captions.es.json from script.mjs and the cache's seconds; [composition-dir]
├── style.css         # the styles      ┐
├── timeline.js       # the timeline    │ inlined into index.html by compose.mjs,
├── stage.js          # the 3D seal     ┘ with ../motion/motion.js
├── index.html        # GENERATED: the composition (every clip timed from the lines' real lengths)
├── captions.es.json  # GENERATED: the Spanish subtitles, one variable per line (c_<shot id>)
├── narration/        # the cache: 33 lines, mono Opus 32 kb/s, with their text and seconds
├── vendor -> ../vendor
└── assets/
    ├── fonts/*.woff  # subsets (Latin, Latin-1, 力) of the OFL fonts identity.ts names
    ├── narration -> ../narration
    └── screens/{features,diagrams} -> docs/assets/{features,diagrams}
teaser/                # the teaser: its own script.mjs, narration/ and generated files (below)
motion/motion.js       # Jhon's motion engine (Apache-2.0, no dependencies)
vendor/                # a pinned copy, no npm install: three-0.187.0 (MIT)
.gitignore             # the CLI's .hyperframes/ state, renders/
```

To change what is said or shown, edit `script.mjs`, then:

```sh
node tools/media/hyperframes/presentation/narrate.mjs "$(mktemp -d)"   # only when a line changed (Kokoro, about a minute for all)
node tools/media/hyperframes/presentation/compose.mjs                   # prints the shot list with its times
node --experimental-strip-types --test tools/media/test/presentation.test.ts tools/media/test/motion.test.ts
```

The test checks that the narration cache holds every line (and nothing
else) and the composition plays each, that each proof's marks land in order
while its screen is held, that every line has its Spanish subtitle
(plain text, tú, at most two rows), and that the composition runs on the
motion engine alone.

`compose.mjs` reads each proof's screen size from `docs/assets/`, so every
proof's screen must be built first (`tools/media/build.ts`, and `diagrams.py`
for the diagrams).

## The motion engine (`motion/motion.js`)

About 200 lines of plain JavaScript, no dependencies, Apache-2.0: the slice
of a tween library this film uses, made deterministic.

```js
const tl = motion.timeline();
tl.fromTo("#a", { x: 230, opacity: 0.35 }, { x: 0, opacity: 1, duration: 0.36, ease: "power4.out" }, 12.4);
tl.to("#m0", { opacity: 0, duration: 0.14, ease: "none" }, 30.1);
tl.set("#w3", { opacity: 1 }, 9.2);   // a zero-length step
motion.set("#w3", { opacity: 0 });    // now, while building
motion.ease("sine.inOut");            // the ease as a function of p
motion.register(tl);                  // window.__timelines["main"]
```

- **Targets**: a selector (several, comma separated), an element or a list.
- **Properties**: `opacity`, `x`, `y`, `yPercent`, `scale`, `rotationY`,
  `transformPerspective`, `transformOrigin`, and `clipPath` and `boxShadow`
  as strings of numbers (the numbers tween; the rest, colours included, is
  kept). An element gets one composed transform, always in the order
  perspective, translate(%), translate(px), rotateY, scale; numbers are
  written rounded to four decimals.
- **Eases**: `none`, `expo.out`, `power1.in`, `power2.in/out/inOut`,
  `power3.out/inOut`, `power4.in/out`, `sine.inOut/out`, with the formulas
  the film was first made with (Known limits; its `expo` is
  `2^(10(p-1))·p + p⁶(1-p)`, not the textbook `2^(10(p-1))`); any other
  name throws.
- **Semantics**: a `fromTo` renders its from-state while the timeline is
  built unless `immediateRender: false`; a `to` starts from its property's
  track at the tween's start, resolved while building; for each property
  of each element the latest-started segment wins and holds its end value;
  before any has started the property shows its base (what `motion.set` or
  an immediate `fromTo` rendered), else the element's own inline value.
- **Determinism**: `tl.render(t)` is a pure function of `t` (no
  `requestAnimationFrame`, timer, `Date` or state between frames), so any
  seek order gives the same DOM. Starts, durations and the playhead are
  rounded to 1e-7 s, so a frame that falls on a tween's boundary, or on a
  value that rounds half-way at four decimals (the intro's camera at frame
  154), lands as it did when the film was approved.
- **HyperFrames 0.9.58**: the runtime pauses the registered timeline, then
  calls `totalTime(t, suppressEvents)` (or `seek(t)`), and reads
  `duration()` and `time()`; `motion.register(tl)` gives it an object with
  `pause`, `play`, `paused`, `time`, `duration`, `totalTime` and `seek`,
  each rendering. The intro's `stage.js` takes its eases from
  `motion.ease` and draws on the runtime's `hf-seek` event.

`tools/media/test/motion.test.ts` checks every ease against the reference
values at 1,001 points (`test/ease-reference.json`, recorded from the first
build's eases before they were removed: a SHA-256 of the float64 values and
every 100th value), seek-order independence, the playhead's grid, a `to`'s
start, `immediateRender` and the HyperFrames shim.

A shot lasts its lead-in (0.35 s after a cut, 0.7 s after a band), its line
and a tail (0.55 s for a claim; a proof is held as above). A mark or a body
line lands where its phrase falls in the line, by its share of the line's
characters (Kokoro's pace is even enough; checked on the stills, not
measured per word). Every line is true to README.md (on `main`), the
docs or a capture's own caption; product words stay as they are in the
Spanish subtitles (neutral Spanish, tú, no voseo). The English video shows a
caption only where the screen does not already say the line (`capEn`: one
shot, the hard lines' "no grant covers").

## Shot list (219.1 s)

| time | chapter | kind | on screen | line |
| --- | --- | --- | --- | --- |
| 0.0–8.0 s | intro | 3D | the 力 seal stamps on a dark wall, rose ink spreads, «Jh*on*», the kicker and the tagline line by line; the camera pushes in | Jhon. Let the agent run. Deliver only what is proven. |
| 8.0–14.4 s | Why Jhon | head | `WHY JHON`; README's first sentence, the second line in the accent | A coding agent left alone is either asked about everything, or trusted with everything. |
| 14.4–20.6 s | | card | Without limits: It acts *outside its task.* | Trusted, it can act outside its task: a destructive command, a write beyond its files. |
| 20.6–27.1 s | | proof | `guard-refusal.png`: the tool call → the refusal → the engine's whole reason | Jhon's guards judge every tool call: a write outside its files is refused. |
| 27.1–32.3 s | | card | Without evidence: It calls a change *done* | It can call a change done, with no test that failed first and passed after. |
| 32.3–38.6 s | | proof | `executions-red-green.png`: the failing test → (the camera pans down) the passing run → its record | With Jhon, the test fails first, then passes, and both runs are on record. |
| 38.6–42.0 s | | card | Without memory: What was decided is *gone* | And what was decided is gone with the session. |
| 42.0–48.8 s | | proof | `ldd-status.png`: the change → its slices → the next step → tests passed | With Jhon, the change stays in view: its slices, the next step, and the last test run. |
| 48.8–56.4 s | Two ways in | list | `jhon start` → Claude Code; `jhon start pi` → pi, with Jhon's shell | There are two ways in: jhon start opens Claude Code, and jhon start pi opens pi, with Jhon's shell. |
| 56.4–64.2 s | | proof | `runtimes-engine.svg`: the two runtimes → the engine → the dashboard | Both runtimes call one engine: the jhon binary. The dashboard only reads what the binary serves. |
| 64.2–71.3 s | | proof | `shell-bar.png`: project → branch → model → context → cost | In pi, the shell's bars show the project, branch, model, context, cost and jobs. |
| 71.3–76.5 s | A repo with no LDD yet | card | Read-only work *just happens.* | In a repo with no LDD yet, read-only work just happens. |
| 76.5–82.4 s | | proof | `ldd-question.png`: the question → the recommended choice | At the first code change, the session asks you once how to deliver and test. |
| 82.4–88.2 s | Seven guards | card | Every tool call, *judged.* | Seven guards judge every tool call, in order, the same in both runtimes. |
| 88.2–94.8 s | | proof | `guards-tool-call.svg`: the seven guards → refuse → ask → allow | The first refusal answers. If none, the first ask. Otherwise, the call passes. |
| 94.8–101.1 s | LDD | card | Value reaches the default branch *in integrable, proven slices.* | With LDD, value reaches the default branch in integrable, proven slices. |
| 101.1–108.0 s | | proof | `ldd-flow.svg`: the slice → the recorded run → RED → GREEN | Each slice: a failing test, recorded as RED. Then the code. Then the tests, green. |
| 108.0–113.5 s | | card | Delivery: No record, *no pull request.* | The pull request opens only with RED before GREEN and a passing verify. |
| 113.5–120.3 s | | proof | `route-refusal.png`: the refusal → `gh pr create` → the unchecked task | Under delivery: change, route-guard refuses the pull request while a task is unchecked. |
| 120.3–125.8 s | Agents | card | One orchestrator *plans.* | One orchestrator talks with you and plans. Focused agents do the work. |
| 125.8–131.0 s | | proof | `agents-launch-context.svg`: the launch message → decisions on record → code map | Each launch carries the decisions on record and a code map. |
| 131.0–136.8 s | | proof | `agents-jobs.png`: the agents → the jobs → tokens and time | Sub-agents and background jobs show live, with their tokens and time. |
| 136.8–142.6 s | You decide | card | A mode and a grant are *yours alone.* | A mode and a grant are yours alone. An agent may read the mode, never set it. |
| 142.6–148.5 s | | proof | `mode-grant.png`: `jhon mode` → `jhon grant push --for 30m` → the live grant | A timed grant widens one class: jhon grant push --for 30m. |
| 148.5–154.4 s | | card | The hard lines: These *stay refused.* | Credentials, .git/, the grants and modes, and Jhon's own files stay refused. |
| 154.4–159.5 s | | proof | `guards-tool-call.svg`: the vcs, framework and secrets rows → the safety row (English caption) | So do the destructive operations no grant covers. |
| 159.5–165.4 s | | card | A folder that is no repo: No repo, *no mode.* | A folder that is no repo has no mode: its calls are judged as in normal mode. |
| 165.4–175.3 s | Measured | card | `jhon audit` *reads a session.* | jhon audit reads a session afterwards, for what went wrong. It flags bypassed refusals, untested delivery and edits before RED. |
| 175.3–180.4 s | | proof | `audit.png`: the finding → `git push origin HEAD:main` → no test run | Here, a push to main with no test run on record. |
| 180.4–186.4 s | | proof | `dashboard-audit.png`: the session's verdict → the session → What to look at → its first item | The dashboard shows the same audit for every session, with what to look at first. |
| 186.4–192.8 s | | proof | `prism-review.png`: the verdict (changes requested) → the record → the finding kept → its place (`export/stream.go:41`) | Every Prism review ends in a verdict on record, with each finding and where it is. |
| 192.8–202.9 s | | numbers | *9 of 9* and *3.4×* (README's bench) | On seeded defects, the native Prism review found the same 9 of 9 as the facet agents, for about 3.4 times fewer weighted tokens. |
| 202.9–212.6 s | Get started | list | Install → the one-line script; Set up → `jhon setup`; Check → `jhon doctor`; Start → `jhon start` | Install with the one-line script, then run jhon setup. jhon doctor runs the health checks. Then jhon start, inside your project. |
| 212.6–219.1 s | close | | the band; the seal stamps; «Let the agent run. / Deliver only what is *proven.*»; github.com/jachavarro2004/jhon-agent | Jhon. Let the agent run. Deliver only what is proven. (the intro's audio again) |

Every terminal capture appears once (ten, the Prism review card with them),
the dashboard's audit once, and every diagram at least once
(`guards-tool-call` twice); the Prism review and dashboard proofs (T191,
L1584, L1586) show the teaser's two screens with the owner's lines. `jhon doctor` is a list row, not a screen (it
cannot pass truthfully in a sandbox: see Known limits).

## The teaser (T141, a preview)

`docs/assets/video/jhon-teaser.mp4` (English captions burned in) and
`jhon-teaser.es.mp4` (Spanish subtitles): 73.3 s, the nine narration lines
the owner accepted (`odd/proposals/t141-plan.md`, "Teaser narration", L1459)
verbatim, one shot each, in the presentation's style. Its screens are the
ones `build.ts` makes in `docs/assets` from the current shell, engine and
dashboard (T190: the Prism review card and the dashboard's audit replaced the
agents diagram's Prism box and the terminal audit, as the plan's beats 6 and
7 asked).

`teaser/` holds only its `script.mjs` (the shots, the lines and `FRAME`: the
intro's two lines, the close's install line, longer holds and one CSS rule), its
`narration/` cache and the generated `index.html` and `captions.es.json`;
`assets/` and `vendor` are links to the presentation's. `compose.mjs` and
`narrate.mjs` take the composition folder as an argument and always use the
presentation's `style.css`, `timeline.js`, `stage.js` and `../motion/motion.js`;
`narrate.mjs` copies a line another composition's cache already holds (the
tagline) instead of speaking it again.

```sh
T=tools/media/hyperframes/teaser
node tools/media/hyperframes/presentation/narrate.mjs "$(mktemp -d)" $T   # only when a line changed
node tools/media/hyperframes/presentation/compose.mjs $T
node --experimental-strip-types --test tools/media/test/teaser.test.ts
```

The test checks that the lines are the accepted table's, in order; that the
English caption of every shot (the default of its variable) and its Spanish
subtitle (`captions.es.json`) are the script's; the narration cache; that it
runs on the motion engine alone; that every screen exists in `docs/assets`;
and that it lasts 60 to 90 s.

| time | kind | on screen | line |
| --- | --- | --- | --- |
| 0.0–8.0 s | intro | the 3D seal, «Jh*on*», «Coding agents write fast. / What can you *trust?*» | Coding agents write fast. The question is what you can trust. |
| 8.0–15.4 s | card (band) | Jhon lets the agent run: One thing of every change: *evidence.* | Jhon lets the agent run, and asks one thing of every change: evidence. |
| 15.4–23.3 s | proof | `executions-red-green.png`: the failing run → (pan) the passing run → its record | A test that failed first, then passed. Recorded, not claimed. |
| 23.3–31.1 s | proof | `ldd-flow.svg` (its two rows): the slice commit, «Tested tree» → delivery | Only the tree that was tested is delivered. |
| 31.1–40.2 s | proof (band) | `guard-refusal.png`: the tool card's refusal → the engine's whole reason | Guards stop only what can't be undone: a destructive command, a write outside the project. |
| 40.2–45.5 s | card | Everything else: Runs free, *inside a sandbox.* | Everything else runs free, inside a sandbox. |
| 45.5–53.4 s | proof | `prism-review.png`, the shell's card: the review and its verdict → the four facets → challenger → arbiter | Risky changes get the Prism review: four facets, a challenger, an arbiter. |
| 53.4–61.2 s | proof | `dashboard-audit.png`: `claude code 2.1.304` → the session's verdict → what to look at | Claude Code or pi. Your model. And an audit of every session. |
| 61.2–73.3 s | close (band) | the seal; the tagline; the install line; github.com/jachavarro2004/jhon-agent | Jhon. Let the agent run. Deliver only what is proven. |

Rendered with the presentation's flags (below), from `teaser/`, without
`HYPERFRAMES_RENDER_DETACHED` (each render takes about 4m 10s): the English
one as is, the Spanish one with `--variables-file "$T/captions.es.json"
--strict-variables`. Measured: 2,198 frames, 73.27 s, H.264 High 1920×1080
30 fps, AAC LC 48 kHz stereo; English 5,297,223 bytes, Spanish 5,307,302
bytes, the same decoded audio (MD5 `abfb6a1a…`). **Two renders of the English
teaser are byte-identical** (sha256 `84a84f7f…b0110f7f`). Rendered again with
the recaptured screens (T190): English 5,111,153 bytes (3m 11s, sha256
`6314db1b…65fcfa0c`), Spanish 5,123,396 bytes (3m 10s), the same 2,198
frames, 73.27 s and decoded audio; not rendered twice, so its byte
identity was not checked again.

Limits: the guards' proof shows a write outside a writer's edit surfaces
(the one refusal captured), not a destructive command nor a write outside the
project; the audit's proof shows a Claude Code session only (no pi session is
captured, and nothing on the dashboard's Summary names the model: its Usage
tab does); the sandbox has no capture, so it is a card.

## Render

Needs Node 22+ and ffmpeg. The CLI is pinned to **hyperframes 0.9.58** and run
with `npx` from the home folder, so the repository gets no `package.json` or
`node_modules`; its headless Chrome (chrome-headless-shell 152) lives in
`~/.cache/hyperframes/chrome`. Telemetry and update checks are off.

```sh
P="$PWD/tools/media/hyperframes/presentation"
O="$PWD/docs/assets/video"
export HYPERFRAMES_NO_TELEMETRY=1 HYPERFRAMES_NO_UPDATE_CHECK=1 HYPERFRAMES_SKIP_SKILLS=1
cd ~
HYPERFRAMES_PLUGIN_CWD="$P" npx --yes hyperframes@0.9.58 browser ensure     # once
# English (a long render: detached, so it survives the shell that started it)
HYPERFRAMES_PLUGIN_CWD="$P" HYPERFRAMES_RENDER_DETACHED=1 nice -n 19 npx --yes hyperframes@0.9.58 render \
  --quality looks --crf 32 --no-browser-gpu -o "$O/jhon-explainer.mp4"
# Spanish subtitles
HYPERFRAMES_PLUGIN_CWD="$P" HYPERFRAMES_RENDER_DETACHED=1 nice -n 19 npx --yes hyperframes@0.9.58 render \
  --quality looks --crf 32 --no-browser-gpu --variables-file "$P/captions.es.json" --strict-variables -o "$O/jhon-explainer.es.mp4"
# Stills
mkdir -p "$O/stills"
for s in intro:4.6 problem:13.8 refusal:26.0 status:47.5 verdicts:93.5 close:218.0; do
  ffmpeg -v error -y -ss "${s#*:}" -i "$O/jhon-explainer.mp4" -frames:v 1 "$O/stills/${s%%:*}.png"
done
```

The render prints `rendered in …` when it is done. To look before
rendering: `… snapshot --at 4.6,26,47.5` (PNGs under
`presentation/.hyperframes/`), `check --json` (lint, runtime, layout,
contrast; it passes with structural warnings only: one file where the CLI
prefers one sub-composition per scene) or `preview` (the Studio).

**`--no-browser-gpu` makes it reproducible**: Chrome draws WebGL in software
(SwiftShader). Pilot 1's two software renders were byte-identical, its GPU
renders were not.

**`--crf 32` keeps each file under 20 MB**: the grain and the rose light
pools cost bitrate. Measured on a 30 s span (20–50 s): crf 28 with grain
every frame was 0.87 Mb/s of video; crf 30 with the grain held two frames
0.66 Mb/s; the AAC narration track is 0.145 Mb/s (stereo, 48 kHz), about
4 MB of each file.

Measured on this machine (16 threads, Intel Iris Xe, 23 GB), software path,
6,677 frames (222.57 s; the first 8.3 s with the 3D seal, the rest DOM), 6 capture
workers: the English video renders in about **8m 36s–9m 15s** (14,837,979
bytes, 14.2 MB; video 0.38 Mb/s), the Spanish one in **9m 33s** (15,350,636
bytes, 14.6 MB). Both carry the same narration (the decoded audio's MD5 is
the same). **Two renders of the English video are byte-identical**
(sha256 `4faee44d…6b4074`; the Spanish one was rendered once). Both files: H.264 High,
yuv420p, 1920×1080, 30 fps; AAC LC 48 kHz stereo. These numbers are the
first build's, before the motion engine (Known limits has the motion
build's renders).

## Known limits

- The film was first made with GSAP 3.15.1. GSAP is free of charge since 2025-04-30 but under Webflow's
  proprietary «Standard No Charge» licence (not open source, not modifiable
  or sublicensable, a clause against competing with Webflow, revocable), so
  Jhon could not ship it in `jhon media video`. The film now runs on Jhon's
  own engine, compared with the GSAP build before GSAP was removed:
  - **Values**: every element's inline transform, opacity, clip-path,
    box-shadow and transform-origin, read at all 6,677 frames (30 fps)
    through the HyperFrames runtime in chrome-headless-shell 152, are the
    same numbers in both builds (338 elements, maximum deviation 0, after
    normalising `translate3d`/`translate` and `scale(a, a)`/`scale(a)`). A
    first engine without the 1e-7 playhead grid differed by 1e-4 on a few
    rounding ties (the intro's camera at frame 154: 1.0338 against 1.0337).
  - **Renders**: the English film rendered from each build with the flags
    above (`--quality looks --crf 32 --no-browser-gpu`) is the same file:
    sha256 `19122c0b…eff458cf`, 12,312,209 bytes, 222.567 s;
    `hyperframes diff` gives SSIM 1 (mean, p05, min, worst cell), the same
    eleven cuts and the same sound. Two renders of the motion build are
    byte-identical. (The first engine's film had SSIM mean 0.9999, p05
    0.9991, min 0.9934 at frame 154, and its two renders differed in four
    frames of the first band seam, 242–243 and 246–247.)
  - These renders lack `command-palette.png` (no longer built), so they were
    never published.

- The published films were rendered again from the motion build and the
  current script (L1545), with the flags above and the cached narration:
  6,201 frames, 206.70 s; English 13,730,355 bytes (9m 5s), Spanish
  14,214,547 bytes (9m 19s), the same decoded audio. They no longer show the
  command palette or the changes view (dropped with the shell's, T169, L1438;
  T174-S1), the bench line names the Prism review and its facet agents
  (T177), and the close reads github.com/jachavarro2004/jhon-agent (L1452).

- Rendered again with the recaptured screens (T190, L1579): the shell's own
  tool cards (T174) with their marks set on the new rows, the agents' and
  jobs' panel whole (eight rows), route-guard's and the audit's words as the
  current engine prints them. The composition's `assets/screens` links
  pointed at `tools/assets/` after the move from `docs/media/` (images
  missing, `[media_load_failed]`); they point at `docs/assets/` again and a
  test holds it. 6,201 frames, 206.70 s, the same cuts and decoded audio
  (MD5 `3298670b…`); English 13,782,043 bytes (13m 17s, sha256
  `9876ff17…13b9fdd4`), Spanish 14,271,190 bytes (9m 53s); each rendered once,
  so their byte identity was not checked again.

- Two proofs added (T191, L1584, L1586): the dashboard's audit after the
  terminal audit, and the Prism review card before the bench, each with the
  owner's line (two new narration lines; the other 31 reused from the cache).
  34 shots, 6,572 frames, 219.07 s (12.36 s longer); the same decoded audio
  in both (MD5 `fb00a06d…`). English 14,945,232 bytes (12m 9s, sha256
  `79bc0003…fdd61321`), Spanish 15,441,129 bytes (13m 39s, sha256
  `38447e6d…ff4f8678`); each rendered once. The stills were not
  extracted again (the six shots they show are unchanged; the close's time
  above is the new one).

- The marks' timing follows each phrase's share of its line's characters,
  not measured word times; a mark can land a few tenths of a second before
  or after its word.
- The proof screens are as large as the frame allows above the subtitle
  band (1700×790 at most), so the widest captures (112 columns: the shell
  bar, the two refusals, `audit`) are set at about 23 px a text cell row:
  legible at 1080p, small on a phone.
- `executions-red-green` is too tall for the frame: the shot shows its
  first run and pans down to the second, rather than showing all of it at once.
- The seam checks of `motion-doctrine` (`ledger.json`, `seam-gate.mjs`)
  were not run; the cuts follow its parameters by hand and were checked on
  snapshots only.
- The ink stain's edge in the intro is value noise: up close it is soft and
  blotchy rather than a real ink bleed (pilot 3's, unchanged).

The captures' own limits (from phase A):

- The engine's words in `guard-refusal`, `route-refusal`, `executions-red-green`, `mode-grant` and `audit` were printed by the real `jhon` binary in a sandbox (an invented project, ~/src/lumen) and are kept as text in `captures.ts`, its scratch path written as /home/dev and the grant's times set to the media clock; they do not change by themselves when the engine's wording does (record them again as its comment says). All five were recorded again on 2026-11-21 (T190): route-guard's refusal and the audit had changed and were replaced; the scope-guard refusal, the two runs' shown lines and the mode and grant lines were unchanged. The dashboard's audit is not kept as text: `build.ts` audits its session on every build. `executions-red-green` leaves out the lint and format checks, the candidate line and the notes (marked `…`); long lines wrap at the panel's width, as a terminal does.
- The shell's tool card shows a refusal's reason in one row cut to the width, so the two refusal screens add the same call fed to the hook (`jhon hook … < call.json`) with its whole answer.
- `jhon doctor` is not captured: in a sandbox it reports rendered-artifact drift and no agents rendered for Claude Code until `jhon sync` runs, and `jhon sync` builds and activates a release (it may push to a mirror), which is not run for the media; with claude, engram and codegraph absent its versions section would not read as a pass either.
- `ldd-question`'s wording is the agent's in a real session; its choices are init.md's.
- No capture of a Claude Code session itself exists (the shell has no render function for it, and the real `claude` is not run for the media): the "two ways in" proof is the runtimes diagram. The dashboard's audit of an invented Claude Code transcript is the one view of such a session.
