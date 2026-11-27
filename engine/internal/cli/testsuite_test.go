package cli

import (
	"slices"
	"testing"
)

// T250: `jhon test` gives `go test ./...` its own timeout. test/sandbox's
// binary took 475 s at load 9-25 and over 600 s under more, past go's
// default 10 minutes, which would fail CI with no test at fault.
func TestJhonTestGivesGoTestAThirtyMinuteTimeout(t *testing.T) {
	i := slices.Index(goTestArgs, "-timeout")
	if goTestArgs[0] != "test" || i < 0 || i+1 >= len(goTestArgs) || goTestArgs[i+1] != "30m" || goTestArgs[len(goTestArgs)-1] != "./..." {
		t.Errorf("go %v, want go test -timeout 30m ./...", goTestArgs)
	}
}
