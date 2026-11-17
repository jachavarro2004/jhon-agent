// Package layout is where a project keeps its LDD record (T57, S2): its
// config, its changes and each change's artifacts, the archive, and the
// main specs. OpenSpec is the one layout today; every other package asks
// this one for those paths instead of naming them.
//
// Paths are relative to the project root, with `/` separators, as git
// names them; At joins one to a root for the filesystem.
package layout

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// Layout is one way a project keeps its LDD record.
type Layout interface {
	// Name is the layout's name.
	Name() string
	// Dir is the directory that holds the whole record.
	Dir() string
	// Detect is whether the project at root has this layout's directory.
	Detect(root string) bool
	// Config is the project's LDD config file.
	Config() string
	// ChangesDir is the directory of the changes.
	ChangesDir() string
	// ChangeDir is one change's directory.
	ChangeDir(change string) string
	// Artifact is one artifact (a file or a directory) of a change.
	Artifact(change, name string) string
	// ArchiveDir is the directory archived changes move to.
	ArchiveDir() string
	// Archive is where change, archived on date (YYYY-MM-DD), lives.
	Archive(date, change string) string
	// SpecsDir is the directory of the main specs.
	SpecsDir() string
	// Ignored is whether rel, a path relative to the root, is the
	// record's: no code, and no test, of the project.
	Ignored(rel string) bool

	// The grammar of the record (T57 slice 1): what a change's artifacts
	// are called and how its tasks and specs are read.

	// Tasks is the name of a change's tasks artifact, under its directory.
	Tasks() string
	// DeltaSpecs is the name of a change's spec-delta directory, under its
	// directory; its specs are spec.md files at any depth.
	DeltaSpecs() string
	// MainSpec is the main spec, relative to the root, a delta spec at
	// delta (a path under DeltaSpecs, `/` separators) updates.
	MainSpec(delta string) string
	// SliceHeading matches a slice's heading in the tasks artifact, its
	// name the first group.
	SliceHeading() *lazyre.Regexp
	// Scenarios are the requirements of the spec src, each with its
	// scenarios, its requirement headings at depth (0: a delta's fixed
	// depth). The error is a delta grammar that cannot be read.
	Scenarios(src string, depth int) ([]Requirement, error)
}

// Native is Jhon's own layout, the same shape under ldd/: ldd/config.yaml,
// ldd/changes/<change>/, ldd/changes/archive/<date>-<change>/, and
// ldd/specs/. `/jhon-ldd init` creates it only where no layout is. A
// folder named ldd is common, so it is detected by its config or its
// changes directory, never by its name alone.
var Native Layout = shaped{"native", "ldd", true}

// OpenSpec is the OpenSpec layout: openspec/config.yaml,
// openspec/changes/<change>/, openspec/changes/archive/<date>-<change>/,
// and openspec/specs/.
var OpenSpec Layout = shaped{"openspec", "openspec", false}

// Layouts are the layouts in the order For detects them.
var Layouts = []Layout{OpenSpec, Native}

// Detected is the layout the project at root has, OpenSpec first; false
// when it has none.
func Detected(root string) (Layout, bool) {
	for _, l := range Layouts {
		if l.Detect(root) {
			return l, true
		}
	}
	return nil, false
}

// For is the layout of the project at root: the one it has, else OpenSpec,
// whose paths name a record nobody has written yet (a project without one
// uses no LDD: it has no config).
func For(root string) Layout {
	if l, ok := Detected(root); ok {
		return l
	}
	return OpenSpec
}

// DirsPattern is every layout's directory as one regexp alternation, for a
// pattern that reads a path of any layout's record.
func DirsPattern() string {
	dirs := make([]string, len(Layouts))
	for i, l := range Layouts {
		dirs[i] = regexp.QuoteMeta(l.Dir())
	}
	return strings.Join(dirs, "|")
}

// Words is text, a message that names OpenSpec's paths (openspec/...), with
// them named as root's layout keeps them: unchanged in an OpenSpec project,
// ldd/... in a native one (L479).
func Words(root, text string) string {
	l := For(root)
	if l == OpenSpec {
		return text
	}
	return strings.ReplaceAll(text, OpenSpec.Dir()+"/", l.Dir()+"/")
}

// IgnoredByAny is whether rel is any layout's record.
func IgnoredByAny(rel string) bool {
	for _, l := range Layouts {
		if l.Ignored(rel) {
			return true
		}
	}
	return false
}

// At is rel, relative to root with `/` separators, as a filesystem path.
func At(root, rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }

// shaped is a layout of one shape under its directory: config.yaml,
// changes/<change>/, changes/archive/<date>-<change>/, specs/.
type shaped struct {
	name, dir string
	// strict detects the layout by its config or changes directory, not its directory alone.
	strict bool
}

func (l shaped) Name() string { return l.name }
func (l shaped) Dir() string  { return l.dir }
func (l shaped) Detect(root string) bool {
	if !l.strict {
		_, err := os.Stat(At(root, l.dir))
		return err == nil
	}
	for _, p := range []string{l.Config(), l.ChangesDir()} {
		if _, err := os.Stat(At(root, p)); err == nil {
			return true
		}
	}
	return false
}
func (l shaped) Config() string                      { return path.Join(l.dir, "config.yaml") }
func (l shaped) ChangesDir() string                  { return path.Join(l.dir, "changes") }
func (l shaped) ChangeDir(change string) string      { return path.Join(l.ChangesDir(), change) }
func (l shaped) Artifact(change, name string) string { return path.Join(l.ChangeDir(change), name) }
func (l shaped) ArchiveDir() string                  { return path.Join(l.ChangesDir(), "archive") }
func (l shaped) Archive(date, change string) string {
	return path.Join(l.ArchiveDir(), date+"-"+change)
}
func (l shaped) SpecsDir() string             { return path.Join(l.dir, "specs") }
func (l shaped) Tasks() string                { return "tasks.md" }
func (l shaped) DeltaSpecs() string           { return "specs" }
func (l shaped) MainSpec(delta string) string { return path.Join(l.SpecsDir(), delta) }
func (l shaped) SliceHeading() *lazyre.Regexp { return sliceHeading }
func (l shaped) Scenarios(src string, depth int) ([]Requirement, error) {
	return shapedScenarios(src, depth)
}
func (l shaped) Ignored(rel string) bool {
	return strings.HasPrefix(filepath.ToSlash(rel), l.dir+"/")
}
