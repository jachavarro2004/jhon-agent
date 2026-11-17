#!/usr/bin/env bash
# Reproducibility check for a release (T52): pack the same commit again and
# compare it, byte for byte, with an earlier pack.
#   tools/release/repro.sh <jhon> <version> <reference-dir>
# Run from the checkout the reference was packed from. The repack skips the
# Go tests (JHON_SYNC_GO_TEST=0): the reference pack already ran them.
set -euo pipefail
[ $# -eq 3 ] || { echo "usage: $0 <jhon> <version> <reference-dir>" >&2; exit 2; }
jhon=$1 version=$2 ref=$3
again=$(mktemp -d)
trap 'rm -rf "$again"' EXIT
JHON_SYNC_GO_TEST=0 "$jhon" release pack --version "$version" --out "$again" >/dev/null
status=0
found=0
for f in "$ref"/jhon-"$version"-linux-*.tar.gz "$ref"/jhon-"$version"-linux-*.spdx.json; do
  [ -f "$f" ] || continue
  found=$((found + 1))
  name=$(basename "$f")
  a=$(sha256sum <"$f" | cut -d' ' -f1)
  b=$(sha256sum 2>/dev/null <"$again/$name" | cut -d' ' -f1 || true)
  if [ "$a" = "$b" ]; then
    echo "reproducible: $name $a"
  else
    echo "::error::$name is not reproducible: $a, then ${b:-missing}"
    status=1
  fi
done
[ "$found" -gt 0 ] || { echo "::error::no jhon-$version artifact in $ref"; exit 1; }
exit "$status"
