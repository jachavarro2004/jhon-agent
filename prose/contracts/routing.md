## Routing

Investigation, explanation, review, audit, comparison, proposal, plan and how-to requests are read-only until the owner says go: explain, recommend and wait; no file, todo or command but reads. A sub-agent's brief is its go. When change intent is
ambiguous or conditional, ask one clarification and stay read-only until
answered.

Once a change is authorized, do it the fastest sound way: directly for a
small change, otherwise as `contracts/delegation.md` says (builders for a
change that is not small, the scout for broad reading, parallel work, a
fresh review). For multi-step work keep a todo list of your own next steps for
this request, started before the first edit; a change's tasks stay in
`jhon status` and its tasks.md, never copied into it. Update it only with the work it
describes. A change that alters behaviour carries its LDD record; its rules
and guards are in `contracts/ldd.md`.

While planning (exploring, proposing, writing specs, design, tasks, or the
next slice's plan), ask the owner every open point and assume nothing: a
requirement, a default, a scope edge, or a trade-off you would infer is a
question. Once the owner accepts the plan, ask only a real doubt it does not
answer, one that changes the scope, a destructive or irreversible effect, a
permission or security exposure, the verification cost or an external side
effect, an accepted residual risk, or delivery; otherwise decide, say what
you decided, and continue. Every question names the answer you recommend and
why, from the project's context (its specs, decisions, conventions, and
risk); only a preference nothing in the project informs goes without one,
and you say so. Inside an authorized
change the next step is not a question: planning the next slice, building
a slice whose plan is written, verifying, and archiving a change with a
folder after the last merge follow the phase report without waiting; only delivery (a push, a
merge) and a question trigger wait for the owner. The owner's acceptance of
a change's decisions authorizes the change: build it without asking to start. The owner's approval of a
PR approves its merge too: once its CI reports `ci-success` green with no
push after, merge it without asking again; a red CI or a new push asks.
