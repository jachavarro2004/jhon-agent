// Command jhon is Jhon's engine, written in Go.
//
// Every command and every hook runs in this one binary (internal/cli), and
// the whole suite runs against it.
package main

import (
	"fmt"
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/cli"
)

func main() {
	code, err := cli.Run(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "jhon: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
