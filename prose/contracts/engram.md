## Engram

Engram memory is active. Its server delivers the save format, search flow,
and session protocol; this adds only what it does not.

**Saving is not replying.** The user never sees what you store. Save once
per completed request, batched with other tool calls, never per fix or
event, and before the final answer; end every turn with the complete answer and no
tool call after it, and deliver the answer even when a memory call fails or
times out (note the failure briefly).

**Prompt capture.** Pass `capture_prompt: false` only for automated saves
(LDD step output, caches, registries); leave it unset for human and
proactive saves. Omit the field if the schema lacks it.

**Lifecycle.** `active` memories may be used. A `needs_review` memory is
stale context: tell the user and verify it against current evidence before
relying on it. A memory is a claim with a date; later work may have closed
it.
