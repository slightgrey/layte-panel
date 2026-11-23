package actions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/fileops"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// One-click WP-Admin: the agent makes a one-time login token (random, valid
// 60 seconds, single use) and a small must-use plugin in the site exchanges
// it for a normal WordPress login cookie. Only a SHA-256 of the token is
// stored (as a transient); the token travels in a POST body, never a URL.

const (
	wpLoginPluginRel = "wp-content/mu-plugins/layte-login.php"
	wpLoginTTL       = "60" // seconds
)

// wpLoginPlugin is the must-use plugin Layte installs. It reacts only to a
// POST with a well-formed layte_login token, consumes the token before using
// it, and only logs in users who can manage the site.
const wpLoginPlugin = `<?php
// Managed by Layte Panel — do not edit by hand (one-click WP-Admin login).
/**
 * Plugin Name: Layte Panel one-click login
 * Description: Lets Layte Panel sign its admin in with a one-time token (valid 60 seconds, single use).
 */
defined( 'ABSPATH' ) || exit;

add_action( 'init', function () {
	if ( 'POST' !== ( $_SERVER['REQUEST_METHOD'] ?? '' ) || empty( $_POST['layte_login'] ) ) {
		return;
	}
	$token = (string) wp_unslash( $_POST['layte_login'] );
	if ( ! preg_match( '/^[a-f0-9]{64}$/', $token ) ) {
		return;
	}
	$key     = 'layte_login_' . substr( hash( 'sha256', $token ), 0, 40 );
	$user_id = (int) get_transient( $key );
	// Single use: whoever deletes it first wins.
	if ( $user_id <= 0 || ! delete_transient( $key ) ) {
		wp_die( esc_html__( 'This login link has expired. Open WP-Admin from Layte Panel again.' ), '', array( 'response' => 403 ) );
	}
	$user = get_user_by( 'id', $user_id );
	if ( ! $user || ! user_can( $user, 'manage_options' ) ) {
		wp_die( esc_html__( 'This account can no longer sign in this way.' ), '', array( 'response' => 403 ) );
	}
	wp_set_current_user( $user_id );
	wp_set_auth_cookie( $user_id, false, is_ssl() );
	do_action( 'wp_login', $user->user_login, $user );
	wp_safe_redirect( admin_url() );
	exit;
}, 1 );
`

// WPLoginParams name a WordPress site created or adopted by Layte.
type WPLoginParams struct {
	Domain     string `json:"domain"`
	Username   string `json:"username"`
	WebRoot    string `json:"web_root,omitempty"` // adopted sites
	PHPVersion string `json:"php_version"`
}

func (p WPLoginParams) Validate() error {
	if err := validDomain(p.Domain); err != nil {
		return err
	}
	if err := validate.SiteUsername(p.Username); err != nil {
		return err
	}
	if p.WebRoot != "" {
		if err := (FSParams{Domain: p.Domain, Username: p.Username, WebRoot: p.WebRoot, Op: "stat", Path: "."}).validate(readOps); err != nil {
			return err
		}
	}
	return validate.PHPVersion(p.PHPVersion, []string{p.PHPVersion})
}

// WPLogin is what the panel needs to sign in: POST token to ActionURL.
type WPLogin struct {
	ActionURL string `json:"action_url"`
	Token     string `json:"token"` // one-time; never logged
	UserID    int    `json:"user_id"`
}

// wpLoginToken makes sure the login plugin is installed, picks the site's
// first administrator, and stores a fresh one-time token for them.
func (d Deps) wpLoginToken(ctx context.Context, _ agent.Actor, p WPLoginParams) (any, error) {
	if err := d.requireInstalledPHP(p.PHPVersion); err != nil {
		return nil, err
	}
	root, as, _, err := d.jail(ctx, FSParams{Domain: p.Domain, Username: p.Username, WebRoot: p.WebRoot})
	if err != nil {
		return nil, err
	}
	// Layte sites keep WordPress in public/; adopted sites in their web root.
	prefix, wpPath := "public/", root+"/public"
	if p.WebRoot != "" {
		prefix, wpPath = "", root
	}
	file := func(req fileops.Request) (fileops.Response, error) {
		req.Root = root
		res, err := d.FileOps.Run(ctx, as, req)
		if err == nil && res.Error != "" {
			err = agent.Errorf("fs_error", "%s", res.Error)
		}
		return res, err
	}
	if st, err := file(fileops.Request{Op: "stat", Path: prefix + "wp-content"}); err != nil || st.Entry == nil || st.Entry.Type != "dir" {
		return nil, agent.Errorf(agent.CodeNotFound, "%s has no wp-content folder; is WordPress installed?", p.Domain)
	}

	// Install (or update) the must-use plugin, as the site user. A file of the
	// same name that Layte didn't write is never replaced.
	plugin := prefix + wpLoginPluginRel
	if cur, err := file(fileops.Request{Op: "read", Path: plugin, Length: 64 << 10}); err == nil {
		if string(cur.Data) != wpLoginPlugin {
			if !strings.Contains(string(cur.Data), "Managed by Layte Panel") {
				return nil, agent.Errorf(agent.CodeInvalidParams, "%s exists and wasn't written by Layte Panel", plugin)
			}
			if _, err := file(fileops.Request{Op: "write", Path: plugin, Chunk: []byte(wpLoginPlugin), Final: true}); err != nil {
				return nil, err
			}
		}
	} else {
		file(fileops.Request{Op: "mkdir", Path: prefix + "wp-content/mu-plugins"}) // may already exist
		if _, err := file(fileops.Request{Op: "write", Path: plugin, Chunk: []byte(wpLoginPlugin), Final: true}); err != nil {
			return nil, err
		}
	}

	wp := func(args ...string) (string, error) {
		phar, err := d.WPCLI.Ensure(ctx)
		if err != nil {
			return "", err
		}
		res, err := d.Runner.RunCmd(ctx, system.Cmd{
			Bin:  phpBin(p.PHPVersion),
			Args: append([]string{phar, "--path=" + wpPath, "--skip-plugins", "--skip-themes"}, args...),
			Env:  []string{"HOME=/home/" + p.Username},
			Dir:  root,
			As:   &as,
		})
		if err != nil {
			return "", fmt.Errorf("wp %s: %s", args[0], failure(res, err, 3))
		}
		return strings.TrimSpace(string(res.Stdout)), nil
	}
	out, err := wp("user", "list", "--role=administrator", "--field=ID", "--orderby=ID", "--order=ASC", "--number=1")
	if err != nil {
		return nil, err
	}
	userID, _ := strconv.Atoi(strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]))
	if userID <= 0 {
		return nil, agent.Errorf(agent.CodeNotFound, "%s has no administrator account", p.Domain)
	}

	raw := make([]byte, 32)
	rand.Read(raw) // crypto/rand.Read never fails (Go 1.24+)
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	key := "layte_login_" + hex.EncodeToString(sum[:])[:40]
	if _, err := wp("transient", "set", key, strconv.Itoa(userID), wpLoginTTL); err != nil {
		return nil, err
	}

	scheme := "http"
	if site, err := d.discoveredSite(ctx, p.Domain); err == nil && site.SSL {
		scheme = "https"
	}
	return WPLogin{ActionURL: scheme + "://" + p.Domain + "/wp-login.php", Token: token, UserID: userID}, nil
}
