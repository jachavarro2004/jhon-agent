package guard

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// An extension glob (`*.json`, extensionGlob) is read by where it can reach,
// not by its name (T87b, L630-L631), only on a line that cannot change what
// lies under the directories the guard walks: every simple command of it is
// a program that reads (extReaders) or moves the line's directory (cd, which
// the guard follows), with no redirection that writes a file. Any other line
// keeps the name rule.

var (
	// extReaders read files or list names and write none.
	extReaders = set("cat", "head", "tail", "wc", "jq", "ls", "echo", "printf", "pwd", "grep", "egrep", "fgrep", "rg", "sort", "uniq",
		"cut", "tr", "less", "more", "file", "stat", "test", "[", "true", "false", "cd", "find", "git", "jhon", "diff", "nl", "column")
	// extWrappers run the next word as the program.
	extWrappers = set("command", "builtin", "exec", "nice", "time")
	// extGitReads are the git subcommands that write no work-tree file.
	extGitReads = set("status", "log", "diff", "show", "ls-files", "grep", "add", "rev-parse")
	// findActs are find's actions that run, delete or write.
	findActs = set("-exec", "-execdir", "-ok", "-okdir", "-delete", "-fprint", "-fprint0", "-fprintf", "-fls")
)

// extLine is whether every command of line is one extReaders allows.
func extLine(line string) bool {
	cmds, err := sh.Parse(line)
	if err != nil {
		return false
	}
	for _, c := range cmds {
		for _, r := range c.Redirects {
			if r.Body != nil || strings.HasPrefix(r.Op, "<") || r.Target == "/dev/null" || (strings.HasSuffix(r.Op, "&") && descriptor.MatchString(r.Target)) {
				continue
			}
			if strings.Contains(r.Op, ">") {
				return false
			}
		}
		if !extCommand(c.Words) {
			return false
		}
	}
	return true
}

// extCommand is whether one simple command's words run an extReaders program as it allows.
func extCommand(words []string) bool {
	i := 0
	for i < len(words) && sh.Assignment.MatchString(words[i]) {
		i++
	}
	for i < len(words) {
		switch w := filepath.Base(words[i]); {
		case extWrappers[w]:
			i++
		case w == "timeout" && i+1 < len(words):
			i += 2
		case w == "env":
			for i++; i < len(words) && sh.Assignment.MatchString(words[i]); i++ {
			}
		default:
			return extProgram(w, words[i+1:])
		}
	}
	return true // assignments only
}

func extProgram(name string, args []string) bool {
	if !extReaders[name] {
		return false
	}
	switch name {
	case "git":
		k := 0
		for k < len(args) && strings.HasPrefix(args[k], "-") {
			k++
		}
		return k < len(args) && extGitReads[args[k]] && !slices.ContainsFunc(args, func(a string) bool {
			return strings.HasPrefix(a, "--output") || strings.HasPrefix(a, "--ext-diff") || strings.HasPrefix(a, "--textconv")
		})
	case "jhon":
		return len(args) > 0 && (args[0] == "status" || args[0] == "risk")
	case "find":
		return !slices.ContainsFunc(args, func(a string) bool { return findActs[a] })
	case "sort":
		return !slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "-o") || strings.HasPrefix(a, "--output") })
	case "uniq":
		ops := 0
		for _, a := range args {
			if a == "-" || !strings.HasPrefix(a, "-") {
				ops++
			}
		}
		return ops < 2
	case "rg":
		return !slices.ContainsFunc(args, func(a string) bool { return a == "--pre" || strings.HasPrefix(a, "--pre=") })
	}
	return true
}
