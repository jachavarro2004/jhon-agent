# tools/media — the README's media, made by code

Every image and video the README shows is made here by code: no screen
recording. The images are SVG, rasterised with `resvg` and encoded with
`ffmpeg`; the terminal captures are Jhon's own shell components rendered
through their real render functions, so a capture changes when the shell
does. The presentation video is a [HyperFrames](https://github.com/heygen-com/hyperframes)
composition (HTML moved by Jhon's own motion engine, `hyperframes/motion/motion.js`,
and rendered to video by a headless Chrome), in `hyperframes/presentation/`.

## Rebuild the images

```sh
node --experimental-strip-types tools/media/build.ts
```

rebuilds the images into `docs/assets/`:

| asset | what |
| --- | --- |
| `brand/jhon-banner.gif` | the animated banner: the wordmark, one line saying what Jhon is, the LDD status typing itself |
| `features/*.png` | the shell's views and the engine's real output: `shell-bar`, `ldd-status`, `agents-jobs`, `ldd-question`, `guard-refusal`, `route-refusal`, `prism-review`, `executions-red-green`, `mode-grant`, `audit` |
| `features/dashboard-audit.png` | the dashboard's page for one session: the engine's audit of an invented Claude Code session (`dashboard.ts`), drawn by headless Chrome at 1600×760, 2× |

Options: `--out <dir>` writes the same tree elsewhere; `--only features,banner,dashboard`
builds some parts. Two runs give byte-identical files (fixed clock and data,
the engine's fixed palette, no metadata, one encoder thread; the dashboard's
page from the same session, the same Chrome and the same fonts).

Needs: Node 22+ (`--experimental-strip-types`), Go (the build compiles the
engine to ask it for rose ink and to audit the dashboard's session), `resvg`,
`ffmpeg`, the shell's `node_modules` (`npm ci` in `shell/`, or
`sh tools/shell-deps.sh`), and the fonts named below installed. The
`dashboard` part also needs chrome-headless-shell: `$JHON_MEDIA_CHROME`, else
the one the HyperFrames CLI keeps in `~/.cache/hyperframes/chrome`
(`npx --yes hyperframes@0.9.58 browser ensure`, `hyperframes/README.md`).

## The presentation video

| asset | what |
| --- | --- |
| `video/jhon-explainer.mp4` | the presentation: about 3.4 minutes, 1920×1080, 30 fps, narrated in English (AAC) |
| `video/jhon-explainer.es.mp4` | the same video and narration, with Spanish subtitles |

The films are rendered into `docs/assets/video/` but not kept in git (`.gitignore`);
they are published on the landing page (github.com/jachavarro2004/jhon-site) and in
the README's players.
| `video/stills/*.png` | six of its shots, decoded from the English video |

It is made and rendered with HyperFrames, not by `build.ts`: its script, its
composition, its narration and how to render it are in
`hyperframes/README.md`.

Tests (the ANSI-to-SVG conversion, the determinism of the image build and of
the dashboard's capture (skipped without chrome-headless-shell), the
diagrams' tokens and bytes, the presentation's narration cache and
Spanish subtitles covering every line, the compositions' screens loading
from `docs/assets`, and the motion engine: its eases, seek-order
independence and HyperFrames contract):

```sh
node --experimental-strip-types --test tools/media/test/*.test.ts
```

## The narration (Kokoro)

The video has one narration, in English, spoken by Kokoro-82M, an
open-weights text-to-speech model run locally through `kokoro-onnx` (no API,
no cost), in the one voice `identity.ts` names (`VOICE`): `af_heart`. Both
videos carry it; the Spanish one shows Spanish subtitles over the same
English audio (a Spanish voice is not made: it did not sound natural).
`hyperframes/presentation/narrate.mjs` speaks each line of the script
(`script.mjs`; `spoken` says names as they are heard, e.g. Jhon as «John»,
`pi` as «pie») through `kokoro.py`, and keeps each line's audio in the
repository, under `hyperframes/presentation/narration/<key>.opus` (mono
Opus, 32 kb/s) with `<key>.json` (its text and seconds). The key hashes the
spoken text, model, voice, phoneme language and speed: a cached line is
never spoken again, so a machine without Kokoro renders the same video from
the cache; files no line uses any more are removed.

Kokoro lives outside the repository, under `~/.cache/jhon-media/kokoro`
(or `$JHON_MEDIA_KOKORO`), about 500 MB in all:

```sh
K=~/.cache/jhon-media/kokoro
uv venv --python 3.12 $K/venv && VIRTUAL_ENV=$K/venv uv pip install kokoro-onnx   # ~159 MB
mkdir -p $K/model && cd $K/model
R=https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.0
curl -LO $R/kokoro-v1.0.onnx    # 325,532,387 bytes (the int8 one is 92,361,271)
curl -LO $R/voices-v1.0.bin     # 28,214,398 bytes
```

Speaking all 31 lines takes about a minute of CPU.

## Identity

Jhon's own look, in one place: `identity.ts`. Every asset and the presentation
video draw from it; nothing here imitates another project's media.

- **Colour** — rose ink, Jhon's one colour source, asked of the engine
  (`jhon palette --pi` and `--json`, `palette.ts`) with the owner's settings
  kept out, so the media always wear the fixed values: the frame gray
  (`frame`) behind everything, the blue ink page (`page`) of a terminal, rose
  (`accent`) for focus and kickers, its fill (`accentFill`) for the seal and
  the transition band, `ink`/`soft` for text, and the fixed state colours
  (`ok`, `warn`, `bad`).
- **Type** — the dashboard's: *Fraunces* for the wordmark and titles (its
  italic in rose, as the dashboard sets its brand's `<em>`), *Space Grotesk*
  for captions and body, *FiraCode Nerd Font Mono* for terminal frames and
  kickers, *Noto Sans CJK JP* for the seal. SVGs name the families; fonts
  are never embedded. Kickers are mono, 600, spaced capitals (the dashboard's
  `.kick`).
- **Wordmark** — the seal, a square of rose fill with 力 (the shell's brand
  glyph, it reads «jhon») and a hard offset shadow, then «Jh*on*».
- **Shape and spacing** — a 4 px grid, square corners everywhere, flat fills,
  the dashboard's hard 3 px shadow (scaled with the asset), one-pixel rules.
  A terminal panel is a page-coloured sheet with a rose rule (`frameSoft`)
  and a head band carrying the small seal and `JHON · <view>`; no window
  chrome, no traffic lights.
- **Motion — "ink and stamp"** — the seal stamps down (lifted and larger,
  then landing as its shadow appears), text is inked left to right as a
  terminal types (a rose block cursor), boxes stamp in one after another,
  and scenes change under a flat rose band that sweeps across. Ease-out
  arrivals, no blur, no gradients, no fades between scenes.

## Files

- `build.ts` — the one command for the images; the feature captions.
- `identity.ts` — fonts, wordmark, kicker, spacing, motion, the voice.
- `kokoro.py` — speaks lines with Kokoro (run by the presentation's `narrate.mjs`).
- `diagrams.py` — the four diagrams (`python3 -I tools/media/diagrams.py
  docs/assets/diagrams`), in identity's tokens, 1280×720, no text under
  14 px; `test/diagrams.test.ts` checks tokens and bytes.
- `palette.ts` — rose ink from the engine.
- `captures.ts` — the shell's views rendered with fake data (invented
  project, files and times; the shell's default settings; a fixed clock),
  painted by pi's own `Theme` loaded from rose ink in truecolor; the Prism
  review card from `jhon_review`'s result details.
- `dashboard.ts` — the dashboard's page for one invented Claude Code session:
  its transcript and project written in a sandbox, `jhon audit <session>
  --html` (this checkout's engine and `dashboard/`), headless Chrome.
- `ansi-svg.ts` — painted terminal rows (SGR escapes) to SVG text on a cell
  grid: each ASCII character placed on its cell (so no ligature forms and no
  font advance drifts), wide glyphs on two cells.
- `frames.ts` — the terminal panel and the feature frame.
- `banner.ts` — the banner.
- `raster.ts` — `resvg` and `ffmpeg`, with the flags that keep bytes stable.
- `hyperframes/` — the presentation video and its motion engine (its own README).

Every claim the video makes must be true to the README, `docs/ldd.md`,
`docs/modes-and-grants.md` or `jhon help`: never a command or flag they do
not name.
