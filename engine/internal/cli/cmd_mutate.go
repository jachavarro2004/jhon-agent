package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/mutate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// `jhon mutate` (contracts/tdd.md): the mutation witness.
//
//	jhon mutate <file> --find <text> --replace <text> -- <test command…>
//	jhon mutate --restore
//
// A witness holds when the mutant fails and the restored code passes: exit
// 0; otherwise exit 1. A usage error changes nothing: exit 2. The file is
// restored on exit and on SIGINT, SIGTERM, and SIGHUP; while a record of a
// killed run exists, the project takes no new mutation.
func init() { register("mutate", runMutate) }

const mutateUsage = `usage: jhon mutate <file> --find <text> --replace <text> -- <test command…>
       jhon mutate --restore
`

func mutateUsageError(msg string) (int, error) {
	fmt.Fprintf(os.Stderr, "jhon mutate: %s\n\n%s", msg, mutateUsage)
	return 2, nil
}

// relative is path.relative: "" for the same path.
func relative(from, to string) string {
	rel, err := filepath.Rel(from, to)
	if err != nil || rel == "." {
		return ""
	}
	return rel
}

// either is yes when ok, else no.
func either(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func runMutate(argv []string) (int, error) {
	wd := cwd()
	root := project.Root(wd)
	if root == "" {
		return mutateUsageError(wd + " is not inside a project")
	}
	dir := mutate.RecordDir(root)

	if len(argv) > 0 && argv[0] == "--restore" {
		if len(argv) > 1 {
			return mutateUsageError("--restore takes no argument")
		}
		pending, err := mutate.Records(dir)
		if err != nil {
			return 1, err
		}
		for _, p := range pending {
			if err := mutate.RestoreOne(p); err != nil {
				return 1, err
			}
			fmt.Printf("restored %s\n", relative(root, p.Rec.File))
		}
		if len(pending) == 0 {
			fmt.Println("nothing to restore")
		}
		return 0, nil
	}

	head, command := argv, []string(nil)
	if dd := slices.Index(argv, "--"); dd >= 0 {
		head, command = argv[:dd], argv[dd+1:]
	}
	var file, find, replace *string
	for i := 0; i < len(head); i++ {
		a := head[i]
		if a == "--find" || a == "--replace" {
			i++
			if i >= len(head) {
				return mutateUsageError(a + " requires a value")
			}
			v := head[i]
			if a == "--find" {
				find = &v
			} else {
				replace = &v
			}
			continue
		}
		if strings.HasPrefix(a, "-") {
			return mutateUsageError("unknown option: " + a)
		}
		if file != nil {
			return mutateUsageError("unexpected argument: " + a)
		}
		file = &a
	}
	if file == nil {
		return mutateUsageError("a file is required")
	}
	if find == nil || *find == "" || replace == nil {
		return mutateUsageError("--find (not empty) and --replace are required")
	}
	if len(command) == 0 {
		return mutateUsageError("a test command is required after --")
	}

	abs := project.Realish(project.Resolve(wd, *file))
	if !project.Within(abs, root) || abs == root {
		return mutateUsageError(fmt.Sprintf("%s is not inside the project %s", *file, root))
	}
	if slices.ContainsFunc(strings.Split(abs, string(filepath.Separator)), func(s string) bool { return slices.Contains(guard.VCSDirs, strings.ToLower(s)) }) {
		return mutateUsageError(*file + " is inside a VCS directory")
	}
	if fi, err := os.Stat(abs); err != nil || !fi.Mode().IsRegular() {
		return mutateUsageError(*file + " is not a file")
	}
	pending, err := mutate.Records(dir)
	if err != nil {
		return 1, err
	}
	if len(pending) > 0 {
		return mutateUsageError("a previous mutation was not restored; run `jhon mutate --restore` first")
	}

	original, err := os.ReadFile(abs)
	if err != nil {
		return 1, err
	}
	text := string([]rune(string(original))) // as Buffer.toString("utf8") decodes it
	at := strings.Index(text, *find)
	if at < 0 {
		return mutateUsageError("the find text does not occur in " + *file)
	}
	if strings.Contains(text[at+len(*find):], *find) {
		return mutateUsageError(fmt.Sprintf("the find text occurs more than once in %s; make it unique", *file))
	}

	m, err := mutate.Begin(dir, fmt.Sprintf("%d-%d.json", time.Now().UnixMilli(), os.Getpid()), abs, original)
	if err != nil {
		return 1, err
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-signals
		if err := m.Restore(); err != nil {
			fmt.Fprintf(os.Stderr, "jhon: %v\n", err)
		}
		os.Exit(1)
	}()

	if err := m.Write([]byte(text[:at] + *replace + text[at+len(*find):])); err != nil {
		if rerr := m.Restore(); rerr != nil {
			return 1, rerr
		}
		return 1, err
	}
	mutant := mutate.Run(command)
	if err := m.Restore(); err != nil {
		return 1, err
	}
	after := mutate.Run(command)

	fmt.Println("witness: mutation")
	fmt.Printf("file: %s\n", relative(root, abs))
	from, _ := js.Stringify(*find)
	to, _ := js.Stringify(*replace)
	fmt.Printf("mutation: %s -> %s\n", from, to)
	fmt.Printf("command: %s\n", strings.Join(command, " "))
	fmt.Printf("mutant: exit %d (%s)\n", mutant.Code, either(mutant.Code == 0, "survived", "failed"))
	if mutant.Code != 0 && mutant.Failure != "" {
		fmt.Printf("failure: %s\n", mutant.Failure)
	}
	fmt.Printf("restored: exit %d (%s)\n", after.Code, either(after.Code == 0, "passed", "failed"))
	if after.Code != 0 && after.Tail != "" {
		fmt.Println(after.Tail)
	}
	if mutant.Code != 0 && after.Code == 0 {
		return 0, nil
	}
	return 1, nil
}
