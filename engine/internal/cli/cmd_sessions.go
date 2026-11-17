package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/session"
)

// `jhon sessions [--json]` (docket AI1): the live
// orchestrator sessions that started before the framework was last
// installed. Read-only; exit 2 on an unknown argument.
func init() { register("sessions", runSessions) }

func runSessions(argv []string) (int, error) {
	asJSON := false
	for _, a := range argv {
		if a == "--json" {
			asJSON = true
			continue
		}
		fmt.Fprintf(os.Stderr, "jhon sessions: unexpected argument: %s\nusage: jhon sessions [--json]\n", a)
		return 2, nil
	}
	r := session.Sessions(session.Sources{})
	if !asJSON {
		fmt.Println(session.Human(r))
		return 0, nil
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return 0, enc.Encode(r)
}
