"""Tests for install.sh (the repository root's, jachavarro2004/jhon-agent) and
dist/public/install.sh (the v0.3.0 bridge's, sdsoftwaresas/jhon): fake uname,
curl and cosign on a temp PATH, a fake release on disk, nothing from the
network or the real HOME."""

import hashlib
import io
import os
import shutil
import signal
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ISSUER = "https://token.actions.githubusercontent.com"
REAL_TOOLS = [
    "sh", "tar", "gzip", "sha256sum", "awk", "sed", "grep", "head", "mktemp",
    "rm", "mkdir", "cp", "cat", "printf", "chmod", "basename", "dirname",
]

FAKE_UNAME = """#!/bin/sh
case "$1" in -s) echo "${FAKE_OS:-Linux}" ;; -m) echo "${FAKE_ARCH:-x86_64}" ;; esac
"""
# Serves URLs from $FAKE_WEB by their path after the host; logs each one.
# A missing file is a 404, and /releases/latest answers $FAKE_LATEST_STATUS
# when set; either fails as curl -f does, its "(22)" line on stderr.
FAKE_CURL = """#!/bin/sh
out=""
url=""
fmt=""
while [ $# -gt 0 ]; do
  case "$1" in -o) out="$2"; shift 2 ;; -w) fmt="$2"; shift 2 ;; --proto) shift 2 ;; -*) shift ;; *) url="$1"; shift ;; esac
done
echo "$url" >> "$FAKE_LOG/curl"
case "$url" in *.sigstore.json) [ -z "${FAKE_SLOW:-}" ] || sleep 30 ;; esac
f="$FAKE_WEB/${url#https://*/}"
code=200
[ -f "$f" ] || code=404
case "$url" in */releases/latest) code=${FAKE_LATEST_STATUS:-$code} ;; esac
if [ "$code" != 200 ]; then
  echo "curl: (22) The requested URL returned error: $code" >&2
  [ -z "$fmt" ] || printf '%s' "$code"
  exit 22
fi
if [ -n "$out" ]; then cat "$f" > "$out"; else cat "$f"; fi
[ -z "$fmt" ] || printf '%s' "$code"
"""
# busybox wget's way: -S's response lines and its error on stderr, exit 8.
FAKE_WGET = """#!/bin/sh
out=""
url=""
while [ $# -gt 0 ]; do
  case "$1" in -O) out="$2"; shift 2 ;; -*) shift ;; *) url="$1"; shift ;; esac
done
echo "$url" >> "$FAKE_LOG/wget"
case "$url" in */releases/latest) code=${FAKE_LATEST_STATUS:-404} ;; *) code=404 ;; esac
echo "  HTTP/1.1 $code Whatever" >&2
echo "wget: server returned error: HTTP/1.1 $code Whatever" >&2
exit 8
"""
FAKE_COSIGN = """#!/bin/sh
printf '%s\\n' "$@" > "$FAKE_LOG/cosign"
exit "${FAKE_COSIGN_EXIT:-0}"
"""
# The release's binary: records its calls and links ~/.local/bin/jhon to itself.
FAKE_JHON = """#!/bin/sh
echo "$*" >> "$FAKE_LOG/jhon"
if [ "$1 $2" = "release install" ]; then
  [ -f "$3" ] && [ -f "$3.sha256" ] || exit 9
  mkdir -p "$HOME/.local/bin" && cp "$0" "$HOME/.local/bin/jhon"
fi
"""


class InstallerTest(unittest.TestCase):
    """The repository root's install.sh: releases of jachavarro2004/jhon-agent."""

    INSTALLER = ROOT / "install.sh"
    REPO = "jachavarro2004/jhon-agent"
    IDENTITY = (
        r"^https://github\.com/jachavarro2004/jhon-agent/\.github/workflows/release\.yml"
        r"@refs/tags/v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-rc\.(0|[1-9]\d*))?$"
    )

    @property
    def no_stable(self):
        return (
            "jhon: no stable release is published yet (only pre-releases); pick one at "
            f"https://github.com/{self.REPO}/releases and run: "
            f"curl -fsSL https://raw.githubusercontent.com/{self.REPO}/main/install.sh | JHON_VERSION=<version> sh"
        )

    def setUp(self):
        self.root = Path(tempfile.mkdtemp(prefix="jhon-install-test-"))
        self.addCleanup(shutil.rmtree, self.root, True)
        self.bin = self.root / "bin"
        self.web = self.root / "web"
        self.log = self.root / "log"
        self.home = self.root / "home"
        self.tmp = self.root / "tmp"
        for d in (self.bin, self.web, self.log, self.home, self.tmp):
            d.mkdir()
        for tool in REAL_TOOLS:
            real = shutil.which(tool)
            self.assertIsNotNone(real, tool)
            (self.bin / tool).symlink_to(real)
        self.script("uname", FAKE_UNAME)
        self.script("curl", FAKE_CURL)

    def script(self, name, body):
        p = self.bin / name
        p.write_text(body)
        p.chmod(0o755)

    def release(self, version, arch="amd64", sha_name=None, bad_sha=False, skip=()):
        asset = f"jhon-{version}-linux-{arch}.tar.gz"
        buf = io.BytesIO()
        with tarfile.open(fileobj=buf, mode="w:gz") as tf:
            data = FAKE_JHON.encode()
            info = tarfile.TarInfo("build/jhon")
            info.mode = 0o755
            info.size = len(data)
            tf.addfile(info, io.BytesIO(data))
        blob = buf.getvalue()
        digest = hashlib.sha256(b"other" if bad_sha else blob).hexdigest()
        d = self.web / self.REPO / "releases/download" / f"v{version}"
        d.mkdir(parents=True)
        files = {
            asset: blob,
            asset + ".sha256": f"{digest}  {sha_name or asset}\n".encode(),
            asset + ".sigstore.json": b"{}",
        }
        for name, content in files.items():
            if name not in skip:
                (d / name).write_bytes(content)
        return asset

    def latest(self, tag):
        d = self.web / "repos" / self.REPO / "releases"
        d.mkdir(parents=True, exist_ok=True)
        (d / "latest").write_text('{\n  "tag_name": "%s",\n  "prerelease": false\n}\n' % tag)

    def env(self, path_extra=True, **env):
        e = {
            "PATH": str(self.bin) + (":" + str(self.home / ".local/bin") if path_extra else ""),
            "HOME": str(self.home),
            "TMPDIR": str(self.tmp),
            "FAKE_WEB": str(self.web),
            "FAKE_LOG": str(self.log),
        }
        e.update(env)
        return e

    def run_installer(self, path_extra=True, **env):
        return subprocess.run(
            ["sh", str(self.INSTALLER)], env=self.env(path_extra, **env), stdin=subprocess.DEVNULL,
            capture_output=True, text=True, timeout=30,
        )

    def logged(self, name):
        p = self.log / name
        return p.read_text().splitlines() if p.exists() else []

    def assert_refused(self, r, message):
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn(message, r.stderr)
        self.assertEqual(self.logged("jhon"), [])
        self.assertEqual(list(self.tmp.iterdir()), [])
        self.assertFalse((self.home / ".local/bin/jhon").exists())

    def assert_installed(self, r, version, arch):
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        asset = f"jhon-{version}-linux-{arch}.tar.gz"
        base = f"https://github.com/{self.REPO}/releases/download/v{version}/"
        self.assertEqual(
            [u for u in self.logged("curl") if "/download/" in u],
            [base + asset, base + asset + ".sha256", base + asset + ".sigstore.json"],
        )
        self.assertIn("sha256 verified", r.stdout)
        calls = self.logged("jhon")
        self.assertEqual(len(calls), 1)
        self.assertRegex(calls[0], r"^release install .*/jhon-install\.[^/]+/" + asset.replace(".", r"\.") + "$")
        self.assertEqual(list(self.tmp.iterdir()), [])

    def test_happy_path_amd64_latest(self):
        self.latest("v0.1.0")
        self.release("0.1.0")
        r = self.run_installer()
        self.assert_installed(r, "0.1.0", "amd64")
        self.assertEqual(self.logged("curl")[0], f"https://api.github.com/repos/{self.REPO}/releases/latest")

    def test_happy_path_arm64(self):
        self.latest("v0.1.0")
        self.release("0.1.0", arch="arm64")
        self.assert_installed(self.run_installer(FAKE_ARCH="aarch64"), "0.1.0", "arm64")

    def test_rc_version_uses_its_tag(self):
        self.release("0.1.0-rc.1")
        r = self.run_installer(JHON_VERSION="0.1.0-rc.1")
        self.assert_installed(r, "0.1.0-rc.1", "amd64")
        self.assertNotIn(f"https://api.github.com/repos/{self.REPO}/releases/latest", self.logged("curl"))

    def test_checksum_mismatch(self):
        self.release("0.1.0", bad_sha=True)
        self.assert_refused(self.run_installer(JHON_VERSION="0.1.0"), "checksum mismatch")

    def test_checksum_naming_another_file(self):
        self.release("0.1.0", sha_name="jhon-0.1.0-linux-arm64.tar.gz")
        self.assert_refused(self.run_installer(JHON_VERSION="0.1.0"), "names 'jhon-0.1.0-linux-arm64.tar.gz'")

    def test_missing_asset(self):
        self.release("0.1.0", skip=("jhon-0.1.0-linux-amd64.tar.gz.sigstore.json",))
        self.assert_refused(self.run_installer(JHON_VERSION="0.1.0"), "cannot download")

    def test_cosign_passing(self):
        self.script("cosign", FAKE_COSIGN)
        asset = self.release("0.1.0")
        r = self.run_installer(JHON_VERSION="0.1.0")
        self.assert_installed(r, "0.1.0", "amd64")
        args = self.logged("cosign")
        self.assertEqual(args[0], "verify-blob")
        self.assertEqual(args[args.index("--certificate-identity-regexp") + 1], self.IDENTITY)
        self.assertEqual(args[args.index("--certificate-oidc-issuer") + 1], ISSUER)
        self.assertTrue(args[args.index("--bundle") + 1].endswith(asset + ".sigstore.json"))
        self.assertTrue(args[-1].endswith("/" + asset))
        self.assertIn("signature verified", r.stdout)

    def test_cosign_failing(self):
        self.script("cosign", FAKE_COSIGN)
        self.release("0.1.0")
        r = self.run_installer(JHON_VERSION="0.1.0", FAKE_COSIGN_EXIT="1")
        self.assert_refused(r, "signature of jhon-0.1.0-linux-amd64.tar.gz does not verify")

    def test_cosign_absent(self):
        self.release("0.1.0")
        r = self.run_installer(JHON_VERSION="0.1.0")
        self.assert_installed(r, "0.1.0", "amd64")
        self.assertIn(
            "signature not checked: cosign is not installed (jhon update verifies signatures itself)", r.stdout
        )

    def test_unsupported_os(self):
        self.assert_refused(self.run_installer(FAKE_OS="Darwin"), "Linux only")
        self.assertEqual(self.logged("curl"), [])

    def test_unsupported_arch(self):
        self.assert_refused(self.run_installer(FAKE_ARCH="riscv64"), "unsupported architecture: riscv64")
        self.assertEqual(self.logged("curl"), [])

    def test_invalid_versions(self):
        for v in ("v01.2.3", "0.1.0-beta.1", "01.2.3", "v0.1.0"):
            with self.subTest(v=v):
                self.assert_refused(self.run_installer(JHON_VERSION=v), "JHON_VERSION must be")
        self.assertEqual(self.logged("curl"), [])

    def test_valid_version_table(self):
        # The verifier's probe (T10j): valid_version itself, run alone in sh.
        text = self.INSTALLER.read_text()
        start = text.index("valid_version() {")
        func = text[start:text.index("\n}\n", start) + 3]
        def valid(v):
            return subprocess.run(["sh", "-c", func + 'valid_version "$1"', "_", v],
                                  env={"PATH": str(self.bin)}).returncode == 0
        for v in ("01.0.0", "v0.1.0", "0.1.0-rc.01", "0.1.0-RC.1", "0.1.0-rc.1-rc.2",
                  "", "\n", "0.1.0\n", " ", "0.1.0 ", ";id", "0.1.0;id"):
            with self.subTest(refused=repr(v)):
                self.assertFalse(valid(v))
        for v in ("0.1.0", "10.20.30", "0.2.0-rc.1"):
            with self.subTest(accepted=v):
                self.assertTrue(valid(v))

    def test_latest_naming_a_prerelease_is_refused(self):
        self.latest("v0.1.0-rc.1")
        self.release("0.1.0-rc.1")
        self.assert_refused(self.run_installer(), "pre-release")

    def test_no_stable_release_yet(self):
        # Only pre-releases are published: GitHub's /releases/latest is 404.
        self.release("0.1.0-rc.1")
        r = self.run_installer()
        self.assert_refused(r, "")
        self.assertEqual(r.stdout, "")
        self.assertEqual(r.stderr, self.no_stable + "\n")
        self.assertEqual(self.logged("curl"), [f"https://api.github.com/repos/{self.REPO}/releases/latest"])

    def test_no_stable_release_yet_with_wget(self):
        (self.bin / "curl").unlink()
        self.script("wget", FAKE_WGET)
        r = self.run_installer()
        self.assert_refused(r, "")
        self.assertEqual(r.stderr, self.no_stable + "\n")

    def test_latest_failing_otherwise_keeps_its_error(self):
        for status in ("500", "403"):
            with self.subTest(status=status):
                r = self.run_installer(FAKE_LATEST_STATUS=status)
                self.assert_refused(r, f"cannot read the latest release from https://api.github.com/repos/{self.REPO}/releases/latest")
                self.assertIn(f"returned error: {status}", r.stderr)
                self.assertNotIn("no stable release", r.stderr)
        (self.bin / "curl").unlink()
        self.script("wget", FAKE_WGET)
        r = self.run_installer(FAKE_LATEST_STATUS="503")
        self.assert_refused(r, "cannot read the latest release from")
        self.assertIn("HTTP/1.1 503", r.stderr)

    def test_no_terminal_prints_next_steps(self):
        self.release("0.1.0")
        r = self.run_installer(JHON_VERSION="0.1.0")
        self.assert_installed(r, "0.1.0", "amd64")
        for line in ("jhon setup", "jhon start", "jhon start pi"):
            self.assertIn(line, r.stdout)
        self.assertNotIn("setup", self.logged("jhon"))
        self.assertNotIn("is not on your PATH", r.stdout)

    def test_path_hint(self):
        self.release("0.1.0")
        r = self.run_installer(path_extra=False, JHON_VERSION="0.1.0")
        self.assert_installed(r, "0.1.0", "amd64")
        self.assertIn(f"{self.home}/.local/bin is not on your PATH", r.stdout)
        self.assertIn("export PATH=\"$HOME/.local/bin:$PATH\"", r.stdout)
        self.assertIn("fish_add_path ~/.local/bin", r.stdout)

    def test_multiline_version_refused_before_any_download(self):
        self.release("0.1.0")
        self.assert_refused(self.run_installer(JHON_VERSION="0.1.0\n../x"), "JHON_VERSION must be")
        self.assertEqual(self.logged("curl"), [])

    def test_malicious_latest_tags_refused_before_any_download(self):
        latest = f"https://api.github.com/repos/{self.REPO}/releases/latest"
        for tag in ["v0.1.0;rm -rf ~", "../../etc", "v../0.1.0", "v0.1.0/../../x", "v01.0.0", "0.1.0", "v0.1.0 "]:
            with self.subTest(tag=tag):
                (self.log / "curl").unlink(missing_ok=True)
                self.latest(tag)
                r = self.run_installer()
                self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
                self.assertEqual(self.logged("curl"), [latest])
                self.assertEqual(list(self.home.iterdir()), [])
                self.assertEqual(list(self.tmp.iterdir()), [])

    def test_term_during_download_cleans_up(self):
        (self.bin / "sleep").symlink_to(shutil.which("sleep"))
        self.release("0.1.0")
        p = subprocess.Popen(
            ["sh", str(self.INSTALLER)], env=self.env(JHON_VERSION="0.1.0", FAKE_SLOW="1"),
            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True,
        )
        for _ in range(200):
            if any(u.endswith(".sigstore.json") for u in self.logged("curl")):
                break
            subprocess.run(["sleep", "0.05"])
        os.killpg(p.pid, signal.SIGTERM)
        p.communicate(timeout=10)
        self.assertNotEqual(p.returncode, 0)
        self.assertEqual(list(self.tmp.iterdir()), [])
        self.assertFalse((self.home / ".local").exists())

    @unittest.skipIf(shutil.which("script") is None, "util-linux script is not installed")
    def test_terminal_opens_setup_after_the_path_hint(self):
        # util-linux script gives the installer a pseudo-terminal, so this runs
        # the `jhon setup </dev/tty` branch.
        self.release("0.1.0")
        e = self.env(path_extra=False, JHON_VERSION="0.1.0")
        e["PATH"] = e["PATH"] + ":" + os.path.dirname(shutil.which("script"))
        r = subprocess.run(
            ["script", "-qec", f"sh {self.INSTALLER}", "/dev/null"], env=e, stdin=subprocess.DEVNULL,
            capture_output=True, text=True, timeout=30,
        )
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(self.logged("jhon")[1:], ["setup"])
        self.assertIn("is not on your PATH", r.stdout)
        self.assertNotIn("Next steps:", r.stdout)


class BridgeInstallerTest(InstallerTest):
    """dist/public/install.sh, kept only for the v0.3.0 bridge: releases of
    sdsoftwaresas/jhon, signed by its workflow (T18)."""

    INSTALLER = ROOT / "dist" / "public" / "install.sh"
    REPO = "sdsoftwaresas/jhon"
    IDENTITY = (
        r"^https://github\.com/sdsoftwaresas/jhon/\.github/workflows/release\.yml"
        r"@refs/tags/v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-rc\.(0|[1-9]\d*))?$"
    )


if __name__ == "__main__":
    unittest.main()
