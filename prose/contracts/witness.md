# Witness

For behavior that already exists, the witness is a mutation: break the code
temporarily, see the test fail, restore, see it pass. Run it with
`jhon mutate <file> --find <text> --replace <text> -- <test command>`, which
restores the file itself, not git. A new test observed
RED needs no mutation. A witness may be reported as:

```text
test: <file> — <test name>
witness: {RED|mutation}
command: <the exact command run>
failure: <the observed failing line>
green: <command>: <observed passing result>
```

Inside a project with `openspec/config.yaml`, `strict_tdd` in
`jhon status --json` reports the project's preference
(`strict_tdd_policy: require|forbid`, else whether its projects
are testable).
