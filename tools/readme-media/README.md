# tools/readme-media — the README's images, made by code

Every image the README shows is made here by code: no screen recording. The
images are SVG, rasterised with `resvg` and encoded with `ffmpeg`; the
terminal captures are Jhon's own shell components rendered through their
real render functions, so a capture changes when the shell does. They import
the shell and the engine, so they stay in the core.

Jhon's videos are not made here: [jhon-studio](https://github.com/jachavarro2004/jhon-studio)
makes presentation videos for any project, Jhon's own among them (an opt-in
integration: `/jhon-studio:video` in Claude Code, `/jhon:video` in pi).

## Rebuild the images

```sh
node --experimental-strip-types tools/readme-media/build.ts
```

rebuilds the images into `docs/assets/`:

| asset | what |
| --- | --- |
| `brand/jhon-banner.gif` | the animated banner: the wordmark, one line saying what Jhon is, the LDD status typing itself |
| `features/*.png` | the shell's Prism review card and the engine's real output: `prism-review`, `audit` (the captions in `build.ts` name the captures written; the others are rendered and not written, `ldd-status` feeding the banner) |
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
(`npx --yes hyperframes@0.9.58 browser ensure`).

Tests (the ANSI-to-SVG conversion, the determinism of the image build and of
the dashboard's capture (skipped without chrome-headless-shell), and the
diagrams' tokens and bytes):

```sh
node --experimental-strip-types --test tools/readme-media/test/*.test.ts
```

## Identity

Jhon's own look, in one place: `identity.ts`. Every asset here draws from
it; nothing here imitates another project's media.

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
  and scenes change under a flat rose band that sweeps across (the banner).
  Ease-out arrivals, no blur, no gradients, no fades between scenes.

## Files

- `build.ts` — the one command for the images; the feature captions.
- `identity.ts` — fonts, wordmark, kicker, spacing, motion.
- `diagrams.py` — the four diagrams (`python3 -I tools/readme-media/diagrams.py
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
