package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

// T128f (L1352 F1, slice 8 L1375, 8b L1379): with no owner value
// sandbox.mode is on where the probe starts a sandbox (Linux) and off where
// it does not, with the probe's problem; off Linux the default reads as off
// (D3). A value in the settings file is the owner's on every system, and
// the host is then never probed.
func TestModeIsOnByDefaultWhereTheSandboxCanRun(t *testing.T) {
	dir := t.TempDir()
	was := started
	t.Cleanup(func() { started = was })
	starts := 0
	started = func(string) *Problem { starts++; return nil }
	blocked := &Problem{"bubblewrap cannot start a sandbox (bwrap: setting up uid map: Permission denied)", usernsFix}
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
		start        *Problem
		mode, source string
		why          string
	}{
		{"linux, runnable", "", host("linux", installed, nil), input(installed), nil, "on", ModeDefault, ""},
		{"linux, no bubblewrap", "", host("linux", machine, nil), input(machine), nil, "off", ModeDefault, "bubblewrap (bwrap) is not on PATH"},
		{"linux, bubblewrap only where a command may write", "", host("linux", planted, nil), inPlanted, nil, "off", ModeDefault, "bubblewrap (bwrap) is on PATH only in a folder a sandboxed command may write (/home/u/project/bin)"},
		{"linux, user namespaces off", "", host("linux", installed, userns), input(installed), nil, "off", ModeDefault, "unprivileged user namespaces are off (user.max_user_namespaces = 0)"},
		{"linux, bubblewrap cannot start", "", host("linux", installed, nil), input(installed), blocked, "off", ModeDefault, blocked.Reason},
		{"linux, owner on, bubblewrap cannot start", "[sandbox]\nmode = \"on\"\n", host("linux", installed, nil), input(installed), blocked, "on", ModeSettings, ""},
		{"darwin", "", Host{OS: "darwin"}, Input{}, nil, "off", ModeDefault, ""},
		{"windows", "", Host{OS: "windows"}, Input{}, nil, "off", ModeDefault, ""},
		{"linux, owner on, runnable", "[sandbox]\nmode = \"on\"\n", host("linux", installed, nil), input(installed), nil, "on", ModeSettings, ""},
		{"linux, owner on, no bubblewrap", "[sandbox]\nmode = \"on\"\n", host("linux", machine, nil), input(machine), nil, "on", ModeSettings, ""},
		{"darwin, owner on", "[sandbox]\nmode = \"on\"\n", Host{OS: "darwin"}, Input{}, nil, "on", ModeSettings, ""},
		{"linux, owner off", "[sandbox]\nmode = \"off\"\n", host("linux", installed, nil), input(installed), nil, "off", ModeSettings, ""},
		{"windows, owner off", "[sandbox]\nmode = \"off\"\n", Host{OS: "windows"}, Input{}, nil, "off", ModeSettings, ""},
	} {
		file := filepath.Join(dir, "settings.toml")
		if err := os.WriteFile(file, []byte(c.file), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("JHON_SETTINGS", file)
		asked := 0
		starts = 0
		started = func(string) *Problem { starts++; return c.start }
		e := Mode(c.host, func() Input { asked++; return c.in })
		got := ""
		if e.Problem != nil {
			got = e.Problem.Reason
		}
		if e.Mode != c.mode || e.Source != c.source || got != c.why {
			t.Errorf("%s: %+v, why %q; want %s (%s), why %q", c.name, e, got, c.mode, c.source, c.why)
		}
		// The input is built and probed only for a Linux host with no
		// owner value, and bubblewrap starts only where Check passes.
		want, wantStarts := 0, 0
		if c.file == "" && c.host.OS == "linux" {
			want = 1
			if c.why == "" || c.start != nil {
				wantStarts = 1
			}
		}
		if asked != want || e.Probed != (want == 1) || starts != wantStarts {
			t.Errorf("%s: input built %d times, probed %v, bubblewrap started %d times; want %d, %v, %d", c.name, asked, e.Probed, starts, want, want == 1, wantStarts)
		}
	}
}
