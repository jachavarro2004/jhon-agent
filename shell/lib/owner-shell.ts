import { basename } from "node:path";

/**
 * The line that tells the orchestrator how the owner runs a command it hands
 * them, read from SHELL. pi has no `!` for the owner (L222): the command is a
 * plain line for their own terminal, written for their shell, so a POSIX
 * shell (or none) gets the line too. Claude Code's note, which keeps `!`, is
 * engine/internal/hook OwnerShellNote.
 */
export function ownerShellNote(shell: string | undefined): string {
  const lead = "pi has no `!`: a command you hand the owner is a plain line they run in their own terminal, written for";
  const name = basename(shell ?? "");
  switch (name) {
    case "fish":
      return `- The owner's shell is fish. ${lead} fish: \`set -x NAME value\` or \`env NAME=value cmd\`, never \`export\`; \`(cmd)\` or \`$(cmd)\`; no heredocs, \`[[ ]]\`, or \`for x in ...; do ...; done\` (fish: \`for x in ...; ...; end\`). Your own Bash tool stays POSIX.`;
    case "nu":
      return `- The owner's shell is nushell. ${lead} nu, or wrapped in \`bash -c '...'\`. Your own Bash tool stays POSIX.`;
  }
  return `- ${lead} ${name || "a POSIX shell"}.`;
}
