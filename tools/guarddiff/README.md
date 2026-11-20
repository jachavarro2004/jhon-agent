# tools/guarddiff

The guards' verdict differential (T128f). It feeds every line of a checked-in set to the guards of two jhon
binaries, as Claude Code calls them, and prints each verdict that differs. It is the gate for moving guard code
into the sandbox: every changed verdict must be listed and accepted, and the pre-push hook runs it against the
last release.

```sh
go run ./tools/guarddiff --old <jhon> [--old-root <tree>] --new <jhon> [--new-root <tree>] [--allow tools/guarddiff/allow.tsv]
go run ./tools/guarddiff gen-fuzz      # rewrites test/guards/corpus/fuzz.tsv from its seed
```

Each changed verdict prints as `id  guard  old -> new  allowed|NOT ALLOWED  reason`, then the new binary's verdict
counts and `guarddiff: <n> lines, <k> changed verdicts (<a> allowed, <u> not on the allow list)`. Exit 1 when a
changed verdict is not on the allow list, 2 when the run cannot be made, 0 otherwise.

- **Calls.** Per line, each binary gets one `jhon hook pre-tool --agent '*'` call naming every guard whose
  matcher (`jhon hook pre-tool --matchers`) takes the tool: safety, vcs, framework, secrets, scope and
  route-guard, with the arguments the Claude Code render gives them. When it does not allow, each guard is
  called alone, route-guard through `jhon hook route-guard`. A verdict is `allow`, `ask`, `refuse` (exit 2 or a
  deny) or `error`. `sandbox.mode` is off for both: the sandbox is no guard.
- **Fixtures.** Built under the user cache directory (`--scratch`) and removed after the run: per place a
  project (`proj`), a repository beside it (`<OTHER>`), a TMPDIR (`<TMP>`) and a HOME holding the framework's
  install (`<HOME>`, `<FW>`), from `test/guards/corpus/trees.txt`. Places: `repo` (a git repository with an
  uncommitted behaviour change), `ldd` (the same with LDD and the change's record), `folder` (no repository),
  `corpus` (corpus_test.go's tree); `ldd` and `repo` take a mode (`ldd:yolo`, `repo:super-yolo`), which each
  binary sets itself with `jhon mode`. Callers: `top-default`, `top-bypass`, `subagent` (a `worker`, jhon-builder's old name, which both binaries read, launched
  with the surfaces `src/a.ts`, `src/parse/`, `svc/src/**`, so scope-guard judges it).
- **The set** (`test/guards/corpus/*.tsv`): the corpus (8 columns, corpus_test.go; a safety-guard line runs at
  the top level, the others in the sub-agent), and 6-column lines `place, fixture, caller, tool, input, source`
  where a place or caller is a name, a comma list or `*` (every one) and the input is Go-quoted (a Bash command,
  else the tool input as JSON, with the placeholders above): `matrix.tsv` (the T59 matrix,
  TestGuarddiffMatrixRows), `rows.tsv` (route-guard and scope-guard) and `fuzz.tsv` (generated, never edited).
  A line's id is `file:line`, with `@place/caller` for a 6-column one.
- **The allow list** (`allow.tsv`): `id, guard, old, new, why`, tab-separated; `*` matches any value.
- **The baseline** (`baseline.txt`): v0.3.7 against the head this tool landed on: no changed verdict.
