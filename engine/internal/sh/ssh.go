package sh

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

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

// sshCanon is the one form an ssh keyword is compared in (T228 L15: the
// keyword's spelling broke three times, so the class is closed once):
// every quote and backslash removed and the case folded. ssh reads a
// keyword that is, or ends in, a quoted part (`Local"Command"`,
// `"ForwardAgent"`), so a keyword is canonicalized before any comparison:
// by SSHCommands, by the forward's denied keys and by the backstop alike.
func sshCanon(s string) string {
	return strings.ToLower(strings.NewReplacer("\"", "", "'", "", "\\", "").Replace(s))
}

// sshKeyword is an -o option's keyword as ssh reads it, canonical
// (sshCanon): the text up to the first `=`, space or tab outside quotes
// (a backslash taking the next character); its value, the rest after the
// separators; and quoted, whether the keyword as written held a quote or a
// backslash.
func sshKeyword(v string) (key, val string, quoted bool) {
	v = strings.TrimLeft(v, " \t")
	end, q := len(v), byte(0)
scan:
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '\\':
			i++
		case c == '=' || c == ' ' || c == '\t':
			end = i
			break scan
		}
	}
	raw := v[:end]
	return sshCanon(raw), strings.TrimSpace(strings.TrimLeft(v[end:], " \t=")), strings.ContainsAny(raw, "\"'\\")
}

// sshNamesCommand is whether a word, canonical (sshCanon), names one of
// sshCommandOptions.
func sshNamesCommand(w string) bool {
	n := sshCanon(w)
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
		key, val, _ := sshKeyword(v)
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
		forward := ForwardCommand(v)
		for _, c := range cmds {
			c.Forward = forward
		}
		out = append(out, cmds...)
	}
	return out, nil
}

// ForwardCommand is whether v, a command an ssh transport runs on this
// machine, is one plain forward to the host it connects to (T228 L12, L14):
// a single simple command (no `;`, `&&`, `||`, `|`, `&`, subshell,
// substitution, expansion, assignment or redirection but to /dev/null or a
// descriptor) that names %h or %p and is one of a named list of forwarders,
// each with only its forwarding options (forwarder). Any other program is
// no forward and keeps its ordinary judgement.
func ForwardCommand(v string) bool {
	if !strings.Contains(v, "%h") && !strings.Contains(v, "%p") {
		return false
	}
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(v), "")
	if err != nil || len(f.Stmts) != 1 {
		return false
	}
	st := f.Stmts[0]
	call, ok := st.Cmd.(*syntax.CallExpr)
	if !ok || st.Background || st.Coprocess || st.Negated || len(call.Assigns) > 0 || len(call.Args) == 0 {
		return false
	}
	for _, r := range st.Redirs {
		if t := r.Word.Lit(); t != "/dev/null" && (r.Op != syntax.DplOut && r.Op != syntax.DplIn) {
			return false
		}
	}
	plain := true
	syntax.Walk(st, func(n syntax.Node) bool {
		switch n.(type) {
		case *syntax.CmdSubst, *syntax.ProcSubst, *syntax.ArithmExp, *syntax.ParamExp, *syntax.Subshell, *syntax.Block:
			plain = false
		}
		return plain
	})
	cmds, err := Parse(v)
	if !plain || err != nil || len(cmds) != 1 {
		return false
	}
	return forwarder(cmds[0].Words, true)
}

// forwarder is whether w is one of the named forwarders, with only its
// forwarding options: ssh or autossh with -W (sshForward); nc or ncat to
// exactly %h and %p (ncForward); socat between standard input and
// TCP:%h:%p or PROXY:…:%h:%p (socatForward); cloudflared access ssh,
// corkscrew, connect-proxy and proxytunnel; and aws ssm start-session with
// no document or an SSH or port-forwarding one (SSMConnection). local is
// set for a command run on this machine; ssh's remote command is not one.
func forwarder(w []string, local bool) bool {
	switch Base(w[0]) {
	case "ssh", "autossh":
		return local && sshForward(w)
	case "nc", "ncat", "netcat":
		return ncForward(w)
	case "socat":
		return socatForward(w)
	case "cloudflared":
		return local && at(w, 1) == "access" && at(w, 2) == "ssh"
	case "corkscrew", "connect-proxy", "proxytunnel":
		return local
	case "aws":
		return local && at(w, 1) == "ssm" && at(w, 2) == "start-session" && SSMConnection(w[3:])
	}
	return false
}

// sshForwardFlags are ssh's options a forward may carry: none of them
// tunnels, listens, forwards the agent or X, reads a config file, takes a
// terminal or runs a command (-R -L -D -A -F -t -X -Y are not here).
const sshForwardFlags = "piloqv46CTNWJM"

// sshForwardDenied are the -o keys a forward may not carry: a command, a
// tunnel or a forward, the agent, X, the environment, a provider library.
var sshForwardDenied = set("proxycommand", "localcommand", "knownhostscommand", "permitlocalcommand", "remotecommand",
	"remoteforward", "localforward", "dynamicforward", "forwardagent", "forwardx11", "forwardx11trusted", "forwardx11timeout",
	"sendenv", "setenv", "tunnel", "tunneldevice", "pkcs11provider", "securitykeyprovider", "xauthlocation", "identityagent",
	"addkeystoagent", "requesttty", "sessiontype", "stdinnull", "forkafterauthentication", "exitonforwardfailure",
	"clearallforwardings", "gatewayports", "streamlocalbindunlink", "streamlocalbindmask", "permitremoteopen", "include", "match")

// sshForward is whether w, an ssh or autossh command line, only forwards:
// -W to %h or %p (with -J for a jump host), only sshForwardFlags and -o keys
// outside sshForwardDenied, and no remote command but exactly nc, ncat or
// socat to %h and %p (with no -W, no -J).
func sshForward(w []string) bool {
	spec := sshGetopt["ssh"]
	autossh := Base(w[0]) == "autossh"
	if autossh {
		spec = "M:" + strings.Replace(spec, "M", "", 1)
	}
	takes := func(c byte) bool {
		i := strings.IndexByte(spec, c)
		return i >= 0 && c != ':' && i+1 < len(spec) && spec[i+1] == ':'
	}
	forwardW, jump := false, false
	var operands []string
	host := false
	for k := 1; k < len(w); k++ {
		a := w[k]
		if a == "--" || len(a) < 2 || a[0] != '-' {
			if a == "--" {
				k++
			}
			if !host && k < len(w) {
				// The host; ssh reads options again right after it.
				host = true
				operands = append(operands, w[k])
				continue
			}
			operands = append(operands, w[k:]...)
			break
		}
		for i := 1; i < len(a); i++ {
			c := a[i]
			if !strings.ContainsRune(sshForwardFlags, rune(c)) || c == 'M' && !autossh {
				return false
			}
			if !takes(c) {
				continue
			}
			v := a[i+1:]
			if v == "" {
				k++
				v = at(w, k)
			}
			switch c {
			case 'W':
				forwardW = strings.Contains(v, "%h") || strings.Contains(v, "%p")
				if !forwardW {
					return false
				}
			case 'J':
				jump = true
			case 'o':
				// A keyword written with a quote or a backslash is never a
				// forward's (T228 L15), whatever it reads as.
				key, _, quoted := sshKeyword(v)
				if quoted || sshForwardDenied[key] {
					return false
				}
				jump = jump || key == "proxyjump"
			}
			break
		}
	}
	command := operands[min(1, len(operands)):]
	switch {
	case forwardW:
		return len(command) == 0
	case jump || len(command) == 0:
		return false
	}
	return forwarder(command, false)
}

// ncForward is whether w, nc or ncat, connects to exactly %h and %p with
// only proxy and timeout options (-w -X -x -G -q, ncat's --proxy and
// --proxy-type, -4 -6 -n -v): never -e, -c, --exec, --sh-exec,
// --lua-exec, a listener or -k.
func ncForward(w []string) bool {
	valued := set("-w", "-X", "-x", "-G", "-q", "--proxy", "--proxy-type", "--proxy-auth", "--wait")
	flags := set("-4", "-6", "-n", "-v", "-vv")
	var operands []string
	for k := 1; k < len(w); k++ {
		a := w[k]
		n, _, eq := strings.Cut(a, "=")
		switch {
		case valued[n] && strings.HasPrefix(n, "--") && eq:
		case valued[a]:
			k++
		case len(a) > 2 && a[0] == '-' && a[1] != '-' && valued[a[:2]]:
		case flags[a]:
		case strings.HasPrefix(a, "-"):
			return false
		default:
			operands = append(operands, a)
		}
	}
	return len(operands) == 2 && (operands[0] == "%h" && operands[1] == "%p" || operands[0] == "%p" && operands[1] == "%h")
}

// socatForward is whether w is socat between standard input (`-`, STDIO,
// STDIN) and TCP:%h:%p, TCP4:, TCP6: or PROXY:<proxy>:%h:%p with proxy
// options only: never EXEC:, SYSTEM:, exec=, a listener, pty or fork.
func socatForward(w []string) bool {
	var addrs []string
	for _, a := range w[1:] {
		switch {
		case a == "-d" || a == "-dd" || a == "-ddd" || a == "-v" || a == "-u" || a == "-U":
		case strings.HasPrefix(a, "-") && a != "-":
			return false
		default:
			addrs = append(addrs, a)
		}
	}
	if len(addrs) != 2 {
		return false
	}
	stdio := func(a string) bool {
		u := strings.ToUpper(a)
		return a == "-" || u == "STDIO" || u == "STDIN" || u == "STDOUT"
	}
	tcp := func(a string) bool {
		head, opts, _ := strings.Cut(a, ",")
		u := strings.ToUpper(head)
		target := ""
		switch {
		case strings.HasPrefix(u, "TCP:") || strings.HasPrefix(u, "TCP4:") || strings.HasPrefix(u, "TCP6:"):
			target = head[strings.IndexByte(head, ':')+1:]
			if opts != "" {
				return false
			}
		case strings.HasPrefix(u, "PROXY:"):
			parts := strings.Split(head, ":")
			if len(parts) != 4 {
				return false
			}
			target = parts[2] + ":" + parts[3]
			for _, o := range strings.Split(opts, ",") {
				if n, _, _ := strings.Cut(o, "="); o != "" && n != "proxyport" && n != "proxyauth" && n != "resolve" {
					return false
				}
			}
		default:
			return false
		}
		return target == "%h:%p"
	}
	return stdio(addrs[0]) && tcp(addrs[1]) || tcp(addrs[0]) && stdio(addrs[1])
}

// ssmConnectionDocuments are the SSM session documents that only connect:
// a shell-less SSH session and the port forwards.
var ssmConnectionDocuments = set("AWS-StartSSHSession", "AWS-StartPortForwardingSession", "AWS-StartPortForwardingSessionToRemoteHost")

// SSMConnection is whether aws ssm start-session's arguments args name no
// document, or one of ssmConnectionDocuments (T228 L14, L15): a session
// that only connects, as ssh does. Any other document
// (AWS-StartInteractiveCommand, AWS-StartNonInteractiveCommand, one that
// runs a shell script, a custom one) runs a command. It fails closed as the
// CLI reads its options: any prefix of --document-name names the document
// (`--document`, `--docu=…`), the last one wins, and --cli-input-json or
// --cli-input-yaml (or a prefix) may name any document, so it is none.
func SSMConnection(args []string) bool {
	doc, named := "", false
	for k, a := range args {
		n, v, eq := strings.Cut(a, "=")
		switch {
		case len(n) < 3 || !strings.HasPrefix(n, "--"):
		case len(n) >= len("--cli-i") && (strings.HasPrefix("--cli-input-json", n) || strings.HasPrefix("--cli-input-yaml", n)):
			return false
		case strings.HasPrefix("--document-name", n):
			if !eq {
				v = at(args, k+1)
			}
			doc, named = v, true
		}
	}
	return !named || ssmConnectionDocuments[doc]
}
