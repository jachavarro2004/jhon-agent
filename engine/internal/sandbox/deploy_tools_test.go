package sandbox

import (
	"slices"
	"testing"
)

// sandbox.deploy_tools (odd/tasks/sandbox-deploy-tools.md S2, S3, S5): a
// program the owner lists runs outside the sandbox under docker's
// conditions: alone on its line or with other programs that do, no pipe,
// no redirect to a file, nothing the shell would still rewrite, and a
// leading cd only into the project.
func TestADeployToolLineGetsDockersProfile(t *testing.T) {
	tools := []string{"aws", "cdk", "sam"}
	d := with(machine, disk{"/home/u/project/infra": "dir", "/home/u/other": "dir"})
	roots := []string{"/home/u/project"}
	for _, c := range []struct{ line, want string }{
		// S3, the owner's examples.
		{"aws s3 sync dist s3://x", Unsandboxed},
		{"cdk deploy", Unsandboxed},
		{"aws s3 ls | grep x", Work},
		{"terraform apply", Work},
		// S5: where docker's line loses unsandboxed, a listed tool's does.
		{"aws s3 ls && make", Work},
		{"aws s3 ls > out.txt", Work},
		{"aws s3 cp $FILE s3://x", Work},
		{"aws s3 ls `pwd`", Work},
		{"aws s3 ls; make", Work},
		{"aws s3 ls; cdk deploy", Unsandboxed}, // as docker ps; docker images
		{"cd /home/u/other && cdk deploy", Work},
		{"cd /home/u/project/infra && cdk deploy", Unsandboxed},
		{"cd infra && sam deploy 2>&1", Unsandboxed},
		{"cdk deploy && docker compose up -d", Unsandboxed},
		{"cdk", Work},
		{"./aws s3 ls", Work},
		{"FOO=1 aws s3 ls", Work},
	} {
		if got := ProfileAt(c.line, "/home/u/project", roots, d.stat, tools); got != c.want {
			t.Errorf("ProfileAt(%q) with %v = %q, want %q", c.line, tools, got, c.want)
		}
	}
	// Listing git, gh or docker widens nothing: their own rules still apply.
	for _, line := range []string{"git status", "gh auth token", "docker login"} {
		if got := ProfileAt(line, "/home/u/project", roots, d.stat, []string{"git", "gh", "docker"}); got != Work {
			t.Errorf("ProfileAt(%q) with git, gh and docker listed = %q, want work", line, got)
		}
	}
}

// S4: with sandbox.deploy_tools unset every line keeps today's profile.
func TestNoDeployToolsKeepsTodaysProfiles(t *testing.T) {
	for line, want := range map[string]string{
		"aws sts get-caller-identity": Work,
		"aws s3 sync dist s3://x":     Work,
		"cdk deploy":                  Work,
		"sam deploy":                  Work,
		"terraform apply":             Work,
		"docker compose up -d":        Unsandboxed,
	} {
		if got := ProfileAt(line, "/home/u/project", []string{"/home/u/project"}, machine.stat, nil); got != want {
			t.Errorf("ProfileAt(%q) with no deploy tools = %q, want %q", line, got, want)
		}
	}
}

// S5: a listed tool runs unsandboxed only when the one the shell finds
// first on PATH is not a file a sandboxed command could have written, as
// docker's.
func TestRunsOutsideRefusesAPlantedDeployTool(t *testing.T) {
	d := with(machine, disk{
		"/usr/bin": "dir", "/usr/bin/bash": "file", "/usr/bin/aws": "file", "/usr/bin/cdk": "file",
		"/home/u/project/bin": "dir", "/home/u/project/bin/aws": "file",
	})
	h := host("linux", d, nil)
	for _, c := range []struct {
		name  string
		tools []string
		path  []string
		line  string
		want  bool
	}{
		{"a listed tool", []string{"aws", "cdk"}, []string{"/usr/bin"}, "aws s3 sync dist s3://x", true},
		{"a planted listed tool", []string{"aws", "cdk"}, []string{"/home/u/project/bin", "/usr/bin"}, "aws s3 ls", false},
		{"a planted listed tool sends another listed one to work", []string{"aws", "cdk"}, []string{"/home/u/project/bin", "/usr/bin"}, "cdk deploy", false},
		{"a planted listed tool after the real one", []string{"aws", "cdk"}, []string{"/usr/bin", "/home/u/project/bin"}, "aws s3 ls", true},
		{"a tool not listed", nil, []string{"/usr/bin"}, "aws s3 ls", false},
	} {
		in := sample(d)
		in.Path = slices.Clone(c.path)
		in.DeployTools = c.tools
		if got := RunsOutside(h, in, []string{"/usr/bin/bash", "-c", c.line}); got != c.want {
			t.Errorf("%s: RunsOutside(%q) = %v, want %v", c.name, c.line, got, c.want)
		}
	}
}
