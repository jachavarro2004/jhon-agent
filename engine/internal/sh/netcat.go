package sh

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// socat and the netcat family run commands and write files their own
// grammar names (T228 L14, L15): socat glues a command or a file to an
// address, and nc, ncat and netcat take a command or a dump file as an
// option's value. What each runs is read as a string the shell runs, as any
// runner's; a file each writes is read as tee writing it, so it is judged as
// that write is.

// socatCommandAddresses are socat's address types whose parameter is a
// command it runs: EXEC (the words), SYSTEM and SHELL (a string the shell
// runs).
var socatCommandAddresses = set("exec", "system", "shell")

// socat is read by class (T228 L17, L18: rounds 9 to 12 each found the
// next socat spelling, so the class is closed once, as L6 closed the
// runners and L10 the ssh keywords). Every option on every address, of
// whatever type, is looked up in socatOptions, a table of socat's own
// option names and the aliases `socat -hhh` lists for them: harmless, a
// path it writes or removes, the folder its command runs in, or (any name
// the table does not hold, or holds as unsafe where it stands) unreadable.
// socat itself refuses an option name it does not know and any shortened
// one, so an option or alias socat adds asks until it is read here. Every
// address that names a filesystem path is read by its type into a write
// (judged as tee) or a removal (judged as rm).

// socatFileAddresses are socat's address types that open a file by its
// path (FILE is OPEN's other name, CREAT CREATE's); a bare path is GOPEN.
var socatFileAddresses = set("open", "file", "gopen", "create", "creat")

// socatPipeAddresses are PIPE and its other names: with a path it makes a
// FIFO there, or writes into the regular file that is there.
var socatPipeAddresses = set("pipe", "fifo", "echo")

// socatBindAddresses are the unix socket listeners and receivers: each
// binds a new socket at its path, and replaces what is there only with an
// unlink option (without one socat stops: the path exists).
var socatBindAddresses = set("unix-listen", "unix-l", "unix-recv", "unix-recvfrom")

// socatConnectAddresses are the unix socket clients: each names a path it
// connects or sends to, and binds one only with bind=.
var socatConnectAddresses = set("unix-connect", "unix-client", "unix", "local", "unix-sendto", "unix-send")

// socatNetwork is whether an address type is of a network family, whose
// parameters are hosts and ports and whose bind= is an address.
func socatNetwork(kind string) bool {
	for _, family := range []string{"tcp", "udp", "sctp", "dccp", "ip", "inet", "openssl", "ssl", "dtls", "socks", "proxy", "vsock"} {
		if strings.HasPrefix(kind, family) {
			return true
		}
	}
	return false
}

// socatNoPath is whether an address type names no filesystem path: the
// network families, the abstract and message-queue namespaces, the generic
// sockets, the standard streams and descriptors, the commands, PTY and
// READLINE (whose paths are options).
func socatNoPath(kind string) bool {
	return socatNetwork(kind) || socatStreams[kind] || socatGenericSocket(kind) ||
		strings.HasPrefix(kind, "abstract") || strings.HasPrefix(kind, "posixmq")
}

var socatStreams = set("", "-", "stdio", "stdin", "stdout", "stderr", "fd", "accept-fd", "accept", "text", "stall", "pty",
	"readline", "interface", "tun", "exec", "system", "shell")

// socatGenericSocket is whether an address type is a generic socket, whose
// address is raw bytes that may hold a unix path the reader cannot read.
func socatGenericSocket(kind string) bool {
	return strings.HasPrefix(kind, "socket-") || kind == "socketpair" || kind == "dgram" || kind == "sendto"
}

// socatOpt is what an option does, as the reader judges it.
type socatOpt int

const (
	optHarmless socatOpt = iota + 1
	optLink              // replaces the file at its value: removed
	optFile              // writes the file at its value
	optUnlink            // removes the address's own path
	optTruncate          // creates or truncates a file address's file, any value (creat=0, ftruncate=0)
	optBind              // a network address, or a unix path it binds: removed
	optOwner             // owner and mode: harmless on a listener or a file socat creates
	optSeek              // an offset: harmless on the side socat only reads
	optChdir             // the folder a command runs in
	optNetwork           // a network setting: harmless on a network-family address
)

// socatOptions are the options the reader knows, under socat 1.8's
// canonical name and every alias `socat -hhh` lists for it. Harmless: a
// serial line's speed, mode, parity, bits and read settings; the
// listener's and the connection's network settings; the child's terminal
// and signals; the open flags. Any other name (umask, path, shell, chroot,
// setuid, substuser, netns, fs-*, ioctl-*, an option socat adds) is not
// here, so the line is unreadable.
var socatOptions = func() map[string]socatOpt {
	m := map[string]socatOpt{}
	add := func(o socatOpt, names string) {
		for _, n := range strings.Fields(names) {
			m[n] = o
		}
	}
	add(optHarmless, `raw termios-rawer rawer sane echo icanon isig cs5 cs6 cs7 cs8 csize cstopb parenb parodd clocal crtscts
		ixon ixoff ixany icrnl inlcr igncr onlcr ocrnl opost istrip echoe echok echonl ispeed ospeed vmin min vtime time
		tiocsctty ctty crnl crlf cr ignoreeof ignoreof escape noecho prompt noprompt
		pty stderr setsid sid sighup sigint sigquit pty-wait-slave wait-slave waitslave pty-interval
		fork max-children maxchildren backlog retry interval intervall forever
		so-reuseaddr reuseaddr so-reuseport reuseport tcp-nodelay nodelay so-keepalive keepalive connect-timeout range
		sourceport sp lowport protocol-family pf proxyport proxy-authorization proxy-auth proxyauth proxy-resolve resolve resolv
		socksport socksuser openssl-verify verify openssl-certificate cert certificate openssl-key key openssl-cafile cafile
		openssl-capath capath openssl-commonname cn commonname openssl-snihost snihost
		o-rdonly o_rdonly rdonly o-wronly o_wronly wronly o-rdwr o_rdwr rdwr o-nonblock ndelay nonblock o-ndelay o_ndelay
		append o-append o-excl excl o_excl o-noctty noctty`)
	add(optLink, "symbolic-link link")
	add(optFile, "history-file history lockfile waitlock")
	add(optUnlink, "unlink-early new unlink-late unlink-close unlink delete remove")
	add(optTruncate, "o-creat creat create o_creat o-create o_create o-trunc trunc o_trunc ftruncate64 ftruncate truncate ftruncate32")
	add(optBind, "bind")
	add(optOwner, "perm mode perm-early perm-late user owner uid user-early user-late group gid group-early group-late")
	add(optSeek, "lseek64-set lseek lseek64 seek seek-set lseek64-end seek-end lseek64-cur seek-cur lseek32-set lseek32-end lseek32-cur")
	add(optChdir, "chdir cd")
	// T228 L19: every option socat 1.8.1.3's `socat -hhh` puts only in the
	// SOCKET, IP4, IP6, TCP, UDP, SCTP, DCCP, OPENSSL, SOCKS, HTTP, RANGE,
	// LISTEN, CHILD and RETRY groups, with every alias it lists (150 options,
	// 330 names), generated from that listing, not chosen by hand: a socket,
	// IP, transport or TLS setting, harmless on a network-family address
	// (socatNetwork) and unread elsewhere. The TLS files (cafile, capath,
	// cert, key, dhparam, egd) are only read. proxy-authorization-file is
	// left out: it reads a file and sends its contents to the proxy. A name
	// the table already reads keeps its reading (bind, fork, verify, …).
	for _, n := range strings.Fields(`accept-timeout acceptconn add-membership add-source-membership addrconfig ai-addrconfig ai-all ai-passive
		ai-v4mapped attach-filter attachfilter authhdr backlog bind bindtodevice broadcast bsdcompat cafile capath
		ccid cert certificate children-shutup cipher cipherlist ciphers cn commonname compress connect-timeout cork
		dccp-set-ccid debug defer-accept defnames detach-filter detachfilter dh dhparam dhparams dns dnsrch dontroute
		dstopts egd error fiosetown forever fork freebind hdrincl hoplimit hopopts http-version if ignorecr igntc info
		interface interval intervall ip-add-membership ip-add-source-membership ip-freebind ip-hdrincl ip-membership
		ip-mtu ip-mtu-discover ip-multicast-if ip-multicast-loop ip-multicast-ttl ip-options ip-pktinfo ip-pktoptions
		ip-recverr ip-recvopts ip-recvtos ip-recvttl ip-retopts ip-router-alert ip-tos ip-transparent ip-ttl
		ipfreebind iphdrincl ipmtu ipmtudiscover ipmulticastloop ipmulticastttl ipoptions ippktinfo ippktoptions
		iprecverr iprecvopts iprecvtos iprecvttl ipretopts iprouteralert iptos ipttl ipv6-add-membership
		ipv6-add-source-membership ipv6-authhdr ipv6-dstopts ipv6-hoplimit ipv6-hopopts ipv6-join-group
		ipv6-join-source-group ipv6-mcloop ipv6-mtu-discover ipv6-multicast-loop ipv6-pktinfo ipv6-recvdstopts
		ipv6-recverr ipv6-recvhoplimit ipv6-recvhopopts ipv6-recvpathmtu ipv6-recvpktinfo ipv6-recvrthdr
		ipv6-recvtclass ipv6-rthdr ipv6-tclass ipv6-unicast-hops ipv6-v6only ipv6only join-group join-source-group
		keepalive keepcnt keepidle keepintvl key linger linger2 listen-timeout max-children max-proto-version
		max-version maxchildren maxfraglen maxseg maxseg-late maxsendfrag mcloop mcloop6 membership min-proto-version
		min-version mss mss-late mtu mtudiscover mtudiscover6 multicast-if multicast-loop multicast-ttl multicastloop
		multicastttl nameserver no-check no-sni nocheck nodelay nosni nsaddr null-eof oobinline openssl-cafile
		openssl-capath openssl-certificate openssl-cipherlist openssl-commonname openssl-compress openssl-dhparam
		openssl-dhparams openssl-egd openssl-key openssl-max-proto-version openssl-maxfraglen openssl-maxsendfrag
		openssl-min-proto-version openssl-no-sni openssl-pseudo openssl-snihost openssl-verify passcred passive
		peercred pf pktinfo pktoptions pktopts priority protocol protocol-family prototype proxy-auth
		proxy-authorization proxy-resolve proxyauth proxyport pseudo quickack range rcvbuf rcvbuf-late rcvlowat
		rcvtimeo recurse recvdstopts recverr recvhoplimit recvhopopts recvopts recvpktinfo recvrthdr recvtos recvttl
		res-debug res-defnames res-dnsrch res-igntc res-maxretrans res-maxretry res-nsaddr res-recurse res-retrans
		res-retry res-stayopen res-usevc resolv resolve retopts retrans retry reuseaddr reuseport routeralert rthdr
		security-authentication security-encryption-network security-encryption-transport securityauthentication
		securityencryptionnetwork securityencryptiontransport setsockopt setsockopt-bin setsockopt-connected
		setsockopt-int setsockopt-listen setsockopt-socket setsockopt-string siocspgrp sndbuf sndbuf-late sndlowat
		sndtimeo snihost so-acceptconn so-attach-filter so-bindtodevice so-broadcast so-bsdcompat so-debug
		so-detach-filter so-dontroute so-error so-keepalive so-linger so-no-check so-oobinline so-passcred so-peercred
		so-priority so-protocol so-prototype so-rcvbuf so-rcvbuf-late so-rcvlowat so-rcvtimeo so-reuseaddr
		so-reuseport so-security-authentication so-security-encryption-network so-security-encryption-transport
		so-sndbuf so-sndbuf-late so-sndlowat so-sndtimeo so-timestamp so-type sockopt sockopt-bin sockopt-conn
		sockopt-int sockopt-listen sockopt-sock sockopt-string sockspass socksport socksuser socktype
		source-membership stayopen syncnt tcp-cork tcp-defer-accept tcp-info tcp-keepcnt tcp-keepidle tcp-keepintvl
		tcp-linger2 tcp-maxseg tcp-maxseg-late tcp-md5sig tcp-nodelay tcp-quickack tcp-syncnt tcp-window-clamp
		timestamp tos transparent ttl type unicast-hops usevc v4mapped v6only verify window-clamp`) {
		if m[n] == 0 {
			m[n] = optNetwork
		}
	}
	return m
}()

// socatRate is a serial speed option, b<rate>.
var socatRate = lazyre.New(`^b[0-9]+$`)

// socatRead is what one socat address does, as the reader reads it.
type socatRead struct {
	commands, writes, removes []string
}

// socatPaths reads one socat address by class (T228 L17, L18). Its type:
// EXEC, SYSTEM and SHELL run their parameter (in the folder chdir= names);
// a file address writes its path on the side socat writes, CREATE and PIPE
// on either side, any other type that names a path writes it; a unix
// client names a path it only connects to, a listener binds a new one.
// Its options, each looked up in socatOptions whatever the type: link=
// removes its value, history-file=, lockfile= and waitlock= write theirs,
// an unlink option removes the address's own path, a truncating one makes
// a file address a write, bind= on an address that is not a network one
// removes its value, owner and mode are harmless only on a listener or a
// file socat creates, an offset only on the side socat only reads, chdir
// only on a command; anything else is unreadable. A serial device
// (socatSerial) is not a write.
func socatPaths(ad socatAddress, written bool) (r socatRead, err error) {
	file := socatFileAddresses[ad.kind]
	command := socatCommandAddresses[ad.kind]
	named := ad.param != "" && !socatNoPath(ad.kind)
	write := named && (file && written || ad.kind == "create" || ad.kind == "creat" || socatPipeAddresses[ad.kind] ||
		!file && !socatBindAddresses[ad.kind] && !socatConnectAddresses[ad.kind])
	creates := ad.kind == "create" || ad.kind == "creat" || socatBindAddresses[ad.kind]
	chdir := ""
	var unsafe []string
	for _, o := range ad.options {
		n, _, _ := strings.Cut(o, "=")
		if t := socatOptions[strings.ToLower(n)]; t == optTruncate && strings.Contains(strings.ToLower(n), "creat") {
			creates = true
		}
	}
	for _, o := range ad.options {
		n, v, _ := strings.Cut(o, "=")
		n = strings.ToLower(n)
		switch t := socatOptions[n]; {
		case t == optHarmless || socatRate.MatchString(n):
		case t == optLink:
			r.removes = append(r.removes, v)
		case t == optFile:
			r.writes = append(r.writes, v)
		case t == optUnlink && named:
			r.removes = append(r.removes, ad.param)
		case t == optUnlink && (socatGenericSocket(ad.kind) || !socatNoPath(ad.kind)):
			unsafe = append(unsafe, n)
		case t == optUnlink:
		case t == optTruncate:
			write = write || file
		case t == optBind && socatNetwork(ad.kind):
		case t == optBind:
			r.removes = append(r.removes, v)
		case t == optOwner && creates, t == optSeek && !written:
		case t == optChdir && command:
			chdir = v
		case t == optNetwork && socatNetwork(ad.kind):
		default:
			unsafe = append(unsafe, n)
		}
	}
	if len(unsafe) > 0 {
		return socatRead{}, &Unreadable{Why: "socat's address " + ad.kind + " carries an option the reader does not read as safe there: " + strings.Join(unsafe, ", ")}
	}
	if command && ad.param != "" {
		c := ad.param
		if chdir != "" {
			q, err := syntax.Quote(chdir, syntax.LangBash)
			if err != nil {
				return socatRead{}, &Unreadable{Why: "socat's chdir " + chdir + " cannot be read"}
			}
			c = "cd " + q + " && " + c
		}
		r.commands = append(r.commands, c)
	}
	if write && !socatSerial(ad.param) {
		r.writes = append(r.writes, ad.param)
	}
	return r, nil
}

// socatSerial is whether p, by its shape once cleaned (the machine is not
// looked at), is a serial character device: /dev/tty*, /dev/pts/*,
// /dev/serial/* (T228 L16, the owner's serial work). socat reading and
// writing one is not judged as a write; a block device and every other
// device are.
func socatSerial(p string) bool {
	c := path.Clean(p)
	rest, tty := strings.CutPrefix(c, "/dev/tty")
	return tty && !strings.Contains(rest, "/") || strings.HasPrefix(c, "/dev/pts/") || strings.HasPrefix(c, "/dev/serial/")
}

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

// socatRuns is what socat runs, writes and removes (T228 L14 to L18): the
// command of each EXEC:, SYSTEM: or SHELL: address (either side of a `!!`
// pair), in the folder its chdir= names, as a string the shell runs; the
// paths socatPaths reads its addresses to write (as tee) and remove (as
// rm), an option it does not read as safe leaving the line unreadable; and
// tee writing the file of each address that writes one (T228 L16): on the
// side socat writes (both sides with no -u or -U; with -u the second, with
// -U the first) every OPEN:, FILE:, GOPEN:, CREATE: or CREAT: address or bare
// path; on either side a CREATE: or CREAT: address and a file address with
// a creating or truncating option (optTruncate); but not a serial
// device (socatSerial). And tee writing the file of the -lf, -L, -W, -r
// and -R options.
func socatRuns(w []string) (Inner, error) {
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
		written := oneway == "" || oneway == "-u" && i == 1 || oneway == "-U" && i == 0
		for _, ad := range socatAddresses(word) {
			r, err := socatPaths(ad, written)
			if err != nil {
				return Inner{}, err
			}
			in.Strings = append(in.Strings, r.commands...)
			for _, p := range r.writes {
				in.Words = append(in.Words, []string{"tee", p})
			}
			for _, p := range r.removes {
				in.Words = append(in.Words, []string{"rm", p})
			}
		}
	}
	return in, nil
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
