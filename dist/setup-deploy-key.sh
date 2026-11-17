#!/bin/sh
# OBSOLETE (T18): kept only for the v0.3.0 bridge, whose release workflow in
# jachavarro2004/jhon (dist/public) still checks out jachavarro2004/jhon-agents
# with the deploy key this script set up once; the key and secret exist, so
# there is no reason to run it again. jachavarro2004/jhon-agent builds its
# releases from itself and needs no deploy key. It goes with dist/public once
# v0.3.1 is out.
#
# Run once by the owner, before the first release (T10b): gives the public
# release workflow in jachavarro2004/jhon read-only access to
# jachavarro2004/jhon-agents.
#
# It generates an ed25519 pair in a private temp dir, stores the private half
# as the JHON_AGENTS_DEPLOY_KEY secret of jachavarro2004/jhon (fed on stdin),
# adds the public half to jhon-agents as a read-only deploy key, and
# destroys the temp dir on exit. The private key is never printed, logged or
# passed as an argument.
set -eu
umask 077

SOURCE_REPO=jachavarro2004/jhon-agents
RELEASE_REPO=jachavarro2004/jhon
SECRET=JHON_AGENTS_DEPLOY_KEY
TITLE="jachavarro2004/jhon release (read-only)"

die() {
	echo "setup-deploy-key: $*" >&2
	exit 1
}

command -v gh >/dev/null 2>&1 || die "gh is not installed"
command -v ssh-keygen >/dev/null 2>&1 || die "ssh-keygen is not installed"
gh auth status >/dev/null 2>&1 || die "gh is not logged in: run gh auth login"
gh repo view "$RELEASE_REPO" >/dev/null 2>&1 ||
	die "$RELEASE_REPO does not exist or is not reachable: create the repo first"
gh repo view "$SOURCE_REPO" >/dev/null 2>&1 ||
	die "$SOURCE_REPO is not reachable with this gh login"

dir=""
cleanup() {
	[ -n "$dir" ] || return 0
	if command -v shred >/dev/null 2>&1; then
		find "$dir" -type f -exec shred -u {} + 2>/dev/null || :
	fi
	rm -rf "$dir"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
dir=$(mktemp -d)

ssh-keygen -q -t ed25519 -N "" -C "$TITLE" -f "$dir/key" >/dev/null

# The secret first: if it fails, no deploy key is left behind. If adding the
# deploy key fails after it, the secret holds a key nothing accepts, and a
# rerun replaces it.
gh secret set "$SECRET" -R "$RELEASE_REPO" <"$dir/key" >/dev/null
gh repo deploy-key add "$dir/key.pub" -R "$SOURCE_REPO" --title "$TITLE" >/dev/null

echo "Added the read-only deploy key \"$TITLE\" to $SOURCE_REPO"
echo "and stored its private half as $SECRET in $RELEASE_REPO."
