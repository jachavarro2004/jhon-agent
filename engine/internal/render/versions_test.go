package render

import (
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// T50g (F7): pi's packages are pinned from the versions table, and every
// tool doctor compares names its command and pin.
func TestPiPackagesArePinnedFromTheVersionsTable(t *testing.T) {
	if piWebAccess != "npm:pi-web-access@"+PiWebAccessVersion {
		t.Errorf("pi-web-access = %s", piWebAccess)
	}
	if piClaudeBridge != "npm:pi-claude-bridge@"+PiClaudeBridgeVersion {
		t.Errorf("pi-claude-bridge = %s", piClaudeBridge)
	}
	if got := js.Prop(piPretty(), "source"); got != "npm:@heyhuynhgiabuu/pi-pretty@"+PiPrettyVersion {
		t.Errorf("pi-pretty source = %v", got)
	}
	want := map[string]string{"claude": ClaudeCodeVersion, "engram": EngramVersion, "codegraph": CodeGraphVersion}
	if len(Tools) != len(want) {
		t.Errorf("tools = %+v", Tools)
	}
	for _, tool := range Tools {
		if want[tool.Command] == "" || tool.Version != want[tool.Command] {
			t.Errorf("tool %+v is not the table's", tool)
		}
	}
}
