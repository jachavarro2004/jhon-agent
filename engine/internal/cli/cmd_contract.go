package cli

import (
	"fmt"
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// `jhon contract [--json]`: the version of the surface jhon-shell (shell/)
// consumes (shell/CONTRACT.md), from contracts/vocabulary.yaml
// `shell_contract`. The shell refuses every guarded call when it differs
// from the one its package.json names. Read-only; exit 2 on a usage error.
func init() { register("contract", runContract) }

const contractUsage = "usage: jhon contract [--json]\n"

func runContract(argv []string) (int, error) {
	json := false
	for _, a := range argv {
		if a != "--json" {
			fmt.Fprintf(os.Stderr, "jhon contract: unknown argument: %s\n%s", a, contractUsage)
			return 2, nil
		}
		json = true
	}
	v, err := contract.Vocabulary("shell_contract", 1)
	if err != nil {
		return 1, err
	}
	if !json {
		fmt.Printf("shell %s\n", v[0])
		return 0, nil
	}
	fmt.Println(js.StringifyValue(js.Obj("shell", v[0])))
	return 0, nil
}
