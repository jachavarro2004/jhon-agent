// Jhon's one identity (odd/tasks/presentation-identity.md S4-S11): the README
// opens with the banner, the tagline and the badge row, has no family section
// (L6), carries the support block, with this repo's jhon-* name, verbatim before
// `## License`; SUPPORT.md and docs/identity.md exist and hold the block; and
// every relative link or image in those files names a file that exists. The
// files are read from the repository root.
package hygiene

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// supportBlock is S9's block, verbatim, with this repo's jhon-* name (L6).
const supportBlock = "## Support jhon-agent\n\n" +
	"jhon-agent is free and open source.\n" +
	"If it helps you, donate any amount you choose,\n" +
	"once or monthly, on GitHub Sponsors.\n" +
	"If you can't donate, a ⭐ on the repo helps\n" +
	"keep the project maintained.\n\n" +
	"[♥ Sponsor on GitHub](https://github.com/sponsors/jachavarro2004)\n"

// badgeStyle is S7's style, the same on every badge.
const badgeStyle = "color=e7bbc8&labelColor=1f1f28"

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func readRoot(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func TestReadmeCarriesTheIdentityKit(t *testing.T) {
	root := repoRoot(t)
	readme := readRoot(t, root, "README.md")

	// S4: the banner first, centred, alt "Jhon", the 61818-byte GIF.
	if !strings.HasPrefix(readme, "<p align=\"center\">\n  <img src=\"docs/assets/brand/jhon-banner.gif\" alt=\"Jhon\"") {
		t.Errorf("README.md does not open with the centred banner, alt \"Jhon\"")
	}
	if fi, err := os.Stat(filepath.Join(root, "docs/assets/brand/jhon-banner.gif")); err != nil || fi.Size() != 61818 {
		t.Errorf("docs/assets/brand/jhon-banner.gif is not the 61818-byte banner: %v", err)
	}

	// S5: the tagline, then S7's badge row under it, in order.
	tagline := strings.Index(readme, "Let the agent run. Deliver only what is proven.")
	second := strings.Index(readme, "An agent framework for Claude Code and pi.")
	if tagline < 0 || second < tagline {
		t.Fatalf("README.md lacks the tagline block")
	}
	badges := []string{
		"https://img.shields.io/github/license/jachavarro2004/jhon-agent?",
		"https://img.shields.io/github/actions/workflow/status/jachavarro2004/jhon-agent/validate.yml?branch=main&label=CI&",
		"https://img.shields.io/github/sponsors/jachavarro2004?label=Sponsor&logo=githubsponsors&",
		"https://img.shields.io/github/stars/jachavarro2004/jhon-agent?style=flat&",
	}
	at := second
	for _, b := range badges {
		i := strings.Index(readme[at:], b)
		if i < 0 {
			t.Errorf("README.md has no badge %s after the tagline, in order", b)
			continue
		}
		at += i
		line := readme[at : at+strings.Index(readme[at:], "\"")]
		if !strings.Contains(line, "style=flat") || !strings.Contains(line, badgeStyle) {
			t.Errorf("badge %s is not style=flat with %s", line, badgeStyle)
		}
	}
	if first := strings.Index(readme, "\n## "); first >= 0 && at > first {
		t.Errorf("the badge row is not under the tagline, before the first section")
	}

	// S8: no family section (L6); S9: the block, then ## License.
	if strings.Contains(readme, "## The Jhon family") {
		t.Errorf("README.md has a family section; L6 drops it")
	}
	if strings.Contains(readme, "## Support Jhon\n") {
		t.Errorf("README.md says \"Support Jhon\"; the block names jhon-agent (L6)")
	}
	support := strings.Index(readme, "\n"+supportBlock)
	license := strings.Index(readme, "\n## License\n")
	if support < 0 || license < 0 || support > license {
		t.Fatalf("README.md wants the support block verbatim, then ## License: %d %d", support, license)
	}
	if strings.Contains(readme, "Licences") {
		t.Errorf("README.md says \"Licences\"; the footer is ## License")
	}
}

func TestSupportAndIdentityDocsHoldTheSupportBlock(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{"SUPPORT.md", "docs/identity.md"} {
		if text := readRoot(t, root, name); !strings.Contains(text, supportBlock) {
			t.Errorf("%s does not hold the support block verbatim", name)
		}
	}
	identity := readRoot(t, root, "docs/identity.md")
	for _, want := range []string{"#e7bbc8", "#53343f", "#e9e0e1", "#1f1f28", "#161618", "Fraunces", "Space Grotesk", "FiraCode Nerd", "jhon-*", badgeStyle, "FUNDING.yml"} {
		if !strings.Contains(identity, want) {
			t.Errorf("docs/identity.md does not state %s", want)
		}
	}
	// S1, S9: no tier, no fixed amount.
	for _, name := range []string{"README.md", "SUPPORT.md", "docs/identity.md"} {
		if m := regexp.MustCompile(`\$\d`).FindString(readRoot(t, root, name)); m != "" {
			t.Errorf("%s names a price (%s); support is any amount", name, m)
		}
	}
}

// link is a markdown link or image target, or an HTML src or href.
var link = regexp.MustCompile(`\]\(([^)\s]+)\)|(?:src|href)="([^"]+)"`)

func TestIdentityFilesLinkOnlyToFilesThatExist(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{"README.md", "SUPPORT.md", "docs/identity.md"} {
		text := readRoot(t, root, name)
		// Code blocks quote snippets other repos copy; they are not links here.
		text = regexp.MustCompile("(?s)```.*?```").ReplaceAllString(text, "")
		for _, m := range link.FindAllStringSubmatch(text, -1) {
			target := m[1] + m[2]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			target, _, _ = strings.Cut(target, "#")
			if _, err := os.Stat(filepath.Join(root, filepath.Dir(name), target)); err != nil {
				t.Errorf("%s links %s, which does not exist", name, m[0])
			}
		}
	}
}
