"""Static checks of dist/setup-deploy-key.sh. The script is never run here: it
creates a real deploy key and secret. These read its text and check that the
private key can only reach gh's stdin and is destroyed on exit."""

import os
import re
import shutil
import signal
import subprocess
import tempfile
import time
import unittest
from pathlib import Path

SCRIPT_PATH = Path(__file__).resolve().parents[1] / "setup-deploy-key.sh"
SCRIPT = SCRIPT_PATH.read_text()
CODE = [l.strip() for l in SCRIPT.splitlines() if l.strip() and not l.lstrip().startswith("#")]

# The private half, as a path: "$dir/key" not followed by ".pub".
PRIVATE = re.compile(r'"?\$\{?dir\}?/key"?(?!\.pub)')


class TestSetupDeployKey(unittest.TestCase):
    def test_says_it_is_obsolete_but_for_the_bridge(self):
        self.assertTrue(SCRIPT.startswith("#!/bin/sh\n# OBSOLETE (T18): kept only for the v0.3.0 bridge"))

    def test_strict_shell_and_private_umask(self):
        self.assertIn("set -eu", CODE)
        self.assertIn("umask 077", CODE)
        self.assertLess(CODE.index("umask 077"), next(i for i, l in enumerate(CODE) if "mktemp" in l))

    def test_parses_as_posix_sh(self):
        subprocess.run(["sh", "-n", str(SCRIPT_PATH)], check=True)

    def test_key_in_a_mktemp_dir(self):
        self.assertIn("dir=$(mktemp -d)", CODE)
        self.assertRegex(SCRIPT, r'ssh-keygen [^\n]*-t ed25519[^\n]*-f "\$dir/key"')

    def test_private_key_never_printed(self):
        for line in CODE:
            if PRIVATE.search(line):
                self.assertNotRegex(line, r"\b(echo|cat|printf|tee|less|more|head|tail)\b", line)

    def test_private_key_only_on_stdin(self):
        uses = [l for l in CODE if PRIVATE.search(l) and not l.startswith("ssh-keygen")]
        self.assertEqual(uses, ['gh secret set "$SECRET" -R "$RELEASE_REPO" <"$dir/key" >/dev/null'])
        self.assertIn("SECRET=JHON_AGENTS_DEPLOY_KEY", CODE)
        self.assertIn("RELEASE_REPO=jachavarro2004/jhon", CODE)

    def test_read_only_deploy_key(self):
        self.assertNotIn("--allow-write", SCRIPT)
        self.assertIn('gh repo deploy-key add "$dir/key.pub" -R "$SOURCE_REPO" --title "$TITLE" >/dev/null', CODE)
        self.assertIn("SOURCE_REPO=jachavarro2004/jhon-agents", CODE)
        self.assertIn('TITLE="jachavarro2004/jhon release (read-only)"', CODE)

    def test_trap_shreds_the_dir(self):
        # The traps come before mktemp, so no signal can leave the dir behind.
        self.assertIn("trap cleanup EXIT", CODE)
        self.assertLess(CODE.index("trap cleanup EXIT"), CODE.index("dir=$(mktemp -d)"))
        self.assertLess(CODE.index("trap 'exit 143' TERM"), CODE.index("dir=$(mktemp -d)"))
        body = SCRIPT.split("cleanup() {", 1)[1].split("\n}", 1)[0]
        self.assertIn("shred -u", body)
        self.assertIn('rm -rf "$dir"', body)

    def test_checks_gh_and_the_repo_first(self):
        first_key = next(i for i, l in enumerate(CODE) if "mktemp" in l)
        before = "\n".join(CODE[:first_key])
        self.assertIn("gh auth status", before)
        self.assertIn('gh repo view "$RELEASE_REPO"', before)
        self.assertIn("create the repo first", before)


MARKER = "PRIVATE-KEY-MARKER-3817645676228d32"
# ssh-keygen: writes the marker as the private key and a public key; FAKE_HANG
# makes it wait, so a signal can arrive while the key is on disk.
FAKE_KEYGEN = """#!/bin/sh
while [ $# -gt 0 ]; do case "$1" in -f) f="$2"; shift 2 ;; *) shift ;; esac; done
echo keygen >> "$FAKE_LOG/calls"
printf '%s\\n' "$MARKER" > "$f"
echo "ssh-ed25519 AAAA test" > "$f.pub"
echo "$f" > "$FAKE_LOG/keyfile"
[ -z "${FAKE_HANG:-}" ] || sleep 30
"""
# gh: logs argv and stdin per call; FAIL_VIEW / FAIL_SECRET make those fail.
FAKE_GH = """#!/bin/sh
echo "gh $*" >> "$FAKE_LOG/argv"
case "$1 $2" in
  "repo view") [ "$3" = jachavarro2004/jhon ] && [ -n "${FAIL_VIEW:-}" ] && exit 1 ;;
  "secret set") cat > "$FAKE_LOG/secret-stdin"; echo secret >> "$FAKE_LOG/calls"; [ -z "${FAIL_SECRET:-}" ] || exit 1 ;;
  "repo deploy-key") echo deploy-key >> "$FAKE_LOG/calls" ;;
esac
exit 0
"""


class TestSetupDeployKeyWithFakes(unittest.TestCase):
    """Runs the script with fake gh and ssh-keygen on a temp PATH."""

    def setUp(self):
        self.root = Path(tempfile.mkdtemp(prefix="jhon-deploy-key-test-"))
        self.addCleanup(shutil.rmtree, self.root, True)
        self.bin, self.log, self.tmp = self.root / "bin", self.root / "log", self.root / "tmp"
        for d in (self.bin, self.log, self.tmp):
            d.mkdir()
        for tool in ["sh", "mktemp", "rm", "find", "shred", "cat", "sleep", "printf"]:
            real = shutil.which(tool)
            if real:
                (self.bin / tool).symlink_to(real)
        for name, body in (("gh", FAKE_GH), ("ssh-keygen", FAKE_KEYGEN)):
            (self.bin / name).write_text(body)
            (self.bin / name).chmod(0o755)

    def env(self, **extra):
        e = {"PATH": str(self.bin), "TMPDIR": str(self.tmp), "FAKE_LOG": str(self.log), "MARKER": MARKER}
        e.update(extra)
        return e

    def read(self, name):
        p = self.log / name
        return p.read_text() if p.exists() else ""

    def run_script(self, **extra):
        return subprocess.run(["sh", str(SCRIPT_PATH)], env=self.env(**extra), stdin=subprocess.DEVNULL,
                              capture_output=True, text=True, timeout=30)

    def test_key_reaches_only_the_secret_stdin_and_the_dir_goes(self):
        r = self.run_script()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(self.read("calls").split(), ["keygen", "secret", "deploy-key"])
        self.assertEqual(self.read("secret-stdin").strip(), MARKER)
        for text in (r.stdout, r.stderr, self.read("argv")):
            self.assertNotIn(MARKER, text)
        self.assertNotIn("--allow-write", self.read("argv"))
        self.assertEqual(list(self.tmp.iterdir()), [])

    def test_failed_secret_adds_no_deploy_key(self):
        r = self.run_script(FAIL_SECRET="1")
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual(self.read("calls").split(), ["keygen", "secret"])
        self.assertEqual(list(self.tmp.iterdir()), [])

    def test_missing_release_repo_generates_nothing(self):
        r = self.run_script(FAIL_VIEW="1")
        self.assertEqual(r.returncode, 1)
        self.assertIn("create the repo first", r.stderr)
        self.assertEqual(self.read("calls"), "")
        self.assertEqual(list(self.tmp.iterdir()), [])

    def test_signals_remove_the_dir(self):
        for sig, code in ((signal.SIGTERM, 143), (signal.SIGINT, 130)):
            with self.subTest(sig=sig):
                (self.log / "keyfile").unlink(missing_ok=True)
                # A non-interactive sh started in the background ignores SIGINT
                # unless it is reset, so start it in its own session.
                p = subprocess.Popen(["sh", str(SCRIPT_PATH)], env=self.env(FAKE_HANG="1"),
                                     stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                     stderr=subprocess.PIPE, start_new_session=True)
                for _ in range(200):
                    if self.read("keyfile"):
                        break
                    time.sleep(0.05)
                self.assertTrue(self.read("keyfile"), "ssh-keygen never ran")
                os.killpg(p.pid, sig)
                p.communicate(timeout=10)
                self.assertEqual(p.returncode, code)
                self.assertEqual(list(self.tmp.iterdir()), [])


if __name__ == "__main__":
    unittest.main()
