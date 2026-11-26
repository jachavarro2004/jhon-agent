package sandbox

// What a line outside the sandbox reads besides the repository's git
// configuration and git folders (T250, odd/tasks/studio-integration-v1.md
// L43): docker's configuration folder, from which it loads CLI plugins
// (DockerConfigProblem), and the .gitattributes a pull reads in the work
// tree, where a pipe makes git wait forever (WorkTreeProblem). Both are
// read once the line is in the registry, where no sandboxed command that
// can write them runs.

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// dockerConfigLimit bounds the config.json DockerConfigProblem reads.
const dockerConfigLimit = 1 << 20

// DockerConfigProblem is why line, run outside the sandbox in in.Cwd, may
// not run for docker's configuration folder, nil when it may (T250, L43d):
// docker and docker-compose load CLI plugins (docker-compose, docker-buildx,
// any docker-<name>) from <config>/cli-plugins and from each folder the
// cliPluginsExtraDirs of <config>/config.json lists, where <config> is what
// docker's --config names (each one given), else $DOCKER_CONFIG, else
// ~/.docker. A sandboxed command that can change the folder, its
// cli-plugins, its config.json or an extra folder (mayChange) could put a
// program there that docker runs outside the sandbox. A relative folder is
// taken from where the line works (its leading cd's folder, or in.Cwd).
// config.json is read only as a regular file, without blocking, at most
// dockerConfigLimit bytes; one that cannot be read lists no folder.
func (in Input) DockerConfigProblem(line string) *Problem {
	programs, err := sh.JudgeString(line, nil, nil, sh.Opts{})
	if err != nil {
		return &Problem{Reason: "the line cannot be read for the folders docker loads its plugins from", Fix: terminalFix}
	}
	cwd := in.Cwd
	if dir, ok := leadingCd(line); ok {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
		cwd = dir
	}
	from := func(p string) string {
		if filepath.IsAbs(p) {
			return filepath.Clean(p)
		}
		return filepath.Join(cwd, p)
	}
	var configs []string
	for _, p := range programs {
		if len(p.Argv) == 0 || p.Argv[0] != "docker" && p.Argv[0] != "docker-compose" {
			continue
		}
		var given []string
		if p.Argv[0] == "docker" {
			args := p.Argv[1:]
			for k := 0; k < len(args) && strings.HasPrefix(args[k], "-"); k++ {
				name, value, attached := strings.Cut(args[k], "=")
				if !slices.Contains(dockerValues, name) {
					continue
				}
				if !attached {
					if k++; k >= len(args) {
						break
					}
					value = args[k]
				}
				if name == "--config" {
					given = append(given, value)
				}
			}
		}
		if len(given) == 0 {
			switch v, set := in.Env["DOCKER_CONFIG"]; {
			case set && v != "":
				given = []string{v}
			default:
				given = []string{filepath.Join(in.Home, ".docker")}
			}
		}
		for _, g := range given {
			if c := from(g); !slices.Contains(configs, c) {
				configs = append(configs, c)
			}
		}
	}
	for _, c := range configs {
		folders := []string{c, filepath.Join(c, "cli-plugins"), filepath.Join(c, "config.json")}
		for _, extra := range dockerExtraDirs(filepath.Join(c, "config.json")) {
			folders = append(folders, from(extra))
		}
		for _, f := range folders {
			if mayChange(in, f) {
				return &Problem{Reason: "docker loads CLI plugins from " + f + ", which a sandboxed command may change, so a plugin it put there would run outside the sandbox", Fix: "name a docker configuration folder (--config, DOCKER_CONFIG) and plugin folders outside the project, /tmp, $TMPDIR, the caches and sandbox.allow_write, such as the default ~/.docker, or run the line yourself in a terminal"}
			}
		}
	}
	return nil
}

// dockerExtraDirs are the cliPluginsExtraDirs of docker's config.json at
// file: read only as a regular file, without blocking (a pipe there would
// make this wait), at most dockerConfigLimit bytes; none when it cannot be
// read.
func dockerExtraDirs(file string) []string {
	if fi, err := os.Lstat(file); err != nil || !fi.Mode().IsRegular() {
		return nil
	}
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return nil
	}
	var config struct {
		CLIPluginsExtraDirs []string `json:"cliPluginsExtraDirs"`
	}
	if json.NewDecoder(io.LimitReader(f, dockerConfigLimit)).Decode(&config) != nil {
		return nil
	}
	return config.CLIPluginsExtraDirs
}

// PullFolders are the folders each git pull of line, started in cwd, works
// in (after its leading cd and its -C folders), each once: the ones whose
// work tree a pull checks files out in (WorkTreeProblem). nil for a line
// the reader cannot read, or one with no pull.
func PullFolders(line, cwd string) []string {
	programs, err := sh.JudgeString(line, nil, nil, sh.Opts{})
	if err != nil || !filepath.IsAbs(cwd) {
		return nil
	}
	if dir, ok := leadingCd(line); ok {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
		cwd = dir
	}
	var out []string
	for _, p := range programs {
		if len(p.Argv) == 0 || p.Argv[0] != "git" {
			continue
		}
		dir, k := gitFolder(p.Argv[1:], cwd)
		if k+1 < len(p.Argv) && p.Argv[1+k] == "pull" && !slices.Contains(out, filepath.Clean(dir)) {
			out = append(out, filepath.Clean(dir))
		}
	}
	return out
}

// WorkTreeProblem is why a git pull may not run outside the sandbox in the
// work tree top (absolute), nil when it may (T250, L43a): a .gitattributes
// in any folder of it that is not a regular file (a pipe, a socket, a
// device, a link) is one git opens, when it checks out a file in that
// folder, and a pipe there makes it wait forever, the line's record in the
// registry with it. The walk follows no link, skips the git folders (`.git`
// entries, which GitFolderProblem walks) and stops at maxGitFolderEntries
// entries: past it the line runs in the sandbox, as for the git folders.
func WorkTreeProblem(top string) *Problem {
	if !filepath.IsAbs(top) {
		return &Problem{Reason: "git names no absolute work tree (" + top + ")", Fix: terminalFix}
	}
	var problem *Problem
	walked := 0
	err := filepath.WalkDir(filepath.Clean(top), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if walked++; walked > maxGitFolderEntries {
			problem = &Problem{Reason: "the work tree " + top + " holds more than " + strconv.Itoa(maxGitFolderEntries) + " entries, too many to check for a .gitattributes git would wait on", Fix: terminalFix}
			return fs.SkipAll
		}
		switch d.Name() {
		case ".git":
			if d.IsDir() {
				return fs.SkipDir
			}
		case ".gitattributes":
			if !d.Type().IsRegular() {
				problem = &Problem{Reason: p + " is not a regular file, and git would wait on it when it checks out a file in its folder", Fix: plantedFix}
				return fs.SkipAll
			}
		}
		return nil
	})
	if problem != nil {
		return problem
	}
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return &Problem{Reason: "the work tree " + top + " cannot be read (" + err.Error() + ")", Fix: terminalFix}
	}
	return nil
}
