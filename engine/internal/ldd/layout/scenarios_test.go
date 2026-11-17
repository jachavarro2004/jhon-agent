package layout_test

// An external test: package ldd, which sets the spec reader the shaped
// layouts use, imports layout.

import (
	"reflect"
	"testing"

	_ "github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
)

const deltaSpec = `# Delta for auth

## ADDED Requirements

### Requirement: Login
The user logs in.

#### Scenario: Valid password
- GIVEN a user
- WHEN the password is right   

- THEN a session starts

#### Scenario: Wrong password
- THEN it is refused

## MODIFIED Requirements

### Requirement: Logout
#### Scenario:
- an unnamed one

## REMOVED Requirements

### Requirement: Remember me
`

const mainSpec = `# auth

## Requirement: Login
The user logs in.

### Scenario: Valid password
- GIVEN a user
`

// A spec's requirements and scenarios read the same in both layouts: the
// delta operation of each block, heading-only ones (REMOVED, RENAMED)
// marked, unnamed scenarios kept, and each body trimmed (G1).
func TestScenariosOfBothLayouts(t *testing.T) {
	want := []layout.Requirement{
		{Name: "Login", Op: "ADDED", Scenarios: []layout.Scenario{
			{Name: "Valid password", Body: "- GIVEN a user\n- WHEN the password is right\n\n- THEN a session starts"},
			{Name: "Wrong password", Body: "- THEN it is refused"},
		}},
		{Name: "Logout", Op: "MODIFIED", Scenarios: []layout.Scenario{{Name: "", Body: "- an unnamed one"}}},
		{Name: "Remember me", Op: "REMOVED", HeadingOnly: true, Scenarios: []layout.Scenario{}},
	}
	wantMain := []layout.Requirement{{Name: "Login", Scenarios: []layout.Scenario{{Name: "Valid password", Body: "- GIVEN a user"}}}}
	for _, l := range []layout.Layout{layout.OpenSpec, layout.Native} {
		got, err := l.Scenarios(deltaSpec, 0)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: Scenarios(delta) = %+v, %v", l.Name(), got, err)
		}
		got, err = l.Scenarios(mainSpec, 2)
		if err != nil || !reflect.DeepEqual(got, wantMain) {
			t.Errorf("%s: Scenarios(main, 2) = %+v, %v", l.Name(), got, err)
		}
		if got, err := l.Scenarios("# nothing\n", 0); err != nil || len(got) != 0 {
			t.Errorf("%s: Scenarios(no requirement) = %+v, %v", l.Name(), got, err)
		}
	}
}
