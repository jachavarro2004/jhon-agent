package sandbox

import (
	"strings"
	"testing"
)

// S5 "como docker": every line shape gets the same profile with a listed
// tool as with docker in its place (odd/tasks/sandbox-deploy-tools.md, T2).
func TestADeployToolLineMatchesDockersShape(t *testing.T) {
	d := with(machine, disk{"/home/u/project/sub": "dir"})
	roots := []string{"/home/u/project"}
	tools := []string{"aws"}
	for _, line := range []string{
		"aws s3 ls | grep x", "aws s3 ls && make", "aws s3 ls; make", "aws s3 ls; aws s3 cp a s3://b",
		"aws s3 ls | aws s3 ls", "aws s3 ls > out", "aws s3 ls >> out", "aws s3 ls 2> e", "aws s3 ls >| out",
		"aws s3 ls < in", "aws s3 ls <<< x", "aws s3 ls > /dev/null 2>&1", "aws s3 ls 1>&2", "aws s3 ls $V",
		`aws s3 ls "$V"`, "aws s3 ls `id`", "aws s3 ls $(id)", "aws s3 ls *", "aws s3 ls ?", "aws s3 ls {a,b}",
		"aws s3 ls '*'", "aws s3 ls ~/x", "env aws s3 ls", "command aws s3 ls", "exec aws s3 ls", "nohup aws s3 ls",
		"time aws s3 ls", "eval aws s3 ls", `bash -c "aws s3 ls"`, "xargs aws < f", "X=1 aws s3 ls", "X=1; aws s3 ls",
		"cd .. && aws s3 ls", "cd ~ && aws s3 ls", "cd / && aws s3 ls", "cd sub; aws s3 ls", "cd sub && aws s3 ls",
		"cd /home/u/project/sub && aws s3 ls", "./aws s3 ls", "/usr/bin/aws s3 ls", "sub/aws s3 ls", `\aws s3 ls`,
		`"aws" s3 ls`, "aws", "aws s3 ls &", "(aws s3 ls)", "{ aws s3 ls; }", "! aws s3 ls", "f(){ aws; }; f",
		"alias x=aws", "if true; then aws s3 ls; fi",
	} {
		docker := strings.ReplaceAll(line, "aws", "docker")
		want := ProfileAt(docker, "/home/u/project", roots, d.stat, nil)
		if got := ProfileAt(line, "/home/u/project", roots, d.stat, tools); got != want {
			t.Errorf("ProfileAt(%q) with aws listed = %q, docker's %q = %q", line, got, docker, want)
		}
	}
}
