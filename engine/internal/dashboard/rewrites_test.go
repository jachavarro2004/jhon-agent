package dashboard

import (
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The report names a change's planning rewrites after its first verify
// post_verify_rewrites (S12); the view reads it, and a report kept with the
// old name, replans, the same.
func TestViewReadsPostVerifyRewrites(t *testing.T) {
	r := report(t).(*js.Object)
	for _, c := range js.Prop(js.Prop(r, "outcomes"), "changes").([]any) {
		o := c.(*js.Object)
		if n := js.Prop(o, "replans"); js.Truthy(n) {
			o.Delete("replans")
			o.Set("post_verify_rewrites", n)
		}
	}
	v := View(r)
	eq(t, at(v, "changes", 0, "attention"),
		`["verify failed 2 time(s)","1 agent(s) stopped short","1 relaunch(es)","the last test record failed","1 test timeout(s)"]`, "beta's attention")
	eq(t, at(v, "changes", 0, "replans"), `1`, "beta's rewrites")
}

// A change with no planning rewrites after its first verify carries
// post_verify_rewrites 0: the view keeps that 0, as it kept replans 0,
// and never reads it as missing.
func TestViewKeepsZeroPostVerifyRewrites(t *testing.T) {
	r := report(t).(*js.Object)
	for _, c := range js.Prop(js.Prop(r, "outcomes"), "changes").([]any) {
		o := c.(*js.Object)
		o.Delete("replans")
		o.Set("post_verify_rewrites", 0.0)
	}
	v := View(r)
	eq(t, at(v, "changes", 0, "replans"), `0`, "a zero count stays 0")
}

// Post-verify rewrites are information, never a cost (S26): planning as
// the change goes rewrites the plan on what exists, so a change whose only
// signal is its rewrites needs no one's look, and the view still shows the
// count.
func TestPostVerifyRewritesNeverNeedAttention(t *testing.T) {
	r := report(t).(*js.Object)
	for _, c := range js.Prop(js.Prop(r, "outcomes"), "changes").([]any) {
		o := c.(*js.Object)
		o.Delete("replans")
		o.Set("post_verify_rewrites", 3.0)
	}
	v := View(r)
	alpha := js.Prop(v, "changes").([]any)[1]
	eq(t, at(alpha, "name"), `"alpha"`, "alpha, with nothing else to see, comes last")
	eq(t, at(alpha, "attention"), `[]`, "rewrites alone need no look")
	eq(t, at(alpha, "replans"), `3`, "the count is still shown")
}
