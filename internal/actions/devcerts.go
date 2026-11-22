package actions

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/fs"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/system"
)

// writeFakeCert creates a self-signed certificate in the certbot layout
// (live/<name>/{cert,chain,fullchain,privkey}.pem). Dev mode and tests only.
func writeFakeCert(fsys system.FS, name string, domains []string, validFor time.Duration) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: domains[0]},
		Issuer:       pkix.Name{CommonName: "Fake LE (dev)"},
		DNSNames:     domains,
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(validFor),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	dir := letsencryptLive + "/" + name
	if err := fsys.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for file, data := range map[string][]byte{"cert.pem": certPEM, "chain.pem": certPEM, "fullchain.pem": certPEM, "privkey.pem": keyPEM} {
		var perm fs.FileMode = 0o644
		if file == "privkey.pem" {
			perm = 0o600
		}
		if err := fsys.WriteFileAtomic(dir+"/"+file, data, perm); err != nil {
			return err
		}
	}
	return nil
}

// fakeCertbot mimics `certbot certonly` and `certbot revoke` in dev mode.
func fakeCertbot(root system.FS, c system.Cmd) (system.Result, error) {
	if len(c.Args) == 0 {
		return system.Result{}, fmt.Errorf("certbot: no command")
	}
	arg := func(flag string) string {
		for i, a := range c.Args {
			if a == flag && i+1 < len(c.Args) {
				return c.Args[i+1]
			}
		}
		return ""
	}
	switch c.Args[0] {
	case "certonly":
		domains := strings.Split(arg("--domains"), ",")
		if err := writeFakeCert(root, arg("--cert-name"), domains, 90*24*time.Hour); err != nil {
			return system.Result{}, err
		}
		if err := writeFakeRenewalConf(root, arg("--cert-name")); err != nil {
			return system.Result{}, err
		}
		time.Sleep(500 * time.Millisecond) // pretend to talk to Let's Encrypt
		return system.Result{Stdout: []byte("Successfully received certificate.\n")}, nil
	case "renew":
		name := arg("--cert-name")
		time.Sleep(500 * time.Millisecond)
		if slices.Contains(c.Args, "--dry-run") {
			return system.Result{Stdout: []byte("Congratulations, all simulated renewals succeeded.\n")}, nil
		}
		b, err := root.ReadFile(letsencryptLive + "/" + name + "/cert.pem")
		if err != nil {
			return system.Result{}, err
		}
		cert, err := parseCert(b)
		if err != nil {
			return system.Result{}, err
		}
		return system.Result{Stdout: []byte("Congratulations, all renewals succeeded.\n")}, writeFakeCert(root, name, cert.DNSNames, 90*24*time.Hour)
	case "revoke":
		root.Remove(letsencryptRenewal + "/" + arg("--cert-name") + ".conf")
		return system.Result{}, root.RemoveAll(letsencryptLive + "/" + arg("--cert-name"))
	}
	return system.Result{}, fmt.Errorf("certbot %s: not faked", c.Args[0])
}

// writeFakeRenewalConf mimics certbot's renewal/<name>.conf, which is what
// makes certbot's timer renew a certificate.
func writeFakeRenewalConf(root system.FS, name string) error {
	if err := root.MkdirAll(letsencryptRenewal, 0o755); err != nil {
		return err
	}
	return root.WriteFileAtomic(letsencryptRenewal+"/"+name+".conf", []byte("# fake renewal config (dev)\n"), 0o644)
}

// seedFakeCerts gives the imported Certbot site a cert that expires soon,
// so the "expiring" state is visible in dev.
func seedFakeCerts(fs system.FS) error {
	if ok, _ := fs.Exists(letsencryptLive + "/blog.example.com/cert.pem"); ok {
		return nil
	}
	if err := writeFakeCert(fs, "blog.example.com", []string{"blog.example.com", "www.blog.example.com"}, 20*24*time.Hour); err != nil {
		return err
	}
	return writeFakeRenewalConf(fs, "blog.example.com")
}
