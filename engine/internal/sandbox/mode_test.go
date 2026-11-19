package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

// T128f (L1352 F1, slice 8 L1375): with no owner value sandbox.mode is on
// where the sandbox can run (Linux, Check finds no problem) and off where it
// cannot, with Check's problem as the reason; off Linux the default reads
// as off (D3). A value in the settings file is the owner's on every system,
// and the host is then never checked.
func TestModeIsOnByDefaultWhereTheSandboxCanRun(t *testing.T) {
	dir := t.TempDir()
	installed := with(machine, disk{"/usr/bin": "dir", "/usr/bin/bwrap": "file"})
	userns := map[string]string{"/proc/sys/user/max_user_namespaces": "0\n"}
	input := func(d disk) Input {
		in := sample(Work, d)
		in.Path = []string{"/usr/bin"}
		return in
	}
	planted := with(machine, disk{"/home/u/project/bin": "dir", "/home/u/project/bin/bwrap": "file"})
	inPlanted := input(planted)
	inPlanted.Path = []string{"/home/u/project/bin"}
	for _, c := range []struct {
		name, file   string
		host         Host
		in           Input
		mode, source string
		why          string
	}{
		{"linux, runnable", "", host("linux", installed, nil), input(installed), "on", ModeDefault, ""},
		{"linux, no bubblewrap", "", host("linux", machine, nil), input(machine), "off", ModeDefault, "bubblewrap (bwrap) is not on PATH"},
		{"linux, bubblewrap only where a command may write", "", host("linux", planted, nil), inPlanted, "off", ModeDefault, "bubblewrap (bwrap) is on PATH only in a folder a sandboxed command may write (/home/u/project/bin)"},
		{"linux, user namespaces off", "", host("linux", installed, userns), input(installed), "off", ModeDefault, "unprivileged user namespaces are off (user.max_user_namespaces = 0)"},
		{"darwin", "", Host{OS: "darwin"}, Input{}, "off", ModeDefault, ""},
		{"windows", "", Host{OS: "windows"}, Input{}, "off", ModeDefault, ""},
		{"linux, owner on, runnable", "[sandbox]\nmode = \"on\"\n", host("linux", installed, nil), input(installed), "on", ModeSettings, ""},
		{"linux, owner on, no bubblewrap", "[sandbox]\nmode = \"on\"\n", host("linux", machine, nil), input(machine), "on", ModeSettings, ""},
		{"darwin, owner on", "[sandbox]\nmode = \"on\"\n", Host{OS: "darwin"}, Input{}, "on", ModeSettings, ""},
		{"linux, owner off", "[sandbox]\nmode = \"off\"\n", host("linux", installed, nil), input(installed), "off", ModeSettings, ""},
		{"windows, owner off", "[sandbox]\nmode = \"off\"\n", Host{OS: "windows"}, Input{}, "off", ModeSettings, ""},
	} {
		file := filepath.Join(dir, "settings.toml")
		if err := os.WriteFile(file, []byte(c.file), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("JHON_SETTINGS", file)
		asked := 0
		mode, source, why := Mode(c.host, func() Input { asked++; return c.in })
		got := ""
		if why != nil {
			got = why.Reason
		}
		if mode != c.mode || source != c.source || got != c.why {
			t.Errorf("%s: %s (%s), why %q; want %s (%s), why %q", c.name, mode, source, got, c.mode, c.source, c.why)
		}
		// The input is built only to check a Linux host with no owner value.
		want := 0
		if c.file == "" && c.host.OS == "linux" {
			want = 1
		}
		if asked != want {
			t.Errorf("%s: the input was built %d times; want %d", c.name, asked, want)
		}
	}
}
