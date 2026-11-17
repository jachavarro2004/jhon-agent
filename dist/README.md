# dist

Jhon is one public repository,
[jachavarro2004/jhon-agent](https://github.com/jachavarro2004/jhon-agent):
the source, the tests, the docs, the installers (`install.sh` and
`install.ps1` at the root) and the release workflow
(`.github/workflows/release.yml`) together. Every release from v0.3.1 on is
built, signed and published there, from the tagged commit of the same
repository: no second repository and no deploy key.

The workflow's path matters: `jhon update` trusts signatures from
`https://github.com/jachavarro2004/jhon-agent/.github/workflows/release.yml@refs/tags/vX.Y.Z[-rc.N]`
(`IdentityPattern` in `engine/internal/update/verify.go`), and `install.sh`
verifies the same identity. Moving or renaming the workflow, or changing its
identity check, needs the same change in both. Each of its jobs runs only
when `github.repository` is `jachavarro2004/jhon-agent`, so a tag pushed to a
fork or a private mirror builds nothing.

Signing needs no key: cosign keyless takes the workflow's GitHub OIDC token (`id-token: write`, granted to the build job only; publish alone gets `contents: write`).

## Releasing vX.Y.Z (or vX.Y.Z-rc.N)

1. The `## [X.Y.Z]` section of `CHANGELOG.md` is the release notes; commit it.
2. Tag the commit `vX.Y.Z` in jachavarro2004/jhon-agent and push the tag. The workflow checks the tag's form, checks out the tag, runs `jhon release pack` (which builds jhon and runs its Go tests) on amd64 and arm64, packs the commit again and requires the same bytes (`tools/release/repro.sh`), signs and verifies each tarball and its SBOM, and publishes the release; an `-rc.N` tag is published as a pre-release, so `jhon update` never offers it (test one with `JHON_RELEASES_URL`).

## What is left for the v0.3.0 bridge

Up to v0.3.0, releases came from a second, public repository built from a
private source. v0.3.0 is the bridge (T18): it is the last release built and
signed by that old pair, so the copies installed before the move, which
trust only the old signer, accept it; and it is the first that reads its
updates from jachavarro2004/jhon-agent and trusts both signers
(`IdentityPattern` and `BridgeIdentityPattern` in `verify.go`). What only
the bridge needs stays in `dist/`, unchanged but for its notes:

| here | in the old public repository, sdsoftwaresas/jhon |
| --- | --- |
| `dist/public/README.md` | `README.md`, Jhon's user manual |
| `dist/public/.github/workflows/release.yml` | `.github/workflows/release.yml`: it checks out the private source sdsoftwaresas/jhon-agents at the tag with the `JHON_AGENTS_DEPLOY_KEY` secret and signs as `https://github.com/sdsoftwaresas/jhon/.github/workflows/release.yml@refs/tags/vX.Y.Z[-rc.N]` |
| `dist/public/install.sh` | `install.sh`: the root's, with the old repository and signer |
| `dist/public/install.ps1` | `install.ps1`: the root's, with the old repository's URL |
| `dist/public/LICENSE` | `LICENSE` |

`dist/setup-deploy-key.sh` made that deploy key and secret once (T10b); it
is obsolete and needs no rerun. `.github/workflows/validate.yml` still smokes
`dist/public/install.ps1`, the same script as the root's but for its URL.

Cutting the bridge:

1. In this repository: the `## [0.3.0]` section of `CHANGELOG.md`, committed, and the tag `v0.3.0` pushed to the private origin, sdsoftwaresas/jhon-agents (the root workflow does not run there).
2. Mirror `dist/public/` at that tag into sdsoftwaresas/jhon and commit it on `main`.
3. In sdsoftwaresas/jhon: the same tag, `v0.3.0`, pushed. Its workflow builds, checks, signs and publishes as above.

Until v0.3.1 is published in jachavarro2004/jhon-agent, `jhon update` on
v0.3.0 finds no release there and says no stable release is published yet.
Once v0.3.1 is out and sdsoftwaresas/jhon is deleted, `dist/public/`,
`dist/setup-deploy-key.sh`, their tests and `BridgeIdentityPattern` can go.

## Release assets

Per arch (amd64, arm64), ten assets in all:

| asset | what it is |
| --- | --- |
| `jhon-X.Y.Z-linux-<arch>.tar.gz` | the release: the source at the tag and `build/jhon` |
| `jhon-X.Y.Z-linux-<arch>.tar.gz.sha256` | its checksum, for `sha256sum -c` |
| `jhon-X.Y.Z-linux-<arch>.tar.gz.sigstore.json` | its cosign keyless bundle |
| `jhon-X.Y.Z-linux-<arch>.spdx.json` | its SBOM, SPDX 2.3 JSON, written by `jhon release pack` (`engine/internal/release/sbom.go`) from the module list go embeds in the binary (go.mod and go.sum, replaces included), the Go standard library, and `shell/package-lock.json`'s packages that are not dev-only (none today) |
| `jhon-X.Y.Z-linux-<arch>.spdx.json.sigstore.json` | the SBOM's cosign keyless bundle, the same identity as the tarball's |

The SBOM needs no third-party tool or action: pack writes it, so it describes
exactly the binary in the tarball and is reproducible with it.

## Reproducible builds

The same commit packs to the same bytes. `jhon release pack` builds with
`CGO_ENABLED=0 go build -trimpath -buildvcs=false`, stamps the version, the
commit and the commit's date through `-ldflags -X` (never the clock), and
writes the tarball itself: entries sorted by name, every mtime the commit's
time, owner 0:0 with no user or group names, modes 0755/0644, GNU headers
with no PAX records, and a gzip header with no name or time. The bytes depend
on the Go toolchain too, which `setup-go` takes from `go.mod`. To check a
release locally, pack it, then run `tools/release/repro.sh build/jhon
<version> <out-dir>` in the same checkout.

## CI cost

Every job of `release.yml` has a time limit, so no run can bill GitHub's
default 6 hours: check the tag 5 minutes, build 35 (rc1 took 14m24s on amd64 and
13m30s on arm64; that build also compiled a terminal multiplexer the release
no longer ships), publish 15. One run per tag; a
second push of the same tag waits rather than cancelling a release half-way.

The release runs in a public repository, where GitHub's standard hosted
runners are free ([GitHub Actions billing](https://docs.github.com/en/billing/managing-billing-for-your-products/managing-billing-for-github-actions/about-billing-for-github-actions)).
The `validate` workflow was disabled while the source was private and billed,
after hung runs reached the 6-hour limit and cost about 35 USD in a day. Its
file now caps every step and cancels a superseded run, but it stays disabled
until the owner turns it back on. It has a second job, `windows` (20
minutes): it builds, vets and tests the packages that compile for Windows
(the `PORTABLE` list, which `test/release` vets for `GOOS=windows`; the rest
of the module is Linux-only) and checks that `install.ps1` parses and fails
cleanly in PowerShell 7 and 5.1 on a runner with no WSL distro to install
into. install.ps1 has no local source, so no install runs there.

The installers' tests are `dist/test/*.py`: both `install.sh` copies run in
full, and `install.ps1` is checked statically. Run them with
`python3 -I -m unittest discover -s dist/test -p 'test_*.py'`.
