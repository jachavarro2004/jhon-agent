# jhon-studio: presentation videos, an opt-in integration

[jhon-studio](https://github.com/jachavarro2004/jhon-studio) makes a narrated
presentation video (MP4, English and Spanish) of any project, from its README,
an LDD change or a release's notes. It is a program of its own repository;
Jhon uses it as an opt-in integration, off by default
(`integrations/studio/manifest.yaml`).

## Turn it on

Install it (its README has the details):

```sh
git clone https://github.com/jachavarro2004/jhon-studio ~/.local/share/jhon-studio && ln -s ~/.local/share/jhon-studio/bin/jhon-studio ~/.local/bin/jhon-studio
```

then turn it on in `jhon setup` (the `jhon-studio` row, under the optional
integrations) or with `jhon settings set integrations.studio true`, and run
`jhon doctor`: its `jhon-studio` section shows each check of
`jhon-studio doctor --json` (node, ffmpeg, Kokoro, HyperFrames, Chrome) and
the fix for each one that fails. `jhon settings set integrations.studio false`
turns it off again. `jhon setup` writes `integrations.studio` only while it is
on and drops the line when you turn it back off, so a release older than the
integration, after `jhon update --rollback`, still reads the settings file
(with it on, turn it off before rolling back to such a release).

## The command

| Runtime | Command | Where it comes from |
|---|---|---|
| Claude Code | `/jhon-studio:video [<source>]` | jhon-studio's Claude Code plugin: `jhon start` adds `--plugin-dir <plugin_dir>`, the folder `jhon-studio integration --json` names |
| pi | `/jhon:video [<source>]` | the shell registers it; at each use it reads the skill `jhon-studio integration --json` names and sends its body, with the arguments after it as `ARGUMENTS: <source>` |

Both run the same skill: check the tools, write `.jhon/studio.yaml`, draft the
brief, write the script, show you a preview and wait for your approval before
the render. `<source>` is optional: `readme` (the default), `ldd:<change>`,
`release:<tag>` or a file.

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
