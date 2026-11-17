package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/compose"
)

// `jhon compose <main-spec> <delta-spec>`: apply a delta
// spec to a main spec, atomically (internal/compose). Exit 0 composed, 1
// refused or failed with the main spec unchanged, 2 usage.
func init() { register("compose", runCompose) }

func runCompose(argv []string) (int, error) {
	if len(argv) != 2 || strings.HasPrefix(argv[0], "-") || strings.HasPrefix(argv[1], "-") {
		fmt.Fprintln(os.Stderr, "usage: jhon compose <main-spec> <delta-spec>")
		return 2, nil
	}
	mainPath, deltaPath := argv[0], argv[1]
	if _, err := compose.ComposeFile(mainPath, deltaPath, compose.OS); err != nil {
		outcome := "failed"
		if refusal := (*compose.Refusal)(nil); errors.As(err, &refusal) {
			outcome = "refused"
		}
		fmt.Fprintf(os.Stderr, "jhon compose: %s: %s\n", outcome, err)
		fmt.Fprintln(os.Stderr, "nothing was written; the main spec is unchanged")
		return 1, nil
	}
	fmt.Printf("composed %s into %s\n", deltaPath, mainPath)
	return 0, nil
}
