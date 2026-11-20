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
// the pointers to it the fixed context carries, and the key sentence of
// each rule moved into it (both whitespace flattened).
type onDemandContract struct {
	file     string
	pointers []pointer
	rules    []string
}

// pointer is a sentence that sends the agent to a contract, and in, the
// prose file of the fixed context that holds it (relative to prose/): the
// orchestrator or an always-on contract.
type pointer struct{ in, text string }

// contractsRoot is how the prose names the contracts directory in a
// pointer; the renderer writes the install's real path in its place
// (atInstall, T146c K3).
const contractsRoot = runtimeRef + "/prose/contracts/"

// atInstall is text with each runtimeRef (contractsRoot included) written
// as the install this render projects, `current`, by its real path: a model
// told `~/...` expands the `~` from the user name in its working directory,
// not from HOME, and a bench run read the owner's install (T146g G4).
func (e *engine) atInstall(text string) string {
	text = strings.ReplaceAll(text, contractsRoot, filepath.Join(e.current(), ProseDir, "contracts")+"/")
	return strings.ReplaceAll(text, runtimeRef, e.current())
}

var onDemandContracts = []onDemandContract{
	{
		file: "contracts/delivery.md",
		pointers: []pointer{
			{"orchestrator.md", "`contracts/<name>.md` here is `" + contractsRoot + "<name>.md`, binding once its trigger applies."},
			{"orchestrator.md", "Before delivering a change (a commit the owner will push, a merge, a release), before its delivery test run, and before a 4R, run `jhon risk` in its tree and read `contracts/delivery.md`."},
		},
		rules: []string{
			"| passive | read the diff back | not asked | none |",
			// T146c K1 (L1222): the high tier's concrete definition, the
			// record sentence and the hand-run note, which FA3 dropped.
			"| high | an independent verification on record: the `verify` agent's passing `verify-report.md`; `jhon deliver check` is not ready without one |",
			"Every change that alters behaviour carries the LDD record it needs: always its RED and GREEN runs and the commit or PR text that says what was asked; tasks.md when Jhon asks for it.",
			"A whole suite run by hand gets a note, never a refusal.",
			"`verify` and an automatic 4R with only the pertinent lenses, recorded in `review-4r.md`; `review_required` always wins",
			"A session no one can answer proceeds and reports the tier and its reasons instead of asking.",
			"**The record is always; the rigor is the change's, not yours.**",
			"Create tasks.md yourself when you see several rules or cases to cover or a design decision on the way.",
			"Name `jhon deliver check`'s result when asking to deliver; an untested revision is called untested.",
			"Order: after the GREEN run, commit the tree it tested, then `jhon deliver check`.",
			"A run whose tested tree is the commit's needs no re-run.",
			"A slice's commit holds exactly the tree of its last recorded passing run: RED, GREEN, recorded run, commit.",
			"**About 400 changed lines per delivery is a note**, never a cap, a forced split, or a reason for `verify`.",
			"A suite the PR's CI runs whole is never run whole here too: its green checks test the delivery.",
			"merge only after the PR's CI is green. Wait for that CI as one background job, `gh run watch <run-id> --exit-status`, whose exit is the notice; never a `sleep` loop that polls it.",
			"A command that did not pass keeps its failed tests (`failed_tests`) and the end of its output (`output_tail`) in the record: read them there.",
			"For a wide or long run, tell the owner the `--dry-run` estimate, and when it should end, as you start the run, never a guess from the files changed.",
			"A 4R's correction is one bounded `worker`, then `verify` confirms (`contracts/review-4r.md`).",
			"Ask the owner only before a 4R that is not due and the owner did not ask for; when the owner asked, run it: the guard's own confirmation is the single ask.",
			"built in slices outlined first, each tasked after the last is delivered, and delivered alone or, with `delivery: change` in `openspec/config.yaml`, committed on the change's branch and merged once with rebase after the last",
		},
	},
	{
		file: "contracts/delegation.md",
		pointers: []pointer{
			{"orchestrator.md", "Before writing a delegation prompt, read `contracts/delegation.md`;"},
			{"orchestrator.md", "On return, read `contracts/delegation.md` before merging."},
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
		pointers: []pointer{
			{"contracts/remote-auth.md", "Before any remote access, push, `gh` write, publish, deploy, or a deletion that loses work or data, read `" + contractsRoot + "asked-operations.md`."},
		},
		rules: []string{
			"Ambient credentials and sessions are not permission; an owner-approved operation uses them unasked.",
			"No wrapper, interpreter, library, or delegate widens the authorized scope.",
			"the owner's `jhon mode yolo` lets them pass without asking until the owner turns it off.",
			"In a normal session only what truly matters asks (S33).",
			"One command that needs several classes asks one question that names them all.",
			"A grant and a mode are the owner's act alone.",
			"The modes' history records every change with `by: \"owner\"`, and `jhon audit` reads it.",
			"(a forced branch delete or worktree removal, `reset --hard`, `clean -f`, a dropped stash, a destructive Docker command: `docker compose down -v`, `docker volume rm`, `prune`, `docker rm -f`; a path in the home directory outside the project)",
			"name each one, say which keep data and which delete it, and run it once the owner approves (class `destroy`: the guard's question, or their `jhon grant destroy`).",
		},
	},
	{
		file: "contracts/witness.md",
		pointers: []pointer{
			{"contracts/tdd.md", "Before testing behavior that already exists or reporting a witness, read `" + contractsRoot + "witness.md`."},
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

// flattened is s with each run of whitespace one space, so a sentence
// matches across line breaks.
func flattened(s string) string { return strings.Join(strings.Fields(s), " ") }

// fixedText is the orchestrator's fixed context as runtime renders it: its
// body, then the always-on layer it carries.
func (e *engine) fixedText(runtime string) string {
	return e.bodyFor(runtime, e.mainAgent(piMain)) + "\n\n" + e.alwaysOnSection(runtime, true)
}

// fixedParts are the prose files the fixed context of runtime is made of,
// relative to prose/, each as runtime renders it, flattened: the
// orchestrator's body, then each always-on file.
func (e *engine) fixedParts(runtime string) [][2]string {
	parts := [][2]string{{"orchestrator.md", flattened(e.bodyFor(runtime, e.mainAgent(piMain)))}}
	for _, rel := range alwaysOn() {
		parts = append(parts, [2]string{rel, flattened(e.atInstall(inRuntime(runtime, readText(filepath.Join(e.prose, rel)))))})
	}
	return parts
}

// where names the prose file of the fixed context that holds a pointer.
func where(in string) string {
	if in == "orchestrator.md" {
		return "orchestrator (orchestrator.md)"
	}
	return "always-on " + in
}

// onDemandProblems are, each against the prose file at fault: a contract
// that is missing, names a runtime term, or no longer states a moved rule;
// a pointer its file does not hold or a runtime's fixed context lost; and
// a moved rule some file of the fixed context still states.
func (e *engine) onDemandProblems() []problem {
	var out []problem
	add := func(rel, msg string) { out = append(out, problem{filepath.Join(e.prose, rel), 0, "on-demand", msg}) }
	parts := map[string][][2]string{}
	fixed := map[string]string{}
	for _, rt := range []string{"claude-code", "pi"} {
		// T146h: the fixed context of a change that is not small is the
		// runtime's full file (lean.go).
		parts[rt], fixed[rt] = e.fixedParts(rt), flattened(e.fullText(rt))
	}
	for _, c := range onDemandContracts {
		path := filepath.Join(e.prose, c.file)
		if !exists(path) {
			add(c.file, c.file+" is missing")
			continue
		}
		body := mustReadText(path)
		if m := runtimePlaceholder.FindString(body); m != "" {
			add(c.file, fmt.Sprintf("%s is read raw, yet names runtime term %s", c.file, m))
		}
		text := flattened(body)
		for _, r := range c.rules {
			if !strings.Contains(text, r) {
				add(c.file, fmt.Sprintf("%s does not state %q", c.file, r))
			}
		}
		for _, p := range c.pointers {
			if !strings.Contains(flattened(readText(filepath.Join(e.prose, p.in))), p.text) {
				add(p.in, fmt.Sprintf("%s does not hold the pointer to %s: %q", p.in, c.file, p.text))
			}
			for _, rt := range []string{"claude-code", "pi"} {
				if !strings.Contains(fixed[rt], e.atInstall(p.text)) {
					add(p.in, fmt.Sprintf("%s's %s does not point to %s with %q", rt, where(p.in), c.file, p.text))
				}
			}
		}
		for _, rt := range []string{"claude-code", "pi"} {
			for _, r := range c.rules {
				if !strings.Contains(fixed[rt], r) {
					continue
				}
				in := "templates/claude-md-block.md"
				for _, part := range parts[rt] {
					if strings.Contains(part[1], r) {
						in = part[0]
						break
					}
				}
				add(in, fmt.Sprintf("%s's fixed context still states %q (%s), moved to %s", rt, r, in, c.file))
			}
		}
	}
	return append(out, e.leanProblems()...)
}
