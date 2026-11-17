package release

import (
	"bytes"
	"encoding/json"
	"runtime/debug"
	"slices"
	"testing"
	"time"
)

func TestSBOM(t *testing.T) {
	info := &debug.BuildInfo{
		GoVersion: "go1.27.0",
		Main:      debug.Module{Path: "github.com/jachavarro2004/jhon-agent"},
		Deps: []*debug.Module{
			{Path: "golang.org/x/sys", Version: "v0.30.0", Sum: "h1:sys="},
			{Path: "github.com/charmbracelet/bubbletea", Version: "v1.3.0", Replace: &debug.Module{Path: "./third_party/bubbletea", Version: ""}},
		},
	}
	lock := []byte(`{"lockfileVersion": 3, "packages": {
		"": {"name": "jhon-shell", "version": "0.2.0"},
		"node_modules/devonly": {"version": "1.0.0", "dev": true, "license": "MIT"},
		"node_modules/@s/y": {"version": "2.1.0", "license": "MIT", "integrity": "sha512-AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8gISIjJCUmJygpKissLS4vMDEyMzQ1Njc4OTo7PD0+Pw=="},
		"node_modules/@s/y/node_modules/z": {"version": "0.1.0"}
	}}`)
	created := time.Date(2026, 11, 6, 23, 40, 39, 0, time.UTC)
	const sum = "ab12"
	b, err := sbom(info, lock, "jhon-0.1.0-linux-amd64.tar.gz", "0.1.0", sum, created)
	must(t, err)
	again, err := sbom(info, lock, "jhon-0.1.0-linux-amd64.tar.gz", "0.1.0", sum, created)
	must(t, err)
	if !bytes.Equal(b, again) {
		t.Fatal("the same inputs gave two SBOMs")
	}

	var doc struct {
		SPDXVersion, DataLicense, SPDXID, Name, DocumentNamespace string
		CreationInfo                                              struct {
			Created  string
			Creators []string
		}
		Packages []struct {
			SPDXID, Name, VersionInfo, PackageFileName, LicenseDeclared, DownloadLocation, Comment string
			FilesAnalyzed                                                                          bool
			Checksums                                                                              []struct{ Algorithm, ChecksumValue string }
			ExternalRefs                                                                           []struct{ ReferenceCategory, ReferenceType, ReferenceLocator string }
		}
		Relationships []struct{ SpdxElementID, RelationshipType, RelatedSpdxElement string }
	}
	must(t, json.Unmarshal(b, &doc))
	equalS := func(name, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %q; want %q", name, got, want)
		}
	}
	equalS("spdxVersion", doc.SPDXVersion, "SPDX-2.3")
	equalS("dataLicense", doc.DataLicense, "CC0-1.0")
	equalS("SPDXID", doc.SPDXID, "SPDXRef-DOCUMENT")
	equalS("name", doc.Name, "jhon-0.1.0-linux-amd64.tar.gz")
	equalS("documentNamespace", doc.DocumentNamespace, "https://github.com/jachavarro2004/jhon-agent/spdx/jhon-0.1.0-linux-amd64.tar.gz-ab12")
	equalS("created", doc.CreationInfo.Created, "2026-11-06T23:40:39Z")
	if !slices.Equal(doc.CreationInfo.Creators, []string{"Tool: jhon-release-pack"}) {
		t.Errorf("creators %q", doc.CreationInfo.Creators)
	}

	var names, purls []string
	for _, p := range doc.Packages {
		names = append(names, p.Name+"@"+p.VersionInfo)
		for _, r := range p.ExternalRefs {
			if r.ReferenceCategory != "PACKAGE-MANAGER" || r.ReferenceType != "purl" {
				t.Errorf("%s: external ref %+v", p.Name, r)
			}
			purls = append(purls, r.ReferenceLocator)
		}
	}
	wantNames := []string{"jhon@0.1.0", "github.com/charmbracelet/bubbletea@v1.3.0", "golang.org/x/sys@v0.30.0", "stdlib@go1.27.0", "@s/y@2.1.0", "z@0.1.0"}
	if !slices.Equal(names, wantNames) {
		t.Errorf("packages %q; want %q (no dev-only npm package)", names, wantNames)
	}
	wantPurls := []string{"pkg:golang/github.com/charmbracelet/bubbletea@v1.3.0", "pkg:golang/golang.org/x/sys@v0.30.0", "pkg:golang/stdlib@go1.27.0", "pkg:npm/%40s/y@2.1.0", "pkg:npm/z@0.1.0"}
	if !slices.Equal(purls, wantPurls) {
		t.Errorf("purls %q; want %q", purls, wantPurls)
	}

	root := doc.Packages[0]
	equalS("root SPDXID", root.SPDXID, "SPDXRef-Package-jhon")
	equalS("root packageFileName", root.PackageFileName, "jhon-0.1.0-linux-amd64.tar.gz")
	if len(root.Checksums) != 1 || root.Checksums[0].Algorithm != "SHA256" || root.Checksums[0].ChecksumValue != sum {
		t.Errorf("root checksums %+v; want SHA256 %s", root.Checksums, sum)
	}
	equalS("bubbletea comment", doc.Packages[1].Comment, "replaced by ./third_party/bubbletea")
	equalS("x/sys comment", doc.Packages[2].Comment, "go.sum h1:sys=")
	y := doc.Packages[4]
	equalS("@s/y license", y.LicenseDeclared, "MIT")
	if len(y.Checksums) != 1 || y.Checksums[0].Algorithm != "SHA512" || y.Checksums[0].ChecksumValue[:8] != "00010203" || len(y.Checksums[0].ChecksumValue) != 128 {
		t.Errorf("@s/y checksums %+v; want its integrity as SHA512 hex", y.Checksums)
	}
	equalS("z license", doc.Packages[5].LicenseDeclared, "NOASSERTION")
	for _, p := range doc.Packages {
		if p.FilesAnalyzed || p.DownloadLocation == "" {
			t.Errorf("%s: filesAnalyzed %v, downloadLocation %q", p.Name, p.FilesAnalyzed, p.DownloadLocation)
		}
	}

	if len(doc.Relationships) != len(doc.Packages) {
		t.Fatalf("%d relationships for %d packages", len(doc.Relationships), len(doc.Packages))
	}
	equalS("describes", doc.Relationships[0].SpdxElementID+" "+doc.Relationships[0].RelationshipType+" "+doc.Relationships[0].RelatedSpdxElement, "SPDXRef-DOCUMENT DESCRIBES SPDXRef-Package-jhon")
	for i, r := range doc.Relationships[1:] {
		equalS("depends", r.SpdxElementID+" "+r.RelationshipType+" "+r.RelatedSpdxElement, "SPDXRef-Package-jhon DEPENDS_ON "+doc.Packages[i+1].SPDXID)
	}

	// Without a lockfile (a release with no shell), the Go half alone.
	b, err = sbom(info, nil, "a.tar.gz", "0.1.0", sum, created)
	must(t, err)
	must(t, json.Unmarshal(b, &doc))
	if len(doc.Packages) != 4 {
		t.Errorf("%d packages without a lockfile; want 4", len(doc.Packages))
	}
}

func TestSBOMName(t *testing.T) {
	if got := SBOMName("0.1.0", "arm64"); got != "jhon-0.1.0-linux-arm64.spdx.json" {
		t.Errorf("SBOMName = %q", got)
	}
}
