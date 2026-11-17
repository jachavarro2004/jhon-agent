package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// stubInstaller stands in for the release install: it records what it was
// given and writes a jhon into the bench HOME, as `jhon release install` does.
type stubInstaller struct {
	home  string
	env   []string
	write bool
}

func (s *stubInstaller) Install(home string, env []string, _ io.Writer) error {
	s.home, s.env = home, env
	if !s.write {
		return nil
	}
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bin, "jhon"), []byte("#!/bin/sh\n"), 0o755)
}

func TestPrepareInstallsJhonIntoTheBenchHomeOnly(t *testing.T) {
	owner, home := t.TempDir(), filepath.Join(t.TempDir(), "jhon-bench", "home")
	s := &stubInstaller{write: true}
	var out bytes.Buffer
	base := []string{"HOME=" + owner, "JHON_ROOT=/checkout", "JHON_SETTINGS=/settings.toml", "XDG_CONFIG_HOME=" + owner + "/.config", "PATH=/usr/bin", "LANG=C"}
	if err := Prepare(home, owner, base, s, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if s.home != home {
		t.Errorf("installed into %s", s.home)
	}
	for _, kv := range s.env {
		if strings.HasPrefix(kv, "JHON_ROOT=") || strings.HasPrefix(kv, "JHON_SETTINGS=") || strings.Contains(kv, owner) {
			t.Errorf("install env carries %s", kv)
		}
	}
	for _, want := range []string{"HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "LANG=C"} {
		if !slices.Contains(s.env, want) {
			t.Errorf("install env lacks %s: %v", want, s.env)
		}
	}
	if fi, err := os.Stat(filepath.Join(home, ".pi")); err != nil || !fi.IsDir() {
		t.Errorf("no %s/.pi, so the pi profile would not render: %v", home, err)
	}
	for _, want := range []string{"installed Jhon in " + home, "HOME=" + home + " claude", "/login", "bench login-check"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if entries, _ := os.ReadDir(owner); len(entries) != 0 {
		t.Errorf("the owner's HOME was touched: %v", entries)
	}
}

func TestPrepareRefusesTheOwnersHomeAndAnInstallWithNoJhon(t *testing.T) {
	owner := t.TempDir()
	var out bytes.Buffer
	if err := Prepare(owner, owner, nil, &stubInstaller{write: true}, &out); err == nil || !strings.Contains(err.Error(), "is your own HOME") {
		t.Fatalf("err %v", err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := Prepare(home, owner, nil, &stubInstaller{}, &out); err == nil || !strings.Contains(err.Error(), "no jhon at") {
		t.Fatalf("err %v", err)
	}
}

func TestLoginCheckAsksClaudeInTheBenchHome(t *testing.T) {
	f := newFake(t)
	for _, c := range []struct {
		auth string
		ok   bool
		want string
	}{
		{`{"loggedIn":true,"authMethod":"claude.ai"}`, true, "logged in"},
		{`{"loggedIn":false}`, false, "HOME=" + f.home + " claude"},
	} {
		if err := os.WriteFile(filepath.Join(f.dir, "auth.json"), []byte(c.auth), 0o644); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		ok, err := LoginCheck(f.home, &out)
		if err != nil || ok != c.ok || !strings.Contains(out.String(), c.want) {
			t.Errorf("%s: ok %t err %v:\n%s", c.auth, ok, err, out.String())
		}
	}
	calls := strings.Join(f.calls(t), "\n")
	if !strings.Contains(calls, "claude auth status --json |HOME="+f.home+"|") {
		t.Errorf("calls:\n%s", calls)
	}
}

func TestBenchEnvReplacesTheOwnersHomeAndConfig(t *testing.T) {
	env := benchEnv([]string{"HOME=/o", "JHON_ROOT=/c", "CLAUDE_CONFIG_DIR=/o/.claude", "PI_CODING_AGENT_DIR=/o/.pi/agent", "XDG_CACHE_HOME=/o/.cache", "PATH=/usr/bin", "TERM=x"}, "/b", "/r/cache")
	want := []string{"TERM=x", "HOME=/b", "PATH=/b/.local/bin:/usr/bin", "XDG_CONFIG_HOME=/b/.config", "XDG_DATA_HOME=/b/.local/share", "XDG_STATE_HOME=/b/.local/state", "XDG_CACHE_HOME=/r/cache", "ENGRAM_DATA_DIR=/r/cache/engram"}
	if !slices.Equal(env, want) {
		t.Fatalf("env\n%q\nwant\n%q", env, want)
	}
}
