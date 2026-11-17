package ldd

import (
	"slices"
	"strings"
	"testing"
)

// `jhon status --json` is a contract (docs/status-json.md): jhon-shell
// (lib/ui/ldd-status.ts, jhon-panel-change.ts) and the dashboard read these
// fields, so each keeps its name and type;
// null stands where the document allows it (S29).
func TestStatusJSONContract(t *testing.T) {
	t.Parallel()
	s := statusJSON(t, cycle, "tasks-partial")
	kinds := map[string]string{
		"ok": "bool", "change": "string", "directory": "string", "artifacts": "array", "tasks": "object",
		"next": "string", "notes": "array", "strict_tdd": "object", "tests": "object|null", "route": "object|null",
		"mode": "string", "needs": "object|null", "folder": "bool", "umbrellas": "array",
	}
	for field, want := range kinds {
		v, present := s[field]
		if !present {
			t.Errorf("%s is missing", field)
			continue
		}
		if got := kind(v); !slices.Contains(strings.Split(want, "|"), got) {
			t.Errorf("%s is %s, want %s", field, got, want)
		}
	}
	tasks := s["tasks"].(map[string]any)
	for _, f := range []string{"total", "completed", "pending", "allComplete", "unchecked", "slices"} {
		if _, ok := tasks[f]; !ok {
			t.Errorf("tasks.%s is missing", f)
		}
	}
	if _, ok := tasks["slice"]; !ok {
		t.Error("tasks.slice is missing (null when no slice is open)")
	}
	art := s["artifacts"].([]any)
	if len(art) == 0 {
		t.Fatal("artifacts is empty")
	}
	for _, f := range []string{"artifact", "state", "path"} {
		if _, ok := art[0].(map[string]any)[f]; !ok {
			t.Errorf("artifacts[].%s is missing", f)
		}
	}
	if _, ok := s["strict_tdd"].(map[string]any)["value"].(bool); !ok {
		t.Error("strict_tdd.value is not a bool")
	}
}

func kind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case string:
		return "string"
	case float64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}
