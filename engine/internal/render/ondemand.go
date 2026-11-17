package render

import (
	"fmt"
	"path/filepath"
	"strings"
)

// T146 FA3 (L1217, L1218): rules every turn does not need left the fixed
// context (the always-on layer and the orchestrator's body) for contracts
// read on demand, on both runtimes. Nothing was removed: each moved rule's
// key sentence is in its contract, and the rendered orchestrator of each
// runtime carries the pointer that sends the agent there when the trigger
// applies. A contract read on demand is raw prose: it names no runtime term.

// onDemandContract is one contract read on demand: its file under prose/,
// the pointer the rendered orchestrator carries (whitespace flattened), and
// the key sentence of each rule moved into it.
type onDemandContract struct {
	file     string
	pointers []string
	rules    []string
}

// contractsRoot is how the orchestrator's header resolves a
// `contracts/<name>.md` it names, and how the always-on layer names one.
const contractsRoot = "~/.local/share/jhon/current/prose/contracts/"

var onDemandContracts = []onDemandContract{
	{
		file: "contracts/delivery.md",
		pointers: []string{
			"`contracts/<name>.md` here is `" + contractsRoot + "<name>.md`, binding once its trigger applies.",
			"Before delivering a change (a commit the owner will push, a merge, a release), before its delivery test run, and before a 4R or a Judgment Day, run `jhon risk` in its tree and read `contracts/delivery.md`.",
		},
		rules: []string{
			"| passive | read the diff back | not asked | none |",
			"| high | an independent verification on record (`contracts/ldd.md`, \"Delivery\") |",
			"`verify` and an automatic 4R with only the pertinent lenses, recorded in `review-4r.md`; `review_required` always wins; Judgment Day only when the owner asks",
			"A session no one can answer proceeds and reports the tier and its reasons instead of asking.",
			"**The record is always; the rigor is the change's, not yours.**",
			"Create tasks.md yourself when you see several rules or cases to cover or a design decision on the way.",
			"Name `jhon deliver check`'s result when asking to deliver; an untested revision is called untested.",
			"Order: commit, then a recorded run (`jhon executions run`) on the clean tree at HEAD, then `jhon deliver check`.",
			"A run whose tested tree is the commit's needs no re-run.",
			"A slice's commit holds exactly the tree of its last recorded passing run: RED, GREEN, recorded run, commit.",
			"**About 400 changed lines per delivery is a note**, never a cap, a forced split, or a reason for `verify`.",
			"A suite the PR's CI runs whole is never run whole here too: its green checks test the delivery.",
			"merge only after the PR's CI is green. Wait for that CI as one background job, `gh run watch <run-id> --exit-status`, whose exit is the notice; never a `sleep` loop that polls it.",
			"A command that did not pass keeps its failed tests (`failed_tests`) and the end of its output (`output_tail`) in the record: read them there.",
			"For a wide or long run, tell the owner the `--dry-run` estimate, and when it should end, as you start the run, never a guess from the files changed.",
			"A 4R's correction is one bounded `worker`, then `verify` confirms (`contracts/review-4r.md`).",
			"Ask the owner only before a 4R or a Judgment Day that is not due and the owner did not ask for; when the owner asked, run it: the guard's own confirmation is the single ask.",
			"built in slices outlined first, each tasked after the last is delivered, and delivered alone or, with `delivery: change` in `openspec/config.yaml`, committed on the change's branch and merged once with rebase after the last",
			"Judgment Day (`contracts/judgment.md`) runs only when the owner asks, or `review_required` names it",
			"One review per code change: 4R reviews code; Judgment Day judges a design decision or a docket, and on code only when a severe 4R finding stays contested.",
		},
	},
	{
		file: "contracts/delegation.md",
		pointers: []string{
			"Before writing a delegation prompt, read `contracts/delegation.md`;",
			"On return, read `contracts/delegation.md` before merging.",
		},
		rules: []string{
			"A count or a price ratio never sizes a task; the counts below are notes.",
			"From the main checkout, pass the change's worktree to sub-agents and inspect (their `cwd` where the tool takes one, else its absolute path in the task and `git -C <worktree>`); never launch agents by running Jhon's internals by hand (`shell/lib`, node imports).",
			"**Hand off by reference.** Name the artifact and its ids instead of paraphrasing them; the executor reads the source.",
			"`## Allowed edit surfaces` heading, one path or narrow glob per line",
			"Before launching a worker on a slice, map with CodeGraph (then reads) every integration point it touches",
			"permissions and their categories or registries, permission seeds",
			"A worker stopped on scope is a planning error: fix the plan, never ask the owner.",
			"**Size each worker slice to fit its context budget; the worker checkpoints its progress and partial state at its context notice and returns what is done and what remains: hand the remainder to a fresh worker.**",
			"A slice that crosses layers runs as passes, one layer each, in dependency order",
			"**Validate consequential premises before building.**",
			"One explore per change: when `openspec/changes/<change>/exploration.md` exists, hand it to the next phase instead of exploring again",
			"A delegation prompt carries the owner's request verbatim, beside your reading of it.",
			"`jhon context <change> --phase none --handoff`",
			"An explore or worker task carries the standing decisions found, each id with one line, and asks it to search for more on what it covers.",
			"A requirement you inferred is stated as an assumption or asked, never presented as the owner's.",
			"a test that also fails on the base is reported as pre-existing, not as this change's blocker.",
			"On return, merge each branch on the change's branch, test once, and remove each worktree (`git worktree remove <path>`), then its branch (`git branch -d <branch>`), one command each, never `&&`.",
		},
	},
	{
		file: "contracts/asked-operations.md",
		pointers: []string{
			"Before any remote access, push, `gh` write, publish, deploy, or a deletion that loses work or data, read `" + contractsRoot + "asked-operations.md`.",
		},
		rules: []string{
			"Ambient SSH agents, sockets, credentials, and authenticated sessions are not permission to use them.",
			"No wrapper, interpreter, library, or delegate widens the authorized scope.",
			"the owner's `jhon mode yolo` lets them pass without asking until the owner turns it off.",
			"In a normal session only what truly matters asks (S33).",
			"One command that needs several classes asks one question that names them all.",
			"A grant and a mode are the owner's act alone.",
			"The modes' history records every change with `by: \"owner\"`, and `jhon audit` reads it.",
			"(a forced branch delete or worktree removal, `reset --hard`, `clean -f`, a dropped stash, a destructive Docker command, a path in the home directory outside the project) runs once the owner approves it (class `destroy`).",
			"name each one, say which keep data and which delete it, and run it once the owner approves (the guard's question, or their `jhon grant destroy`).",
		},
	},
	{
		file: "contracts/witness.md",
		pointers: []string{
			"Before testing behavior that already exists or reporting a witness, read `" + contractsRoot + "witness.md`.",
		},
		rules: []string{
			"For behavior that already exists, the witness is a mutation: break the code temporarily, see the test fail, restore, see it pass.",
			"`jhon mutate <file> --find <text> --replace <text> -- <test command>`, which restores the file itself, not git.",
			"A new test observed RED needs no mutation.",
			"witness: {RED|mutation}",
			"`strict_tdd` in `jhon status --json` reports the project's preference",
		},
	},
}

// fixedText is the orchestrator's fixed context as runtime renders it: its
// body, then the always-on layer it carries.
func (e *engine) fixedText(runtime string) string {
	return e.bodyFor(runtime, e.mainAgent(piMain)) + "\n\n" + e.alwaysOnSection(runtime, true)
}

// onDemandProblems are the moved rules a contract no longer states, the
// pointers a runtime's orchestrator lost, a moved rule still in the fixed
// context, and a runtime term in a contract read on demand.
func (e *engine) onDemandProblems() []string {
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	var out []string
	for _, c := range onDemandContracts {
		path := filepath.Join(e.prose, c.file)
		if !exists(path) {
			out = append(out, c.file+" is missing")
			continue
		}
		body := mustReadText(path)
		if m := runtimePlaceholder.FindString(body); m != "" {
			out = append(out, fmt.Sprintf("%s is read raw, yet names runtime term %s", c.file, m))
		}
		text := flat(body)
		for _, r := range c.rules {
			if !strings.Contains(text, r) {
				out = append(out, fmt.Sprintf("%s does not state %q", c.file, r))
			}
		}
		for _, rt := range []string{"claude-code", "pi"} {
			fixed := flat(e.fixedText(rt))
			for _, p := range c.pointers {
				if !strings.Contains(fixed, p) {
					out = append(out, fmt.Sprintf("%s's orchestrator does not point to %s with %q", rt, c.file, p))
				}
			}
			for _, r := range c.rules {
				if strings.Contains(fixed, r) {
					out = append(out, fmt.Sprintf("%s's fixed context still states %q, moved to %s", rt, r, c.file))
				}
			}
		}
	}
	return out
}
