// T238 (odd/tasks/studio-integration-v1.md S5): /jhon:video's launch path
// with Jhon's sandbox on, on a stand-in jhon-studio installed as its manifest
// says (~/.local/share/jhon-studio, linked from ~/.local/bin). The real
// program's Chrome inside the sandbox is jhon-studio's own work (its T9);
// this proves Jhon's side: the launch reads the tool's answer, and the
// skill's first steps, run as the agent runs them (`jhon sandbox run
// --project <project> --profile work -- sh -c <line>`), reach the tool and
// write in the project's .jhon/ and the cache, while the sandbox still
// refuses a write outside.
package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// studioStandIn installs a stand-in jhon-studio in the box's home: its
// `integration --json` names its plugin folder and its skill and declares
// the jhon it needs; `doctor` passes; `init` writes .jhon/studio.yaml;
// `video brief readme` writes the brief under .jhon/studio/<id>/ and a
// cache file, and prints the brief's path. It puts ~/.local/bin on PATH.
func studioStandIn(t *testing.T, b *box) (plugin, skill string) {
	t.Helper()
	share := filepath.Join(b.home, ".local", "share", "jhon-studio")
	plugin = filepath.Join(share, "plugin", "claude-code")
	skill = filepath.Join(plugin, "skills", "video", "SKILL.md")
	support.Write(t, share, map[string]string{
		"plugin/claude-code/skills/video/SKILL.md": "---\nname: video\ndescription: Make a video.\n---\n\n# Make a video\n\nRun `jhon-studio doctor`, then `jhon-studio init`, then `jhon-studio video brief readme`.\n",
		"bin/jhon-studio": "#!/bin/sh\ncase \"$*\" in\n" +
			"  'integration --json') printf '{\"version\":\"0.1.0\",\"plugin_dir\":\"%s\",\"skill\":\"%s\",\"requires_jhon\":\">=0.5.0-rc.1\",\"contract\":\"jhon.integration/v1\"}\\n' '" + plugin + "' '" + skill + "' ;;\n" +
			"  'doctor') echo 'ok    node'; echo 'ok    ffmpeg' ;;\n" +
			"  'init') mkdir -p .jhon && printf 'narration: en\\n' > .jhon/studio.yaml && echo .jhon/studio.yaml ;;\n" +
			"  'video brief readme') mkdir -p .jhon/studio/readme \"$HOME/.cache/jhon-studio\" && head -1 README.md > .jhon/studio/readme/brief.md && : > \"$HOME/.cache/jhon-studio/voice\" && echo .jhon/studio/readme/brief.md ;;\n" +
			"  'touch-install') : > '" + filepath.Join(share, "state") + "' ;;\n" +
			"  *) exit 2 ;;\nesac\n",
	})
	if err := os.Chmod(filepath.Join(share, "bin", "jhon-studio"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(b.home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(share, "bin", "jhon-studio"), filepath.Join(bin, "jhon-studio")); err != nil {
		t.Fatal(err)
	}
	for i, kv := range b.env {
		if path, ok := strings.CutPrefix(kv, "PATH="); ok {
			b.env[i] = "PATH=" + bin + ":" + path
		}
	}
	return plugin, skill
}

func TestVideoLaunchPathRunsWithTheSandboxOn(t *testing.T) {
	b := newBox(t)
	b.settings(t, "schema = 1\n\n[integrations]\nstudio = true\n\n[sandbox]\nmode = \"on\"\n")
	plugin, skill := studioStandIn(t, &b)
	support.Write(t, b.proj, map[string]string{"README.md": "# A sample project\n"})

	// Claude Code: jhon start loads the plugin the stand-in names, with no
	// note (the tests' jhon is a development build, which satisfies any
	// minimum).
	seen := filepath.Join(b.base, "claude-argv")
	claude := filepath.Join(b.base, "claude")
	if err := os.WriteFile(claude, []byte("#!/bin/sh\nfor a; do printf '%s\\n' \"$a\"; done > '"+seen+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := support.Jhon(t, b.proj, append(b.env, "JHON_CLAUDE="+claude), "start")
	argv, _ := os.ReadFile(seen)
	if r.Code != 0 || r.Stderr != "" || !strings.Contains(string(argv), "--plugin-dir\n"+plugin+"\n") {
		t.Fatalf("jhon start: exit %d, stderr %q, claude's argv %q; want 0, no note, --plugin-dir %s", r.Code, r.Stderr, argv, plugin)
	}

	// pi: /jhon:video reads the skill the answer names; inside the sandbox
	// the answer and the skill are as readable.
	r = b.sh(t, b.proj, "work", "jhon-studio integration --json && head -2 '"+skill+"'")
	if r.Code != 0 || !strings.Contains(r.Stdout, `"skill":"`+skill+`"`) || !strings.Contains(r.Stdout, "---\nname: video\n") {
		t.Fatalf("the answer inside the sandbox: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}

	// The skill's first steps, as the agent runs them with the sandbox on.
	for _, c := range []struct{ line, out string }{
		{"jhon-studio doctor", "ok    node\nok    ffmpeg\n"},
		{"jhon-studio init", ".jhon/studio.yaml\n"},
		{"jhon-studio video brief readme", ".jhon/studio/readme/brief.md\n"},
	} {
		r := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--project", b.proj, "--profile", "work", "--", "sh", "-c", c.line)
		if r.Code != 0 || r.Stdout != c.out || r.Stderr != "" {
			t.Fatalf("%s: exit %d, stdout %q, stderr %q; want 0, %q, none", c.line, r.Code, r.Stdout, r.Stderr, c.out)
		}
	}
	for file, want := range map[string]string{
		filepath.Join(b.proj, ".jhon", "studio.yaml"):                  "narration: en\n",
		filepath.Join(b.proj, ".jhon", "studio", "readme", "brief.md"): "# A sample project\n",
		filepath.Join(b.home, ".cache", "jhon-studio", "voice"):        "",
	} {
		if got := support.Read(t, file); got != want {
			t.Errorf("%s holds %q, want %q", file, got, want)
		}
	}

	// The sandbox is on: the tool's own install folder stays read-only.
	r = b.sh(t, b.proj, "work", "jhon-studio touch-install")
	if r.Code == 0 || exists(filepath.Join(b.home, ".local", "share", "jhon-studio", "state")) {
		t.Errorf("a write into ~/.local/share/jhon-studio: exit %d, stderr %q; want it refused", r.Code, r.Stderr)
	}
}
