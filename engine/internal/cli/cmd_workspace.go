package cli

import (
	"fmt"
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// `jhon workspace`: the owner's pointer from a sibling repository to its
// workspace's primary (T57 W2, L1299).
//
//	jhon workspace join <primary>   run in the sibling: record the pointer
//	jhon workspace leave            remove this repository's pointer
//	jhon workspace list [--json]    every pointer
//
// Joining and leaving are the owner's acts, which the safety guard refuses
// from an agent; listing is anyone's. The primary names its members in its
// LDD config (`workspace:`); a pointer it does not list is no member. Exit
// 0; 1 when this folder or the primary is no git repository, or is the
// primary itself; 2 on a usage error. Nothing is written but on success.
func init() { register("workspace", runWorkspace) }

const workspaceUsage = "usage: jhon workspace join <primary> | leave | list [--json]\n"

func runWorkspace(argv []string) (int, error) {
	sub := ""
	if len(argv) > 0 {
		sub = argv[0]
	}
	switch {
	case (sub == "--help" || sub == "-h") && len(argv) == 1:
		fmt.Print(workspaceUsage)
		return 0, nil
	case sub == "list" && (len(argv) == 1 || (len(argv) == 2 && argv[1] == "--json")):
		return workspaceList(len(argv) == 2)
	case (sub == "join" && len(argv) == 2) || (sub == "leave" && len(argv) == 1):
	default:
		fmt.Fprint(os.Stderr, workspaceUsage)
		return 2, nil
	}
	here := workspace.Main(cwd())
	if here == "" {
		return workspaceFail(project.Realish(cwd()) + " is no git repository")
	}
	if sub == "leave" {
		p, had, err := workspace.Leave(here)
		if err != nil {
			return 1, err
		}
		if !had {
			fmt.Printf("no workspace pointer for %s; nothing removed\n", here)
			return 0, nil
		}
		fmt.Printf("left the workspace of %s: %s\n", p.Primary, here)
		return 0, nil
	}
	arg := project.Resolve(cwd(), argv[1])
	primary := workspace.Main(arg)
	switch {
	case primary == "":
		return workspaceFail(project.Realish(arg) + " is no git repository")
	case primary == here:
		return workspaceFail("a repository is no member of its own workspace")
	}
	if err := workspace.Join(here, primary); err != nil {
		return 1, err
	}
	fmt.Printf("joined the workspace of %s: %s\n", primary, here)
	if workspace.Of(here).Role != workspace.Member {
		fmt.Fprintf(os.Stderr, "jhon workspace: %s's LDD config lists no path to %s; it is no member until it does\n", primary, here)
	}
	return 0, nil
}

func workspaceFail(msg string) (int, error) {
	fmt.Fprintln(os.Stderr, "jhon workspace: "+msg)
	return 1, nil
}

func workspaceList(asJSON bool) (int, error) {
	pointers := workspace.Pointers()
	if asJSON {
		out, err := js.Stringify(pointers)
		if err != nil {
			return 1, err
		}
		fmt.Println(out)
		return 0, nil
	}
	if len(pointers) == 0 {
		fmt.Println("no workspace pointers")
	}
	for _, p := range pointers {
		fmt.Printf("%s  -> %s  (joined %s)\n", p.Root, p.Primary, p.JoinedAt)
	}
	return 0, nil
}
