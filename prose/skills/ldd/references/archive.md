## Archive

Archive only a change with a folder and its RED before GREEN on record
(`red_before_green` in `jhon status --json`). A change with no folder has
nothing to archive: its commit or PR text and its runs are its record.

Close a change: merge its delta specs into the main specs, then move it into
the archive. File content never passes through the model: compose and move
with the tools below, never by reading and rewriting files.

1. `jhon status {change-name} --json`: record in the archive report, as
   facts, any unchecked task and a verification that did not pass. Report the change as it is at close, not as an earlier report
   described it.
2. For each delta spec with a main spec, run
   `jhon compose "openspec/specs/{domain}/spec.md" "openspec/changes/{change-name}/specs/{domain}/spec.md"`.
   A nonzero exit wrote nothing: report `blocked` with its stderr, and do
   not archive.
3. For a domain without a main spec, copy the file with the shell and check
   it:
   ```bash
   mkdir -p "openspec/specs/{domain}"
   cp "openspec/changes/{change-name}/specs/{domain}/spec.md" "openspec/specs/{domain}/spec.md"
   diff "openspec/changes/{change-name}/specs/{domain}/spec.md" "openspec/specs/{domain}/spec.md"
   ```
4. From the project root, run
   `jhon archive "{change-name}" "YYYY-MM-DD"`
   with today's date. It refuses an existing destination, moves with
   `git mv` or `mv`, and checks the result with `diff -r`. Include its
   `method:` and `readback:` lines, or its stderr, verbatim; a failure is
   final. Its last line suggests a fresh session for the next change: pass
   it to the owner once.
5. Write the archive report in
   `openspec/changes/archive/{YYYY-MM-DD}-{change-name}/`; save to Engram
   only a non-obvious decision.

Archive after the change's code is merged. The archive commit may ride
with other work or go on its own; how it is delivered is the owner's call.
