# What safety-guard reads, and where it stops

safety-guard reads what a Bash line will run when it can read it: the
command itself, the strings a shell, an interpreter or a runner is given,
the text a program reads on its input or from a `<(…)` file, a script or a
git configuration file on disk, and a value a program runs as a command.
When it cannot read what will run (a fetched or generated script, a
descriptor it does not follow), it asks at the top level and refuses a
sub-agent. It is a filter, not a container: it reads the text of a line,
and some spellings still slip past it. What contains a command is the OS
sandbox (`sandbox.mode on`): the project, the temporary folders and the
caches are writable, everything else is read-only, whatever the guard let
through. In super-yolo, the owner's own mode, a line the guard cannot read
runs with a note, so there the sandbox is the only containment.
