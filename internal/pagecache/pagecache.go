// Package pagecache holds a site's page cache settings (nginx FastCGI cache
// for WordPress sites Layte created) and where its files live.
package pagecache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Settings are a site's page cache settings.
type Settings struct {
	Enabled    bool `json:"enabled,omitempty"`
	TTLMinutes int  `json:"ttl_minutes,omitempty"` // how long a cached page stays fresh; 0 = DefaultTTL
}

const (
	DefaultTTL = 60   // minutes
	maxTTL     = 1440 // a day
	// Root is the folder that holds one cache folder per site.
	Root = "/var/cache/nginx/layte"
	// MarkerName is the file the site's must-use plugin creates in the
	// site's tmp/ folder (<site>/tmp, or /home/<user>/tmp for adopted sites)
	// when content changes; the agent then purges.
	MarkerName = "layte-purge-cache"
	// PluginRel is the must-use plugin, relative to the WordPress folder.
	PluginRel = "wp-content/mu-plugins/layte-page-cache.php"
)

// Validate checks the expiry (whole minutes, 1 to 1440).
func (s Settings) Validate() error {
	if s.TTLMinutes != 0 && (s.TTLMinutes < 1 || s.TTLMinutes > maxTTL) {
		return fmt.Errorf("cache expiry must be between 1 and %d minutes", maxTTL)
	}
	return nil
}

// TTL is the effective expiry in minutes.
func (s Settings) TTL() int {
	if s.TTLMinutes == 0 {
		return DefaultTTL
	}
	return s.TTLMinutes
}

// Dir is the site's cache folder. domain must be validated.
func Dir(domain string) string { return Root + "/" + domain }

// Zone is the nginx keys_zone name for the site: readable, plus a short hash
// so "a-b.com" and "a.b.com" never share a name.
func Zone(domain string) string {
	sum := sha256.Sum256([]byte(domain))
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, domain)
	if len(safe) > 40 {
		safe = safe[:40]
	}
	return "layte_" + safe + "_" + hex.EncodeToString(sum[:])[:8]
}

// Plugin is the must-use plugin Layte installs while the cache is on. It
// only touches a marker file in the site's own tmp/ folder (WordPress can't
// delete nginx's cache files); the agent sees it and purges the cache.
const Plugin = `<?php
/**
 * Plugin Name: Layte Page Cache
 * Description: Managed by Layte Panel. Asks the panel to clear this site's page cache when content changes. Removed when the page cache is turned off.
 */

if ( ! defined( 'ABSPATH' ) ) {
	exit;
}

function layte_page_cache_purge() {
	static $done = false;
	if ( $done ) {
		return;
	}
	$done = true;
	// The marker goes to the site's tmp/ folder (never public): Layte's PHP
	// pool sets sys_temp_dir to it. Without it (WP-CLI), <site>/public/ is
	// ABSPATH, so <site>/tmp/ is next to it.
	$dir = ini_get( 'sys_temp_dir' );
	if ( ! $dir ) {
		$dir = dirname( ABSPATH ) . '/tmp';
	}
	@touch( rtrim( $dir, '/' ) . '/` + MarkerName + `' );
}

add_action(
	'save_post',
	function ( $post_id ) {
		if ( wp_is_post_revision( $post_id ) || wp_is_post_autosave( $post_id ) ) {
			return;
		}
		layte_page_cache_purge();
	}
);

foreach ( array(
	'deleted_post',
	'trashed_post',
	'untrashed_post',
	'comment_post',
	'edit_comment',
	'transition_comment_status',
	'switch_theme',
	'customize_save_after',
	'wp_update_nav_menu',
	'activated_plugin',
	'deactivated_plugin',
	'upgrader_process_complete',
	'update_option_permalink_structure',
	'update_option_blogname',
	'update_option_blogdescription',
	'woocommerce_product_set_stock',
) as $layte_hook ) {
	add_action( $layte_hook, 'layte_page_cache_purge' );
}
`
