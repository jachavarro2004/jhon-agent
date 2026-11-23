# Certification of v0.4.0

## Verdict

v0.4.0 ships. `bench certify` said FAIL on both profiles (jhon: 9 of 20
metrics, jhon-nan: 5 of 20). I read every failed metric run by run, and
none is a regression of v0.4.0 that is still open:

- Three are cost or scope that the owner accepted on record: the cost of the
  verified route on `contacts-migration`, the schema docs that task writes,
  and jhon-nan's active time against v0.3.7 runs that failed early.
- The others are single runs, guard rules v0.3.7 already had, or bookkeeping
  edits after the last test run.
- All five gates passed on both profiles: the mode matrix, the Go suite, the
  shell deps, the shell suite and tsc.

The first certification found one real regression (T208), one bench gap
(T209) and one guard false positive (T210). All three were fixed before the
tag, and the cells they touched were measured again.

The owner's decisions, word for word:

| Failure | Decision | Log |
|---|---|---|
| Cost of `contacts-migration` on the verified route | "Aceptarlo como costo por diseño" (accept it as a cost by design) | L1644 |
| Schema docs written on `contacts-migration` (out of scope) | "Sí, documentar el esquema es correcto" (yes, documenting the schema is right) | L1644 |
| jhon-nan active time, and the tag | "Sí, acepto y saca el tag v0.4.0" (yes, I accept; cut the v0.4.0 tag) | L1664 |

The `Lnnnn` references point to entries of the maintainer's plan log; the
`Tnnn` ones are its tasks.

## What was certified

| | |
|---|---|
| Candidate | `b4d0e53` (main with T208, T209 and T210) |
| Baseline | v0.3.7, `76709b2` |
| Profiles | `jhon`: Claude Code on `opus`, pi on `claude-bridge/claude-opus-5-5@high`. `jhon-nan`: Claude Code on `opus`, pi on `nan/deepseek-v4-flash@high` |
| Runtimes | Claude Code (`jhon` condition) and pi (`pi-jhon` condition) |
| Tasks | 10: account-import, contacts-migration, invoice-slices, late-fee, pagination-bug, search-accents, shipping-rules, slow-suite, two-modules, workspace-budgets (the long pnpm workspace task, new in v0.4) |
| Variants | 4: plain (Jhon sets LDD up), ldd, ldd-native, ldd-ask. v0.3.7 has no ldd-native or ldd-ask, so those cells are compared with its ldd cell and judged on the absolute rules |
| Repeats | 3 valid runs per cell: 80 candidate cells (240 runs) and 40 baseline cells (120 runs) per profile |
| Gates | mode matrix, go suite, shell deps, shell suite, tsc (`bench certify --gates`) |

The other checks before the tag:

- guarddiff against v0.3.7 passed: 4618 lines and 1032 changed verdicts, all
  on the allow list. After T210 only its 9 new verdicts changed, all of them
  allow-listed.
- `jhon doctor` passed.
- The installers passed 34 of 34 (`install.ps1` is checked statically here;
  PowerShell runs in CI).
- The audit of the owner's real sessions passed.
- Release pack and reproducibility passed on a clean clone at `a16b9a5`:
  both the tarball and the SBOM pack to the same bytes again.

## How it ran

1. **Baselines.** v0.3.7 ran for both profiles: 3 repeats, every variant it
   has, parallel 2.
2. **Candidates.** main ran for both profiles, at `a16b9a5`.
3. **Environment failures.** Three things broke the runs, and none of them
   came from Jhon:
   - The laptop suspended for about 4 h 20 min. Go timers count only awake
     time, so the whole bench paused. The 2 runs that span the suspend were
     set aside.
   - A network outage made 38 runs fail with
     `API Error: Can't reach the API server ... EAI_AGAIN`.
   - The runner died with the session that started it, after 186 candidate
     runs.

   Every such run was **set aside, not deleted**: it was moved to a
   `runs-set-aside/` folder, and `why.txt` says why (`network` or
   `suspend`). Each cell was then filled to exactly 3 valid runs, one run
   at a time, in fresh results folders, under `systemd-inhibit` so the
   laptop could not sleep. `bench run` refuses an existing results folder,
   so the re-runs could not resume in place.
4. **First certification** (candidate `a16b9a5`). jhon failed 10 of 20
   metrics and jhon-nan 7 of 20. Classifying those failures found T208,
   T209 and T210.
5. **Fixes and re-measure.** T208, T209 and T210 were merged; main became
   `b4d0e53`, which was installed in the bench HOME. 120 runs were done
   again. The old runs of those cells were set aside under
   `runs-set-aside-t208/`, never deleted.

   | Side and profile | Cells re-run | Runs | Hidden checks passed |
   |---|---|---|---|
   | candidate jhon | contacts-migration, every variant and condition (T208) | 24 | 24 |
   | candidate jhon | invoice-slices and slow-suite pi-jhon, every variant (T209) | 24 | 24 |
   | baseline jhon | invoice-slices and slow-suite pi-jhon, plain and ldd (T209 hit v0.3.7 too) | 12 | 12 |
   | candidate jhon-nan | contacts-migration (T208) | 24 | 24 |
   | candidate jhon-nan | invoice-slices and slow-suite pi-jhon (T209) | 24 | 24 |
   | baseline jhon-nan | invoice-slices and slow-suite pi-jhon | 12 | 3 |

   The other cells keep their `a16b9a5` runs. Between `a16b9a5` and
   `b4d0e53` the changes were T208 (the record's history), T209 (bench
   only), T210 (9 guard verdicts) and docs.
6. **Second certification (v2).** `bench certify --gates` ran per profile,
   with `TMPDIR` outside `/tmp` (see the gate's TMPDIR below). Its results
   follow.

## Results (v2)

From each profile's `certify.json`. A metric counts cells (80 per profile)
unless it says otherwise. The cost limit is 1.15 times the baseline's
geometric mean. The pooled metrics fail only when a one-sided Fisher test
gives p < 0.05.

| Metric | jhon | jhon-nan |
|---|---|---|
| repeats | PASS, 0 of 80 | PASS, 0 of 80 |
| final checks | PASS, 0 of 80 | PASS, 0 of 80 |
| per-message checks | PASS, 0 of 80 | PASS, 0 of 80 |
| regressions | PASS, 0 of 80 | PASS, 0 of 80 |
| rework | **FAIL**, 7 of 80 | **FAIL**, 1 of 80 |
| red before green | PASS, 240/240 vs 240/240, p=1 | PASS, 238/240 vs 224/240, p=1 |
| tests run before end | **FAIL**, 235/240 vs 240/240, p=0.031 | PASS, 218/240 vs 208/240, p=0.944 |
| out of scope | **FAIL**, 7/240 runs vs 0/240, p=0.007 | PASS, 1/240 vs 0/240, p=0.5 |
| refusals and declined dialogs | **FAIL**, 4 of 80 | **FAIL**, 5 of 80 |
| plain sets up ldd with a record | **FAIL**, 1 of 20 | PASS, 0 of 20 |
| ldd-ask layout and record | PASS, 0 of 20 | **FAIL**, 1 of 20 |
| ldd-native active with a record | **FAIL**, 1 of 20 | **FAIL**, 1 of 20 |
| quota-weighted tokens | **FAIL**, 1.30 | PASS, 1.13 |
| turns | **FAIL**, 1.17 | PASS, 1.12 |
| active time | **FAIL**, 1.17 | **FAIL**, 1.32 |
| gate mode matrix | PASS | PASS |
| gate go suite | PASS | PASS |
| gate shell deps | PASS | PASS |
| gate shell suite | PASS | PASS |
| gate tsc | PASS | PASS |
| **Verdict** | **FAIL, 9 of 20** | **FAIL, 5 of 20** |

## Each failed metric

### Cost: tokens 1.30, turns 1.17, active time 1.17 (jhon)

- **Where:** four `contacts-migration` cells: ldd and ldd-native, on
  Claude Code and on pi. They cost 3.4 to 6 times the baseline's tokens,
  2.3 to 3.2 times its turns and 3.5 to 5 times its active time.
- **Why:** the task's root migration is high risk (T148), so v0.4 takes the
  verified route and delegates to a checker and a Prism review. v0.3.7 did
  not read that migration as high risk, so it did neither.
- **Evidence:** without those four cells the geometric means are 1.13,
  1.07 and 1.01, all under 1.15.
- **Class:** cost by design.
- **Decision:** "Aceptarlo como costo por diseño".

### Active time 1.32 (jhon-nan)

- **Where:** the same four `contacts-migration` cells (3.2 to 4.5 times;
  they wait on the checker and the Prism agents), plus invoice-slices ldd
  and ldd-native on pi.
- **Evidence:**
  - Without contacts-migration the mean is 1.18.
  - Without the two invoice-slices pi cells as well, it is 1.09.
  - Those cells are compared with v0.3.7 runs that all failed early: 0 of
    6 hidden checks passed, in 1 to 2.5 min. v0.4 passes all of them, in
    5 to 17 min, with 2 to 6 sub-agents.
- **Class:** cost by design, plus a baseline that is faster because it
  fails.
- **Decision:** "Sí, acepto y saca el tag v0.4.0".

### Out of scope: 7 of 240 runs (jhon)

- **README.md, 5 runs** (contacts-migration): the agent documents the
  schema it changed. Allowed: "Sí, documentar el esquema es correcto".
- **`.codegraph/.gitignore`, 1 run** (workspace-budgets ldd-ask pi): a
  code-index tool's own folder. The same decision says to ignore
  `.codegraph/` in the task templates; that change is not in v0.4.0.
- **`.gitignore`, 1 run:** a `*.bak` line. It happened in one run.

jhon-nan's single out-of-scope run (1/240) passes the metric.

### Tests run before end: 235 of 240 (jhon)

- **Where:** one run each in contacts-migration ldd (Claude Code and pi),
  contacts-migration ldd-native pi, two-modules ldd pi and
  workspace-budgets ldd-native pi.
- **Class:** bookkeeping, not untested code. The last edits were records
  (`verify-report.md`, tasks.md ticks, the TDD evidence), made after the
  last test run.
- **Evidence:** without the accepted contacts-migration runs it is 238 of
  240.

### Refusals and declined dialogs (jhon 4 of 80, jhon-nan 5 of 80)

Each failing cell has a refusal in 1 of its 3 runs. The exception is jhon's
contacts-migration ldd-native on Claude Code, with a refusal in 2 of its 3
runs.

- **Class:** guard rules v0.3.7 already had, refusing what they should:
  `rm -r $(...)`, and `git checkout --` / `git restore` used to undo work.
- **The undo:** on contacts-migration ldd-native, after the high-risk
  notice, the agent tries to undo its work and the guard refuses. That is
  right, and T211 finds what pushes the agent toward the undo.
- **The T210 run:** jhon-nan's workspace-budgets ldd-native run is the
  false positive T210 fixed. That cell was not re-run, so its run from
  `a16b9a5` still counts.

### Records: plain, ldd-ask and ldd-native (1 of 20 each)

- **contacts-migration ldd-native (Claude Code), both profiles:** the
  record is missing in 1 of 3 runs. After the guard refused the undo, the
  agent stopped without the folder record (T211).
- **jhon's workspace-budgets plain and jhon-nan's workspace-budgets ldd-ask
  pi:** single runs, read as noise.

### Rework (jhon 7 of 80, jhon-nan 1 of 80)

Rework is an edit of a file that a passing test run already covered, within
the same owner message.

- **Most cells:** single runs, or a checker's findings that the agent acted
  on.
- **jhon's workspace-budgets ldd:** rework in 3 of 3 runs. T211 reads the
  checker findings behind it.

## Issues found and fixed before the tag

- **T208, real regression.** A change lost its RED when it got its folder
  mid-change. Every red-before-green failure of the first certification was
  contacts-migration. The folder's record now reads the runs recorded
  before the folder, but only while the checked-out branch names the
  change. An independent verify found that a RED from another branch could
  count; the fix round closed it. Red before green went from 228/240 to
  240/240 (jhon).
- **T209, bench gap.** The pi driver ended a message while a background
  sub-agent still ran, so it killed pi before the checker finished. It hit
  v0.3.7 too, so both sides were re-measured.
- **T210, guard false positive.** The safety guard took a module a test
  imports (`../src/main.ts`) for a file the test deletes. v0.3.7 refused it
  too, so it was an old bug, not a regression. An adversarial verify found
  holes in the first fix; the second round closed them. guarddiff changed
  only the 9 allow-listed verdicts.
- **T201, bench.** What looked like a hung run was the laptop's suspend.
  Two real gaps were fixed: a pi that stays after its last message, and an
  orphan that holds stdout past the deadline. A run no longer holds its
  slot past its timeout.
- **The gate's TMPDIR.** `bench certify` builds the gates' scratch under
  `/tmp`, where test/guards' outside-temp tests fail. This is an old,
  environmental failure; CI passes. Both v2 certifications ran with
  `TMPDIR` outside `/tmp`, and the code fix is T206.

## What certification does not prove

- **That v0.4 is better than v0.3.7.** It is a regression gate. Most tasks
  are short and v0.3.7 already passes them (jhon baseline: 122 of 122 runs).
- **Small differences.** Three repeats are too few to rank close rates; the
  single-run failures above are called noise by reading the runs, not by
  statistics.
- **Real projects.** The tasks are synthetic. workspace-budgets is the one
  long, realistic task, and nothing comes from the owner's real projects
  (T202).
- **The owner's experience.** Nothing measures how long the owner waits,
  the reply language, or the quality of a review (T200, T202).
- **`b4d0e53` for every cell.** Cells T208/T209/T210 did not touch keep
  their `a16b9a5` runs.
- **Release pack and reproducibility at the tag.** They were checked at
  `a16b9a5`; the release workflow runs them again on the tag.
- **Other platforms and models.** Only Linux and the two profiles' models
  ran. macOS and Windows run unsandboxed and were not benchmarked.

## Follow-ups (after the tag)

- **T211:** find what in the high-risk notice pushes the agent to undo
  instead of keeping and recording its work, and read the workspace-budgets
  ldd checker findings behind its rework.
- **T202:** the bench learns from the owner's real projects: synthetic
  tasks shaped like their layouts, migrations, deploys and long changes,
  never their code or names.
- **T206:** certification that survives the real world:
  - gate scratch outside `/tmp`;
  - `bench run` resumes or fills a cell to N valid runs;
  - environment failures are told apart from Jhon's;
  - one detached command runs the whole matrix.
- **T200:** `jhon audit` measures how long the owner waited on foreground
  work, and `jhon doctor` cites pi 1.5.2's primitives.
- **T199:** `jhon review run` shows its cost and roles, and `ldd/risk.go`
  stops trusting an `origin/HEAD` that points at a feature branch.
- **`.codegraph/` in the bench templates:** the bench's task templates
  ignore `.codegraph/`, so a code index's own `.gitignore` is not counted
  out of scope (the owner's out-of-scope decision; not in v0.4.0).
