package sandbox

import (
	"strings"
	"testing"
)

// L39 (round-3 verify, blocker): with sandbox.docker off no docker or
// podman socket is reachable inside, wherever the engine keeps it.
func TestNoContainerSocketAnywhereWithTheSettingOff(t *testing.T) {
	socks := []string{"/run/docker.sock", "/var/run/docker.sock", "/run/podman/podman.sock", "/home/u/.docker/desktop/docker.sock", "/home/u/.docker/run/docker.sock"}
	d := with(machine, disk{"/run/docker.sock": "file", "/var/run/docker.sock": "file", "/run/podman": "dir", "/run/podman/podman.sock": "file",
		"/home/u/.docker": "dir", "/home/u/.docker/desktop": "dir", "/home/u/.docker/desktop/docker.sock": "file", "/home/u/.docker/run": "dir", "/home/u/.docker/run/docker.sock": "file"})
	in := sample(d)
	in.Env["DOCKER_HOST"] = "unix:///home/u/.docker/desktop/docker.sock"
	args := Args(in)
	for _, p := range socks {
		hidden := false
		for i := 0; i+1 < len(args); i++ {
			covers := args[i+1] == p || strings.HasPrefix(p, args[i+1]+"/")
			if args[i] == "--tmpfs" && covers {
				hidden = true
			}
			if args[i] == "--dev-bind" && args[i+1] == "/dev/null" && i+2 < len(args) && (args[i+2] == p || strings.HasPrefix(p, args[i+2]+"/")) {
				hidden = true
			}
		}
		if !hidden {
			t.Errorf("%s stays reachable with sandbox.docker off:\n%s", p, strings.Join(args, " "))
		}
	}
}

// The same with the socket named only by DOCKER_HOST, CONTAINER_HOST or a
// docker context (Input.ContainerHosts) anywhere, and none of them hidden
// with sandbox.docker on.
func TestContainerSocketsNamedElsewhereFollowTheSetting(t *testing.T) {
	d := with(machine, disk{"/srv/a": "dir", "/srv/a/docker.sock": "file", "/srv/b": "dir", "/srv/b/podman.sock": "file", "/srv/c": "dir", "/srv/c/ctx.sock": "file",
		"/run/docker.sock": "file", "/home/u/.docker": "dir", "/home/u/.docker/run": "dir"})
	in := sample(d)
	in.Env["DOCKER_HOST"] = "unix:///srv/a/docker.sock"
	in.Env["CONTAINER_HOST"] = "unix:///srv/b/podman.sock"
	in.ContainerHosts = []string{"unix:///srv/c/ctx.sock", "tcp://127.0.0.1:2375", "ssh://host"}
	got := strings.Join(Args(in), " ")
	for _, p := range []string{"/srv/a/docker.sock", "/srv/b/podman.sock", "/srv/c/ctx.sock", "/run/docker.sock"} {
		if !strings.Contains(got, "--dev-bind /dev/null "+p) {
			t.Errorf("%s is not hidden with sandbox.docker off:\n%s", p, got)
		}
	}
	if !strings.Contains(got, "--tmpfs /home/u/.docker/run") {
		t.Errorf("~/.docker/run is not hidden:\n%s", got)
	}
	in.Docker = true
	got = strings.Join(Args(in), " ")
	for _, p := range []string{"/srv/a/docker.sock", "/srv/b/podman.sock", "/srv/c/ctx.sock", "/run/docker.sock", "/home/u/.docker/run"} {
		if strings.Contains(got, p) {
			t.Errorf("%s is hidden with sandbox.docker on:\n%s", p, got)
		}
	}
}
