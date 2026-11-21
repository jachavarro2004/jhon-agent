package settings

import (
	"errors"
	"strings"
	"testing"
)

// sandbox.deploy_tools (odd/tasks/sandbox-deploy-tools.md S2, S6): a list
// of bare program names, empty by default; the file's list is read as
// written and an entry with a slash or whitespace is refused.
func TestDeployToolsIsAListOfProgramNames(t *testing.T) {
	vals, err := Load(write(t, ""), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "sandbox.deploy_tools"); !same(v.Value, []string{}) || v.Source != SourceDefault || v.Kind != "names" {
		t.Errorf("sandbox.deploy_tools = %#v (%s, %s), want an empty list of names by default", v.Value, v.Source, v.Kind)
	}
	vals, err = Load(write(t, "[sandbox]\ndeploy_tools = [\"aws\", \"cdk\"]\n"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "sandbox.deploy_tools"); !same(v.Value, []string{"aws", "cdk"}) || v.Source != SourceFile {
		t.Errorf("sandbox.deploy_tools = %#v (%s), want the file's list", v.Value, v.Source)
	}
	t.Setenv("JHON_SETTINGS", write(t, "[sandbox]\ndeploy_tools = [\"aws\", \"sam\"]\n"))
	if got := Names("sandbox.deploy_tools"); !same(got, []string{"aws", "sam"}) {
		t.Errorf("Names = %#v, want the file's list", got)
	}
	t.Setenv("JHON_SETTINGS", write(t, "[sandbox]\ndeploy_tools = [\"./aws\"]\n"))
	if got := Names("sandbox.deploy_tools"); got != nil {
		t.Errorf("Names of a file that cannot be read = %#v, want none", got)
	}
	for _, c := range []struct{ text, bad string }{
		{"[sandbox]\ndeploy_tools = \"aws\"\n", `sandbox.deploy_tools: "aws" is not accepted`},
		{"[sandbox]\ndeploy_tools = [\"/usr/bin/aws\"]\n", `sandbox.deploy_tools: "/usr/bin/aws" is not accepted`},
		{"[sandbox]\ndeploy_tools = [\"./aws\"]\n", `sandbox.deploy_tools: "./aws" is not accepted`},
		{"[sandbox]\ndeploy_tools = [\"a b\"]\n", `sandbox.deploy_tools: "a b" is not accepted`},
		{"[sandbox]\ndeploy_tools = [\"\"]\n", `sandbox.deploy_tools: "" is not accepted`},
		{"[sandbox]\ndeploy_tools = [1]\n", `sandbox.deploy_tools: 1 is not accepted`},
	} {
		if _, err := Load(write(t, c.text), noEnv); err == nil || !strings.Contains(err.Error(), c.bad) {
			t.Errorf("%q: %v, want an error holding %q", c.text, err, c.bad)
		}
	}
}

// S6: set takes the names separated by commas, "" empties the list, and a
// refused entry writes nothing.
func TestSetDeployTools(t *testing.T) {
	path := write(t, "schema = 1\n")
	v, err := Set(path, "sandbox.deploy_tools", "aws, cdk")
	if err != nil || !same(v.Value, []string{"aws", "cdk"}) {
		t.Fatalf("Set = %#v, %v", v.Value, err)
	}
	want := "schema = 1\n\n[sandbox]\ndeploy_tools = [\"aws\", \"cdk\"]\n"
	if got := read(t, path); got != want {
		t.Errorf("the file holds\n%s\nwant\n%s", got, want)
	}
	for _, raw := range []string{"./aws", "/usr/bin/aws", "a b", "aws,,cdk", "aws,"} {
		var inv *InvalidError
		if _, err := Set(path, "sandbox.deploy_tools", raw); !errors.As(err, &inv) {
			t.Errorf("Set(%q) = %v, want it refused", raw, err)
		}
	}
	if got := read(t, path); got != want {
		t.Errorf("a refused value changed the file:\n%s", got)
	}
	if v, err := Set(path, "sandbox.deploy_tools", ""); err != nil || !same(v.Value, []string{}) {
		t.Errorf("Set(\"\") = %#v, %v; want the empty list", v.Value, err)
	}
	if got, want := read(t, path), "schema = 1\n\n[sandbox]\ndeploy_tools = []\n"; got != want {
		t.Errorf("after the reset the file holds\n%s\nwant\n%s", got, want)
	}
}
