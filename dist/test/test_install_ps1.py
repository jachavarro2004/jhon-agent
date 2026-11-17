"""Static checks of install.ps1 (the repository root's, jachavarro2004/jhon-agent;
dist/public/install.ps1 is the v0.3.0 bridge's copy). PowerShell is not run here (no
pwsh on the Linux machine that wrote these): they read the script's text and
check its command strings, its messages and its version regex, which is
also run against install.sh's own valid_version."""

import re
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PUBLIC = ROOT / "dist" / "public"
SCRIPT = (ROOT / "install.ps1").read_text()
INSTALL_SH = ROOT / "install.sh"


def dotnet(pattern):
    """A .NET pattern as Python's re reads it: \\z ends the string in both."""
    return pattern.replace(r"\z", r"\Z")


def valid_version_sh(tmp):
    """install.sh's valid_version, alone, in a file sh can source."""
    text = INSTALL_SH.read_text()
    start = text.index("valid_version() {")
    body = text[start:text.index("\n}\n", start) + 3]
    path = Path(tmp) / "valid_version.sh"
    path.write_text(body)
    return path


def ps_single_quoted(name):
    m = re.search(r"^\s*\$" + name + r" = '([^']*)'$", SCRIPT, re.M)
    assert m, name
    return m.group(1)


_TMP = tempfile.TemporaryDirectory(prefix="jhon-ps1-test-")
VALID_VERSION_SH = valid_version_sh(_TMP.name)


class TestInstallPs1(unittest.TestCase):
    def test_strict_mode_and_stop_stay_inside_the_script_block(self):
        # Through `irm | iex` the script runs in the caller's scope: StrictMode,
        # the error preference and the functions must not stay in the session.
        body = SCRIPT.split("\n& {\n", 1)
        self.assertEqual(len(body), 2, "the script runs its body in & { ... }")
        self.assertNotIn("Set-StrictMode", body[0])
        self.assertNotIn("function ", body[0])
        self.assertIn("    Set-StrictMode -Version Latest", body[1])
        self.assertIn("    $ErrorActionPreference = 'Stop'", body[1])

    def test_wsl_stderr_does_not_raise_native_command_error(self):
        wsl = SCRIPT.split("function Wsl {", 1)[1].split("\n    }", 1)[0]
        self.assertIn("$ErrorActionPreference = 'Continue'", wsl)
        self.assertLess(wsl.index("'Continue'"), wsl.index("& wsl.exe @args 2>&1"))
        self.assertRegex(SCRIPT, r"\$ErrorActionPreference = 'Continue'\n\s*& wsl\.exe @pick -e sh -c \$command")

    def test_version_check_is_case_sensitive_and_anchored_at_the_end(self):
        self.assertIn("$env:JHON_VERSION -cnotmatch $VersionPattern", SCRIPT)
        self.assertTrue(ps_single_quoted("VersionPattern").endswith("\\z"))
        self.assertTrue(ps_single_quoted("DistroPattern").endswith("\\z"))

    def test_windows_powershell_51_syntax(self):
        code = "\n".join(l for l in SCRIPT.splitlines() if not l.lstrip().startswith("#"))
        self.assertNotIn("?.", code)
        self.assertNotIn("??", code)
        self.assertIsNone(re.search(r"\s\?\s[^:]+\s:\s", code), "ternary")
        self.assertNotIn("&&", code)
        self.assertNotIn("||", code)

    def test_no_wsl_explains_install_and_exits_1(self):
        self.assertIn("Get-Command wsl.exe", SCRIPT)
        self.assertIn("WSL is not installed", SCRIPT)
        self.assertRegex(SCRIPT, r"function Fail\(\[string\]\$Message, \[int\]\$Code = 1\)")
        self.assertIn("if ($PSCommandPath) { exit $LASTEXITCODE }", SCRIPT)

    def test_checks_status_list_version_and_linux(self):
        for needle in ["Wsl --status", "Wsl -l -v", "$target.Version -ne 2",
                       "Wsl @pick -e uname -s", "Wsl @pick -e sh -c 'command -v curl'"]:
            self.assertIn(needle, SCRIPT)

    def test_no_distro_points_to_wsl_install_without_running_it(self):
        self.assertIn("WSL has no Linux distribution installed.", SCRIPT)
        self.assertIn('"    wsl --install`n"', SCRIPT)
        self.assertNotRegex(SCRIPT, r"&\s*wsl\.exe\s+--install")

    def test_happy_path_command(self):
        self.assertEqual(ps_single_quoted("InstallShUrl"),
                         "https://raw.githubusercontent.com/jachavarro2004/jhon-agent/main/install.sh")
        self.assertIn('$command = "curl -fsSL $InstallShUrl | ${envPrefix}sh"', SCRIPT)
        self.assertIn("& wsl.exe @pick -e sh -c $command", SCRIPT)
        self.assertIn("$code = $LASTEXITCODE", SCRIPT)
        self.assertIn('Fail "the Linux installer failed inside $name (exit $code)" $code', SCRIPT)

    def test_distro_passes_dash_d(self):
        self.assertIn("[string]$Distro = ''", SCRIPT)
        self.assertIn("$pick = @('-d', $name)", SCRIPT)
        pattern = re.compile(dotnet(ps_single_quoted("DistroPattern")))
        for ok in ["Ubuntu", "Ubuntu-24.04", "Debian", "openSUSE-Tumbleweed"]:
            self.assertTrue(pattern.match(ok), ok)
        for bad in ["", "a b", "x;rm", "$(x)", "-d"]:
            self.assertFalse(pattern.match(bad), bad)

    def test_version_regex_equals_install_sh(self):
        self.assertIn('$envPrefix = "JHON_VERSION=$($env:JHON_VERSION) "', SCRIPT)
        self.assertIn("JHON_VERSION must be X.Y.Z or X.Y.Z-rc.N", SCRIPT)
        ps = ps_single_quoted("VersionPattern")
        self.assertIn(ps[:-2] + "$", INSTALL_SH.read_text())
        pattern = re.compile(dotnet(ps))
        cases = ["0.1.0", "10.20.30", "0.2.0-rc.1", "v0.1.0", "01.2.3", "0.2.0-rc.01", "0.1",
                 "0.1.0-beta", "0.1.0; rm -rf ~", "0.1.0 ", "", "0.1.0-RC.1", "0.1.0\n", "0.1.0\n../x"]
        for v in cases:
            sh = subprocess.run(
                ["sh", "-c", '. "$1"; valid_version "$2"', "sh", str(VALID_VERSION_SH), v]).returncode == 0
            self.assertEqual(bool(pattern.match(v)), sh, repr(v))

    def test_after_install_says_how_to_start(self):
        self.assertIn('"  wsl -d $name -e jhon start"', SCRIPT)
        self.assertIn("run: jhon start", SCRIPT)
        self.assertIn("Native Windows (no WSL) is not supported yet.", SCRIPT)

    def test_bridge_copy_differs_only_by_repository(self):
        # dist/public keeps the installers of the old public repository for
        # the v0.3.0 bridge (T18): the same scripts, its own repository.
        for name in ("install.ps1", "install.sh"):
            bridge = (PUBLIC / name).read_text()
            self.assertIn("sdsoftwaresas/jhon/main/install.sh", bridge)
            moved = (bridge.replace("sdsoftwaresas/jhon/main/", "jachavarro2004/jhon-agent/main/")
                     .replace('REPO="sdsoftwaresas/jhon"', 'REPO="jachavarro2004/jhon-agent"')
                     .replace(r"github\.com/sdsoftwaresas/jhon/", r"github\.com/jachavarro2004/jhon-agent/"))
            self.assertEqual(moved, (ROOT / name).read_text(), name)


if __name__ == "__main__":
    unittest.main()
