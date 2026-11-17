# Integrations

Each integration Jhon works with is described by one manifest,
`integrations/<name>/manifest.yaml`, schema `jhon.integration/v1` (T58a).
The engine reads them through `engine/internal/integrations` (compiled in by
`embed.go`) and names no integration itself; `jhon validate` refuses a
malformed manifest, naming the file and the field.

| Field | Meaning |
|---|---|
| `schema` | `jhon.integration/v1`; another version is refused |
| `name` | the folder's name, unique |
| `display` | the name the setup UI shows |
| `kind` | `mcp-stdio`, `mcp-http`, `mcp-connector` or `cli` |
| `order` | its place among the others (settings, setup rows, servers), unique |
| `setting` | the settings key that turns it off |
| `default` | the setting's default |
| `required` | `true`: the setting cannot be false (required for now) |
| `when_off` | `refused` (required), `render` (the render drops its capabilities, server and body lines) or `launch` (the runtime's launch keeps it out) |
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
