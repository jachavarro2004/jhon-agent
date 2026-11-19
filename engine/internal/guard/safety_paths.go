package guard

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// A Finding is one effect the safety guard judged: a deny, or an ask of a class.
type Finding struct {
	Level, What, Why, Alt, Cls string
	// RootSet says the finding names the repository it targets: Root, or "" for none a grant can name.
	RootSet bool
	Root    string
	// Write marks an ask that comes from a write's target.
	Write bool
}

const (
	unreadable = "unreadable"
	// unknown is a program or a target the guard cannot see: never grantable.
	unknown = "unknown"
	// gitConfig is a git configuration write that changes what git runs: never grantable.
	gitConfig = "git-config"
	// editInstead is the allowed form for a command that overwrites working-tree files.
	editInstead = "change a file's content with Edit or Write; for a mutation witness, `jhon mutate <file> --find <text> --replace <text> -- <test command>` restores the file itself"
	// child stands in for "some entry under this directory".
	child = "jhon-safety-child"
	// UnknownRoot is a finding's repository the guard cannot tell (GIT_DIR
	// and the work tree disagree): no grant covers it, and its mode is normal.
	UnknownRoot = "<unknown>"
)

func deny(what, why string) Finding { return Finding{Level: "deny", What: what, Why: why} }

// ask is an ask finding whose repository is the directory it runs in.
func ask(cls, what string) Finding { return Finding{Level: "ask", Cls: cls, What: what} }

// askAt is an ask finding that names its repository; "" is none a grant can name.
func askAt(cls, what, root string) Finding {
	return Finding{Level: "ask", Cls: cls, What: what, RootSet: true, Root: root}
}

// destroy is an ask of the destroy class: a deletion that loses work or data,
// which runs once the owner approves it (`jhon grant destroy`, or the
// prompt's yes) and never on an agent's own say.
func destroy(what, why string) Finding { return ask("destroy", what+" ("+why+")") }

// destroyAlt is destroy with the form an agent uses instead.
func destroyAlt(what, why, alt string) Finding {
	f := destroy(what, why)
	f.Alt = alt
	return f
}

func writeAsk(cls, what string) Finding {
	f := ask(cls, what)
	f.Write = true
	return f
}

// ── argument helpers ────────────────────────────────────────────────────────

var shortCluster = lazyre.New(`^-[A-Za-z]`)

// shortFlags is every short flag letter in the clusters of args, up to a value-taking letter.
func shortFlags(args []string, valueLetters string) []byte {
	var out []byte
	for _, a := range args {
		if a == "--" {
			break
		}
		if !shortCluster.MatchString(a) || strings.HasPrefix(a, "--") {
			continue
		}
		for i := 1; i < len(a); i++ {
			out = append(out, a[i])
			if strings.IndexByte(valueLetters, a[i]) >= 0 {
				break
			}
		}
	}
	return out
}

func hasFlag(flags []byte, letters string) bool {
	return slices.ContainsFunc(flags, func(c byte) bool { return strings.IndexByte(letters, c) >= 0 })
}

// longs is the long options by name, before any --.
func longs(args []string) []string {
	if dd := slices.Index(args, "--"); dd >= 0 {
		args = args[:dd]
	}
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "--") && a != "--" {
			n, _, _ := strings.Cut(a, "=")
			out = append(out, n)
		}
	}
	return out
}

func hasLong(args []string, names ...string) bool {
	return slices.ContainsFunc(longs(args), func(l string) bool { return slices.Contains(names, l) })
}

// operands is the operands of args, skipping the values of valued options and of value letters ending a cluster.
func operands(args []string, valued map[string]bool, valueLetters string) []string {
	var out []string
	for k := 0; k < len(args); k++ {
		a := args[k]
		if a == "--" {
			out = append(out, args[k+1:]...)
			break
		}
		if strings.HasPrefix(a, "--") {
			if valued[a] {
				k++
			}
			continue
		}
		if shortCluster.MatchString(a) {
			cluster := a[1:]
			at := strings.IndexFunc(cluster, func(r rune) bool { return strings.ContainsRune(valueLetters, r) })
			if at >= 0 && at == len(cluster)-1 {
				k++
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

func nonOptions(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

func at(xs []string, k int) string {
	if k >= 0 && k < len(xs) {
		return xs[k]
	}
	return ""
}

func set(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// ── paths ───────────────────────────────────────────────────────────────────

var (
	varRef     = lazyre.New(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)
	varDefault = lazyre.New(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:?-)([^}]*)\}`)
	subst      = lazyre.New(`\$\(…\)|` + "`…`" + `|[<>]\(…\)|\(…\)`)
)

// pathOf is a word as the absolute path it names, or "" when it cannot be
// told: an unknown variable or a substitution anywhere in it, ~user, or a
// relative word in an unknown directory ("").
func pathOf(w, dir string, ctx *Ctx, vars map[string]string) string {
	if w == "" {
		return ""
	}
	if strings.HasPrefix(w, "~") {
		if w != "~" && !strings.HasPrefix(w, "~/") {
			return ""
		}
		w = ctx.Home + w[1:]
	}
	unknownVar := false
	// ${NAME:-default} and ${NAME-default}: the value when it is set (and,
	// with the colon, not empty), else the default (L479).
	w = varDefault.ReplaceAllStringFunc(w, func(m string) string {
		g := varDefault.FindStringSubmatch(m)
		n, colon, def := g[1], g[2] == ":-", g[3]
		v, set := vars[n]
		if !set {
			switch n {
			case "HOME":
				v, set = ctx.Home, true
			case "PWD":
				v, set = dir, dir != ""
			case "TMPDIR", "USER", "XDG_STATE_HOME":
				v, set = os.LookupEnv(n)
			default:
				unknownVar = true
				return m
			}
		}
		if !set || (colon && v == "") {
			return def
		}
		return v
	})
	w = varRef.ReplaceAllStringFunc(w, func(m string) string {
		n := varRef.FindStringSubmatch(m)[1]
		if v, ok := vars[n]; ok {
			return v
		}
		v, ok := "", false
		switch n {
		case "HOME":
			v, ok = ctx.Home, true
		case "PWD":
			v, ok = dir, dir != ""
		case "TMPDIR", "USER":
			v, ok = os.LookupEnv(n)
		case "XDG_STATE_HOME":
			// As jhon's state path reads it (grant.Dir): the hook's value
			// when absolute, else ~/.local/state, so no spelling of the
			// owner's state escapes its refusal (L477).
			v, ok = os.Getenv(n), true
			if !filepath.IsAbs(v) {
				v = filepath.Join(ctx.Home, ".local", "state")
			}
		}
		if !ok {
			unknownVar = true
			return m
		}
		return v
	})
	if unknownVar || strings.Contains(w, "$") || subst.MatchString(w) {
		return ""
	}
	if filepath.IsAbs(w) {
		return filepath.Clean(w)
	}
	if dir == "" {
		return ""
	}
	return project.Resolve(dir, w)
}

// globRegex is a glob as a regex: segment-wide (`*` within one segment) or find's (`*` across).
func globRegex(p, star string, escapes, ci bool) *regexp.Regexp {
	var b strings.Builder
	if ci {
		b.WriteString("(?i)")
	}
	b.WriteString("^")
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '*':
			b.WriteString(star)
		case c == '?':
			if star == "[^/]*" {
				b.WriteString("[^/]")
			} else {
				b.WriteString(".")
			}
		case escapes && c == '\\' && i+1 < len(p):
			i++
			b.WriteString(regexp.QuoteMeta(p[i : i+1]))
		case c == '[':
			j := strings.IndexByte(p[i+1:], ']')
			if j < 0 {
				b.WriteString(`\[`)
			} else {
				class := p[i+1 : i+1+j]
				if strings.HasPrefix(class, "!") {
					class = "^" + class[1:]
				}
				b.WriteString("[" + class + "]")
				i += j + 1
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return regexp.MustCompile(`$^`)
	}
	return re
}

func segRegex(seg string) *regexp.Regexp { return globRegex(seg, "[^/]*", false, true) }

func isGlob(seg string) bool { return strings.ContainsAny(seg, "*?[") }

// VCSDirs are the VCS directories no agent writes.
var VCSDirs = []string{".git", ".hg", ".svn"}

// vcsHit is the VCS directory a path passes through (a glob segment that can match one counts), or "".
func vcsHit(abs string) string { return vcsSegment(abs, true) }

// vcsSegment is vcsHit, reading a glob segment as the names it matches only
// when globs is true: code that calls no glob function writes a `.*?` (a
// regular expression in it) as those three characters, never as `.git`.
func vcsSegment(abs string, globs bool) string {
	for _, p := range []string{abs, project.Realish(abs)} {
		for _, seg := range strings.Split(p, string(filepath.Separator)) {
			if slices.Contains(VCSDirs, strings.ToLower(seg)) {
				return seg
			}
			// `*` does not match a dotfile; a pattern that starts with `.` can.
			if globs && isGlob(seg) && strings.HasPrefix(seg, ".") && slices.ContainsFunc(VCSDirs, segRegex(seg).MatchString) {
				return seg
			}
		}
	}
	return ""
}

// globBase is the leading part of a path before its first glob segment; ok is false without one.
func globBase(abs string) (string, bool) {
	segs := strings.Split(abs, string(filepath.Separator))
	i := slices.IndexFunc(segs, isGlob)
	if i < 0 {
		return "", false
	}
	if b := strings.Join(segs[:i], string(filepath.Separator)); b != "" {
		return b, true
	}
	return string(filepath.Separator), true
}

// outsideWhy is why deleting abs is destructive, or "" when it lands inside
// the project or a scratch root (never the root itself); root allows a
// recursive mode or owner change to land on a root.
func outsideWhy(abs string, ctx *Ctx, root bool) string {
	if vcs := vcsHit(abs); vcs != "" {
		return "it reaches the VCS directory " + vcs
	}
	if grant.InGrants(abs) {
		return "it reaches the grants directory"
	}
	if project.IsClaudeSettings(abs) {
		return "it reaches a Claude Code settings file, which configures the guards"
	}
	p := project.Realish(abs)
	g, globbed := globBase(p)
	covers := func(x string) bool { return project.Within(x, p) || (globbed && project.Within(x, g)) }
	if p == "/" || (globbed && g == "/") {
		return "it reaches /"
	}
	if covers(ctx.Home) {
		return "it reaches the home directory"
	}
	for _, r := range ctx.Projects {
		if covers(r) && !(root && p == r) {
			return "it reaches the project root"
		}
	}
	if slices.ContainsFunc(ctx.Roots, func(r string) bool { return project.Within(p, r) && (root || p != r) }) ||
		slices.ContainsFunc(ctx.Deletable, func(t string) bool { return project.Within(p, t) && p != t }) {
		return ""
	}
	// A linked worktree of the project, and an entry of a Claude Code memory
	// directory, are the project's own to remove.
	if slices.ContainsFunc(ctx.Worktrees, func(w string) bool { return project.Within(p, w) }) {
		return ""
	}
	memory := filepath.Join(ctx.Home, ".claude", "projects")
	if project.Within(p, memory) && len(p) > len(memory)+1 && memoryEntry.MatchString(p[len(memory)+1:]) {
		return ""
	}
	return outsideRoots
}

// outsideRoots is outsideWhy's answer for a path in no project and no
// scratch root; inside the home directory, such a delete is the owner's to
// approve (deleteFinding).
const outsideRoots = "it is outside the project and the scratch roots (/tmp, $TMPDIR, ~/.cache)"

// writeWhy is outsideWhy for a recursive mode or owner change, or a dd:
// a write, so a path outside the project is the sandbox's (T128f).
func writeWhy(abs string, ctx *Ctx) string {
	if why := outsideWhy(abs, ctx, true); why != outsideRoots {
		return why
	}
	return ""
}

// deleteFinding is the finding of deleting abs, which outsideWhy refused
// with why: an ask of the destroy class for a path under the home directory
// that holds no credentials, a deny for anything else.
func deleteFinding(what, why, abs string, ctx *Ctx) Finding {
	p := project.Realish(abs)
	if why == outsideRoots && project.Within(p, ctx.Home) && p != ctx.Home && protectedWhy(abs, ctx) == "" {
		return destroy(what, why)
	}
	return deny(what, why)
}

var memoryEntry = lazyre.New(`^[^/]+/memory/.`)

// ── shell writes ────────────────────────────────────────────────────────────

var (
	// protectedHome is the HOME paths that hold credentials or the framework.
	protectedHome = []string{".ssh", ".gnupg", ".aws", ".config/gh", ".netrc", ".config/jhon", ".local/share/jhon", ".local/bin/jhon"}
	deviceWriteOK = lazyre.New(`^/dev/(?:null|stdout|stderr|tty|zero|fd/\d+|shm(?:/.*)?)$`)
)

// ProtectedHome is protectedHome for the OS sandbox (T128a), by what each
// path holds: credentials (credentialHomes), which a command does not see,
// and the framework, which it reads and never writes.
func ProtectedHome() (credentials, framework []string) {
	credentials = slices.Clone(credentialHomes)
	for _, p := range protectedHome {
		if !slices.Contains(credentials, p) {
			framework = append(framework, p)
		}
	}
	return credentials, framework
}

// protectedWhy is why writing abs is never allowed, or "".
func protectedWhy(abs string, ctx *Ctx) string {
	if vcs := vcsHit(abs); vcs != "" {
		return "it writes into the VCS directory " + vcs
	}
	if grant.InGrants(abs) {
		return "it writes into the grants directory"
	}
	if project.IsClaudeSettings(abs) {
		return "it writes a Claude Code settings file, which configures the guards"
	}
	for _, p := range []string{abs, project.Realish(abs)} {
		for _, r := range protectedHome {
			if project.Within(p, filepath.Join(ctx.Home, r)) {
				return "it writes into ~/" + r + ", which holds credentials or the framework"
			}
		}
	}
	return ""
}

type link struct{ from, to string }

// Links are the links a command created, in the order it created them.
type Links struct{ list []link }

func (l *Links) set(from, to string) {
	for i := range l.list {
		if l.list[i].from == from {
			l.list[i].to = to
			return
		}
	}
	l.list = append(l.list, link{from, to})
}

func (l *Links) clone() *Links { return &Links{list: slices.Clone(l.list)} }

// followLinks is abs read through the links this command created earlier.
func followLinks(abs string, st *State) string {
	for n := 0; n < 8; n++ {
		i := slices.IndexFunc(st.Links.list, func(k link) bool { return project.Within(abs, k.from) })
		if i < 0 {
			return abs
		}
		k := st.Links.list[i]
		abs = k.to + abs[len(k.from):]
	}
	return abs
}

// judgeWrite is the findings of one shell write of the word w: a device,
// a path no agent writes (protectedWhy), or a credentials file
// (credentialWhy). Any other target, one the guard cannot resolve too,
// passes: the sandbox bounds where it lands (T128f, L1366).
func judgeWrite(w, label string, ctx *Ctx, vars map[string]string, st *State) []Finding {
	a := pathOf(w, st.Dir, ctx, vars)
	if a != "" {
		a = followLinks(a, st)
	}
	if why := credentialWhy(w, a); why != "" {
		return []Finding{deny(label, why)}
	}
	if a == "" {
		return nil
	}
	if strings.HasPrefix(a, "/dev/") {
		if deviceWriteOK.MatchString(a) {
			return nil
		}
		return []Finding{deny(label, "it writes a device")}
	}
	if why := protectedWhy(a, ctx); why != "" {
		return []Finding{deny(label, why)}
	}
	return nil
}

// credentialWhy is why a shell write or delete of the word w, resolved
// to abs ("" when it cannot be), is refused: it overwrites or removes a
// credentials file, often in no history, by its name or a glob naming one
// (L1368). Reading one is not limited.
func credentialWhy(w, abs string) string {
	if CredentialFile(w) || credentialGlobWord(w) || (abs != "" && CredentialFile(abs)) {
		return CredentialsWhy(w)
	}
	return ""
}

func isDirAt(w string, st *State, ctx *Ctx, vars map[string]string) bool {
	a := pathOf(w, st.Dir, ctx, vars)
	if a == "" {
		return false
	}
	info, err := os.Stat(followLinks(a, st))
	return err == nil && info.IsDir()
}

type destValued struct {
	valued  map[string]bool
	letters string
}

var destOpts = map[string]destValued{
	"cp":      {set("-S", "--suffix", "-t", "--target-directory"), "St"},
	"mv":      {set("-S", "--suffix", "-t", "--target-directory"), "St"},
	"ln":      {set("-S", "--suffix", "-t", "--target-directory"), "St"},
	"install": {set("-m", "--mode", "-o", "--owner", "-g", "--group", "-S", "--suffix", "-t", "--target-directory"), "mogSt"},
}

type pair struct{ written, source string }

// destinations is the paths cp, mv, ln, and install write: the -t
// directory and each source's name in it, or the last operand, and the
// source's name in it when it is a directory; for ln, each link and what it
// points at; for mv, the sources it removes.
func destinations(name string, args []string, st *State, ctx *Ctx, vars map[string]string) (writes []string, links []pair, sources []string) {
	o := destOpts[name]
	tdir, hasT, noTarget := "", false, false
	for k := 0; k < len(args); k++ {
		a := args[k]
		if a == "--" {
			break
		}
		if a == "-t" || a == "--target-directory" {
			tdir, hasT = at(args, k+1), true
			k++
			continue
		}
		if strings.HasPrefix(a, "--target-directory=") {
			tdir, hasT = a[19:], true
			continue
		}
		if a == "--no-target-directory" {
			noTarget = true
			continue
		}
		if shortCluster.MatchString(a) && !strings.HasPrefix(a, "--") {
			for i := 1; i < len(a); i++ {
				if a[i] == 'T' {
					noTarget = true
				}
				if a[i] == 't' {
					tdir, hasT = a[i+1:], true
					if tdir == "" {
						k++
						tdir = at(args, k)
					}
					break
				}
				if strings.IndexByte(o.letters, a[i]) >= 0 {
					break
				}
			}
		}
	}
	ops := operands(args, o.valued, o.letters)
	if name == "install" && (hasFlag(shortFlags(args, o.letters), "d") || slices.Contains(longs(args), "--directory")) {
		return ops, nil, nil
	}
	inside := func(d, src string) string {
		b := sh.Base(strings.TrimRight(src, "/"))
		// A name the guard cannot read still lands in the directory: judge the directory.
		if strings.Contains(b, "$") || strings.Contains(b, "…") || b == "" {
			return d
		}
		return strings.TrimRight(d, "/") + "/" + b
	}
	var pairs []pair
	switch {
	case hasT:
		for _, s := range ops {
			pairs = append(pairs, pair{inside(tdir, s), s})
		}
	case len(ops) == 0:
	case name == "ln" && len(ops) == 1:
		pairs = []pair{{sh.Base(strings.TrimRight(ops[0], "/")), ops[0]}}
	default:
		dest := ops[len(ops)-1]
		srcs := ops[:len(ops)-1]
		dirDest := !noTarget && (strings.HasSuffix(dest, "/") || len(srcs) > 1 || isDirAt(dest, st, ctx, vars))
		for _, s := range srcs {
			if dirDest {
				pairs = append(pairs, pair{inside(dest, s), s})
			} else {
				pairs = append(pairs, pair{dest, s})
			}
		}
	}
	if hasT {
		writes = append(writes, tdir)
	}
	for _, p := range pairs {
		if !slices.Contains(writes, p.written) {
			writes = append(writes, p.written)
		}
	}
	if name == "ln" {
		links = pairs
	}
	if name == "mv" {
		for _, p := range pairs {
			sources = append(sources, p.source)
		}
	}
	return writes, links, sources
}
