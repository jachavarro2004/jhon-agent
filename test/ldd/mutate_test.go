// `jhon mutate` (contracts/tdd.md): the mutation witness without git. It
// breaks one spot of a file, runs the test, restores the file byte for byte,
// and runs the test again: a witness holds when the mutant fails and the
// restored code passes. The original is saved under ~/.cache/jhon/mut before
// the file changes, so `jhon mutate --restore` recovers it after a crash.
//
// Every fixture is a temporary git repository with a file and a "test" that
// is a node one-liner checking the file's content.
package ldd

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const mutateSrc = "export const add = (a, b) => a + b;\n"

// mutateCheck is a test that passes while add() adds.
var mutateCheck = []string{"node", "-e", "process.exit(require('fs').readFileSync('src/add.js','utf8').includes('a + b') ? 0 : 1)"}

type mutateProject struct{ root, home string }

func newMutateProject(t *testing.T) mutateProject {
	t.Helper()
	p := mutateProject{support.GitRepo(t, temp(t)), temp(t)}
	support.Write(t, p.root, map[string]string{"src/add.js": mutateSrc})
	return p
}

func (p mutateProject) mutate(t *testing.T, args ...string) support.Result {
	t.Helper()
	return support.Jhon(t, p.root, support.Env("HOME="+p.home, "XDG_CACHE_HOME="), append([]string{"mutate"}, args...)...)
}

func (p mutateProject) pending(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join(p.home, ".cache", "jhon", "mut")
	out := []string{}
	if !exists(dir) {
		return out
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(path, ".json") {
			rel, _ := filepath.Rel(dir, path)
			out = append(out, rel)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mutateArgs(file, find, replace string, test ...string) []string {
	args := []string{file, "--find", find, "--replace", replace}
	if test != nil {
		args = append(append(args, "--"), test...)
	}
	return args
}

func TestMutateWitness(t *testing.T) {
	t.Parallel()
	p := newMutateProject(t)
	r := p.mutate(t, mutateArgs("src/add.js", "a + b", "a - b", mutateCheck...)...)
	equal(t, r.Code, 0, r.Out())
	match(t, r.Out(), `(?m)^witness: mutation$`)
	match(t, r.Out(), `(?m)^mutant: exit 1 \(failed\)$`)
	match(t, r.Out(), `(?m)^restored: exit 0 \(passed\)$`)
	equal(t, support.Read(t, filepath.Join(p.root, "src", "add.js")), mutateSrc)
	equal(t, p.pending(t), []string{}, "the backup outlived a clean restore")
}

// Found mutating an executable script: the mutant and the restore were written without
// the file's mode, so an executable came back 644 and its tests could not run
// it.
func TestMutateKeepsMode(t *testing.T) {
	t.Parallel()
	p := newMutateProject(t)
	file := filepath.Join(p.root, "src", "add.js")
	if err := os.Chmod(file, 0o755); err != nil {
		t.Fatal(err)
	}
	seen := filepath.Join(p.home, "mode-seen")
	q, _ := json.Marshal(seen)
	r := p.mutate(t, mutateArgs("src/add.js", "a + b", "a - b", "node", "-e",
		`const fs=require('fs');fs.appendFileSync(`+string(q)+`, (fs.statSync('src/add.js').mode & 0o777).toString(8)+'\n');process.exit(fs.readFileSync('src/add.js','utf8').includes('a + b')?0:1)`)...)
	equal(t, r.Code, 0, r.Out())
	equal(t, strings.Split(strings.TrimSpace(support.Read(t, seen)), "\n"), []string{"755", "755"}, "the mutant's mode, then the restored file's")
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, info.Mode().Perm(), fs.FileMode(0o755))
}

func TestMutateFailureLine(t *testing.T) {
	t.Parallel()
	p := newMutateProject(t)
	noisy := []string{"node", "-e", "if (!require('fs').readFileSync('src/add.js','utf8').includes('a + b')) { console.error('not ok 1 - add adds'); console.error('  }'); process.exit(1) }"}
	r := p.mutate(t, mutateArgs("src/add.js", "a + b", "a - b", noisy...)...)
	equal(t, r.Code, 0, r.Out())
	match(t, r.Out(), `(?m)^failure: not ok 1 - add adds$`)
}

func TestMutateSurvivor(t *testing.T) {
	t.Parallel()
	p := newMutateProject(t)
	r := p.mutate(t, mutateArgs("src/add.js", "add", "plus", mutateCheck...)...)
	equal(t, r.Code, 1, r.Out())
	match(t, r.Out(), `(?m)^mutant: exit 0 \(survived\)$`)
	equal(t, support.Read(t, filepath.Join(p.root, "src", "add.js")), mutateSrc)
}

func TestMutateRefusals(t *testing.T) {
	t.Parallel()
	p := newMutateProject(t)
	support.Write(t, p.root, map[string]string{"src/twice.js": "a + b; a + b;\n"})
	outside := filepath.Join(p.home, "x.js")
	support.Write(t, p.home, map[string]string{"x.js": "a + b\n"})
	for _, args := range [][]string{
		mutateArgs("src/add.js", "nope", "x", mutateCheck...),
		mutateArgs("src/twice.js", "a + b", "x", mutateCheck...),
		mutateArgs(outside, "a + b", "x", mutateCheck...),
		mutateArgs(".git/HEAD", "ref", "x", mutateCheck...),
		mutateArgs("src/add.js", "a + b", "a - b"),
	} {
		r := p.mutate(t, args...)
		equal(t, r.Code, 2, strings.Join(args, " ")+": "+r.Out())
	}
	equal(t, support.Read(t, filepath.Join(p.root, "src", "add.js")), mutateSrc)
	equal(t, support.Read(t, filepath.Join(p.root, "src", "twice.js")), "a + b; a + b;\n")
	equal(t, support.Read(t, outside), "a + b\n")
	equal(t, p.pending(t), []string{})
}

func TestMutateRestoreAfterCrash(t *testing.T) {
	t.Parallel()
	p := newMutateProject(t)
	// A crash after the backup and the mutation: the record exists, the file
	// is mutated.
	run := p.mutate(t, mutateArgs("src/add.js", "a + b", "a - b", "node", "-e", "process.kill(process.ppid, 'SIGKILL')")...)
	if run.Code == 0 {
		t.Errorf("exit 0: %s", run.Out())
	}
	equal(t, support.Read(t, filepath.Join(p.root, "src", "add.js")), strings.Replace(mutateSrc, "a + b", "a - b", 1), "the kill left the mutant in place")
	equal(t, len(p.pending(t)), 1)
	blocked := p.mutate(t, mutateArgs("src/add.js", "a - b", "a * b", mutateCheck...)...)
	equal(t, blocked.Code, 2, blocked.Out())
	match(t, blocked.Out(), `jhon mutate --restore`)
	restore := p.mutate(t, "--restore")
	equal(t, restore.Code, 0, restore.Out())
	match(t, restore.Out(), `restored src/add\.js`)
	equal(t, support.Read(t, filepath.Join(p.root, "src", "add.js")), mutateSrc)
	equal(t, p.pending(t), []string{})
}
