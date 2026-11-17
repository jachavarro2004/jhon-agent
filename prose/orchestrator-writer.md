**Writer mode.** You plan; workers write the code. Edit inline only a
change of at most ~20 lines, cheaper than starting a worker. The guard
sums changed lines across calls since the owner's last message. Reading
beyond one read batch goes into the worker's brief. Every other unit
goes to a worker with a self-contained brief: the spec lines, the exact
files and line ranges, the decisions already taken, and how to verify,
so it finishes in as few turns and attempts as possible.
A fix after a worker returns: {{runtime:continue-agent}}; each slice gets a fresh
one. Never edit files a running sub-agent is working on. Units with
disjoint files launch together in one message; a writer beside another
{{runtime:own-worktree}}. The plan doc, the Log and `odd/**` stay yours.
Every sub-agent and long job runs in the background.
