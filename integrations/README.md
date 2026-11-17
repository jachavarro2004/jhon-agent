# Integrations

Each integration Jhon works with is described by one manifest,
`integrations/<name>/manifest.yaml`, schema `jhon.integration/v1` (T58a).
The engine reads them through `engine/internal/integrations` (compiled in by
`embed.go`) and names no integration itself; `jhon validate` refuses a
malformed manifest, naming the file and the field.

| Field | Meaning |
|---|---|
| `schema` | `jhon.integration/v1`; another major version is refused (below) |
| `name` | the folder's name, unique |
| `display` | the name the setup UI shows |
| `kind` | `mcp-stdio`, `mcp-http`, `mcp-connector` or `cli` |
| `order` | its place among the others (settings, setup rows, servers), unique |
| `setting` | the settings key that turns it off |
| `default` | the setting's default |
| `required` | `true`: the setting cannot be false; only Engram and CodeGraph are, and `jhon validate` refuses a set where they are not, or another one is |
| `when_off` | `refused` (required), `render` (the render drops its capabilities, server and body lines) or `launch` (the runtime's launch keeps it out); render, settings, setup, doctor, launch and the dashboard read it from here, so a new optional integration turns off with no code naming it |
| `capabilities` | the `prose/agents.yaml` capabilities whose tools are its own |
| `claude-code.servers` | its MCP server ids in Claude Code: a tool is `mcp__<server>__<tool>` |
| `pi.server`, `pi.command`, `pi.args`, `pi.url` | its server in pi's `mcp.json` |
| `pi.tools.<capability>` | a capability's tools in pi where pi's names differ; else Claude Code's under the pi server's prefix |
| `doctor.binary`, `doctor.version`, `doctor.source`, `doctor.probe`, `doctor.url` | what `jhon doctor` checks; `doctor.version` is the pin setup installs |
| `setup.summary`, `setup.summary_narrow`, `setup.detail`, `setup.locked_note` | the setup UI's row text |
| `setup.install.<go\|npm\|brew\|manual>` | the install commands, `{version}` standing for the pin |

The format is a small YAML subset: `key: value`, a flow list `[a, b]`, a
block indented two spaces, `#` comments, and a value in double quotes when
it holds `: ` or ` #`. A field v1 does not know is refused.

## Schema versions

A manifest names its schema's major version only: `schema:
jhon.integration/v<N>`. This binary reads `jhon.integration/v1`, and the
manifests it runs on are compiled into it, so a running Jhon only ever reads
its own; a newer manifest is met by `jhon validate` run from an older
binary on a newer tree.

- **Within v1** (no version change): add a manifest, an optional
  integration (`required: false`); change any value (a pin, a server, a
  capability, the order, the setup text); add an entry to an open block
  (`pi.tools.<capability>`, a `setup.install` way v1 knows). Every field
  keeps its name, type and meaning.
- **Needs v2**: add, remove or rename a field; change a field's type or
  meaning; add a value to a closed set (`kind`, `when_off`, a `setup.install`
  way) or give `doctor.probe` a meaning besides a version flag and `remote`; make another integration required. A v1 reader
  refuses an unknown field and an unknown value, so any of these would break
  it.
- **A binary that meets another major** refuses the manifest in `jhon
  validate`, naming the file, the field and the version it reads, and
  reads none of its other fields: update Jhon to the release that ships it.
  A value not of the form `jhon.integration/v<N>` is refused the same way.
  For Engram's manifest at v2 it prints:

  ```text
  integration      integrations/engram/manifest.yaml  schema: jhon.integration/v2 is another major version; this binary reads jhon.integration/v1 (integrations/README.md)
  ```
