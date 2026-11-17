package release

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"runtime/debug"
	"sort"
	"strings"
	"time"
)

// The software bill of materials of a release artifact (T52): an SPDX 2.3
// JSON document per tarball, jhon-<version>-linux-<arch>.spdx.json beside
// it, signed with it. Its Go half is what the binary was built from (the
// module list go embeds in it, go.sum's hashes and the replaces included,
// and the Go standard library), and its npm half the shell's lockfile
// packages that are not dev-only. It is deterministic: the same artifact
// gives the same bytes, its time the commit's.

// SBOMName is the SBOM of version for arch.
func SBOMName(version, arch string) string {
	return fmt.Sprintf("jhon-%s-linux-%s.spdx.json", version, arch)
}

type spdxChecksum struct {
	Algorithm     string `json:"algorithm"`
	ChecksumValue string `json:"checksumValue"`
}

type spdxRef struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
}

type spdxPackage struct {
	SPDXID                string         `json:"SPDXID"`
	Name                  string         `json:"name"`
	VersionInfo           string         `json:"versionInfo"`
	PackageFileName       string         `json:"packageFileName,omitempty"`
	PrimaryPackagePurpose string         `json:"primaryPackagePurpose,omitempty"`
	DownloadLocation      string         `json:"downloadLocation"`
	FilesAnalyzed         bool           `json:"filesAnalyzed"`
	Checksums             []spdxChecksum `json:"checksums,omitempty"`
	LicenseConcluded      string         `json:"licenseConcluded"`
	LicenseDeclared       string         `json:"licenseDeclared"`
	CopyrightText         string         `json:"copyrightText"`
	ExternalRefs          []spdxRef      `json:"externalRefs,omitempty"`
	Comment               string         `json:"comment,omitempty"`
}

type spdxRelationship struct {
	SpdxElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSpdxElement string `json:"relatedSpdxElement"`
}

type spdxDocument struct {
	SPDXVersion       string `json:"spdxVersion"`
	DataLicense       string `json:"dataLicense"`
	SPDXID            string `json:"SPDXID"`
	Name              string `json:"name"`
	DocumentNamespace string `json:"documentNamespace"`
	CreationInfo      struct {
		Created  string   `json:"created"`
		Creators []string `json:"creators"`
	} `json:"creationInfo"`
	Packages      []spdxPackage      `json:"packages"`
	Relationships []spdxRelationship `json:"relationships"`
}

const noAssertion = "NOASSERTION"

// dependency is a package the artifact holds, with nothing asserted
// beyond what its manifest says.
func dependency(id, name, version, purl string) spdxPackage {
	return spdxPackage{
		SPDXID: id, Name: name, VersionInfo: version,
		DownloadLocation: noAssertion, LicenseConcluded: noAssertion, LicenseDeclared: noAssertion, CopyrightText: noAssertion,
		ExternalRefs: []spdxRef{{"PACKAGE-MANAGER", "purl", purl}},
	}
}

// sbom is the SPDX document of the artifact (its file name and sha256),
// built from info, the binary's build info, and lock, the shell's
// package-lock.json (none when nil).
func sbom(info *debug.BuildInfo, lock []byte, artifact, ver, sum string, created time.Time) ([]byte, error) {
	var doc spdxDocument
	doc.SPDXVersion, doc.DataLicense, doc.SPDXID = "SPDX-2.3", "CC0-1.0", "SPDXRef-DOCUMENT"
	doc.Name = artifact
	doc.DocumentNamespace = "https://github.com/jachavarro2004/jhon-agent/spdx/" + artifact + "-" + sum
	doc.CreationInfo.Created = created.UTC().Format(time.RFC3339)
	doc.CreationInfo.Creators = []string{"Tool: jhon-release-pack"}
	doc.Packages = []spdxPackage{{
		SPDXID: "SPDXRef-Package-jhon", Name: "jhon", VersionInfo: ver, PackageFileName: artifact,
		PrimaryPackagePurpose: "APPLICATION", DownloadLocation: noAssertion,
		Checksums:        []spdxChecksum{{"SHA256", sum}},
		LicenseConcluded: noAssertion, LicenseDeclared: noAssertion, CopyrightText: noAssertion,
	}}

	deps := append([]*debug.Module(nil), info.Deps...)
	sort.Slice(deps, func(i, j int) bool { return deps[i].Path < deps[j].Path })
	for i, m := range deps {
		p := dependency(fmt.Sprintf("SPDXRef-Package-go-%d", i+1), m.Path, m.Version, "pkg:golang/"+m.Path+"@"+m.Version)
		if r := m.Replace; r != nil {
			p.Comment = "replaced by " + r.Path
			if r.Version != "" {
				p.Comment += "@" + r.Version
			}
			if r.Sum != "" {
				p.Comment += ", go.sum " + r.Sum
			}
		} else if m.Sum != "" {
			p.Comment = "go.sum " + m.Sum
		}
		doc.Packages = append(doc.Packages, p)
	}
	doc.Packages = append(doc.Packages, dependency("SPDXRef-Package-go-stdlib", "stdlib", info.GoVersion, "pkg:golang/stdlib@"+info.GoVersion))

	if lock != nil {
		npm, err := npmPackages(lock)
		if err != nil {
			return nil, err
		}
		doc.Packages = append(doc.Packages, npm...)
	}

	doc.Relationships = []spdxRelationship{{"SPDXRef-DOCUMENT", "DESCRIBES", "SPDXRef-Package-jhon"}}
	for _, p := range doc.Packages[1:] {
		doc.Relationships = append(doc.Relationships, spdxRelationship{"SPDXRef-Package-jhon", "DEPENDS_ON", p.SPDXID})
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// npmPackages are the packages of a package-lock.json (v2 or v3) that are
// not dev-only and not workspace links, sorted by their path in it.
func npmPackages(lock []byte) ([]spdxPackage, error) {
	var l struct {
		Packages map[string]struct {
			Name, Version, License, Integrity string
			Dev, Link                         bool
		} `json:"packages"`
	}
	if err := json.Unmarshal(lock, &l); err != nil {
		return nil, fmt.Errorf("package-lock.json: %v", err)
	}
	keys := make([]string, 0, len(l.Packages))
	for k, p := range l.Packages {
		if k != "" && !p.Dev && !p.Link {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []spdxPackage
	for i, k := range keys {
		p := l.Packages[k]
		name := p.Name
		if name == "" {
			_, name, _ = cutLast(k, "node_modules/")
		}
		purl := "pkg:npm/" + purlEscape(name) + "@" + p.Version
		if scope, base, ok := strings.Cut(name, "/"); ok {
			purl = "pkg:npm/" + purlEscape(scope) + "/" + purlEscape(base) + "@" + p.Version
		}
		s := dependency(fmt.Sprintf("SPDXRef-Package-npm-%d", i+1), name, p.Version, purl)
		if p.License != "" {
			s.LicenseDeclared = p.License
		}
		if algo, sum, ok := strings.Cut(p.Integrity, "-"); ok && algo == "sha512" {
			if b, err := base64.StdEncoding.DecodeString(sum); err == nil {
				s.Checksums = []spdxChecksum{{"SHA512", hex.EncodeToString(b)}}
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// cutLast is strings.Cut around the last sep; s whole when there is none.
func cutLast(s, sep string) (before, after string, found bool) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return "", s, false
}

// purlEscape is a purl segment: path-escaped, '@' (a scope's) included.
func purlEscape(s string) string { return strings.ReplaceAll(url.PathEscape(s), "@", "%40") }
