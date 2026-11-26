package sh

import "strings"

// A wrap is what a wrapper's words (Wrappers) give: the words of the
// program it runs, the string env -S splits into them instead, the folder
// env -C or sudo -D moves it to, and env's NAME=value words.
type wrap struct {
	rest    []string
	split   *string
	chdir   *string
	assigns []string
}

// wrapped reads a wrapper's options as getopt does (T228 L5): a long option
// takes its value after `=` or as the next word; a short one in a cluster
// (`-la x`, `-oL`, `-vS'…'`) takes the rest of the cluster, else the next
// word; `--` ends the options. Each option's count of values is in
// Wrappers; one it does not name takes none.
func wrapped(name string, w []string) wrap {
	values := Wrappers[name]
	var out wrap
	k := 1
	set := func(opt, v string) {
		switch {
		case name == "env" && (opt == "-S" || opt == "--split-string"):
			out.split = str(v)
		case name == "env" && (opt == "-C" || opt == "--chdir"), name == "sudo" && (opt == "-D" || opt == "--chdir"):
			out.chdir = str(v)
		}
	}
	for k < len(w) {
		a := w[k]
		if name == "env" && Assignment.MatchString(a) {
			out.assigns = append(out.assigns, a)
			k++
			continue
		}
		if a == "--" {
			k++
			break
		}
		if len(a) < 2 || a[0] != '-' {
			break
		}
		if strings.HasPrefix(a, "--") {
			n, v, eq := strings.Cut(a, "=")
			if !eq && values[n] > 0 {
				v = at(w, k+1)
				k += values[n]
			}
			set(n, v)
			k++
			if out.split != nil {
				return out
			}
			continue
		}
		for i := 1; i < len(a); i++ {
			opt := "-" + a[i:i+1]
			if values[opt] == 0 {
				continue
			}
			v := a[i+1:]
			if v == "" {
				v = at(w, k+1)
				k += values[opt]
			}
			set(opt, v)
			break
		}
		k++
		if out.split != nil {
			return out
		}
	}
	if name == "timeout" && k < len(w) {
		k++ // the duration
	}
	if k < len(w) {
		out.rest = w[k:]
	}
	return out
}

// XargsCommand is the command xargs runs, its words without the operands
// xargs reads; nil when xargs runs none of its own (echo).
func XargsCommand(w []string) []string {
	k := 1
	for k < len(w) && strings.HasPrefix(w[k], "-") {
		a := w[k]
		// A cluster of bare letters ending in d takes the delimiter as the next word (L491).
		if i := strings.IndexByte(a, 'd'); len(a) > 2 && a[1] != '-' && i == len(a)-1 && strings.Trim(a[1:i], "0rtpx") == "" {
			k++
		}
		if xargsValues[a] {
			k += 2
		} else {
			k++
		}
	}
	if k >= len(w) {
		return nil
	}
	return w[k:]
}
