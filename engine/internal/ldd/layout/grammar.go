package layout

// This file is the grammar the shaped layouts (OpenSpec, Native) share:
// the `## Slice <n>: <name>` heading of tasks.md (contracts/ldd.md,
// "Slices") and the requirement, scenario and delta operation (ADDED,
// MODIFIED, ...) headings of a spec (contracts/spec-delta.md).

import (
	"errors"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// Requirement is one requirement of a spec as a layout reads it.
type Requirement struct {
	// Name is the requirement's name.
	Name string
	// Op is the delta operation of its block ("ADDED", "MODIFIED", ...),
	// "" outside one.
	Op string
	// HeadingOnly is whether Op is a heading-only operation (REMOVED),
	// whose requirement needs no scenario.
	HeadingOnly bool
	// Scenarios are its scenarios in order, an unnamed one included.
	Scenarios []Scenario
}

// Scenario is one scenario of a requirement: its name ("" for an unnamed
// one) and its body, the lines under its heading, each without trailing
// blanks, the blank lines around them dropped.
type Scenario struct {
	Name, Body string
}

// jsSpace and jsDot are JavaScript's \s and . as package js spells them
// (js.Space, js.Dot; a test holds them equal): the package stays portable
// to Windows (validate.yml's PORTABLE list, through project), which js,
// with its POSIX signal names, is not.
const (
	jsSpace = `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`
	jsDot   = `[^\n\r\x{2028}\x{2029}]`
)

// sliceHeading is a `## Slice <n>: <name>` heading of tasks.md. The word
// is spelled as classes: Go's (?i) also folds ſ to s, JavaScript's i flag
// without u does not.
var sliceHeading = lazyre.New(`^#{2,4}` + jsSpace + `+([sS][lL][iI][cC][eE]` + jsSpace + `+\d+\b` + jsDot + `*?)` + jsSpace + `*$`)

// specReader reads a spec with the delta grammar of contracts/spec-delta.md
// as package requirements parses it; package ldd sets it (ReadSpecsWith),
// so this package needs none of requirements' imports.
var specReader func(src string, depth int) ([]Requirement, error)

// ReadSpecsWith sets how the shaped layouts read a spec's requirements and
// scenarios (Layout.Scenarios).
func ReadSpecsWith(read func(src string, depth int) ([]Requirement, error)) { specReader = read }

// errNoSpecReader is a Scenarios call in a program that links no spec reader.
var errNoSpecReader = errors.New("layout: no spec reader: package ldd is not linked")

// shapedScenarios reads src with the spec reader requirements set.
func shapedScenarios(src string, depth int) ([]Requirement, error) {
	if specReader == nil {
		return nil, errNoSpecReader
	}
	return specReader(src, depth)
}
