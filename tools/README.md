# tools/

Development tools for this repository; nothing here is installed.

| Path | What it is |
|---|---|
| `bench/` | The benchmarks (T39): the same tasks run with Claude Code clean, Claude Code with Jhon and pi with the Jhon shell, in a dedicated bench HOME; each task's hidden acceptance test decides success. `go run ./tools/bench prepare|login-check|run|report`; see `bench/README.md`. Runs spend the owner's Claude quota: `--dry-run` first, and `--budget-points` stops them. |
| `coverage/` | The rule coverage matrix: `go run ./tools/coverage [filter]`. For every validation rule it injects one real defect into each layer the rule covers, expects `jhon validate` to report it there, and restores the tree with `git restore`. It needs a clean, committed tree. `jhon test` runs it last. |
| `release/repro.sh` | The reproducibility check of a release (T52): `tools/release/repro.sh <jhon> <version> <reference-dir>`, run in the checkout the reference was packed from, packs the same commit again (without re-running the Go tests) and compares each `jhon-<version>-linux-*` artifact with the reference byte for byte; any difference fails. The release workflow runs it after its pack, before signing. |
| `git-hooks/pre-commit` | Refuses a commit whose tree does not build or does not validate. Install it once per clone: `git config core.hooksPath tools/git-hooks`; `jhon doctor` reports whether it is active. |
| `git-hooks/pre-push` | When a push updates `main`, refuses it unless go vet and staticcheck pass on the whole module and the conformance suite `go test -p 4 ./test/...` passes; pushes of other branches run nothing. A sha that already passed them on this machine (a second remote, a re-push) is not checked again: it prints `pre-push: <sha> already passed; skipping`; the record is `.git/jhon/pre-push-passed`, keyed by the sha and a hash of the hook, and a failed run records nothing. Installed with the pre-commit hook (`core.hooksPath`). |
