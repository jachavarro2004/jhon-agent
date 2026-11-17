package discover

// The formatter of an edited file (contracts/ldd.md, "Code checks"):
// the owning project's own formatter, the fixing side of its format check,
// so style is never an agent's call. Only a formatter the project states
// counts: the Go toolchain's gofmt, rustfmt at the crate's edition, ruff
// where it is configured (through the project's uv or poetry), and prettier
// where the project installed it.

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// A Formatter rewrites one file in place: Argv runs in Dir, the owning
// project's directory, and Tool names it.
type Formatter struct {
	Dir  string
	Argv []string
	Tool string
}

var (
	crateEdition = lazyre.New(`(?m)^edition\s*=\s*"(\d{4})"`)
	prettierExts = []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts"}
)

// owner is the directory of the nearest project holding file, never past a
// repository's root or into a directory discovery excludes; "" when none.
func owner(file string) string {
	for dir := filepath.Dir(file); ; dir = filepath.Dir(dir) {
		if excluded[filepath.Base(dir)] {
			return ""
		}
		if slices.ContainsFunc(Markers, func(m Marker) bool { return project.Exists(filepath.Join(dir, m.File)) }) {
			return dir
		}
		if isRepo(dir) || dir == filepath.Dir(dir) {
			return ""
		}
	}
}

// FormatterFor is the formatter of the file at the absolute path file, or
// nil when its project states none for it.
func FormatterFor(file string) *Formatter {
	dir := owner(file)
	if dir == "" {
		return nil
	}
	rel, err := filepath.Rel(dir, file)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil
	}
	has := func(name string) bool { return project.Exists(filepath.Join(dir, name)) }
	switch ext := filepath.Ext(file); {
	case ext == ".go" && has("go.mod"):
		return &Formatter{dir, []string{"gofmt", "-w", rel}, "gofmt"}
	case ext == ".rs" && has("Cargo.toml"):
		src, _ := read(filepath.Join(dir, "Cargo.toml"))
		edition := "2015" // rustfmt's own default, as Cargo's for a crate that names none
		if m := crateEdition.FindStringSubmatch(src); m != nil {
			edition = m[1]
		}
		return &Formatter{dir, []string{"rustfmt", "--edition", edition, rel}, "rustfmt"}
	case ext == ".py" && has("pyproject.toml"):
		for _, c := range checks(dir, dir, []string{"pyproject.toml"}) {
			if c.Kind == formatKind() {
				// The format check is `[<manager> run ]ruff format --check .`.
				argv := strings.Fields(strings.TrimSuffix(c.Command, " --check ."))
				return &Formatter{dir, append(argv, rel), "ruff"}
			}
		}
	case slices.Contains(prettierExts, ext) && has("package.json"):
		if bin := filepath.Join(dir, "node_modules", ".bin", "prettier"); project.Exists(bin) {
			return &Formatter{dir, []string{bin, "--write", rel}, "prettier"}
		}
	}
	return nil
}

// formatKind is `code_check_kind`'s format kind.
func formatKind() string {
	kinds, _ := checkVocab()
	return kinds[1]
}
