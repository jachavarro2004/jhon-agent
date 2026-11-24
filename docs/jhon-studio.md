# jhon-studio: presentation videos, an opt-in integration

[jhon-studio](https://github.com/jachavarro2004/jhon-studio) makes a narrated
presentation video (MP4, English and Spanish) of any project, from its README,
an LDD change or a release's notes. It is a program of its own repository;
Jhon uses it as an opt-in integration, off by default
(`integrations/studio/manifest.yaml`).

## Turn it on

Turn it on in `jhon setup` (the `jhon-studio` row, under the optional
integrations): setup installs it from its signed release at its Install step.
Or turn it on with `jhon settings set integrations.studio true` and run
`jhon update`, which installs it the same way. Then run
`jhon doctor`: its `jhon-studio` section says where it came from and shows each check of
`jhon-studio doctor --json` (node, ffmpeg, Kokoro, HyperFrames, Chrome) and
the fix for each one that fails. `jhon settings set integrations.studio false`
turns it off again. `jhon setup` writes `integrations.studio` only while it is
on and drops the line when you turn it back off, so a release older than the
integration, after `jhon update --rollback`, still reads the settings file
(with it on, turn it off before rolling back to such a release).

## Install and update from its releases

jhon installs jhon-studio from its signed GitHub releases
(github.com/jachavarro2004/jhon-studio, the manifest's `doctor.source`),
as it installs itself:

1. It reads the repository's releases and takes the newest one whose
   `requires_jhon` the running jhon satisfies ("The jhon it needs", below):
   it downloads `jhon-studio-<version>.tar.gz` with its `.sha256` and
   `.sigstore.json`, checks the sha256, then the cosign keyless signature:
   it must come from that repository's `.github/workflows/release.yml` at
   the tag `v<version>`, through GitHub's OIDC issuer. It then unpacks the
   tarball into a temporary folder and asks the unpacked program's
   `integration --json`, which must name the release's version. A newer
   release that needs a newer jhon is passed over for the next older one.
2. It unpacks it into `~/.local/share/jhon-studio/<version>` and links
   `~/.local/bin/jhon-studio` to `<version>/bin/jhon-studio` atomically.
   The version that was linked before stays on disk.

Pre-releases (`-rc.N`) are taken only by a jhon that is itself a
pre-release or a development build; a stable jhon takes stable releases
only. Nothing unverified is installed: a missing or failing checksum or
signature, a tarball with an absolute or `..` path, a link (symbolic or
hard) or a device, or a program that answers another version stops the
install with one message ending in `nothing was changed`. A
`~/.local/bin/jhon-studio` that jhon did not link (a file, or a link
elsewhere) is never replaced: move it away for jhon to install.

`jhon update` updates it after jhon itself (`--check` only says whether a
newer release that supports this jhon is out; `--json` lists it under
`tools`), and `jhon update --rollback` links its previous version again,
with jhon's. `jhon doctor` says which release is installed and where;
`jhon doctor --remote` also says whether a newer one that supports this jhon
is out. To install a release by hand, follow jhon-studio's README
("Install").

### A development clone

To work on jhon-studio itself, clone it instead (jhon-studio's README,
"Development"): a git checkout at `~/.local/share/jhon-studio` is a
development install. `jhon setup` and `jhon update` leave it alone, never
replacing it, and `jhon doctor` reports it as a development install. Move it
away for jhon to install releases there.

## The command

| Runtime | Command | Where it comes from |
|---|---|---|
| Claude Code | `/jhon-studio:video [<source>]` | jhon-studio's Claude Code plugin: `jhon start` adds `--plugin-dir <plugin_dir>`, the folder `jhon-studio integration --json` names |
| pi | `/jhon:video [<source>]` | the shell registers it; at each use it reads the skill `jhon-studio integration --json` names and sends its body, with the arguments after it as `ARGUMENTS: <source>` |

Both run the same skill: check the tools, write `.jhon/studio.yaml`, draft the
brief, write the script, show you a preview and wait for your approval before
the render. `<source>` is optional: `readme` (the default), `ldd:<change>`,
`release:<tag>` or a file.

You need not name the command: with jhon-studio on, the main session's text
(lean and full, both runtimes) says that a request for a video, or
"jhon:video", means this skill, and points to this page, which every install
carries at `~/.local/share/jhon/current/docs/jhon-studio.md` (the rest of
`docs/` stays in the repository). In pi the agent follows the skill by
reading the `skill` file `jhon-studio integration --json` names. The text
is rendered at `jhon sync` (setup runs it): after `jhon settings set
integrations.studio true|false`, run `jhon sync` for the sentence to come or
go.

With Jhon's sandbox on (`sandbox.mode`), the skill's commands run as every
other command of the agent, inside it: jhon-studio reads its install and
writes in the project's `.jhon/` and the cache. Chrome inside the sandbox,
for web screens, is jhon-studio's own work (its T9).

## The jhon it needs

jhon-studio says what jhon it runs with in `jhon-studio integration --json`:
`requires_jhon` (a SemVer range, `">=0.5.0-rc.1"` for v0.1.0) and `contract`
(`jhon.integration/v1`); `integrations/README.md` ("The jhon a tool needs")
has the rules. When the running jhon is older than its minimum, or the
contract is not one jhon speaks, or its range is one jhon cannot read
(plain `>=`, `>`, `<=`, `<`, `=` on full x.y.z versions, spaces for AND and
`||` for OR; no `^`, `~` or `x`), jhon-studio is off for that run with one
note, and Jhon works as it does with it off:

```text
jhon: jhon-studio 0.2.0 requires jhon >=0.6.0, and this is jhon 0.5.0-rc.1: jhon-studio is off for this run; update jhon (jhon update), or install a jhon-studio version that supports jhon 0.5.0-rc.1
```

`jhon start` loads no plugin, pi has no `/jhon:video`, `jhon setup` shows the
note under the `jhon-studio` check, and `jhon doctor`'s `jhon-studio` section
shows it as a warning instead of the checks. With a version that fits, doctor
says what it requires and the running jhon; an older jhon-studio that declares
nothing is accepted, and doctor notes that it declares no minimum. A
development build of jhon (`0.0.0-dev+<commit>`, from `jhon sync`) satisfies
any minimum, and doctor says it was not checked. The version in
`integrations/studio/manifest.yaml` is the one Jhon was tested with: doctor
shows `jhon-studio <installed> (tested with <version>)`, never as a
requirement.

## While it is off, or not answering

Off (the default), nothing about video is shown or loaded: `jhon start` adds
no plugin and never runs `jhon-studio`, pi has no `/jhon:video`, `jhon doctor`
has no `jhon-studio` row, and `jhon setup` checks no `jhon-studio`.

On but not answering (not on PATH, `jhon-studio integration --json` fails, or
it hangs past 10 s, when it is stopped with every process it started),
`jhon start` still starts Claude Code, without the plugin, and prints one note:

```text
jhon: integrations.studio is on, but jhon-studio is not on PATH; this session starts without its plugin
```

and pi's `/jhon:video` says why, with the fix (`jhon doctor`, or turning it
off). The dashboard's integrations card shows it `off`, `unavailable` (on, not
on PATH) or `on`.
