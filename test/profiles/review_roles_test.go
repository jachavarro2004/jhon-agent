// review-refuter and review-validator are agents again (T118f, L730, L731):
// a profile may give each its own model, with no warning, and `jhon profile
// json` lists them for the panel like the lenses.
package profiles

import (
	"encoding/json"
	"testing"
)

func TestReviewRoleRows(t *testing.T) {
	t.Parallel()
	h, _ := home(t, `{"active":"p","profiles":{"p":{"agents":{"review-refuter":{"pi":"prov/ref@medium"},"review-validator":{"pi":"prov/val@high"}}}}}`)
	r := run(t, h, "profile", "json")
	want(t, r, 0, "")
	if r.Stderr != "" {
		t.Errorf("a review-refuter or review-validator row warns: %s", r.Stderr)
	}
	var got struct {
		Profiles []struct {
			Name   string
			Agents []map[string]string
		}
	}
	if err := json.Unmarshal([]byte(r.Stdout), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, r.Stdout)
	}
	rows := map[string]map[string]string{}
	for _, p := range got.Profiles {
		for _, a := range p.Agents {
			if a["runtime"] == "pi" {
				rows[p.Name+" "+a["agent"]] = a
			}
		}
	}
	for key, model := range map[string]string{"p review-refuter": "prov/ref@medium", "p review-validator": "prov/val@high"} {
		if a := rows[key]; a["model"] != model || a["source"] != "profile agent "+key[2:]+".pi" {
			t.Errorf("%s = %v, want model %s from its profile row", key, a, model)
		}
	}
	for _, key := range []string{"default review-refuter", "default review-validator"} {
		if rows[key] == nil {
			t.Errorf("%s has no row in the panel", key)
		}
	}
}
