package actions

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/system"
)

// The dev ufw fake keeps its state in the fake root, so rules survive restarts.
const fakeUFWState = "/etc/ufw/layte-dev-state.json"

type fakeUFWRule struct{ To, Action, From string }

type fakeUFW struct {
	Active bool
	Rules  []fakeUFWRule
}

func loadFakeUFW(root system.FS) fakeUFW {
	st := fakeUFW{Active: true, Rules: []fakeUFWRule{
		{"22/tcp", "ALLOW IN", "Anywhere"},
		{"80,443/tcp", "ALLOW IN", "Anywhere"},
		{"3306/tcp", "ALLOW IN", "10.0.0.0/24"},
	}}
	if b, err := root.ReadFile(fakeUFWState); err == nil {
		json.Unmarshal(b, &st)
	}
	return st
}

func (f fakeUFW) save(root system.FS) error {
	root.MkdirAll("/etc/ufw", 0o755)
	b, _ := json.Marshal(f)
	return root.WriteFileAtomic(fakeUFWState, b, 0o644)
}

func fakeUFWCmd(root system.FS, c system.Cmd) (system.Result, error) {
	st := loadFakeUFW(root)
	args := c.Args
	if len(args) > 0 && args[0] == "--force" {
		args = args[1:]
	}
	out := func(s string) system.Result { return system.Result{Stdout: []byte(s)} }
	switch {
	case len(args) == 2 && args[0] == "status" && args[1] == "verbose":
		if !st.Active {
			return out("Status: inactive\n"), nil
		}
		return out("Status: active\nLogging: on (low)\nDefault: deny (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n"), nil
	case len(args) == 2 && args[0] == "status" && args[1] == "numbered":
		var b strings.Builder
		b.WriteString("Status: active\n\n     To                         Action      From\n     --                         ------      ----\n")
		for i, r := range st.Rules {
			fmt.Fprintf(&b, "[%2d] %-26s %-11s %s\n", i+1, r.To, r.Action, r.From)
		}
		return out(b.String()), nil
	case len(args) >= 1 && args[0] == "allow":
		r := fakeUFWRule{Action: "ALLOW IN", From: "Anywhere"}
		if len(args) == 2 { // allow 22/tcp
			r.To = args[1]
		} else {
			proto, port := "", ""
			for i := 1; i+1 < len(args); i++ {
				switch args[i] {
				case "proto":
					proto = args[i+1]
				case "from":
					if args[i+1] != "any" {
						r.From = args[i+1]
					}
				case "port":
					port = args[i+1]
				}
			}
			r.To = port
			if proto != "" {
				r.To += "/" + proto
			}
		}
		if slices.Contains(st.Rules, r) {
			return out("Skipping adding existing rule\n"), nil
		}
		st.Rules = append(st.Rules, r)
		return out("Rule added\n"), st.save(root)
	case len(args) == 2 && args[0] == "delete":
		n, _ := strconv.Atoi(args[1])
		if n < 1 || n > len(st.Rules) {
			return system.Result{Stderr: []byte("ERROR: Could not find rule")}, &system.ExitError{Bin: c.Bin, ExitCode: 1}
		}
		st.Rules = slices.Delete(st.Rules, n-1, n)
		return out("Rule deleted\n"), st.save(root)
	case len(args) == 1 && (args[0] == "enable" || args[0] == "disable"):
		st.Active = args[0] == "enable"
		return out("Firewall is " + map[bool]string{true: "active and enabled on system startup", false: "stopped and disabled on system startup"}[st.Active] + "\n"), st.save(root)
	}
	return system.Result{}, fmt.Errorf("ufw %v: not faked", args)
}

func fakeUnattendedUpgrade() (system.Result, error) {
	time.Sleep(1500 * time.Millisecond)
	return system.Result{Stdout: []byte("Checking: libssl3t64 ([<Origin component:'main' archive:'noble-security'>])\n" +
		"Checking: openssl ([<Origin component:'main' archive:'noble-security'>])\n" +
		"pkgs that look like they should be upgraded: libssl3t64\nopenssl\n" +
		"All upgrades installed (dev mode: nothing was really installed)\n")}, nil
}
