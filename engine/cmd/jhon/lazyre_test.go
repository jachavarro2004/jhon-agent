package main

import (
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// Every expression the binary declares compiles: compiled on first use, a
// broken pattern would otherwise fail only the command that reaches it.
func TestEveryDeclaredExpressionCompiles(t *testing.T) {
	if err := lazyre.CompileAll(); err != nil {
		t.Fatal(err)
	}
}
