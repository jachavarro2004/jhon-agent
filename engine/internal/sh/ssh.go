package sh

import "strings"

// An ssh transport value (GIT_SSH_COMMAND, core.sshCommand, any *SSH*
// command variable) is read with ssh's own option grammar (T228 L10: its
// option reading broke twice when it read spellings, so the class is
// closed): the words are parsed as OpenSSH's getopt parses them, and a word
// that names a local-command option the grammar did not resolve leaves the
// line unreadable.

// sshGetopt is each program's getopt option string, from OpenSSH's ssh.c,
// scp.c and sftp.c: a letter followed by `:` takes a value (the rest of its
// word, else the next word); any other is a flag, and flags bundle (`-vo
// X`, `-4oX`). Checked against OpenSSH 10.6's usage text; a letter missing
// here is read as a flag, and the backstop below keeps a misread -o unreadable.
var sshGetopt = map[string]string{
	"ssh":  "1246ab:c:e:fgi:kl:m:no:p:qstvxAB:CD:E:F:GI:J:KL:MNO:P:Q:R:S:TVw:W:XYyZ",
	"scp":  "12346ABCTdfOpqRrstvD:F:J:M:P:S:X:c:i:l:o:",
	"sftp": "1246AafhNpqrvCc:D:i:l:o:s:S:b:B:F:J:P:R:X:",
}

// sshCommandOptions are ssh's options whose value it runs as a command on
// this machine: ProxyCommand, LocalCommand (with PermitLocalCommand) and
// KnownHostsCommand (RemoteCommand runs on the host; ProxyJump runs ssh).
var sshCommandOptions = []string{"proxycommand", "localcommand", "knownhostscommand"}

// sshKeyword is an -o option's name as ssh reads it: the text up to `=`,
// a space or a tab, one pair of quotes around it removed, case-folded; and
// its value, the rest after the separators.
func sshKeyword(v string) (key, val string) {
	v = strings.TrimLeft(v, " \t")
	if len(v) > 0 && (v[0] == '"' || v[0] == '\'') {
		if end := strings.IndexByte(v[1:], v[0]); end >= 0 {
			key, val = v[1:1+end], v[2+end:]
			return strings.ToLower(key), strings.TrimSpace(strings.TrimLeft(val, " \t="))
		}
	}
	i := strings.IndexAny(v, "= \t")
	if i < 0 {
		return strings.ToLower(v), ""
	}
	return strings.ToLower(v[:i]), strings.TrimSpace(strings.TrimLeft(v[i:], " \t="))
}

// sshNamesCommand is whether a word, case-folded with its quotes and
// backslashes removed, names one of sshCommandOptions.
func sshNamesCommand(w string) bool {
	n := strings.ToLower(strings.NewReplacer("\"", "", "'", "", "\\", "").Replace(w))
	for _, o := range sshCommandOptions {
		if strings.Contains(n, o) {
			return true
		}
	}
	return false
}

// SSHCommands is the commands an ssh, scp or sftp command line w runs on
// this machine through its -o options, parsed with the program's getopt
// grammar (sshGetopt): `--` ends the options, and ssh reads options again
// right after its host. A value of `none` runs none, and a -F file is not
// read. A word that names a local-command option (sshNamesCommand) that the
// grammar did not read as such an option's value is an error: the line is
// unreadable.
func SSHCommands(w []string) ([]string, error) {
	spec := sshGetopt[Base(at(w, 0))]
	if spec == "" {
		spec = sshGetopt["ssh"]
	}
	takes := func(c byte) (known, valued bool) {
		i := strings.IndexByte(spec, c)
		return i >= 0 && c != ':', i >= 0 && i+1 < len(spec) && spec[i+1] == ':'
	}
	var out []string
	resolved := map[int]bool{}
	option := func(k int, v string) {
		key, val := sshKeyword(v)
		// PermitLocalCommand names LocalCommand in its own name, and runs nothing.
		if key == "permitlocalcommand" && !sshNamesCommand(val) {
			resolved[k] = true
		}
		for _, o := range sshCommandOptions {
			if key == o {
				resolved[k] = true
				if val != "" && !strings.EqualFold(val, "none") {
					out = append(out, val)
				}
			}
		}
	}
	host := Base(at(w, 0)) != "ssh"
	for k := 1; k < len(w); k++ {
		a := w[k]
		if a == "--" {
			break
		}
		if len(a) < 2 || a[0] != '-' {
			// ssh's host: its options may follow it; any other operand ends them.
			if host {
				break
			}
			host = true
			continue
		}
		for i := 1; i < len(a); i++ {
			known, valued := takes(a[i])
			if !known || !valued {
				continue
			}
			v, idx := a[i+1:], k
			if v == "" {
				k++
				v, idx = at(w, k), k
			}
			if a[i] == 'o' {
				option(idx, v)
			}
			break
		}
	}
	for k, v := range w[1:] {
		if sshNamesCommand(v) && !resolved[k+1] {
			return nil, &Unreadable{Why: Base(w[0]) + "'s word " + v + " names a command option the reader does not read as one"}
		}
	}
	return out, nil
}

// sshRunsCommands is the commands an ssh command line runs on this machine
// (SSHCommands), each read as a command line; a command that does not
// parse is unreadable.
func sshRunsCommands(w []string) ([]*Command, error) {
	vals, err := SSHCommands(w)
	if err != nil {
		return nil, err
	}
	var out []*Command
	for _, v := range vals {
		cmds, err := Parse(v)
		if err != nil {
			return nil, &Unreadable{Why: "the command " + Base(w[0]) + " runs, " + v + ", does not parse"}
		}
		out = append(out, cmds...)
	}
	return out, nil
}
