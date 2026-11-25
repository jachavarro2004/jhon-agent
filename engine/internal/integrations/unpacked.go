package integrations

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// SystemPath is the system's folders an unpacked program finds commands in
// (AskUnpacked), after its own bin and its interpreter's folder.
const SystemPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// AskUnpacked is `<program> integration --json` of an unpacked release's
// program, before it is installed (T242, odd/tasks/studio-integration-v1.md
// L18 A2): it runs with an environment of HOME (home, also its working
// folder) and PATH (the program's own folder, its interpreter's when its
// first line is `#!/usr/bin/env [-S] <name>` and the caller finds <name>,
// then SystemPath) and nothing else, bounded by askTimeout; its answer is
// read up to maxAnswer bytes; when it returns, its process group is killed,
// and on Linux every process it left behind, a setsid one too (reaping). A
// program that is not executable, or that a noexec mount keeps from
// running, says so.
func AskUnpacked(m Manifest, program, home string) (Answer, error) {
	bin := m.Doctor.Binary
	fi, err := os.Stat(program)
	if err != nil {
		return Answer{}, fmt.Errorf("%s at %s: %v", bin, program, err)
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return Answer{}, fmt.Errorf("%s at %s is not executable", bin, program)
	}
	path := []string{filepath.Dir(program)}
	if d := interpreterDir(program); d != "" {
		path = append(path, d)
	}
	args := []string{"integration", "--json"}
	ctx, cancel := context.WithTimeout(context.Background(), askTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = []string{"HOME=" + home, "PATH=" + strings.Join(append(path, SystemPath), ":")}
	cmd.Dir = home
	ownGroup(cmd)
	cmd.WaitDelay = time.Second
	var out bounded
	cmd.Stdout = &out
	var started bool
	err = reaping(func() error {
		if err := cmd.Start(); err != nil {
			return err
		}
		started = true
		err := cmd.Wait()
		killGroup(cmd)
		return err
	})
	if !started && err != nil {
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
			if mountedNoexec(filepath.Dir(program)) {
				return Answer{}, fmt.Errorf("cannot run %s at %s: its file system is mounted noexec; set TMPDIR to a folder on a file system where programs may run", bin, program)
			}
		}
		return Answer{}, fmt.Errorf("cannot run %s at %s: %w", bin, program, err)
	}
	b, err := answered(ctx, bin, args, err, &out)
	if err != nil {
		return Answer{}, err
	}
	return readAnswer(m, b)
}

// interpreterDir is the folder, on the caller's PATH, of the interpreter
// program's first line names through env (`#!/usr/bin/env node`,
// `#!/usr/bin/env -S node --flag`); "" for any other first line.
func interpreterDir(program string) string {
	f, err := os.Open(program)
	if err != nil {
		return ""
	}
	defer f.Close()
	line, _ := bufio.NewReader(f).ReadString('\n')
	rest, ok := strings.CutPrefix(line, "#!")
	words := strings.Fields(rest)
	if !ok || len(words) < 2 || filepath.Base(words[0]) != "env" {
		return ""
	}
	for _, w := range words[1:] {
		if strings.HasPrefix(w, "-") || strings.Contains(w, "=") {
			continue
		}
		if strings.Contains(w, "/") {
			return ""
		}
		p, err := exec.LookPath(w)
		if err != nil || !filepath.IsAbs(p) {
			return ""
		}
		return filepath.Dir(p)
	}
	return ""
}
