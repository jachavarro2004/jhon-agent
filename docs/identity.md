# Jhon's identity

Every public Jhon repository (jhon-agent, jhon-studio, the profile
jachavarro2004, the landing, and any later one) carries the same identity.
This file is its one tracked source: a new repo copies these rules, and a
change to them lands here first. The rules come from
`odd/tasks/presentation-identity.md` (S1-S11).

## Mark

- The animated banner [`jhon-banner.gif`](assets/brand/jhon-banner.gif)
  (61818 bytes) opens every README, centred, alt "Jhon".
- Each repo keeps a byte-identical copy of jhon-agent's
  `docs/assets/brand/jhon-banner.gif`: at `docs/assets/brand/jhon-banner.gif`,
  or at `assets/brand/jhon-banner.gif` in the profile and the landing.
- jhon-studio puts the line "jhon-studio — presentation videos for any
  project" under it.
- The landing shows the same GIF as its brand mark in the hero.

```html
<p align="center">
  <img src="docs/assets/brand/jhon-banner.gif" alt="Jhon" width="760">
</p>
```

## Words

The tagline block, verbatim, right under the banner in every README and in
the landing's hero:

```html
<p align="center">
  <b>Let the agent run. Deliver only what is proven.</b><br>
  An agent framework for Claude Code and pi.
</p>
```

jhon-studio adds its own one-liner after the tagline.

## Look

Colours are rose ink, from
[`engine/internal/palette/palette.go`](../engine/internal/palette/palette.go):

| Role | Hex | palette.go |
|---|---|---|
| primary | `#e7bbc8` | `Fallback["primary"]` |
| fill | `#53343f` | `Fallback["primaryContainer"]` |
| ink | `#e9e0e1` | `Fallback["onSurface"]` |
| page | `#1f1f28` | `Page` |
| frame | `#161618` | `Frame` |

Fonts, everywhere:

| Use | Font |
|---|---|
| display | Fraunces |
| text | Space Grotesk |
| mono | FiraCode Nerd (`"FiraCode Nerd Font"`, then `"FiraCode Nerd Font Mono"`) |

The dashboard's [`app.css`](../dashboard/app.css) falls back to these
colours and fonts; the landing's `style.css` uses the same. jhon-studio's
default palette for its users' videos stays neutral: it is the product's
default for other projects, not Jhon's identity.

## Badges

One row under the tagline in every README, in this order: License
(Apache-2.0), CI, GitHub Sponsors, Stars, each for that repo (the profile's
row points at jhon-agent). Every badge is shields.io, `style=flat`, with
`color=e7bbc8&labelColor=1f1f28`. A repo with no CI workflow of its own uses
jhon-agent's `validate.yml`. Replace `<repo>` and `<workflow>`:

```html
<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/jachavarro2004/<repo>?style=flat&color=e7bbc8&labelColor=1f1f28" alt="License: Apache-2.0"></a>
  <a href="https://github.com/jachavarro2004/<repo>/actions/workflows/<workflow>"><img src="https://img.shields.io/github/actions/workflow/status/jachavarro2004/<repo>/<workflow>?branch=main&label=CI&style=flat&color=e7bbc8&labelColor=1f1f28" alt="CI"></a>
  <a href="https://github.com/sponsors/jachavarro2004"><img src="https://img.shields.io/github/sponsors/jachavarro2004?label=Sponsor&logo=githubsponsors&style=flat&color=e7bbc8&labelColor=1f1f28" alt="GitHub Sponsors"></a>
  <a href="https://github.com/jachavarro2004/<repo>/stargazers"><img src="https://img.shields.io/github/stars/jachavarro2004/<repo>?style=flat&color=e7bbc8&labelColor=1f1f28" alt="Stars"></a>
</p>
```

## Support block

Verbatim in every README (before `## License`), in every SUPPORT.md, and in
the landing's sponsor section. In a product repo, `<name>` is its own
jhon-* name (jhon-agent, jhon-studio), never bare "Jhon". The social repos,
jhon-site (README, SUPPORT.md and the page's sponsor section) and the profile
jachavarro2004, are not products and lead straight to jhon-agent: they say
"Support jhon-agent", with "repo" in the star line linked to
https://github.com/jachavarro2004/jhon-agent. There is no family section in
any repo. In
jhon-agent the block reads:

```markdown
## Support jhon-agent

jhon-agent is free and open source.
If it helps you, donate any amount you choose,
once or monthly, on GitHub Sponsors.
If you can't donate, a ⭐ on the repo helps
keep the project maintained.

[♥ Sponsor on GitHub](https://github.com/sponsors/jachavarro2004)
```

Sponsorship is a free donation only: no tier, no fixed amount, no price list
anywhere, monthly or one-time. A star is asked for plainly, with no reward.

## Community files

Every repo carries one copy of each (no shared `.github` repo):

| File | Rule |
|---|---|
| `.github/FUNDING.yml` | `github: [jachavarro2004]`, byte-identical to jhon-agent's [FUNDING.yml](../.github/FUNDING.yml) |
| `LICENSE` | Apache-2.0, Copyright 2026 Jhon Chavarro, byte-identical to jhon-agent's [LICENSE](../LICENSE) |
| `CODE_OF_CONDUCT.md` | Contributor Covenant 2.1, byte-identical to jhon-agent's [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) |
| `SECURITY.md` | private reporting; jhon-agent's [SECURITY.md](../SECURITY.md) |
| `SUPPORT.md` | where to ask, then the support block; jhon-agent's [SUPPORT.md](../SUPPORT.md) |
| `CONTRIBUTING.md` | code repos only, adapted to the repo; jhon-agent's [CONTRIBUTING.md](../CONTRIBUTING.md) |
| issue and PR templates | code repos only, adapted to the repo; jhon-agent's [`.github/ISSUE_TEMPLATE/`](../.github/ISSUE_TEMPLATE/) and [pull_request_template.md](../.github/pull_request_template.md) |

Files that are identical across repos are byte-identical copies of
jhon-agent's. The README's footer heading is `## License` everywhere (never
"Licences").

## Checking a repo against the kit

- The banner is the 61818-byte GIF and opens the README; the tagline block
  and the badge row follow it.
- No `## The Jhon family` section.
- `## Support <name>` (the product's jhon-* name; jhon-agent in jhon-site and
  the profile) is verbatim and sits right before `## License`.
- No `$` amount or tier appears in any README, SUPPORT.md or sponsor text.
- Every relative link and image points to a file that exists.

In jhon-agent, `go test ./test/hygiene/...` checks the README, SUPPORT.md
and this file, and `node --test dashboard/test/` checks `app.css`'s colours
and fonts.
