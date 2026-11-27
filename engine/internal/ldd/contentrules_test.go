package ldd

import (
	"strings"
	"testing"
)

// T268: the content rules answer as their regexps do, on any text; the
// keyword test before each one only spares it the texts it cannot match.
func TestContentRulesAnswerAsTheirRegexps(t *testing.T) {
	texts := []string{
		"", "plain words\n", "x := exec.Command(\"ls\")", "EXEC.COMMAND", "import subprocess", "child_process.spawn",
		"spawn(x)", "respawn(x)", "go func() {}", "go funcs", "sync.Mutex", "SYNC.waitgroup", "import asyncio", "threading",
		"Promise.all([a])", "retries", "retry", "retro", "backoff", "timeout", "timeouts", "ctx.WithTimeout", "WithDeadline",
		"ſubprocess", "bacKoff", "baK", "drop table users", "DROP TABLE x", "drop tables", "truncate t", "TRUNCATE TABLE t",
		"delete from x", "deleted from", "a drop of rain", strings.Repeat("noise ", 1000) + "retry",
		"\xc5", "\xc5\xbfync.Mutex", "\xe2\x84", "\xff\xfedrop table x", "DELETE\xffFROM t", "ſync.WaitGroup", "WITHDEADLINE", "TimeOut",
		"go\u00a0func", "drop\u00a0table t", "\xe2\x84\xaaafka backoff",
	}
	for _, s := range texts {
		if got, want := resilient(s), resilienceCode.MatchString(s); got != want {
			t.Errorf("resilient(%q) = %v, want the regexp's %v", s, got, want)
		}
		if got, want := destructiveIn(s), matchI(destructive, s); got != want {
			t.Errorf("destructiveIn(%q) = %v, want the regexp's %v", s, got, want)
		}
	}
}
