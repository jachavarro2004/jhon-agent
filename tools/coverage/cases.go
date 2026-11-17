package main

// A case injects one defect into one file of one layer: the first occurrence
// of find is replaced by replace (every occurrence when INJECT_ALL is set),
// and rule must then fire on a line naming layer. A file is named as the
// validator names it: relative to prose/, unless it names one of the tree's
// other top directories.
type defectCase struct {
	rule, layer, file, find, replace string
	// rebuild marks a defect in the renderer's own Go source, which it
	// compiles in, so the binary is rebuilt around the case.
	rebuild bool
}

func defect(rule, layer, file, find, replace string) defectCase {
	return defectCase{rule, layer, file, find, replace, false}
}

func codeDefect(rule, layer, file, find, replace string) defectCase {
	return defectCase{rule, layer, file, find, replace, true}
}

// cases is the matrix. A rule proven in one layer is not proven: each rule is
// exercised in every layer that carries it.
var cases = []defectCase{
	// broken-ref — a reference that names no file
	defect("broken-ref", "contracts", "contracts/ldd.md", "(`contracts/runtime-boundaries.md`)", "(`contracts/ghost.md`)"),
	defect("broken-ref", "agents", "agents/worker.md", "~/.local/share/jhon/current/prose/contracts/ldd.md", "~/.local/share/jhon/current/prose/contracts/ghost.md"),
	defect("broken-ref", "plugin", "runtimes/claude-code/plugin/skills/jhon-ldd/SKILL.md", "Read-only.", "Read-only. See `contracts/ghost.md`."),
	defect("broken-ref", "skills", "skills/ldd/references/spec.md", "~/.local/share/jhon/current/prose/contracts/spec-delta.md", "~/.local/share/jhon/current/prose/contracts/ghost.md"),

	// malformed-tilde — the home-reference typo, not a markdown fence
	defect("malformed-tilde", "agents", "agents/verify.md", "`~/.local/share/jhon/current/prose/contracts/ldd.md`", "`~~/.local/share/jhon/current/prose/contracts/ldd.md`"),
	defect("malformed-tilde", "skills", "skills/ldd/references/spec.md", "~/.local/share/jhon/current/prose/contracts/spec-delta.md", "~~/.local/share/jhon/current/prose/contracts/spec-delta.md"),

	// slash-command — only /jhon-ldd exists, and only as a skill of the release's plugin
	defect("slash-command", "plugin", "runtimes/claude-code/plugin/skills/jhon-ldd/SKILL.md", "Write the change's intent", "Run /ldd-explore first. Write the change's intent"),
	defect("slash-command", "skills", "skills/ldd/references/init.md", "## Init", "## Init\n\nThen run /ldd-new."),
	defect("slash-command", "contracts", "contracts/ldd.md", "Value reaches the default branch", "Run /jhon-ldd-bogus. Value reaches the default branch"),

	// plugin-skill — a command is a user-invoked skill named as its directory
	defect("plugin-skill", "plugin", "runtimes/claude-code/plugin/skills/jhon-ldd/SKILL.md", "disable-model-invocation: true", "disable-model-invocation: false"),
	defect("plugin-skill", "plugin", "runtimes/claude-code/plugin/skills/jhon-ldd/SKILL.md", "name: jhon-ldd", "name: ghost"),

	// verdict-vocab — the verdict line jhon status reads stays two-valued
	defect("verdict-vocab", "skills", "skills/ldd/references/verify.md", "verdict: pass | fail", "verdict: pass | fail | warn"),

	// manifest — a declared agent with no body
	defect("manifest", "manifest", "agents.yaml", "  - name: worker", "  - name: ghost-agent\n    tier: mid\n    caps: [read]\n    description: \"ghost\"\n  - name: worker"),

	// manifest — a key or line the minimal reader does not understand fails
	// instead of being skipped
	defect("manifest", "manifest", "agents.yaml", "version: 1", "version: 1\nmodels: {}"),
	defect("manifest", "manifest", "agents.yaml", "  - name: worker\n    tier: strong", "  - name: worker\n    teir: mid\n    tier: mid"),
	defect("manifest", "manifest", "agents.yaml", "  - name: worker\n", "  - name: worker\n    just some words\n"),

	// manifest — a tier value is model or model@effort, and a Claude Code effort
	// is one the runtime documents (docket AX2)
	defect("manifest", "manifest", "agents.yaml", "strong: claude-opus-5-5@medium", "strong: claude-opus-5-5@turbo"),
	defect("manifest", "manifest", "agents.yaml", "strong: claude-opus-5-5@medium", "strong: claude-opus-5-5@"),
	defect("manifest", "manifest", "agents.yaml", "cheap: claude-bridge/claude-haiku-5-5@medium", "cheap: @low"),
	// manifest — an agent's own effort is a documented value, on a tier whose
	// Claude model takes effort (docket AX)
	defect("manifest", "manifest", "agents.yaml", "  - name: worker\n    tier: strong\n", "  - name: worker\n    tier: strong\n    effort: turbo\n"),

	// agent-template — the manifest's vars and the template's placeholders agree
	defect("agent-template", "agents.yaml", "agents/templates/review-lens.md", "## Scope", "## Scope for {{audience}}"),
	defect("agent-template", "agents.yaml", "agents.yaml", "      judge: A", "      judge: A\n      colour: red"),

	// top-level-tools — session memory is the top-level agent's alone, judged on the
	// projected tool sets, whichever capability grants a tool
	defect("top-level-tools", "agents.yaml", "agents.yaml", "mcp__engram__mem_session_summary, mcp__plugin_engram_engram__mem_session_summary, ", ""),
	defect("top-level-tools", "agents.yaml", "agents.yaml", "caps: [read, write, bash, memory, codegraph]\n    description: \"Implement a scoped task", "caps: [read, write, bash, memory, memory-session, codegraph]\n    description: \"Implement a scoped task"),
	defect("top-level-tools", "agents.yaml", "agents.yaml", "mcp__plugin_engram_engram__mem_judge]", "mcp__plugin_engram_engram__mem_judge, mcp__plugin_engram_engram__mem_context]"),
	defect("vocabulary", "contracts", "contracts/vocabulary.yaml", "top_level_tools: [mem_session_summary, mem_context, mem_current_project, mem_review]", "top_level_tools: []"),

	// vocabulary — the values the code reads
	defect("vocabulary", "contracts", "contracts/vocabulary.yaml", "next_action: [propose, spec, design, tasks, apply, verify, archive, select-change, start-change]", "next_action: []"),
	defect("vocabulary", "contracts", "contracts/vocabulary.yaml", "envelope_verdict: [pass, fail]", "envelope_verdict: []"),
	// a slice's limits, which vcs-guard reads at runtime, are positive numbers
	defect("vocabulary", "contracts", "contracts/vocabulary.yaml", "slice_limits:\n  tasks: 12\n", "slice_limits:\n  tasks: twelve\n"),

	// jhon-command — every `jhon <subcommand>` named in prose is one jhon dispatches
	defect("jhon-command", "skills", "skills/ldd/references/archive.md", "jhon compose \"openspec/specs", "jhon archive-compose \"openspec/specs"),
	defect("jhon-command", "plugin", "runtimes/claude-code/plugin/skills/jhon-ldd/SKILL.md", "Run `jhon status <change>` from", "Run `jhon state <change>` from"),
	defect("jhon-command", "agents", "agents/worker.md", "`jhon status <change> --json`", "`jhon derive <change> --json`"),
	defect("jhon-command", "contracts", "contracts/ldd.md", "`jhon status <change> --json` reads it", "`jhon derive <change>` reads it"),
	defect("jhon-command", "orchestr", "orchestrator.md", "`jhon deliver check <change>` says", "`jhon ship <change>` says"),
	defect("jhon-command", "README", "README.md", "| `jhon compose` |", "| `jhon merge` |"),
	defect("jhon-command", "skills", "skills/judgment-day/SKILL.md", "`jhon judgment append <record> <event-file>`", "`jhon judgement append <record> <event-file>`"),

	// spec-delta — the delta contract is self-consistent and the spec template teaches every section
	defect("spec-delta", "contracts", "contracts/spec-delta.md", "  REMOVED: [Reason]", "  REMOVED: [Rationale]"),
	defect("spec-delta", "contracts", "contracts/spec-delta.md", "delta_heading_only: [REMOVED, RENAMED]", "delta_heading_only: [REMOVED, RETIRED]"),
	defect("spec-delta", "skills", "skills/ldd/references/spec.md", "## RENAMED Requirements", "## RENAMED Reqs"),

	// judgment-vocab — the contract's lists agree, and the skill's verdict line is one of them
	defect("judgment-vocab", "skills", "skills/judgment-day/SKILL.md", "`JUDGMENT: CLOSED ✅`", "`JUDGMENT: RESOLVED ✅`"),
	defect("judgment-vocab", "contracts", "contracts/judgment.md", "judgment_human_outcomes: [confirmed, dropped]", "judgment_human_outcomes: [confirmed, overruled]"),
	defect("judgment-vocab", "contracts", "contracts/judgment.md", "  docket: 6", "  docket: 0"),

	// claude-md-template — the CLAUDE.md block template places every ALWAYS_ON group
	defect("claude-md-template", "templates", "templates/claude-md-block.md", "{{routing}}", "{{routes}}"),

	// runtime-boundaries — a safety refusal is a stop, reported verbatim, reached
	// by no other route (a delegate included), never recorded as a technique, and
	// resolved by the owner; the contract is always-on and the orchestrator never
	// bypasses an executor's refusal
	defect("runtime-boundaries", "contracts", "contracts/runtime-boundaries.md", "**A safety refusal is a stop.**", "**A safety refusal is a hint.**"),
	defect("runtime-boundaries", "contracts", "contracts/runtime-boundaries.md", "Report it verbatim;", "Summarise it;"),
	defect("runtime-boundaries", "contracts", "contracts/runtime-boundaries.md", "a delegate asked to do what\n  the refusal prevented", "a delegate"),
	defect("runtime-boundaries", "contracts", "contracts/runtime-boundaries.md", "**A refusal is not a failure.**", "**A refusal is a failure.**"),
	defect("runtime-boundaries", "contracts", "contracts/runtime-boundaries.md", "**Record the refusal, never the way around it**", "**Record the refusal and the way around it**"),
	defect("runtime-boundaries", "contracts", "contracts/runtime-boundaries.md", "using the allowed form is compliance", "using the allowed form is optional"),
	defect("runtime-boundaries", "contracts", "contracts/runtime-boundaries.md", "**The owner resolves it**", "**The agent resolves it**"),
	defect("runtime-boundaries", "orchestr", "orchestrator.md", "Never perform,\n  propose, or instruct a bypass", "You may\n  propose a bypass"),
	codeDefect("runtime-boundaries", "render", "engine/internal/render/render.go", "\t\t\"contracts/runtime-boundaries.md\",\n", ""),

	// runtime-matrix — agents.yaml declares every primitive for every runtime,
	// well formed, with repository evidence behind `verified`, and a contract
	// delivery and observation route per runtime (docket AG1)
	defect("runtime-matrix", "agents", "agents.yaml", "    skills: { capability: true, verification: documented, version: \"1.4.0\", source: \"pi skills: settings skills paths and package skills\" }\n", ""),
	defect("runtime-matrix", "agents", "agents.yaml", "source: \"test/testdata/audit/apply-bypass-archive: a real session", "source: \"a real session"),
	defect("runtime-matrix", "agents", "agents.yaml", "  pi: { max_instruction_bytes: none,", "  pi: { max_instruction_bytes: 32k,"),
	defect("runtime-matrix", "agents", "agents.yaml", "  pi: jhon audit\n", ""),
	defect("runtime-matrix", "contracts", "contracts/vocabulary.yaml", "primitive_verification: [verified, documented, unknown]", "primitive_verification: [verified, documented, rumoured]"),
	// docket AG addendum: verified cites what was executed and observed. A unit
	// test is not evidence, a real session verifies only a primitive that needs no
	// refusal, and the spike record format is declared in the vocabulary
	defect("runtime-matrix", "agents", "agents.yaml", "source: \"test/testdata/audit/apply-bypass-archive: a real session", "source: \"test/render.test.ts: a real session"),
	defect("runtime-matrix", "agents", "agents.yaml", "source: \"test/testdata/audit/apply-tdd-evidence: a real session", "source: \"test/testdata/audit/clean: a real session"),
	defect("runtime-matrix", "agents", "agents.yaml", "version: \"2.1.310\", source: \"test/testdata/spikes/claude-code-plugin-agent-hooks/record.json", "version: \"2.1.310\", source: \"test/testdata/audit/apply-bypass-archive: a real session, which shows no refusal honoured; record.json"),
	defect("runtime-matrix", "contracts", "contracts/vocabulary.yaml", "spike_record_fields: [runtime, ", "spike_fields: [runtime, "),

	// runtime-guarantee — every behavioural anchor declares a level for every
	// runtime; enforced rests on a verified primitive the render projects, and
	// behavioural on an observation route and an audit check (docket AG2)
	defect("runtime-guarantee", "agents", "agents.yaml", "  delivery/same-tree: { claude-code: behavioural, pi: behavioural }\n", ""),
	defect("runtime-guarantee", "agents", "agents.yaml", "  isolation/no-other-tree: { claude-code: behavioural, pi: behavioural }", "  isolation/no-other-tree: { claude-code: behavioural }"),
	defect("runtime-guarantee", "agents", "agents.yaml", "  claude-code: jhon audit\n  pi: jhon audit", "  claude-code: jhon audit\n  pi: none"),
	defect("runtime-guarantee", "agents", "agents.yaml", "  runtime-boundaries/stop: { claude-code: unsupported,", "  runtime-boundaries/stop: { claude-code: behavioural,"),
	defect("runtime-guarantee", "agents", "agents.yaml", "    agent-tool-allowlist: { capability: true, verification: verified, version: \"1.4.0\"", "    agent-tool-allowlist: { capability: false, verification: verified, version: \"1.4.0\""),
	defect("runtime-guarantee", "agents", "agents.yaml", "    agent-tool-allowlist: { capability: true, verification: verified, version: \"2.1.280\"", "    agent-tool-allowlist: { capability: true, verification: documented, version: \"2.1.280\""),
	defect("runtime-guarantee", "agents", "agents.yaml", "  isolation/no-other-tree: { claude-code: behavioural,", "  isolation/no-other-tree: { claude-code: behavioural, claude-code@windows: behavioural,"),
	defect("runtime-guarantee", "contracts", "contracts/vocabulary.yaml", "guarantee_levels: [enforced, behavioural, unsupported]", "guarantee_levels: [enforced, behavioural]"),

	// audit-registry — every behavioural anchor has an audit_rules entry, the
	// checks named are exactly those internal/auditcheck exports, and none has a reason
	defect("audit-registry", "contracts", "contracts/vocabulary.yaml", "  - anchor: runtime-boundaries/no-technique", "  - anchor: runtime-boundaries/removed"),
	defect("audit-registry", "contracts", "contracts/vocabulary.yaml", "    check: isolation-bypass", "    check: isolation-quoted"),
	defect("audit-registry", "contracts", "contracts/vocabulary.yaml", "    reason: it defines which errors are refusals; refusal-bypass applies the definition and does not test it\n", ""),
	defect("audit-registry", "contracts", "contracts/vocabulary.yaml", "    check: isolation-bypass\n    evidence_class: deterministic", "    check: isolation-bypass\n    evidence_class: probable"),
	codeDefect("audit-registry", "internal/audit", "engine/internal/auditcheck/auditcheck.go", "\"archive-without-red\"}", "\"archive-without-red\", \"phantom\"}"),
	// a deterministic entry declares validation_status, and the real fixtures an
	// entry names exist and are marked real; the old field names are refused
	defect("audit-registry", "contracts", "contracts/vocabulary.yaml", "    validation_status: verified\n    real_clean: [general-isolation-stop]", "    real_clean: [general-isolation-stop]"),
	defect("audit-registry", "contracts", "contracts/vocabulary.yaml", "    real_violation: [apply-worktree-bypass]", "    real_violation: [clean]"),
	defect("audit-registry", "contracts", "contracts/vocabulary.yaml", "    real_violation: [apply-worktree-bypass]", "    real_negative: [apply-worktree-bypass]"),
	// safety — repository-state operations are any writer's; the safety guard
	// refuses destructive ones and asks for outward-facing ones, for every agent
	// and the top-level session; after a runtime isolation refusal, the refused
	// write in another tree is a bypass
	defect("safety", "orchestr", "orchestrator.md", "`jhon hook safety-guard` refuses irreversible\n  operations to every agent and to this thread", "`jhon hook safety-guard` advises"),
	defect("safety", "contracts", "contracts/ldd.md", "Any agent that holds Bash may perform repository-state operations locally", "Only one agent performs repository-state operations"),
	defect("safety", "contracts", "contracts/ldd.md", "`jhon hook safety-guard`\nrefuses irreversible operations to every agent and the top-level session", "`jhon hook safety-guard` reviews operations"),
	defect("safety", "contracts", "contracts/ldd.md", "It asks for the outward-facing ones", "It allows the outward-facing ones"),
	defect("safety", "contracts", "contracts/ldd.md", "making the refused write there is a\nbypass", "making the refused write there is allowed"),
	defect("safety", "skills", "skills/ldd/references/apply.md", "never create or select another worktree", "create another worktree if needed"),
	defect("audit-registry", "contracts", "contracts/vocabulary.yaml", "  - anchor: isolation/no-other-tree", "  - anchor: isolation/removed-tree"),
	defect("audit-registry", "contracts", "contracts/vocabulary.yaml", "  - anchor: safety/destructive-deny", "  - anchor: safety/removed-deny"),
	defect("audit-registry", "contracts", "contracts/vocabulary.yaml", "  - anchor: safety/outward-ask", "  - anchor: safety/removed-ask"),
	defect("audit-registry", "contracts", "contracts/vocabulary.yaml", "  - anchor: efficiency/writer-scope", "  - anchor: efficiency/removed-scope"),
	defect("manifest", "manifest", "agents.yaml", "    bounded: true", "    bounded: yes"),
	codeDefect("audit-registry", "internal/audit", "engine/internal/auditcheck/auditcheck.go", "\"refusal-bypass\", \"isolation-bypass\",", "\"refusal-bypass\","),

	// delivery — the delivered revision is the tested revision: its anchors are
	// registered to delivered-untested, and the check is exported
	defect("audit-registry", "contracts", "contracts/vocabulary.yaml", "  - anchor: delivery/tested-revision", "  - anchor: delivery/removed-revision"),
	defect("audit-registry", "contracts", "contracts/vocabulary.yaml", "  - anchor: delivery/same-tree", "  - anchor: delivery/removed-tree"),
	defect("audit-registry", "contracts", "contracts/vocabulary.yaml", "  - anchor: delivery/high-risk-verified", "  - anchor: delivery/removed-risk"),
	codeDefect("audit-registry", "internal/audit", "engine/internal/auditcheck/auditcheck.go", "\"isolation-bypass\", \"delivered-untested\",", "\"isolation-bypass\","),
}
