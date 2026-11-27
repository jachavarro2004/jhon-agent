package guard

// A Card is why a guard exists (contracts/ldd.md, principle 3: nothing is a
// contract): the risk it covers and the real case that put it there. `jhon
// audit --all` sets beside it what the guard did since a date, so a guard
// that only gets in the way, or never stops anything, comes up to be
// removed or replaced.
type Card struct {
	Guard string
	// Covers is the risk a refusal of the guard prevents.
	Covers string
	// Because is the real case, or the measured cost, that justified it.
	Because string
}

// Cards are one card per guard of Matchers, in its order.
var Cards = []Card{
	{"inspect-guard", "a reviewer changes what it reviews", "the Prism reviewers must stay read-only for their verdict to mean anything: their Bash runs only read-only programs (T271), and Jhon's own readers in inspect_allow"},
	{"safety-guard", "a destructive or irreversible command (docker rm -f and volume deletions included), an outward-facing one without the owner's consent, a read-only reader printing a credentials file, or a sub-agent's unbounded wait", "agents ran forced pushes, hard resets, and pushes without asking; on 2026-10-27 two workers in a client project left `until ...; sleep` loops running and the owner had to stop them; credential contents are the owner's alone (S10, T271)"},
	{"vcs-guard", "a write into a VCS directory or the grants, an agent writing Claude Code's settings, or an Edit or Write outside the project (asked)", "a write inside .git/ corrupts the repository; the sandbox wraps Bash, not the file tools (T161, L1420)"},
	{"framework-guard", "an agent edits the framework or its installed release while it runs on it", "docket AV2: an agent writing the release it is running changes its own rules mid-session"},
	{"secrets-guard", "an agent reads, searches, edits or writes a file that holds credentials (.env, keys, credentials.json) or a home credentials directory; a template such as .env.example passes", "2026-11-03: the owner handles credentials themself and asked that no agent touch them, while templates of them stay readable and editable; 2026-11-19 (T161, L1420): a read that reaches the model's provider cannot be undone"},
	{"scope-guard", "a bounded writer edits files outside the surfaces its handoff names (a shell write whose target cannot be resolved runs with a note)", "workers touched files beyond their task, and their diffs collided with the orchestrator's"},
	{"route-guard", "a bounded writer launched without its scope or with a surface that matches nothing, a change without its record, a slice commit of an untested tree, a PR without its docs, or a merge without rebase", "2026-10-28, a client project, HU-ABC-007: stale memories won over the project's config (one PR per slice), and the owner had to ask for the docs twice; 36 of 98 client-project writer launches before 2026-10-21 carried no Allowed edit surfaces, so nothing held them to their task; 2026-11-18 (L1364 b): a writer's brief named a path that did not exist, and scope-guard refused the real file"},
}

// CardOf is guard's card; ok is false for a guard without one.
func CardOf(guard string) (Card, bool) {
	for _, c := range Cards {
		if c.Guard == guard {
			return c, true
		}
	}
	return Card{}, false
}
