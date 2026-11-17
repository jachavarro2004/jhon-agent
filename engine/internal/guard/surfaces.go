// Package guard holds what the guards decide beyond one hook: a writer's
// edit surfaces, shared with `jhon audit`.
package guard

import (
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

var (
	// fileWord is a word that names a file: a path whose last part has an extension.
	fileWord = lazyre.New(`^/?(?:[\w.@~-]+/)*[\w@-][\w.@-]*\.[A-Za-z0-9]+$`)
	// pathWord is a word that can only name a file where a file is expected
	// (a redirection's target, tee's or sed -i's operand): no variable, glob,
	// substitution, or space left in it.
	pathWord   = lazyre.New("^[^\\s$*?`(){}\\[\\]<>|;&…]+$")
	writesAll  = map[string]bool{"touch": true, "truncate": true, "rm": true, "unlink": true, "shred": true, "patch": true}
	writesLast = map[string]bool{"cp": true, "mv": true, "install": true, "ln": true, "rsync": true}
	whitespace = lazyre.New(`\s`)
)

// A WriteGroup is files a command writes: one definite file, or every path
// one interpreter's writing code names (Code), of which it writes at least one.
type WriteGroup struct {
	Files   []string
	Code    bool
	Targets []string
}

func unique(xs []string) []string {
	var out []string
	for _, x := range xs {
		if !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// WriteGroups is the files a Bash command writes, in groups, absolute,
// resolved against cwd and every cd it makes. A command the reader cannot
// read writes nothing here.
func WriteGroups(command, cwd string) []WriteGroup {
	groups, _ := scanWrites(command, cwd)
	return groups
}

// An UnresolvedWrite is a write whose file the reader cannot name (T94,
// L652, L656): a variable or substitution target, git apply, patch, or an
// interpreter's code that writes with no path it names. Dir is the
// directory it runs in; Targets the files its code names, if any.
type UnresolvedWrite struct {
	Label, Dir string
	Targets    []string
}

// UnresolvedWrites is every write of a Bash command whose file the reader
// cannot name; a command it cannot read has none here.
func UnresolvedWrites(command, cwd string) []UnresolvedWrite {
	_, unresolved := scanWrites(command, cwd)
	return unresolved
}

var (
	// stdWrite is code writing to standard output or error, never a file.
	stdWrite = lazyre.New(`(?:sys|process)\.std(?:out|err)\.write|\$std(?:out|err)\.write|STD(?:OUT|ERR)->`)
	// patchDry are git apply's options that write nothing to the work tree.
	patchDry = []string{"--check", "--stat", "--numstat", "--summary", "--cached"}
)

// scanWrites is WriteGroups and UnresolvedWrites in one reading. A word with
// a variable reads through the line's own assignments, a scratch directory
// mktemp -d made (scratchVars), HOME, TMPDIR and PWD; any other is
// unresolved.
func scanWrites(command, cwd string) ([]WriteGroup, []UnresolvedWrite) {
	cmds, err := sh.Parse(command)
	if err != nil {
		return nil, nil
	}
	var out []WriteGroup
	var unresolved []UnresolvedWrite
	dir := cwd
	assigned := assignedIn(cmds)
	// A fresh mktemp variable, the line's own scratch, writes outside (T94b).
	scratch := freshVars(command, cwd)
	if scratch == nil {
		scratch = map[string]string{}
	}
	for k, v := range scratchVars(command, &Ctx{Cwd: cwd}) {
		if scratch[k] == "" {
			scratch[k] = v
		}
	}
	// expand is w with its variables read; ok is false when one cannot be.
	expand := func(w string) (string, bool) {
		if !strings.ContainsAny(w, "$`") {
			return w, true
		}
		if strings.Contains(w, "`") || strings.Contains(w, "$(") {
			return "", false
		}
		ok := true
		e := os.Expand(w, func(k string) string {
			// `${k:-default}` and `${k-default}`: the default stands for an unset k.
			k, def, hasDef := strings.Cut(k, "-")
			if hasDef {
				k = strings.TrimSuffix(k, ":")
			}
			v, known := scratch[k], scratch[k] != ""
			if !known {
				v, known = assigned[k]
			}
			if !known {
				switch k {
				case "HOME":
					v, known = project.Home(), project.Home() != ""
				case "TMPDIR":
					v = os.Getenv("TMPDIR")
					known = filepath.IsAbs(v)
					if v == "" && hasDef {
						v, known = def, true
					}
				case "PWD":
					v, known = dir, dir != ""
				}
			}
			if !known || v == "" || strings.ContainsAny(v, "$`{}") {
				ok = false
			}
			return v
		})
		return e, ok && e != ""
	}
	// A whole line `rm $(mktemp -d)` removes the directory it makes (mktempRm).
	scratchRm := mktempRm(command)
	lost := func(label string) {
		if !scratchRm {
			unresolved = append(unresolved, UnresolvedWrite{Label: label, Dir: dir})
		}
	}
	file := func(w string) string {
		if fileWord.MatchString(w) {
			return project.Resolve(dir, w)
		}
		return ""
	}
	one := func(w string) {
		e, ok := expand(w)
		if !ok {
			lost(w + " (a target the guard cannot resolve)")
			return
		}
		if f := file(e); f != "" {
			out = append(out, WriteGroup{Files: []string{f}})
		}
	}
	// named is a word where only a file can stand, extension or not; a
	// device is no file.
	named := func(w string) {
		e, ok := expand(w)
		if !ok {
			lost(w + " (a target the guard cannot resolve)")
			return
		}
		if pathWord.MatchString(e) && !strings.HasPrefix(e, "/dev/") {
			out = append(out, WriteGroup{Files: []string{project.Resolve(dir, e)}})
		}
	}
	for _, c := range cmds {
		ps, err := sh.CommandPrograms(c, nil, nil, sh.Opts{})
		if err != nil {
			continue
		}
		for _, p := range ps {
			var args []string
			if len(p.Argv) > 1 {
				args = p.Argv[1:]
			}
			var operands []string
			for _, a := range args {
				if !strings.HasPrefix(a, "-") {
					operands = append(operands, a)
				}
			}
			if (p.Name == "cd" || p.Name == "pushd") && len(operands) > 0 && operands[0] != "" {
				dir = project.Resolve(dir, operands[0])
				continue
			}
			// Standard output's redirection, `&>` included; another
			// descriptor's (`2> err.log`) is a log, never an edit.
			for _, r := range p.Redirects {
				if strings.Contains(r.Op, ">") && !strings.HasSuffix(r.Op, "&") && (r.N == "" || r.N == "1") {
					named(r.Target)
				}
			}
			// A string with a slash: a bare name in code is as often a key or a glob as a file.
			if p.Code != nil && sh.FileWrite.MatchString(stdWrite.ReplaceAllString(*p.Code, "")) {
				var named []string
				for _, t := range sh.CodeStrings(*p.Code) {
					if strings.Contains(t, "/") && !whitespace.MatchString(t) {
						named = append(named, t)
					}
				}
				named = unique(named)
				var files, targets []string
				for _, t := range named {
					if f := file(t); f != "" {
						files = append(files, f)
					}
					targets = append(targets, project.Resolve(dir, t))
				}
				if files = unique(files); len(files) > 0 {
					out = append(out, WriteGroup{Files: files, Code: true, Targets: targets})
				} else {
					// Code that writes and names no file: every bare file name
					// it holds, and every file operand after it (sys.argv,
					// ARGV, process.argv), is a target; none means any file
					// (T94, L656; T94b, L667).
					for _, t := range sh.CodeStrings(*p.Code) {
						if !strings.Contains(t, "/") && fileWord.MatchString(t) {
							targets = append(targets, project.Resolve(dir, t))
						}
					}
					for _, a := range codeOperands(args, *p.Code) {
						if e, ok := expand(a); ok && pathWord.MatchString(e) {
							targets = append(targets, project.Resolve(dir, e))
						}
					}
					unresolved = append(unresolved, UnresolvedWrite{Label: p.Name + " code (a target the guard cannot resolve)", Dir: dir, Targets: unique(targets)})
				}
			}
			var git sh.Git
			if p.Name == "git" {
				git = sh.GitParts(args)
			}
			switch {
			case p.Name == "sed" || p.Name == "perl":
				for _, o := range inPlaceFiles(p.Name, args) {
					named(o)
				}
			case p.Name == "tee":
				for _, o := range operands {
					named(o)
				}
			case p.Name == "git" && len(operands) > 0 && slices.Contains([]string{"mv", "rm", "checkout", "restore"}, operands[0]):
				for _, o := range operands[1:] {
					one(o)
				}
			case p.Name == "git" && git.Sub == "apply":
				// git apply writes every file its patch names (L652), after
				// any global option (`git -C <dir> apply`, T94b).
				if !slices.ContainsFunc(git.Rest, func(a string) bool { return slices.Contains(patchDry, a) }) || slices.Contains(git.Rest, "--apply") || slices.Contains(git.Rest, "--index") {
					lost("git apply (a target the guard cannot resolve)")
				}
			case p.Name == "curl" || p.Name == "wget" || p.Name == "dd":
				for _, o := range outputFiles(p.Name, args) {
					if o != "-" {
						named(o)
					}
				}
			case writesAll[p.Name]:
				for _, o := range operands {
					one(o)
				}
				// patch writes every file its patch names, whatever its operands (L652).
				if p.Name == "patch" && !slices.Contains(args, "--dry-run") {
					lost("patch (a target the guard cannot resolve)")
				}
			case writesLast[p.Name]:
				for _, f := range copyTargets(args) {
					one(f)
				}
			}
		}
	}
	return out, unresolved
}

// outputFiles is the files dd of=, curl, or wget writes: curl -o / --output
// (a short-option bundle's first o) and -O / --remote-name[-all] (the URL's
// base name), under --output-dir; wget -O / --output-document (a bundle's
// first O), else each URL's base name, under -P / --directory-prefix. wget's
// -o is its log, not counted; nor is stdout (-).
func outputFiles(name string, args []string) []string {
	if name == "dd" {
		var out []string
		for _, a := range args {
			if v, ok := strings.CutPrefix(a, "of="); ok {
				out = append(out, v)
			}
		}
		return out
	}
	wget := name == "wget"
	outFlag, logFlag, dirFlag := byte('o'), byte(0), byte(0)
	outLong, dirLong := "--output", "--output-dir"
	if wget {
		outFlag, logFlag, dirFlag = 'O', 'o', 'P'
		outLong, dirLong = "--output-document", "--directory-prefix"
	}
	var outputs, urls []string
	dir, remote := "", false
	next := func(i *int) string {
		if *i+1 < len(args) {
			*i++
			return args[*i]
		}
		return ""
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == outLong:
			outputs = append(outputs, next(&i))
		case strings.HasPrefix(a, outLong+"="):
			outputs = append(outputs, a[len(outLong)+1:])
		case a == dirLong:
			dir = next(&i)
		case strings.HasPrefix(a, dirLong+"="):
			dir = a[len(dirLong)+1:]
		case wget && a == "--output-file":
			i++
		case !wget && (a == "--remote-name" || a == "--remote-name-all"):
			remote = true
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") && len(a) > 1:
		bundle:
			for k := 1; k < len(a); k++ {
				c := a[k]
				if !wget && c == 'O' {
					remote = true
					continue
				}
				if c != outFlag && c != logFlag && c != dirFlag {
					continue
				}
				v := a[k+1:]
				if v == "" {
					v = next(&i)
				}
				switch c {
				case outFlag:
					outputs = append(outputs, v)
				case dirFlag:
					dir = v
				}
				break bundle
			}
		case strings.Contains(a, "://"):
			urls = append(urls, a)
		}
	}
	// wget's -P holds only the files it names from the URL.
	if wget && len(outputs) > 0 {
		dir = ""
	}
	if !wget && remote || wget && len(outputs) == 0 {
		for _, u := range urls {
			if b := urlBase(u); b != "" {
				outputs = append(outputs, b)
			}
		}
	}
	var out []string
	for _, o := range outputs {
		if o == "" || o == "-" {
			continue
		}
		if dir != "" && !filepath.IsAbs(o) && !strings.HasPrefix(o, "/dev/") {
			o = filepath.Join(dir, o)
		}
		out = append(out, o)
	}
	return out
}

// urlBase is the base name of a URL's path, its query and fragment cut; ""
// when the path is empty or ends in a slash.
func urlBase(u string) string {
	_, rest, ok := strings.Cut(u, "://")
	if !ok {
		return ""
	}
	rest, _, _ = strings.Cut(rest, "#")
	rest, _, _ = strings.Cut(rest, "?")
	_, p, ok := strings.Cut(rest, "/")
	if !ok || p == "" || strings.HasSuffix(p, "/") {
		return ""
	}
	return path.Base(p)
}

// inPlaceFiles is the files sed or perl edits in place (sed -i[suffix],
// --in-place[=suffix], perl -pi, -i.bak): its operands after the script,
// which is the first operand unless -e or -f gave it; nil when it does not
// edit in place.
func inPlaceFiles(name string, args []string) []string {
	inPlace, script := false, false
	var operands []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			operands = append(operands, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(a, "--in-place"):
			inPlace = true
		case name == "sed" && (a == "--expression" || a == "--file"):
			script = true
			i++
		case name == "sed" && (strings.HasPrefix(a, "--expression=") || strings.HasPrefix(a, "--file=")):
			script = true
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") && len(a) > 1:
			// A cluster of short options: -i takes its suffix attached, -e and
			// -f the next word or the rest, perl's -M, -m, -I, -l the rest.
		cluster:
			for k := 1; k < len(a); k++ {
				switch a[k] {
				case 'i':
					inPlace = true
					break cluster
				case 'e', 'E', 'f':
					if a[k] == 'E' && name == "sed" {
						continue
					}
					if a[k] == 'f' && name == "perl" {
						continue
					}
					script = true
					if k == len(a)-1 {
						i++
					}
					break cluster
				case 'l':
					if name == "sed" && k == len(a)-1 {
						i++
					}
					break cluster
				case 'M', 'm', 'I', 'x', '0', 'C', 'd', 'D', 'V':
					if name == "perl" {
						break cluster
					}
				}
			}
		case a != "":
			operands = append(operands, a)
		}
	}
	if !inPlace {
		return nil
	}
	if !script && len(operands) > 0 {
		operands = operands[1:]
	}
	return operands
}

// copyTargets is the files cp, mv, install, ln, or rsync writes: its last
// operand, or each source's name in it when it is a directory (named with a
// trailing slash, or by -t / --target-directory).
func copyTargets(args []string) []string {
	var operands []string
	target := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-t" || a == "--target-directory":
			if i+1 < len(args) {
				target = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--target-directory="):
			target = strings.TrimPrefix(a, "--target-directory=")
		case strings.HasPrefix(a, "-"):
		default:
			operands = append(operands, a)
		}
	}
	if target == "" {
		if len(operands) < 2 {
			return nil
		}
		target, operands = operands[len(operands)-1], operands[:len(operands)-1]
		if !strings.HasSuffix(target, "/") {
			return []string{target}
		}
	}
	var out []string
	for _, src := range operands {
		out = append(out, filepath.Join(target, filepath.Base(src)))
	}
	return out
}

// WrittenFiles is every file WriteGroups names, once.
func WrittenFiles(command, cwd string) []string {
	var out []string
	for _, g := range WriteGroups(command, cwd) {
		out = append(out, g.Files...)
	}
	return unique(out)
}

// WritesOutside is the files a Bash command writes outside surfaces: each
// definite file out of scope, and every file of a code group none of whose
// targets is in scope.
func WritesOutside(command, cwd string, surfaces []string) []string {
	return WritesOutsideIn(command, cwd, func(abs string) bool { return InSurfaces(abs, surfaces) })
}

// WritesOutsideIn is WritesOutside with in deciding whether a file is in
// scope: the scope guard's rule, which the audit's measurement shares.
func WritesOutsideIn(command, cwd string, in func(abs string) bool) []string {
	var out []string
	for _, g := range WriteGroups(command, cwd) {
		var outside []string
		for _, f := range g.Files {
			if !in(f) {
				outside = append(outside, f)
			}
		}
		if g.Code {
			if !slices.ContainsFunc(g.Targets, in) {
				out = append(out, outside...)
			}
		} else {
			out = append(out, outside...)
		}
	}
	return unique(out)
}

var (
	heading     = lazyre.New(`(?im)^#{1,6}[ \t]+Allowed edit surfaces[ \t]*$`)
	anyHeading  = lazyre.New(`^\s*#{1,6}\s`)
	listMarker  = lazyre.New(`^[-*+]\s+`)
	numberMark  = lazyre.New(`^\d+[.)]\s+`)
	noteSplit   = lazyre.New(`\s+(?:—|–|-|:)\s+|:\s|\s+\(`)
	surfaceLike = lazyre.New(`[/.*]`)
)

// SurfacesOf is the surfaces a launch prompt lists under `## Allowed edit
// surfaces`; ok is false when the prompt has no such heading.
func SurfacesOf(prompt string) (out []string, ok bool) {
	loc := heading.FindStringIndex(prompt)
	if loc == nil {
		return nil, false
	}
	out = []string{}
	for _, raw := range strings.Split(prompt[loc[1]:], "\n") {
		if anyHeading.MatchString(raw) {
			break
		}
		line := numberMark.ReplaceAllString(listMarker.ReplaceAllString(strings.TrimSpace(raw), ""), "")
		if line == "" {
			continue
		}
		word := strings.TrimSpace(noteSplit.Split(strings.ReplaceAll(line, "`", ""), 2)[0])
		// A path, a directory, or a glob: no spaces, and a slash, a dot, or a star.
		if whitespace.MatchString(word) || !surfaceLike.MatchString(word) {
			if len(out) > 0 {
				break
			}
			continue
		}
		out = append(out, strings.TrimPrefix(word, "./"))
	}
	return out, true
}

// matches is whether root-relative path rel is under a surface: a file, a
// directory (and everything under it), or a glob.
func matches(surface, rel string) bool {
	if strings.HasSuffix(surface, "/") {
		return strings.HasPrefix(rel, surface)
	}
	if !strings.ContainsAny(surface, "*?") {
		return rel == surface || strings.HasPrefix(rel, surface+"/")
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(surface); {
		switch {
		case strings.HasPrefix(surface[i:], "**/"):
			b.WriteString(".*")
			i += 3
		case strings.HasPrefix(surface[i:], "**"):
			b.WriteString(".*")
			i += 2
		case surface[i] == '*':
			b.WriteString("[^/]*")
			i++
		case surface[i] == '?':
			b.WriteString("[^/]")
			i++
		default:
			b.WriteString(regexp.QuoteMeta(surface[i : i+1]))
			i++
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	return err == nil && re.MatchString(rel)
}

// TestFile is a test file by the common conventions.
var TestFile = lazyre.New(`(?:^|/)(?:tests?|__tests__|spec)/|(?:^|/)test_[^/]*$|[._-](?:test|spec)\.[\w]+$|_test\.go$`)

// RepoOf is the repository abs lies in: the nearest ancestor holding .git; "" outside any.
func RepoOf(abs string) string {
	for d := filepath.Dir(abs); ; d = filepath.Dir(d) {
		if project.Exists(filepath.Join(d, ".git")) {
			return d
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// InSurfaces is whether abs is in scope: outside any repository, a test
// file, an LDD change artifact, or under a listed surface.
func InSurfaces(abs string, surfaces []string) bool { return InSurfacesOf(abs, surfaces, RepoOf(abs)) }

// InSurfacesOf is InSurfaces with the repository root given ("" for none).
func InSurfacesOf(abs string, surfaces []string, root string) bool {
	if root == "" {
		return true
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return true
	}
	r := filepath.ToSlash(rel)
	if TestFile.MatchString(r) || strings.HasPrefix(r, layout.For(root).ChangesDir()+"/") {
		return true
	}
	for _, s := range surfaces {
		if filepath.IsAbs(s) {
			if rs, err := filepath.Rel(root, s); err == nil {
				s = filepath.ToSlash(rs)
			}
		}
		if matches(s, r) {
			return true
		}
	}
	return false
}

// codeOperands is the operands after an interpreter's inline code: the
// script's arguments, which its code may open (sys.argv[1], ARGV[0]).
func codeOperands(args []string, code string) []string {
	var out []string
	seen := false
	for _, a := range args {
		switch {
		case !seen && (a == code || a == "-" || strings.Contains(code, a) && strings.ContainsAny(a, " ;(")):
			seen = true
		case seen && !strings.HasPrefix(a, "-"):
			out = append(out, a)
		}
	}
	return out
}
