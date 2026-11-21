# Security policy

Jhon runs next to a coding agent with access to your files, your shell and
your credentials, so a security problem in it matters. Thank you for
reporting one privately.

## Supported versions

Only the latest release gets security fixes. Update with `jhon update` before
you report, and say which version you ran (`jhon version`).

## Reporting a vulnerability

Please do not open a public issue, discussion or pull request for a
vulnerability.

- **Preferred:** open a private report from the repository's Security tab
  (Report a vulnerability). It reaches the maintainer only.
- **If you cannot use GitHub:** write to jhon87195@outlook.com.

Include what you can:

- what an attacker or a misbehaving agent can do, and what it needs first;
- the steps or the command that shows it, and the Jhon version and runtime
  (Claude Code or pi);
- whether a guard, the sandbox or a grant was meant to stop it.

## What to expect

- An acknowledgement within 7 days.
- An assessment, and a fix or a mitigation plan, as soon as the problem is
  understood. You are kept informed along the way.
- Credit in the release notes and the advisory, unless you ask not to be
  named.

## Scope

In scope: the `jhon` binary, the guards and the sandbox, the shell extension,
the dashboard, the installer and the release artifacts (their signatures and
the SBOM).

Out of scope: problems in Claude Code, pi, a model provider or another
upstream project; report those to them. Behaviour that a guard is documented
not to cover (see the README) is not a vulnerability, though a report that
shows a realistic accidental harm it should cover is welcome.
