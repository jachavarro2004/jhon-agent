package guard

import "testing"

// lineFacts reads the parsed line (L791): quoting and spacing do not hide
// a command or an assignment.
func TestLineFactsPoisoned(t *testing.T) {
	for _, c := range []string{
		"L=$(mktemp); printf -vL /etc/x", `L=$(mktemp); "source" x`, `L=$(mktemp); "." x`, `L=$(mktemp); x=1 . ./s`,
		"L=$(mktemp); while . ./s; do :; done", "f() { :; }; L=$(mktemp)", "CDPATH=/; L=$(mktemp)", "L=$(mktemp); $X y",
		"L=$(mktemp); ((n=1))", "L=$(mktemp); echo ${n:=1}", "L=$(mktemp); wait -p n", `L=$(mktemp); 'ali'as m=x`,
		// L793: the allowlist (fourth round).
		"L=$(mktemp); {printf,-vL,x}", "BASH_CMDS[mktemp]=x; L=$(mktemp)", `L=$(mktemp); p\rintf x`, `L=$(mktemp); $'source' x`,
		"X=1; L=$(mktemp)", "x[0]=1; L=$(mktemp)", "x+=1; L=$(mktemp)", "L=$(mktemp); [[ -n x ]]", "L=$(mktemp); ((x=1))",
		"L=$(mktemp); printf x", "L=$(mktemp); pushd /", "L=$(mktemp); time true", "L=$(mktemp); {L}>/x", "x=1 X=2 true; L=$(mktemp)",
		"IFS=$(mktemp); L=$(mktemp)", "for X in a; do :; done; L=$(mktemp)", "L=$(mktemp); ./*.sh",
		// L796: every arithmetic context evaluates a variable's value.
		`y=P; z="${y}ATH=0"; : $((z)); L=$(mktemp)`, "z=x; L=$(mktemp); : $[z]", "L=$(mktemp); echo ${x:z}",
		"L=$(mktemp); echo ${x:1:z}", "L=$(mktemp); echo ${x: -1}", "L=$(mktemp); echo ${a[z]}", "L=$(mktemp); echo ${a[$z]}",
		`L=$(mktemp); echo "${a[0+z]}"`, "L=$(mktemp); echo ${#a[z]}", "y=a; L=$(mktemp); echo ${!y}", "L=$(mktemp); echo ${x@P}",
		"L=$(mktemp); [ -v 'a[TMPDIR=0]' ]", "L=$(mktemp); test -v x", `L=$(mktemp); [ -v "a[z]" ]`, "L=$(mktemp); [ $o 'a[z]' ]",
		"o=-v; s=a; L=$(mktemp); [ $o $s ]", `L=$(mktemp); test "$o" "$s"`, `L=$(mktemp); [ x = "a[$z]" ]`,
		"L=$(mktemp); for ((i=0;i<1;i++)); do :; done", "L=$(mktemp); ((z))", "L=$(mktemp); let z", "L=$(mktemp)/x",
	} {
		if f := lineFacts(c); f != nil {
			t.Errorf("%s: not poisoned: %+v", c, f)
		}
	}
	for c, want := range map[string]bool{
		"d=$(mktemp -d) && d[0]=/x && rm -r $d": false, "d=$(mktemp -d) && rm -r $d": true, "d=$(mktemp -d); d+=x": false,
		"for d in /x; do :; done; d=$(mktemp -d)": false, "d=$(env mktemp -d)": false, "d=$(mktemp -d 2>/dev/null)": false,
		"d=$(mktemp -d; true)": false, `d="$(mktemp -d)"`: true, "cd /; d=$(mktemp -d -p .)": false, "cd .; d=$(mktemp -d)": true,
		`d=$(mktemp -d "$TMPDIR/x.XXXX")`: true, `d=$(mktemp -d "$HOME/x.XXXX")`: false, "d=$(PATH=/x mktemp -d)": false,
		"d=$(mktemp -d) true": false, "x=1; d=$(mktemp -d); echo $x; [ -d $d ]; test -n x; pwd; : ; false": true, "d=$(mktemp -d); d2=x": true,
		"d=$(mktemp -d); x=1 d=2 true": false,
		"cd /etc && d=$(mktemp -d) && y=D && z=\"${y,}=0\" && : $((z)) && rm -r $d": false, "d=$(mktemp -d); echo ${a[0]} ${a[@]} ${b[*]}": true,
		`d=$(mktemp -d); [ -d "$d" ] && test -n x`: true, "d=$(mktemp -d)/x": false, "d=$(mktemp -d); x=1 python3 a | tail -1 && cat <(echo $d)": true,
	} {
		if got := lineFacts(c).resolves("d"); got != want {
			t.Errorf("%s: resolves %v, want %v", c, got, want)
		}
	}
}

// L830: zsh's lowercase special names (path is PATH there) are not
// assignable on a proven line, as a statement, a prefix, a for variable or
// the tracked mktemp one.
func TestLineFactsZshSpecialNames(t *testing.T) {
	for _, n := range []string{"path", "cdpath", "fpath", "manpath", "module_path", "mailpath", "psvar"} {
		for _, c := range []string{
			n + "=/x; L=$(mktemp)", n + "=/x true; L=$(mktemp)", "L=$(mktemp); " + n + "=$L/e",
			"for " + n + " in /x; do :; done; L=$(mktemp)", n + "=$(mktemp -d)", n + "=; L=$(mktemp)",
		} {
			if f := lineFacts(c); f != nil {
				t.Errorf("%s: not poisoned: %+v", c, f)
			}
		}
	}
	// Names that only resemble them stay assignable.
	for _, c := range []string{"paths=/x; L=$(mktemp)", "my_path=/x; L=$(mktemp)", "p=/x; L=$(mktemp)", "for f in /x; do :; done; L=$(mktemp)"} {
		if f := lineFacts(c); f == nil || !f.resolves("L") {
			t.Errorf("%s: poisoned", c)
		}
	}
}
