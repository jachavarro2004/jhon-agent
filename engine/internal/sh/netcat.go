package sh

import "strings"

// socat and the netcat family run commands and write files their own
// grammar names (T228 L14, L15): socat glues a command or a file to an
// address, and nc, ncat and netcat take a command or a dump file as an
// option's value. What each runs is read as a string the shell runs, as any
// runner's; a file each writes is read as tee writing it, so it is judged as
// that write is.

// socatCommandAddresses are socat's address types whose parameter is a
// command it runs: EXEC (the words), SYSTEM and SHELL (a string the shell
// runs). An option of the same name is read the same way.
var socatCommandAddresses = set("exec", "system", "shell")

// socatWriteOptions are the socat options that make an OPEN or GOPEN
// address (or a bare path, which is GOPEN) create, truncate or append to
// its file.
var socatWriteOptions = set("creat", "o-creat", "o_creat", "trunc", "o-trunc", "o_trunc", "append", "o-append", "o_append")

// socatValued are socat's options that take the next word as their value;
// socatFileOptions of them name a file socat writes (the log, the sniff
// dumps, the lock files).
var (
	socatValued      = set("-b", "-T", "-t", "-lp", "-lf", "-L", "-W", "-S", "-r", "-R")
	socatFileOptions = set("-lf", "-L", "-W", "-r", "-R")
)

// socatAddress is one socat address as socat reads it: its type (before
// the first `:` of its first field, case-folded), its parameter, and its
// options; quotes and backslashes removed.
type socatAddress struct {
	kind, param string
	options     []string
}

// socatRuns is what socat runs and writes (T228 L14, L15): the command of
// each EXEC:, SYSTEM: or SHELL: address (either side of a `!!` pair) and of
// any exec=, system= or shell= option, as a string the shell runs; and
// tee writing the file of each CREATE: address, of each OPEN: or GOPEN:
// address (or bare path) that creates, truncates or appends, and of the
// -lf, -L, -W, -r and -R options. With -u the first address is only read,
// with -U the second.
func socatRuns(w []string) Inner {
	var in Inner
	var addrs []string
	oneway := ""
	for k := 1; k < len(w); k++ {
		a := w[k]
		switch {
		case a == "-u" || a == "-U":
			oneway = a
		case socatFileOptions[a]:
			k++
			if f := at(w, k); f != "" {
				in.Words = append(in.Words, []string{"tee", "-a", f})
			}
		case strings.HasPrefix(a, "-lf") && len(a) > 3:
			in.Words = append(in.Words, []string{"tee", "-a", a[3:]})
		case socatValued[a]:
			k++
		case len(a) > 1 && a[0] == '-':
		default:
			addrs = append(addrs, a)
		}
	}
	for i, word := range addrs {
		readOnly := oneway == "-u" && i == 0 || oneway == "-U" && i == 1
		for _, ad := range socatAddresses(word) {
			if socatCommandAddresses[ad.kind] {
				in.Strings = append(in.Strings, ad.param)
			}
			write, appends := ad.kind == "create" || ad.kind == "creat", false
			for _, o := range ad.options {
				n, v, ok := strings.Cut(o, "=")
				n = strings.ToLower(n)
				if ok && socatCommandAddresses[n] {
					in.Strings = append(in.Strings, v)
				}
				if socatWriteOptions[n] && (ad.kind == "open" || ad.kind == "gopen") {
					write = true
					appends = appends || strings.Contains(n, "append")
				}
			}
			if write && !readOnly && ad.param != "" {
				words := []string{"tee", ad.param}
				if appends {
					words = []string{"tee", "-a", ad.param}
				}
				in.Words = append(in.Words, words)
			}
		}
	}
	return in
}

// socatAddresses reads a socat address word as socat does: `!!` joins two
// addresses and `,` separates an address's options, except inside quotes
// (single or double, which may nest) or a (), [] or {} group; a backslash
// takes the next character as it is; quotes and backslashes are removed,
// the groups' brackets kept. A first field with no `:` that holds a `/` is
// a path, which socat opens as GOPEN.
func socatAddresses(word string) []socatAddress {
	var out []socatAddress
	var fields []string
	var cur strings.Builder
	colon := -1
	var stack []byte
	closer := map[byte]byte{'(': ')', '[': ']', '{': '}'}
	end := func() {
		fields = append(fields, cur.String())
		cur.Reset()
	}
	flush := func() {
		end()
		ad := socatAddress{kind: strings.ToLower(fields[0]), options: fields[1:]}
		if colon >= 0 {
			ad.kind, ad.param = strings.ToLower(fields[0][:colon]), fields[0][colon+1:]
		} else if strings.Contains(fields[0], "/") {
			ad.kind, ad.param = "gopen", fields[0]
		}
		out = append(out, ad)
		fields, colon = nil, -1
	}
	top := func() byte {
		if len(stack) == 0 {
			return 0
		}
		return stack[len(stack)-1]
	}
	for i := 0; i < len(word); i++ {
		c := word[i]
		quoted := top() == '"' || top() == '\''
		switch {
		case c == '\\' && i+1 < len(word):
			i++
			cur.WriteByte(word[i])
		case c == '"' || c == '\'':
			if top() == c {
				stack = stack[:len(stack)-1]
			} else {
				stack = append(stack, c)
			}
		case quoted:
			cur.WriteByte(c)
		case closer[c] != 0:
			stack = append(stack, closer[c])
			cur.WriteByte(c)
		case len(stack) > 0 && c == top():
			stack = stack[:len(stack)-1]
			cur.WriteByte(c)
		case len(stack) > 0:
			cur.WriteByte(c)
		case c == '!' && i+1 < len(word) && word[i+1] == '!':
			i++
			flush()
		case c == ',':
			end()
		case c == ':' && len(fields) == 0 && colon < 0:
			colon = cur.Len()
			cur.WriteByte(c)
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

// ncatLong are ncat's long options the guard reads, by the name
// getopt_long matches a prefix of (`--sh-exe`, `--ex=`, `--lua`, `--out`).
var ncatLong = []string{"exec", "sh-exec", "lua-exec", "output", "hex-dump"}

// ncValued are the nc, ncat and netcat short options that take a value.
const ncValued = "ceiIOopPqsTVwxXgGmM"

// ncRuns is what nc, ncat or netcat runs and writes for a connection
// (T228 L14, L15): -e and ncat's --exec (a program and its words), -c and
// --sh-exec (a string the shell runs) as strings the shell runs;
// --lua-exec as lua running its script; and tee writing the dump file of
// -o (and ncat's --output), and of ncat's -x and --hex-dump (nc's -x is a
// proxy). A long option is read by any prefix of its name, as getopt_long
// takes a unique one.
func ncRuns(w []string) Inner {
	var in Inner
	ncat := Base(w[0]) == "ncat"
	take := func(opt, v string) {
		switch opt {
		case "exec", "sh-exec", "e", "c":
			in.Strings = append(in.Strings, v)
		case "lua-exec":
			in.Words = append(in.Words, []string{"lua", v})
		case "output", "hex-dump", "o", "x":
			if v != "" {
				in.Words = append(in.Words, []string{"tee", v})
			}
		}
	}
	for k := 1; k < len(w); k++ {
		a := w[k]
		if a == "--" {
			break
		}
		if strings.HasPrefix(a, "--") {
			n, v, eq := strings.Cut(a[2:], "=")
			for _, long := range ncatLong {
				if n == "" || !strings.HasPrefix(long, n) {
					continue
				}
				if !eq {
					k++
					v = at(w, k)
				}
				take(long, v)
				break
			}
			continue
		}
		if len(a) < 2 || a[0] != '-' {
			continue
		}
		for i := 1; i < len(a); i++ {
			c := a[i]
			if !strings.ContainsRune(ncValued, rune(c)) {
				continue
			}
			v := a[i+1:]
			if v == "" {
				k++
				v = at(w, k)
			}
			if c != 'x' || ncat {
				take(string(c), v)
			}
			break
		}
	}
	return in
}
