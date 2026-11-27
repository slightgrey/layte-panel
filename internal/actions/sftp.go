package actions

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"math/big"
	"path"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

// SFTP access per site: an SFTP-only login, jailed to the site's folder,
// that works as the site's own user (so files keep the right owner).
//
//   - Layte's sshd drop-in (sftpConfigPath) applies only to members of the
//     group layte-sftp: chroot to /srv/layte-sftp/<user>, internal-sftp only
//     (no shell, no forwarding), keys from a root-owned file. Members of
//     layte-sftp-password may also log in with a password. The main
//     sshd_config and every other login are left alone.
//   - The jail /srv/layte-sftp/<user> is root-owned (sshd requires that) and
//     holds only the site folder, bind-mounted by a Layte systemd .mount unit
//     so it survives reboots: /home/<user>/<domain> for sites Layte created,
//     the web root for adopted ones.
//   - Subdomain sites share their main site's Linux user, so one login sees
//     them all: each extra site folder gets its own mount in the same jail
//     (unit layte-sftp-<user>.<domain>.service; a username has no dots).
//   - sshd is reloaded, never restarted (open sessions stay), and every
//     change is tested with sshd -t first.

const (
	sftpGroup         = "layte-sftp"
	sftpPasswordGroup = "layte-sftp-password"
	sftpConfigPath    = "/etc/ssh/sshd_config.d/zz-layte-sftp.conf"
	sftpKeysDir       = "/etc/ssh/layte-sftp"
	sftpJailRoot      = "/srv/layte-sftp"
	systemdUnitDir    = "/etc/systemd/system"
	sshdBin           = "/usr/sbin/sshd"
	groupaddBin       = "/usr/sbin/groupadd"
	chpasswdBin       = "/usr/sbin/chpasswd"
	mountpointBin     = "/usr/bin/mountpoint"
	mountBin          = "/usr/bin/mount"
	umountBin         = "/usr/bin/umount"
	// MaxSFTPKeys is how many keys one site may have.
	MaxSFTPKeys = 20
)

// sftpRules are the restrictions for every SFTP login (both Match blocks).
const sftpRules = `    ChrootDirectory ` + sftpJailRoot + `/%u
    ForceCommand internal-sftp -u 0022
    AuthorizedKeysFile ` + sftpKeysDir + `/%u
    KbdInteractiveAuthentication no
    AllowTcpForwarding no
    AllowAgentForwarding no
    AllowStreamLocalForwarding no
    X11Forwarding no
    PermitTunnel no
    PermitTTY no
`

// sftpConfig is the sshd drop-in. sshd uses the first value it finds for
// each setting, so the password group's block comes first.
const sftpConfig = system.Marker + ` — do not edit by hand
# SFTP logins for sites (site → Files & SFTP). Members of ` + sftpGroup + ` are
# jailed to their site's folder with SFTP only; members of ` + sftpPasswordGroup + `
# may also use a password. No other login is affected.
Match Group ` + sftpPasswordGroup + `
    PasswordAuthentication yes
` + sftpRules + `
Match Group ` + sftpGroup + `
    PasswordAuthentication no
` + sftpRules

var (
	sshdTest   = system.Cmd{Bin: sshdBin, Args: []string{"-t"}}
	sshdReload = system.Cmd{Bin: systemctl, Args: []string{"reload", "ssh"}}
)

// SFTPParams name a site Layte manages (created: Username; adopted:
// Username + WebRoot) and, for sftp.set, its keys and password option.
type SFTPParams struct {
	Domain   string   `json:"domain"`
	Username string   `json:"username"`
	WebRoot  string   `json:"web_root,omitempty"`
	Keys     []string `json:"keys,omitempty"`     // authorized_keys lines (normalised)
	Password bool     `json:"password,omitempty"` // allow password logins
	// Extra are the user's other sites (subdomain sites Layte created), whose
	// folders the same login reaches. sftp.set removes mounts not listed.
	Extra []string `json:"extra,omitempty"`
}

// MaxSFTPSites is how many folders one SFTP login may hold.
const MaxSFTPSites = 101

func (p SFTPParams) Validate() error {
	if p.Username == "" {
		return errors.New("username is required")
	}
	if err := (FSParams{Domain: p.Domain, Username: p.Username, WebRoot: p.WebRoot, Op: "stat", Path: "."}).validate(readOps); err != nil {
		return err
	}
	if len(p.Keys) > MaxSFTPKeys {
		return fmt.Errorf("at most %d keys per site", MaxSFTPKeys)
	}
	if len(p.Extra) >= MaxSFTPSites {
		return fmt.Errorf("at most %d sites per SFTP login", MaxSFTPSites)
	}
	for _, dom := range p.Extra {
		if p.WebRoot != "" {
			return errors.New("adopted sites have no extra sites")
		}
		if err := validDomain(dom); err != nil {
			return err
		}
		if dom == p.Domain {
			return errors.New("an extra site repeats the main one")
		}
	}
	for _, k := range p.Keys {
		n, _, err := NormalizeSSHKey(k)
		if err != nil {
			return err
		}
		if n != k {
			return errors.New("keys must be in normalised form")
		}
	}
	return nil
}

// NormalizeSSHKey parses one public key (as in authorized_keys) and returns
// it as "type base64 [comment]" without options, plus its SHA256 fingerprint.
// Only ed25519, ecdsa and RSA keys of 2048 bits or more are accepted.
func NormalizeSSHKey(line string) (string, string, error) {
	line = strings.TrimSpace(line)
	if strings.ContainsAny(line, "\r\n") {
		return "", "", errors.New("one key per entry")
	}
	pub, comment, options, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return "", "", fmt.Errorf("not an SSH public key (paste the .pub file's line, e.g. ssh-ed25519 AAAA… you@pc)")
	}
	if len(options) > 0 {
		return "", "", errors.New("key options (like command=…) aren't allowed")
	}
	switch pub.Type() {
	case ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521,
		ssh.KeyAlgoSKED25519, ssh.KeyAlgoSKECDSA256:
	case ssh.KeyAlgoRSA:
		if ck, ok := pub.(ssh.CryptoPublicKey); ok {
			if rk, ok := ck.CryptoPublicKey().(*rsa.PublicKey); ok && rk.N.BitLen() < 2048 {
				return "", "", errors.New("RSA keys must have at least 2048 bits (ed25519 is better)")
			}
		}
	default:
		return "", "", fmt.Errorf("key type %s isn't allowed (use ed25519)", pub.Type())
	}
	out := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	// A comment is just a label: keep printable, harmless characters only.
	comment = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(comment))
	if comment != "" {
		if len(comment) > 100 {
			comment = comment[:100]
		}
		out += " " + comment
	}
	return out, ssh.FingerprintSHA256(pub), nil
}

// sftpTarget resolves the folder the login reaches, checked like the file
// manager does (Layte site user; adopted: nginx serves the domain from the
// web root and its Layte pool runs as the user).
func (d Deps) sftpTarget(ctx context.Context, p SFTPParams) (string, error) {
	root, _, readOnly, err := d.jail(ctx, FSParams{Domain: p.Domain, Username: p.Username, WebRoot: p.WebRoot})
	if err != nil {
		return "", err
	}
	if readOnly {
		return "", agent.Errorf(agent.CodeInvalidParams, "%s is read-only here", p.Domain)
	}
	if cred, err := d.Users.Lookup(p.Username); err != nil || cred.UID < 1000 {
		return "", agent.Errorf(agent.CodeInvalidParams, "%s is not a site user", p.Username)
	}
	return root, nil
}

func sftpJail(user string) string               { return sftpJailRoot + "/" + user }
func sftpMountPoint(user, domain string) string { return sftpJail(user) + "/" + domain }

// sftpUnit is the systemd service that bind-mounts the site folder into
// the jail (a .mount unit's name would have to encode the path).
func sftpUnit(user string) string { return "layte-sftp-" + user + ".service" }

// sftpExtraUnit mounts one of the user's other sites. Usernames have no
// dots, so the "<user>." prefix only matches that user's units.
func sftpExtraUnit(user, domain string) string {
	return "layte-sftp-" + user + "." + domain + ".service"
}

// sftpExtraUnits lists the extra-site units installed for user, by domain.
func (d Deps) sftpExtraUnits(user string) (map[string]string, error) {
	prefix := "layte-sftp-" + user + "."
	paths, err := d.FS.Glob(systemdUnitDir + "/" + prefix + "*.service")
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, p := range paths {
		name := path.Base(p)
		domain := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".service")
		if validDomain(domain) == nil {
			out[domain] = name
		}
	}
	return out, nil
}

// mountSFTP installs and starts the unit that bind-mounts root at where.
func (d Deps) mountSFTP(ctx context.Context, unit, domain, root, where string) error {
	if err := d.FS.MkdirAll(where, 0o755); err != nil {
		return err
	}
	if !safeUnitPath.MatchString(root) || !safeUnitPath.MatchString(where) {
		return agent.Errorf(agent.CodeInvalidParams, "SFTP needs a folder path without spaces or special characters (%s)", root)
	}
	if err := d.FS.MkdirAll(systemdUnitDir, 0o755); err != nil {
		return err
	}
	err := system.WriteConfig(ctx, d.FS, d.Runner, system.WriteSpec{
		Path: systemdUnitDir + "/" + unit, Content: []byte(sftpUnitContent(domain, root, where)),
		Reload: system.Cmd{Bin: systemctl, Args: []string{"daemon-reload"}},
	})
	if err != nil {
		return err
	}
	if res, err := d.Runner.Run(ctx, systemctl, "enable", "--now", unit); err != nil {
		return fmt.Errorf("start %s: %s", unit, failure(res, err, 3))
	}
	return nil
}

// unmountSFTP stops and removes a mount unit, then the empty mount point.
func (d Deps) unmountSFTP(ctx context.Context, unit, where string) error {
	unitPath := systemdUnitDir + "/" + unit
	if ok, _ := d.FS.Exists(unitPath); ok {
		if res, err := d.Runner.Run(ctx, systemctl, "disable", "--now", unit); err != nil {
			return fmt.Errorf("stop %s: %s", unit, failure(res, err, 3))
		}
		daemonReload := system.Cmd{Bin: systemctl, Args: []string{"daemon-reload"}}
		if err := system.RemoveConfig(ctx, d.FS, d.Runner, system.RemoveSpec{Path: unitPath, Test: daemonReload, Reload: daemonReload}); err != nil {
			return err
		}
	}
	// Only remove the mount point once nothing is mounted there.
	if _, err := d.Runner.Run(ctx, mountpointBin, "-q", where); err != nil {
		d.FS.Remove(where) // empty directory
	}
	return nil
}

// safeUnitPath: systemd splits ExecStart on spaces and expands % and \, so
// the paths in the unit may only use plain characters.
var safeUnitPath = regexp.MustCompile(`^/[A-Za-z0-9._@+/-]+$`)

func sftpUnitContent(domain, what, where string) string {
	return system.Marker + ` — do not edit by hand
# Puts ` + domain + `'s folder into its SFTP jail (Layte Panel).
[Unit]
Description=Layte SFTP folder for ` + domain + `
After=local-fs.target
ConditionPathIsMountPoint=!` + where + `

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=` + mountBin + ` --bind ` + what + ` ` + where + `
ExecStop=` + umountBin + ` ` + where + `

[Install]
WantedBy=multi-user.target
`
}

// SFTPResult describes a site's SFTP login after sftp.set.
type SFTPResult struct {
	Username string   `json:"username"`
	Folder   string   `json:"folder"`            // what the login sees, e.g. /example.com
	Folders  []string `json:"folders,omitempty"` // with the extra sites: /example.com, /blog.example.com
	Warnings []string `json:"warnings,omitempty"`
}

// sftpSet turns SFTP on for the site, or updates its keys and password
// option. Idempotent.
func (d Deps) sftpSet(ctx context.Context, _ agent.Actor, p SFTPParams) (any, error) {
	root, err := d.sftpTarget(ctx, p)
	if err != nil {
		return nil, err
	}
	run := func(bin string, args ...string) error {
		if res, err := d.Runner.Run(ctx, bin, args...); err != nil {
			return fmt.Errorf("%s %s: %s", bin, strings.Join(args, " "), failure(res, err, 3))
		}
		return nil
	}

	// 1. Groups, and the user in them.
	for _, g := range []string{sftpGroup, sftpPasswordGroup} {
		if err := run(groupaddBin, "--force", "--system", g); err != nil {
			return nil, err
		}
	}
	if err := run(gpasswdBin, "--add", p.Username, sftpGroup); err != nil {
		return nil, err
	}

	// 2. Keys (root-owned: the site can't add its own).
	if err := d.FS.MkdirAll(sftpKeysDir, 0o755); err != nil {
		return nil, err
	}
	keys := strings.Join(p.Keys, "\n")
	if keys != "" {
		keys += "\n"
	}
	if err := d.FS.WriteFileAtomic(sftpKeysDir+"/"+p.Username, []byte(keys), 0o644); err != nil {
		return nil, fmt.Errorf("write keys: %w", err)
	}

	// 3. The jail and the bind mount of each site folder.
	jail := sftpJail(p.Username)
	for _, dir := range []string{sftpJailRoot, jail} {
		if err := d.FS.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		if err := d.FS.Chown(dir, 0, 0); err != nil {
			return nil, err
		}
		if err := d.FS.Chmod(dir, 0o755); err != nil {
			return nil, err
		}
	}
	if err := d.mountSFTP(ctx, sftpUnit(p.Username), p.Domain, root, sftpMountPoint(p.Username, p.Domain)); err != nil {
		return nil, err
	}
	folders := []string{"/" + p.Domain}
	installed, err := d.sftpExtraUnits(p.Username)
	if err != nil {
		return nil, err
	}
	for _, dom := range p.Extra {
		extraRoot, err := d.sftpTarget(ctx, SFTPParams{Domain: dom, Username: p.Username})
		if err != nil {
			return nil, err
		}
		if err := d.mountSFTP(ctx, sftpExtraUnit(p.Username, dom), dom, extraRoot, sftpMountPoint(p.Username, dom)); err != nil {
			return nil, err
		}
		folders = append(folders, "/"+dom)
		delete(installed, dom)
	}
	for dom, unit := range installed { // sites no longer on the login
		if err := d.unmountSFTP(ctx, unit, sftpMountPoint(p.Username, dom)); err != nil {
			return nil, err
		}
	}

	// 4. Password logins only when asked; otherwise the password is locked.
	if p.Password {
		if err := run(gpasswdBin, "--add", p.Username, sftpPasswordGroup); err != nil {
			return nil, err
		}
	} else {
		d.Runner.Run(ctx, gpasswdBin, "--delete", p.Username, sftpPasswordGroup) // not a member: fine
		if err := run(usermodBin, "--lock", p.Username); err != nil {
			return nil, err
		}
	}

	// 5. sshd: Layte's drop-in (sshd -t, reload; restored if the test fails).
	if err := system.WriteConfig(ctx, d.FS, d.Runner, system.WriteSpec{
		Path: sftpConfigPath, Content: []byte(sftpConfig), Test: sshdTest, Reload: sshdReload,
	}); err != nil {
		return nil, configError(err)
	}
	return SFTPResult{Username: p.Username, Folder: "/" + p.Domain, Folders: folders, Warnings: d.sftpWarnings(ctx, p.Username)}, nil
}

// sftpWarnings: sshd settings of the admin's that would block the login.
func (d Deps) sftpWarnings(ctx context.Context, user string) []string {
	res, err := d.Runner.Run(ctx, sshdBin, "-T", "-C", "user="+user+",host=localhost,addr=127.0.0.1")
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), " ")
		if (k == "allowusers" || k == "allowgroups") && v != "" && !slices.Contains(strings.Fields(v), user) && !slices.Contains(strings.Fields(v), sftpGroup) {
			out = append(out, fmt.Sprintf("your sshd_config sets %s %s, which keeps %s out; add %s there", k, v, user, sftpGroup))
		}
	}
	return out
}

// sftpDisable turns SFTP off for the site (idempotent).
func (d Deps) sftpDisable(ctx context.Context, _ agent.Actor, p SFTPParams) (any, error) {
	if _, err := d.Users.Lookup(p.Username); err != nil {
		return nil, err
	}
	if ok, _ := d.FS.Exists("/home/" + p.Username + "/" + siteUserMarker); !ok {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s is not a Layte site user", p.Username)
	}
	// Lock and leave the groups first, so no new login starts while tearing down.
	d.Runner.Run(ctx, usermodBin, "--lock", p.Username)
	for _, g := range []string{sftpPasswordGroup, sftpGroup} {
		d.Runner.Run(ctx, gpasswdBin, "--delete", p.Username, g)
	}
	d.FS.Remove(sftpKeysDir + "/" + p.Username)

	extra, err := d.sftpExtraUnits(p.Username)
	if err != nil {
		return nil, err
	}
	for dom, unit := range extra {
		if err := d.unmountSFTP(ctx, unit, sftpMountPoint(p.Username, dom)); err != nil {
			return nil, err
		}
	}
	if err := d.unmountSFTP(ctx, sftpUnit(p.Username), sftpMountPoint(p.Username, p.Domain)); err != nil {
		return nil, err
	}
	d.FS.Remove(sftpJail(p.Username)) // only once empty
	return map[string]bool{"disabled": true}, nil
}

// passwordAlphabet avoids look-alikes (0/O, 1/l/I).
const passwordAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newPassword(n int) (string, error) {
	b := make([]byte, n)
	max := big.NewInt(int64(len(passwordAlphabet)))
	for i := range b {
		x, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = passwordAlphabet[x.Int64()]
	}
	return string(b), nil
}

// SFTPPassword is returned once by sftp.password; it is never stored or logged.
type SFTPPassword struct {
	Password string `json:"password"`
}

// sftpPassword sets a new random password for the site's SFTP login (only
// while password logins are on) and returns it.
func (d Deps) sftpPassword(ctx context.Context, _ agent.Actor, p SFTPParams) (any, error) {
	if _, err := d.sftpTarget(ctx, p); err != nil {
		return nil, err
	}
	res, err := d.Runner.Run(ctx, "/usr/bin/id", "-nG", p.Username)
	if err != nil || !slices.Contains(strings.Fields(string(res.Stdout)), sftpPasswordGroup) {
		return nil, agent.Errorf(agent.CodeInvalidParams, "password logins are off for %s", p.Domain)
	}
	pw, err := newPassword(24)
	if err != nil {
		return nil, err
	}
	// Through stdin: the password never appears in a process list.
	if r, err := d.Runner.RunCmd(ctx, system.Cmd{Bin: chpasswdBin, Stdin: []byte(p.Username + ":" + pw + "\n")}); err != nil {
		return nil, fmt.Errorf("chpasswd: %s", failure(r, err, 3))
	}
	return SFTPPassword{Password: pw}, nil
}
