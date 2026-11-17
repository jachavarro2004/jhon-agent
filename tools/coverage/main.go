// Command coverage is the coverage matrix for the validation rules.
//
// A rule proven in one layer is not proven. The defect batch that motivated
// this harness survived precisely because each rule was tested where it had
// already been fixed, and every other layer went unchecked while --validate
// reported ok.
//
// Each case injects one real defect into one layer, asserts the expected rule
// fires on a file in that layer, and restores. The tree must be committed
// before running: restoration is `git restore --source=HEAD`.
//
//	go run ./tools/coverage          run every case
//	go run ./tools/coverage engram   run cases whose "rule layer" matches a substring
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const module = "module github.com/jachavarro2004/jhon-agent"

var (
	root   string
	filter string
	pass   int
	fail   int
	failed []string
)

func main() {
	if len(os.Args) > 1 {
		filter = os.Args[1]
	}
	var err error
	if root, err = findRoot(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if out, err := git("status", "--porcelain"); err != nil || strings.TrimSpace(out) != "" {
		fmt.Fprintln(os.Stderr, "working tree is dirty; commit before running so injections can be reverted")
		os.Exit(1)
	}

	// An interrupted case must not leave its defect behind.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		restore()
		os.Exit(130)
	}()

	// The renderer under test: the binary's render-engine, built at build/jhon.
	if !build() {
		fmt.Fprintln(os.Stderr, "the binary does not build")
		os.Exit(1)
	}

	fmt.Println("baseline")
	if out := validate(""); hasLinePrefix(out, "validation: ok") {
		fmt.Println("  clean, no false positives")
	} else {
		fmt.Println("  >>> BASELINE DIRTY — fix before trusting any result below <<<")
		fmt.Print(out)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("rule             layer      result")
	fmt.Println("----------------------------------------------")
	for _, c := range cases {
		run(c)
	}
	fmt.Println("----------------------------------------------")
	fmt.Printf("passed %d   failed %d\n", pass, fail)
	if fail > 0 {
		fmt.Printf("\nnot detected:\n")
		for _, f := range failed {
			fmt.Printf("  %s\n", f)
		}
		os.Exit(1)
	}

	runtimeRules()

	if out := validate(""); hasLinePrefix(out, "validation: ok") {
		fmt.Println()
		fmt.Printf("passed %d   failed %d   baseline restored clean\n", pass, fail)
	} else {
		fmt.Println()
		fmt.Println(">>> TREE LEFT DIRTY <<<")
		fmt.Print(out)
		os.Exit(1)
	}
	if fail > 0 {
		os.Exit(1)
	}
}

// findRoot walks up from the working directory to the repository's module root.
func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && bytes.Contains(b, []byte(module+"\n")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not inside the jhon-agent repository (no go.mod declaring %q above the working directory)", module)
		}
		dir = parent
	}
}

func git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	return string(out), err
}

func build() bool {
	cmd := exec.Command("go", "build", "-o", "build/jhon", "./engine/cmd/jhon")
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run() == nil
}

// validate runs `jhon render-engine --validate` against the tree and returns
// its combined output; the exit code is not the verdict, the lines are. A
// non-empty home runs it against that HOME instead of the caller's.
func validate(home string) string {
	cmd := exec.Command(filepath.Join(root, "build/jhon"), "render-engine", "--validate")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "JHON_ROOT="+root)
	if home != "" {
		cmd.Env = append(cmd.Env, "HOME="+home)
	}
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// restore puts the tree back at HEAD. git refuses while another process holds
// its index lock, and a defect left behind would break every later case on
// the same file, so it retries, and stops the run when it cannot.
func restore() {
	var out []byte
	var err error
	for range 10 {
		cmd := exec.Command("git", "restore", "--source=HEAD", "--staged", "--worktree", ".")
		cmd.Dir = root
		if out, err = cmd.CombinedOutput(); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	fmt.Fprintf(os.Stderr, "the tree could not be restored after a case: %v\n%s", err, out)
	os.Exit(1)
}

func lines(out string) []string { return strings.Split(out, "\n") }

func hasLinePrefix(out, prefix string) bool {
	for _, l := range lines(out) {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

// fired reports whether rule fired on a line naming layer.
func fired(out, rule, layer string) bool {
	for _, l := range lines(out) {
		if strings.HasPrefix(l, "  "+rule+" ") && strings.Contains(l, layer) {
			return true
		}
	}
	return false
}

// resolve names a file as the validator does: relative to prose/, unless it
// names one of the tree's other top directories.
func resolve(file string) string {
	for _, top := range []string{"engine/", "prose/", "runtimes/", "test/", "tools/", "docs/"} {
		if strings.HasPrefix(file, top) {
			return file
		}
	}
	if file == "README.md" {
		return file
	}
	return "prose/" + file
}

// inject edits the file literally, so multi-line anchors work. An anchor that
// is not found is an error: a case whose anchor no longer exists means the
// file changed shape and the case is no longer testing what it claims to.
func inject(file, find, replace string) error {
	p := filepath.Join(root, file)
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	s := string(b)
	if !strings.Contains(s, find) {
		return fmt.Errorf("ANCHOR NOT FOUND in %s", file)
	}
	n := 1
	if os.Getenv("INJECT_ALL") != "" {
		n = -1
	}
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	return os.WriteFile(p, []byte(strings.Replace(s, find, replace, n)), fi.Mode().Perm())
}

func run(c defectCase) {
	if filter != "" && !strings.Contains(c.rule+" "+c.layer, filter) {
		return
	}
	file := resolve(c.file)
	// A missing anchor is reported as a failure, not skipped: silently
	// passing would leave a rule unproven.
	if err := inject(file, c.find, c.replace); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Printf("  %-16s %-10s ANCHOR MISSING (%s)\n", c.rule, c.layer, file)
		fail++
		failed = append(failed, c.rule+"/"+c.layer+": anchor")
		restore()
		return
	}

	if c.rebuild {
		build()
	}
	out := validate("")
	restore()
	if c.rebuild {
		build()
	}

	if fired(out, c.rule, c.layer) {
		fmt.Printf("  %-16s %-10s detected\n", c.rule, c.layer)
		pass++
	} else {
		fmt.Printf("  %-16s %-10s >>> NOT DETECTED <<<\n", c.rule, c.layer)
		fail++
		failed = append(failed, c.rule+"/"+c.layer)
	}
}

func report(name string, detected bool) {
	if detected {
		fmt.Printf("  %-27s detected\n", name)
		pass++
	} else {
		fmt.Printf("  %-27s >>> NOT DETECTED <<<\n", name)
		fail++
	}
}

// runtimeRules act on file metadata or on generated files outside the repo,
// so they are exercised separately rather than by injecting into a tracked
// file.
func runtimeRules() {
	fmt.Println()
	fmt.Println("runtime rules")

	// Documented entry points: existence and the executable bit are file
	// metadata, not content.
	report("executable", withoutExec("tools/git-hooks/pre-commit", func(out string) bool {
		return hasLinePrefix(out, "  executable ")
	}))

	// The rules on runtime files run against a scratch HOME holding today's
	// rendered CLAUDE.md block, so no case ever edits a real runtime. Nothing
	// is installed there: only a release installs (docket AV1), and the
	// marker rule needs the file alone. A stale block is installed state,
	// which --check reports as DRIFT, not a --validate rule.
	scratch, err := os.MkdirTemp("", "jhon-coverage-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(scratch)
	claudeMd := filepath.Join(scratch, ".claude/CLAUDE.md")
	today, err := os.ReadFile(filepath.Join(root, "test/testdata/claude-md/today.md"))
	if err == nil {
		err = os.MkdirAll(filepath.Dir(claudeMd), 0o755)
	}
	if err == nil {
		err = os.WriteFile(claudeMd, today, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// A dangling symlink in a runtime directory: planted in the scratch
	// HOME's commands directory, so the case runs on any machine, CI
	// included, without touching an installed runtime.
	commands := filepath.Join(scratch, ".claude/commands")
	probe := filepath.Join(commands, "zz-coverage-probe.md")
	if err := os.MkdirAll(commands, 0o755); err == nil && os.Symlink("/nonexistent/target", probe) == nil {
		out := validate(scratch)
		os.Remove(probe)
		report("dangling-symlink", hasLinePrefix(out, "  dangling-symlink "))
	} else {
		report("dangling-symlink", false)
	}

	if out := validate(scratch); hasLinePrefix(out, "validation: ok") {
		runtimeCase(scratch, "claude-md-markers", claudeMd, func(s string) string {
			return strings.Replace(s, "<!-- jhon:end -->", "", 1)
		})
		runtimeCase(scratch, "claude-md-markers", claudeMd, func(s string) string {
			return s + "\n<!-- jhon:start -->\n"
		})
	} else {
		fmt.Println("  scratch HOME baseline       >>> NOT CLEAN <<<")
		fmt.Print(out)
		fail++
	}
}

// withoutExec clears a file's executable bits for one validation, restores
// its mode, and judges the output.
func withoutExec(rel string, detected func(string) bool) bool {
	p := filepath.Join(root, rel)
	fi, err := os.Stat(p)
	if err != nil {
		return false
	}
	mode := fi.Mode().Perm()
	if err := os.Chmod(p, mode&^0o111); err != nil {
		return false
	}
	out := validate("")
	if err := os.Chmod(p, mode); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return detected(out)
}

// runtimeCase edits a file in the scratch HOME, validates against that HOME,
// and puts the file back byte for byte.
func runtimeCase(home, rule, file string, edit func(string) string) {
	if filter != "" && !strings.Contains(rule, filter) {
		return
	}
	saved, err := os.ReadFile(file)
	if err != nil {
		report(rule, false)
		return
	}
	if err := os.WriteFile(file, []byte(edit(string(saved))), 0o644); err != nil {
		report(rule, false)
		return
	}
	out := validate(home)
	if err := os.WriteFile(file, saved, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	report(rule, hasLinePrefix(out, "  "+rule+" "))
}
