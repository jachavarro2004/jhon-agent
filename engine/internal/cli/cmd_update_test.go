package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/update"
)

// The update screens' backend: a newer release whose checksum does not
// match shows as not verified, and Update refuses it saying why, before
// anything is installed.
func TestTheTUIUpdaterRefusesAnUnverifiedRelease(t *testing.T) {
	name := release.ArtifactName("0.2.0", runtime.GOARCH)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/") {
		case "latest":
			var assets []map[string]string
			for _, n := range []string{name, name + ".sha256", name + ".sigstore.json"} {
				assets = append(assets, map[string]string{"name": n, "browser_download_url": srv.URL + "/" + n})
			}
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.2.0", "published_at": "2026-11-01T10:00:00Z", "assets": assets})
		case name:
			w.Write([]byte("tarball"))
		case name + ".sha256":
			w.Write([]byte(strings.Repeat("0", 64) + "  " + name + "\n"))
		default:
			w.Write([]byte("{}"))
		}
	}))
	defer srv.Close()
	u := &tuiUpdater{c: &update.Client{URL: srv.URL + "/latest", HTTP: srv.Client(), Arch: runtime.GOARCH, Current: "0.1.0"}, r: release.Roots{Share: t.TempDir()}}
	defer u.close()
	rel, err := u.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if rel.Latest != "0.2.0" || rel.Current != "0.1.0" || rel.Verified || rel.Date != "2026-11-01" || rel.Source != update.Source {
		t.Fatalf("Latest = %+v", rel)
	}
	if err := u.Update(rel); err == nil || err.Error() != "checksum mismatch for "+name+"; nothing was changed" {
		t.Fatalf("Update = %v", err)
	}
	if err := u.Rollback("0.0.9"); err == nil || err.Error() != "jhon 0.0.9 is not kept on disk; nothing was changed" {
		t.Fatalf("Rollback = %v", err)
	}
}
