package actions

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"
)

// sshguard (installed by the admin, recommended on small servers) blocks IPs
// after failed SSH logins, longer for repeat offenders. The Security page
// shows what it's doing; Layte doesn't change its settings.

const (
	sshguardBin      = "/usr/sbin/sshguard"
	sshguardEnemies  = "/var/lib/sshguard/enemies" // permanent blocklist (if enabled)
	nftBin           = "/usr/sbin/nft"
	journalctlBin    = "/usr/bin/journalctl"
	sshguardMaxShown = 10
)

// SSHGuardStatus is sshguard's state on the Security page.
type SSHGuardStatus struct {
	Installed  bool            `json:"installed"`
	Running    bool            `json:"running"`
	BlockedNow int             `json:"blocked_now"` // IPs (or ranges) blocked right now
	Permanent  int             `json:"permanent"`   // IPs on the permanent blocklist
	Attacks24h int             `json:"attacks_24h"` // attack events sshguard saw
	Blocks24h  int             `json:"blocks_24h"`
	Recent     []SSHGuardBlock `json:"recent"` // latest blocks, newest first
}

// SSHGuardBlock is one "Blocking" event.
type SSHGuardBlock struct {
	Time    time.Time `json:"time"`
	Address string    `json:"address"`
	For     string    `json:"for"` // e.g. "120 secs"
}

var (
	// 2026-11-25T16:03:26+00:00 host sshguard[1]: Blocking "1.2.3.4/32" for 120 secs (3 attacks in 3 secs, …)
	reSSHGuardBlock = regexp.MustCompile(`^(\S+) \S+ sshguard\[\d+\]: Blocking "([^"]+)" for (\d+ \w+)`)
	reNftElements   = regexp.MustCompile(`(?s)elements = \{([^}]*)\}`)
)

func (d Deps) sshguardStatus(ctx context.Context) SSHGuardStatus {
	st := SSHGuardStatus{Recent: []SSHGuardBlock{}}
	if ok, _ := d.FS.Exists(sshguardBin); !ok {
		return st
	}
	st.Installed = true
	if r, err := d.Runner.Run(ctx, systemctl, "is-active", "sshguard"); err == nil && strings.TrimSpace(string(r.Stdout)) == "active" {
		st.Running = true
	}
	for _, family := range []string{"ip", "ip6"} {
		r, err := d.Runner.Run(ctx, nftBin, "list", "set", family, "sshguard", "attackers")
		if err != nil {
			continue
		}
		st.BlockedNow += nftElements(string(r.Stdout))
	}
	if b, err := d.FS.ReadFile(sshguardEnemies); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Count(line, "|") >= 3 {
				st.Permanent++
			}
		}
	}
	r, err := d.Runner.Run(ctx, journalctlBin, "-u", "sshguard", "--since", "-24 hours", "--no-pager", "-o", "short-iso")
	if err == nil {
		st.Attacks24h, st.Blocks24h, st.Recent = parseSSHGuardLog(string(r.Stdout))
	}
	return st
}

// nftElements counts the addresses in `nft list set` output.
func nftElements(out string) int {
	m := reNftElements.FindStringSubmatch(out)
	if m == nil {
		return 0
	}
	n := 0
	for _, e := range strings.Split(m[1], ",") {
		if strings.TrimSpace(e) != "" {
			n++
		}
	}
	return n
}

// parseSSHGuardLog counts attacks and blocks and returns the latest blocks.
func parseSSHGuardLog(out string) (attacks, blocks int, recent []SSHGuardBlock) {
	recent = []SSHGuardBlock{}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "]: Attack from ") {
			attacks++
			continue
		}
		m := reSSHGuardBlock.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		blocks++
		t, err := time.Parse("2006-01-02T15:04:05-07:00", m[1])
		if err != nil {
			continue
		}
		recent = append(recent, SSHGuardBlock{Time: t.UTC(), Address: strings.TrimSuffix(strings.TrimSuffix(m[2], "/32"), "/128"), For: m[3]})
	}
	slices.Reverse(recent)
	if len(recent) > sshguardMaxShown {
		recent = recent[:sshguardMaxShown]
	}
	return attacks, blocks, recent
}
