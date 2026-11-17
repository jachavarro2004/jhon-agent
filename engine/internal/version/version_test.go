package version

import "testing"

func TestString(t *testing.T) {
	defer func(v, c, d string) { Version, Commit, Date = v, c, d }(Version, Commit, Date)
	for _, c := range []struct{ v, c, d, want string }{
		{"0.0.0-dev", "", "", "0.0.0-dev"},
		{"0.1.0", "0123456789abcdef", "2026-11-06T23:40:39-05:00", "0.1.0 (0123456789ab, 2026-11-06T23:40:39-05:00)"},
		{"0.1.0", "abc", "", "0.1.0 (abc)"},
	} {
		Version, Commit, Date = c.v, c.c, c.d
		if got := String(); got != c.want {
			t.Errorf("String() = %q, want %q", got, c.want)
		}
	}
}

func TestLDFlags(t *testing.T) {
	want := "-X " + Package + ".Version=0.1.0 -X " + Package + ".Commit=c -X " + Package + ".Date=d"
	if got := LDFlags("0.1.0", "c", "d"); got != want {
		t.Errorf("LDFlags = %q, want %q", got, want)
	}
}
