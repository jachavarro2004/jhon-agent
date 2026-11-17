#!/bin/sh
#
# Install jhon-shell's devDependencies when they are missing or stale (L564).
#
# The gates (pre-commit's type check, `bench certify --gates`) need
# shell/node_modules; they once depended on a hand-made symlink. This runs
# `npm ci` from shell/package-lock.json, preferring npm's offline cache, only
# when node_modules is absent or stale: its record of the tree npm built
# (node_modules/.package-lock.json) names other packages or versions than
# the lockfile. By content, not mtime, so a fresh checkout of the same lock
# is current; checking a current tree is one short node run, so a normal
# commit stays fast.
#
# Usage: tools/shell-deps.sh [shell-dir]   (default: <repo>/shell)
# Exit 0 when the tree is current or was installed; 1 with the reason.

set -u

dir="${1:-$(git rev-parse --show-toplevel)/shell}"
lock="$dir/package-lock.json"
mods="$dir/node_modules"
# npm ci writes node_modules/.package-lock.json, its record of the tree it built.
stamp="$mods/.package-lock.json"

if [ ! -f "$lock" ]; then
  echo "shell-deps: $lock not found" >&2
  exit 1
fi
current() {
  [ -f "$stamp" ] && [ -e "$mods/.bin/tsc" ] && command -v node >/dev/null 2>&1 || return 1
  node -e '
    const fs = require("fs");
    const want = JSON.parse(fs.readFileSync(process.argv[1], "utf8")).packages || {};
    const have = JSON.parse(fs.readFileSync(process.argv[2], "utf8")).packages || {};
    const keys = (o) => Object.keys(o).filter((k) => k !== "").sort();
    const same = (a, b) => a.version === b.version && a.resolved === b.resolved && a.integrity === b.integrity;
    const w = keys(want), h = keys(have);
    process.exit(w.length === h.length && w.every((k, i) => k === h[i] && same(want[k], have[k])) ? 0 : 1);
  ' "$lock" "$stamp"
}
if current; then
  exit 0
fi
if [ -L "$mods" ]; then
  # npm ci would remove the tree the link points at, which is not this checkout's.
  echo "shell-deps: $mods is a symlink to a missing or stale tree; remove the link, then run: (cd $dir && npm ci)" >&2
  exit 1
fi
if ! command -v npm >/dev/null 2>&1; then
  echo "shell-deps: npm not found; install Node.js, then run: (cd $dir && npm ci)" >&2
  exit 1
fi
echo "shell-deps: installing jhon-shell's devDependencies (npm ci in $dir)" >&2
if ! (cd "$dir" && npm ci --prefer-offline --no-audit --no-fund >&2); then
  echo "shell-deps: npm ci failed in $dir" >&2
  exit 1
fi
