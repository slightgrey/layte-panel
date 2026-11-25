package actions

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

const (
	ufwBin               = "/usr/sbin/ufw"
	unattendedUpgradeBin = "/usr/bin/unattended-upgrade"
	authLog              = "/var/log/auth.log"
)

// alwaysOpen are ports Layte never lets you close (besides SSH): the panel
// itself is served by nginx on 80/443.
var alwaysOpen = []string{"80", "443"}

type UFWRule struct {
	Num    int    `json:"num"`
	To     string `json:"to"`     // "22/tcp", "80,443/tcp", "8000:8100/tcp"
	Action string `json:"action"` // ALLOW IN, DENY IN, LIMIT IN, …
	From   string `json:"from"`
	V6     bool   `json:"v6"`
	Text   string `json:"text"` // the rule as ufw prints it, to confirm deletes
	Locked bool   `json:"locked"`
}

type UFWStatus struct {
	Installed       bool      `json:"installed"`
	Active          bool      `json:"active"`
	DefaultIncoming string    `json:"default_incoming"`
	Rules           []UFWRule `json:"rules"`
}

type FailedLogin struct {
	IP    string   `json:"ip"`
	Count int      `json:"count"`
	Last  string   `json:"last"`
	Users []string `json:"users"`
}

type SecurityOverview struct {
	UFW            UFWStatus        `json:"ufw"`
	SSHPorts       []string         `json:"ssh_ports"`
	RebootRequired bool             `json:"reboot_required"`
	FailedLogins   []FailedLogin    `json:"failed_logins"`
	FailedTotal    int              `json:"failed_total"`
	Cloudflare     CloudflareStatus `json:"cloudflare"`
	SSHGuard       SSHGuardStatus   `json:"sshguard"`
}

var (
	reUFWRule    = regexp.MustCompile(`^\[\s*(\d+)\]\s+(.+?)\s{2,}((?:ALLOW|DENY|REJECT|LIMIT)(?: IN| OUT| FWD)?)\s{2,}(.+?)\s*$`)
	reUFWDefault = regexp.MustCompile(`Default:\s*(\w+) \(incoming\)`)
	reFailed     = regexp.MustCompile(`sshd\[\d+\]: (?:Failed (?:password|publickey) for (?:invalid user )?(\S+)|Invalid user (\S*)) from ([0-9a-fA-F.:]+)`)
	reSSHPort    = regexp.MustCompile(`(?m)^\s*Port\s+(\d+)\s*$`)
)

func (d Deps) securityOverview(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	ports := d.sshPorts()
	ufw, err := d.ufwStatus(ctx, ports)
	if err != nil {
		return nil, err
	}
	logins, total := d.failedLogins()
	reboot, _ := d.FS.Exists("/run/reboot-required")
	return SecurityOverview{UFW: ufw, SSHPorts: ports, RebootRequired: reboot, FailedLogins: logins, FailedTotal: total,
		Cloudflare: d.cloudflareStatus(), SSHGuard: d.sshguardStatus(ctx)}, nil
}

// sshPorts reads Port lines from sshd_config (and its .d drop-ins); default 22.
func (d Deps) sshPorts() []string {
	files, _ := d.FS.Glob("/etc/ssh/sshd_config.d/*.conf")
	files = append([]string{"/etc/ssh/sshd_config"}, files...)
	var ports []string
	for _, f := range files {
		b, err := d.FS.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range reSSHPort.FindAllStringSubmatch(string(b), -1) {
			if !slices.Contains(ports, m[1]) {
				ports = append(ports, m[1])
			}
		}
	}
	if len(ports) == 0 {
		ports = []string{"22"}
	}
	return ports
}

func (d Deps) ufwStatus(ctx context.Context, sshPorts []string) (UFWStatus, error) {
	st := UFWStatus{Rules: []UFWRule{}}
	verbose, err := d.Runner.Run(ctx, ufwBin, "status", "verbose")
	if errors.Is(err, system.ErrNotFound) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("ufw status: %s", verbose.Output())
	}
	st.Installed = true
	out := string(verbose.Stdout)
	st.Active = strings.Contains(out, "Status: active")
	if m := reUFWDefault.FindStringSubmatch(out); m != nil {
		st.DefaultIncoming = m[1]
	}
	if !st.Active {
		return st, nil
	}
	numbered, err := d.Runner.Run(ctx, ufwBin, "status", "numbered")
	if err != nil {
		return st, fmt.Errorf("ufw status numbered: %s", numbered.Output())
	}
	st.Rules = parseUFWNumbered(string(numbered.Stdout), sshPorts)
	return st, nil
}

func parseUFWNumbered(out string, sshPorts []string) []UFWRule {
	rules := []UFWRule{}
	for _, line := range strings.Split(out, "\n") {
		m := reUFWRule.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		to := strings.TrimSpace(m[2])
		r := UFWRule{Num: n, To: strings.TrimSuffix(to, " (v6)"), Action: m[3], From: m[4], V6: strings.HasSuffix(to, "(v6)")}
		r.Text = strings.Join(strings.Fields(m[2]+" "+m[3]+" "+m[4]), " ")
		r.Locked = Protects(r, sshPorts)
		rules = append(rules, r)
	}
	return rules
}

// Protects reports whether removing r could lock you out: an ALLOW rule from
// anywhere for SSH, 80 or 443.
func Protects(r UFWRule, sshPorts []string) bool {
	if !strings.HasPrefix(r.Action, "ALLOW") && !strings.HasPrefix(r.Action, "LIMIT") {
		return false
	}
	if !strings.HasPrefix(r.From, "Anywhere") {
		return false
	}
	portPart, _, _ := strings.Cut(r.To, "/")
	if strings.EqualFold(portPart, "OpenSSH") || strings.EqualFold(portPart, "Nginx Full") {
		return true
	}
	for _, p := range strings.Split(portPart, ",") {
		if slices.Contains(sshPorts, p) || slices.Contains(alwaysOpen, p) {
			return true
		}
	}
	return false
}

// failedLogins summarises failed SSH logins from the tail of auth.log.
func (d Deps) failedLogins() ([]FailedLogin, int) {
	_, size, err := d.FS.ReadRange(authLog, 0, 0)
	if err != nil {
		return []FailedLogin{}, 0
	}
	const window = 2 << 20
	start := size - window
	if start < 0 {
		start = 0
	}
	b, _, err := d.FS.ReadRange(authLog, start, window)
	if err != nil {
		return []FailedLogin{}, 0
	}
	byIP := map[string]*FailedLogin{}
	total := 0
	for _, line := range strings.Split(string(b), "\n") {
		m := reFailed.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		user, ip := m[1]+m[2], m[3]
		if net.ParseIP(ip) == nil {
			continue
		}
		total++
		f := byIP[ip]
		if f == nil {
			f = &FailedLogin{IP: ip, Users: []string{}}
			byIP[ip] = f
		}
		f.Count++
		f.Last = strings.Fields(line)[0]
		if user != "" && len(f.Users) < 5 && !slices.Contains(f.Users, user) {
			f.Users = append(f.Users, user)
		}
	}
	out := make([]FailedLogin, 0, len(byIP))
	for _, f := range byIP {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > 50 {
		out = out[:50]
	}
	return out, total
}

var rePortSpec = regexp.MustCompile(`^[0-9]{1,5}(:[0-9]{1,5})?$`)

type UFWAllowParams struct {
	Port  string `json:"port"`  // "8080" or "8000:8100"
	Proto string `json:"proto"` // tcp | udp | "" (both; single ports only)
	From  string `json:"from"`  // "" = anywhere, or an IP / CIDR
}

func (p UFWAllowParams) Validate() error {
	if !rePortSpec.MatchString(p.Port) {
		return errors.New("port must be a number or a range like 8000:8100")
	}
	for _, s := range strings.Split(p.Port, ":") {
		if n, _ := strconv.Atoi(s); n < 1 || n > 65535 {
			return errors.New("ports go from 1 to 65535")
		}
	}
	if p.Proto != "" && p.Proto != "tcp" && p.Proto != "udp" {
		return errors.New("protocol must be tcp or udp")
	}
	if strings.Contains(p.Port, ":") && p.Proto == "" {
		return errors.New("port ranges need a protocol (tcp or udp)")
	}
	if p.From != "" {
		if net.ParseIP(p.From) == nil {
			if _, _, err := net.ParseCIDR(p.From); err != nil {
				return errors.New("source must be an IP address or a network like 203.0.113.0/24")
			}
		}
	}
	return nil
}

// ufwAllow adds an allow rule: ufw allow [proto P] from SRC to any port N.
func (d Deps) ufwAllow(ctx context.Context, _ agent.Actor, p UFWAllowParams) (any, error) {
	args := []string{"allow"}
	if p.Proto != "" {
		args = append(args, "proto", p.Proto)
	}
	from := p.From
	if from == "" {
		from = "any"
	}
	args = append(args, "from", from, "to", "any", "port", p.Port)
	if res, err := d.Runner.Run(ctx, ufwBin, args...); err != nil {
		return nil, fmt.Errorf("ufw allow: %s", res.Output())
	}
	return map[string]bool{"added": true}, nil
}

type UFWDeleteParams struct {
	Num    int    `json:"num"`
	Expect string `json:"expect"` // rule text the user saw; must still match
}

func (p UFWDeleteParams) Validate() error {
	if p.Num < 1 || p.Num > 10000 || p.Expect == "" {
		return errors.New("rule number and text are required")
	}
	return nil
}

// ufwDelete removes one rule by number, after checking that the number still
// points at the rule the user saw (numbers shift when rules change) and that
// it isn't protecting SSH or the panel.
func (d Deps) ufwDelete(ctx context.Context, _ agent.Actor, p UFWDeleteParams) (any, error) {
	st, err := d.ufwStatus(ctx, d.sshPorts())
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(st.Rules, func(r UFWRule) bool { return r.Num == p.Num })
	if i < 0 || st.Rules[i].Text != p.Expect {
		return nil, agent.Errorf(agent.CodeInvalidParams, "the firewall rules changed; reload and try again")
	}
	if st.Rules[i].Locked {
		return nil, agent.Errorf(agent.CodeInvalidParams, "this rule keeps SSH or the panel reachable and can't be removed here")
	}
	if res, err := d.Runner.Run(ctx, ufwBin, "--force", "delete", strconv.Itoa(p.Num)); err != nil {
		return nil, fmt.Errorf("ufw delete: %s", res.Output())
	}
	return map[string]bool{"deleted": true}, nil
}

type UFWEnableParams struct {
	Enable bool `json:"enable"`
}

func (p UFWEnableParams) Validate() error { return nil }

// ufwSetEnabled turns the firewall on (after making sure SSH, 80 and 443 are
// allowed, so you can't lock yourself out) or off.
func (d Deps) ufwSetEnabled(ctx context.Context, _ agent.Actor, p UFWEnableParams) (any, error) {
	if !p.Enable {
		if res, err := d.Runner.Run(ctx, ufwBin, "disable"); err != nil {
			return nil, fmt.Errorf("ufw disable: %s", res.Output())
		}
		return map[string]bool{"active": false}, nil
	}
	for _, port := range append(d.sshPorts(), alwaysOpen...) {
		if res, err := d.Runner.Run(ctx, ufwBin, "allow", port+"/tcp"); err != nil {
			return nil, fmt.Errorf("ufw allow %s: %s", port, res.Output())
		}
	}
	if res, err := d.Runner.Run(ctx, ufwBin, "--force", "enable"); err != nil {
		return nil, fmt.Errorf("ufw enable: %s", res.Output())
	}
	return map[string]bool{"active": true}, nil
}

// updatesInstall installs pending security updates with unattended-upgrade
// (it follows /etc/apt/apt.conf.d/50unattended-upgrades: security by default).
func (u *updates) install(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	// Outside the agent's sandbox, like every package install (see
	// outsideSandbox): a kernel security update must be able to write /lib/modules.
	res, err := u.deps.Runner.RunCmd(ctx, outsideSandbox(system.Cmd{
		Bin:  unattendedUpgradeBin,
		Args: []string{"-v"},
		Env:  []string{"DEBIAN_FRONTEND=noninteractive"},
	}))
	u.mu.Lock()
	u.cached = nil // the pending list changed
	u.mu.Unlock()
	if errors.Is(err, system.ErrNotFound) {
		return nil, agent.Errorf(agent.CodeNotFound, "unattended-upgrades is not installed (apt install unattended-upgrades)")
	}
	if err != nil {
		return nil, fmt.Errorf("unattended-upgrade: %s", failure(res, err, 8))
	}
	return map[string]string{"output": lastLines(res.Output(), 15)}, nil
}
