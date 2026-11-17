package cli

import (
	"fmt"
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
)

// `jhon release pack [--version <v>] [--out <dir>]` and
// `jhon release install <artifact.tar.gz>` (T3, internal/release): the two
// halves `jhon sync` composes. pack builds the artifact of the committed
// HEAD (the release CI's step); install verifies one and activates it, with
// no checkout and no toolchain. Exit 2 on a usage error.
func init() { register("release", runRelease) }

const releaseUsage = "usage: jhon release pack [--version <v>] [--out <dir>]\n       jhon release install <artifact.tar.gz>\n"

func runRelease(argv []string) (int, error) {
	usage := func(msg string) (int, error) {
		fmt.Fprintf(os.Stderr, "jhon release: %s\n%s", msg, releaseUsage)
		return 2, nil
	}
	if len(argv) == 0 {
		return usage("pack or install")
	}
	switch argv[0] {
	case "pack":
		flags := map[string]string{"--version": "", "--out": ""}
		for i := 1; i < len(argv); i++ {
			a := argv[i]
			if _, ok := flags[a]; !ok {
				return usage("unexpected argument: " + a)
			}
			if i+1 >= len(argv) || argv[i+1] == "" {
				return usage(a + " needs a value")
			}
			flags[a], i = argv[i+1], i+1
		}
		if v := flags["--version"]; v != "" && !release.VersionPattern.MatchString(v) {
			return usage("--version is letters, digits, '.', '+', '_' and '-': " + v)
		}
		return release.Pack(release.Locate(), out(), flags["--version"], flags["--out"]), nil
	case "install":
		if len(argv) != 2 {
			return usage("install takes one artifact")
		}
		// As sync: migrate the old settings first, or the install would
		// read telemetry as off.
		autoMigrate(settings.Path())
		return release.InstallArtifact(release.Locate(), out(), argv[1]), nil
	}
	return usage("unknown subcommand: " + argv[0])
}
