package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/slightgrey/layte-panel/internal/auth"
	"github.com/slightgrey/layte-panel/internal/store"
)

// runAdmin handles `layte admin create|reset-password|reset-2fa --username NAME`.
// In production run it as the layte user so the DB stays owned by it:
//
//	sudo -u layte layte admin create --username alice
func runAdmin(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: layte admin create|reset-password|reset-2fa --username NAME")
	}
	sub, args := args[0], args[1:]

	fs := flag.NewFlagSet("admin "+sub, flag.ExitOnError)
	cf := addConfigFlags(fs)
	username := fs.String("username", "", "admin username")
	fs.Parse(args)

	if err := auth.ValidateUsername(*username); err != nil {
		return err
	}
	cfg, err := cf.load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	st, err := store.Open(ctx, cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()
	kr, err := auth.LoadOrCreateKey(cfg.SecretKeyFile)
	if err != nil {
		return err
	}
	in := newPrompter()

	switch sub {
	case "create":
		if _, err := st.UserByUsername(ctx, *username); err == nil {
			return fmt.Errorf("user %q already exists", *username)
		}
		hash, err := in.newPassword()
		if err != nil {
			return err
		}
		secretEnc, err := in.enrollTOTP(kr, *username)
		if err != nil {
			return err
		}
		if _, err := st.CreateUser(ctx, *username, hash, secretEnc); err != nil {
			return err
		}
		fmt.Printf("\nAdmin %q created.\n", *username)

	case "reset-password":
		u, err := st.UserByUsername(ctx, *username)
		if err != nil {
			return fmt.Errorf("user %q: %w", *username, err)
		}
		hash, err := in.newPassword()
		if err != nil {
			return err
		}
		if err := st.SetPassword(ctx, u.ID, hash); err != nil {
			return err
		}
		fmt.Println("Password changed; all sessions for this user were logged out and any lockout cleared.")

	case "reset-2fa":
		u, err := st.UserByUsername(ctx, *username)
		if err != nil {
			return fmt.Errorf("user %q: %w", *username, err)
		}
		secretEnc, err := in.enrollTOTP(kr, *username)
		if err != nil {
			return err
		}
		if err := st.SetTOTPSecret(ctx, u.ID, secretEnc); err != nil {
			return err
		}
		fmt.Println("Two-factor secret replaced; all sessions for this user were logged out.")

	default:
		return fmt.Errorf("unknown admin command %q", sub)
	}
	return nil
}

// prompter reads answers from the terminal (hidden for passwords), or line by
// line from stdin when it's piped (for scripted installs).
type prompter struct {
	tty    bool
	reader *bufio.Reader
}

func newPrompter() *prompter {
	return &prompter{tty: term.IsTerminal(int(os.Stdin.Fd())), reader: bufio.NewReader(os.Stdin)}
}

func (p *prompter) line(prompt string, hidden bool) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if p.tty && hidden {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	s, err := p.reader.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimRight(s, "\r\n"), nil
}

func (p *prompter) newPassword() (string, error) {
	pw, err := p.line(fmt.Sprintf("New password (min %d chars): ", auth.MinPasswordLen), true)
	if err != nil {
		return "", err
	}
	if err := auth.ValidatePassword(pw); err != nil {
		return "", err
	}
	again, err := p.line("Repeat password: ", true)
	if err != nil {
		return "", err
	}
	if pw != again {
		return "", errors.New("passwords do not match")
	}
	return auth.HashPassword(pw)
}

// enrollTOTP creates a secret, shows it, and requires one valid code before
// returning it encrypted — so nobody gets locked out by a mistyped secret.
func (p *prompter) enrollTOTP(kr *auth.Keyring, username string) ([]byte, error) {
	enr, err := auth.NewTOTP(username)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, `
Two-factor authentication is required. Add this account to your
authenticator app (1Password, Bitwarden, Google Authenticator, ...):

  Secret: %s
  URI:    %s

`, enr.Secret, enr.URL)
	for attempt := 0; attempt < 3; attempt++ {
		code, err := p.line("Enter the 6-digit code from the app: ", false)
		if err != nil {
			return nil, err
		}
		if _, ok := auth.CheckTOTP(enr.Secret, strings.TrimSpace(code), time.Now(), 0); ok {
			return kr.Seal([]byte(enr.Secret), []byte(username))
		}
		fmt.Fprintln(os.Stderr, "That code didn't match; check the device clock and try again.")
	}
	return nil, errors.New("TOTP verification failed")
}
