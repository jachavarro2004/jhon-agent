package sh

import (
	"reflect"
	"testing"
)

// T228 L15: one canonical ssh keyword reader; SSM spellings fail closed;
// socat's addresses read as socat reads them; ncat's long options by prefix;
// socat's and nc's file writes.
func TestSSHKeywordCanonical(t *testing.T) {
	type kv struct {
		key, val string
		quoted   bool
	}
	for v, want := range map[string]kv{
		"ForwardAgent yes":           {"forwardagent", "yes", false},
		"ForwardAgent=yes":           {"forwardagent", "yes", false},
		" ForwardAgent = yes":        {"forwardagent", "yes", false},
		"PROXYCOMMAND\ta":            {"proxycommand", "a", false},
		`Forward"Agent" yes`:         {"forwardagent", "yes", true},
		`Forward"Agent"=yes`:         {"forwardagent", "yes", true},
		`"ForwardAgent" yes`:         {"forwardagent", "yes", true},
		`PermitLocal"Command" yes`:   {"permitlocalcommand", "yes", true},
		`Local"Command" curl x`:      {"localcommand", "curl x", true},
		`Pkcs11"Provider" ./e.so`:    {"pkcs11provider", "./e.so", true},
		`'proxycommand' a`:           {"proxycommand", "a", true},
		`Send\Env *`:                 {"sendenv", "*", true},
		`"Forward Agent" yes`:        {"forward agent", "yes", true},
		"ServerAliveInterval 30":     {"serveraliveinterval", "30", false},
		"StrictHostKeyChecking=no":   {"stricthostkeychecking", "no", false},
		`Strict"HostKeyChecking" no`: {"stricthostkeychecking", "no", true},
	} {
		k, val, q := sshKeyword(v)
		if got := (kv{k, val, q}); got != want {
			t.Errorf("sshKeyword(%q) = %+v, want %+v", v, got, want)
		}
	}
	for v, want := range map[string]bool{
		"ssh -W %h:%p -o StrictHostKeyChecking=no b": true, "ssh -W %h:%p -o 'ServerAliveInterval 30' b": true,
		`ssh -W %h:%p -o 'Strict"HostKeyChecking" no' b`: false, `ssh -W %h:%p -o 'Forward"Agent" yes' b`: false,
		`ssh -W %h:%p -o '"SendEnv" T' b`: false, `ssh -W %h:%p -o 'Remote\Forward 9:x:22' b`: false,
	} {
		if got := ForwardCommand(v); got != want {
			t.Errorf("ForwardCommand(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestSSMConnectionSpellings(t *testing.T) {
	for args, want := range map[string]bool{
		"--document AWS-StartSSHSession": true, "--document-nam AWS-StartSSHSession": true, "--docu=AWS-StartPortForwardingSession": true,
		"--document AWS-StartInteractiveCommand": false, "--do=Custom": false, "--d X": false,
		"--document-name AWS-StartSSHSession --document-name AWS-StartInteractiveCommand": false,
		"--document-name AWS-StartInteractiveCommand --document AWS-StartSSHSession":      true,
		"--cli-input-json file://x": false, "--cli-input-yaml=file://x": false, "--cli-i x": false,
		"--cli-input-j file://x --document-name AWS-StartSSHSession": false,
		"--target i --reason x": true, "--debug --target i": true,
	} {
		if got := SSMConnection(splitWords(args)); got != want {
			t.Errorf("SSMConnection(%q) = %v, want %v", args, got, want)
		}
	}
}

func TestSocatAddresses(t *testing.T) {
	for word, want := range map[string][]socatAddress{
		`EXEC:"rm x",pty`:             {{"exec", "rm x", []string{"pty"}}},
		`EXEC:"a x,y"`:                {{"exec", "a x,y", []string{}}},
		`exec:'a,b',pty,stderr`:       {{"exec", "a,b", []string{"pty", "stderr"}}},
		`SYSTEM:echo [a,b]; x,pty`:    {{"system", "echo [a,b]; x", []string{"pty"}}},
		`SYSTEM:echo {a,b} (c,d),pty`: {{"system", "echo {a,b} (c,d)", []string{"pty"}}},
		`SYSTEM:echo a\,b`:            {{"system", "echo a,b", []string{}}},
		`SYSTEM:"echo \"in\""`:        {{"system", `echo "in"`, []string{}}},
		`SYSTEM:"echo a!!b"`:          {{"system", "echo a!!b", []string{}}},
		`EXEC:a!!STDOUT`:              {{"exec", "a", []string{}}, {"stdout", "", []string{}}},
		`OPEN:f,creat,trunc`:          {{"open", "f", []string{"creat", "trunc"}}},
		`/dev/ttyS0,raw`:              {{"gopen", "/dev/ttyS0", []string{"raw"}}},
		`TCP:h:1`:                     {{"tcp", "h:1", []string{}}},
		`-`:                           {{"-", "", []string{}}},
	} {
		got := socatAddresses(word)
		for i := range got {
			if got[i].options == nil {
				got[i].options = []string{}
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("socatAddresses(%q) = %+v, want %+v", word, got, want)
		}
	}
	for line, want := range map[string]Inner{
		"socat - CREATE:f":              {Words: [][]string{{"tee", "f"}}},
		"socat - creat:f":               {Words: [][]string{{"tee", "f"}}},
		"socat - OPEN:f,creat":          {Words: [][]string{{"tee", "f"}}},
		"socat - OPEN:f,o-append":       {Words: [][]string{{"tee", "-a", "f"}}},
		"socat - GOPEN:f,trunc":         {Words: [][]string{{"tee", "f"}}},
		"socat - ./f,trunc":             {Words: [][]string{{"tee", "./f"}}},
		"socat -u OPEN:f,trunc -":       {},
		"socat -U - OPEN:f,trunc":       {},
		"socat -U OPEN:f,trunc -":       {Words: [][]string{{"tee", "f"}}},
		"socat - OPEN:f":                {},
		"socat - /dev/ttyS0,raw":        {},
		"socat -lf log - TCP:h:1":       {Words: [][]string{{"tee", "-a", "log"}}},
		"socat -T 5 -r dump - TCP:h:1":  {Words: [][]string{{"tee", "-a", "dump"}}},
		"socat - SYSTEM:'echo_a,b',pty": {Strings: []string{"echo a,b"}},
		"socat - TCP:h:1,system=x":      {Strings: []string{"x"}},
	} {
		if got := socatRuns(splitWords(line)); !reflect.DeepEqual(got, want) {
			t.Errorf("socatRuns(%q) = %+v, want %+v", line, got, want)
		}
	}
}

func TestNcRunsPrefixesAndWrites(t *testing.T) {
	for line, want := range map[string]Inner{
		"ncat --sh-exe x h 1":   {Strings: []string{"x"}},
		"ncat --sh x h 1":       {Strings: []string{"x"}},
		"ncat --ex=x h 1":       {Strings: []string{"x"}},
		"ncat --e x h 1":        {Strings: []string{"x"}},
		"ncat --lua f h 1":      {Words: [][]string{{"lua", "f"}}},
		"ncat --out f h 1":      {Words: [][]string{{"tee", "f"}}},
		"ncat --hex-dump=f h 1": {Words: [][]string{{"tee", "f"}}},
		"ncat -x f h 1":         {Words: [][]string{{"tee", "f"}}},
		"ncat -o f h 1":         {Words: [][]string{{"tee", "f"}}},
		"nc -o f h 1":           {Words: [][]string{{"tee", "f"}}},
		"nc -x proxy:1 h 1":     {},
		"ncat --ssl h 443":      {},
		"ncat --send-only h 1":  {},
		"ncat --proxy p:1 h 1":  {},
		"ncat -- --exec x":      {},
	} {
		if got := ncRuns(splitWords(line)); !reflect.DeepEqual(got, want) {
			t.Errorf("ncRuns(%q) = %+v, want %+v", line, got, want)
		}
	}
}
