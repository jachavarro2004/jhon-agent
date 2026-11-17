// The Strict TDD resolution is unit-tested in internal/ldd. What stays here is
// the contract data it reads its vocabulary from.
package ldd

import (
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestStrictTDDVocabulary(t *testing.T) {
	t.Parallel()
	vocab := support.Read(t, support.Path("prose", "contracts", "vocabulary.yaml"))
	match(t, vocab, `(?m)^strict_tdd_source: \[policy, derived, legacy, tdd\.md\]$`)
	match(t, vocab, `(?m)^strict_tdd_policy: \[require, forbid\]$`)
}
