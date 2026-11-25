<?php
/**
 * Plugin Name: Layte Object Cache (drop-in)
 * Description: Managed by Layte Panel. Loads Layte's Redis object cache from wp-content/mu-plugins/layte-object-cache/. Turn it off in Layte Panel.
 *
 * WordPress loads this file before any plugin. If Layte's cache code is
 * missing, nothing is defined here, and WordPress uses its own cache.
 *
 * @package Layte_Object_Cache
 */

defined( 'ABSPATH' ) || exit;

if ( is_readable( WP_CONTENT_DIR . '/mu-plugins/layte-object-cache/cache.php' ) ) {
	require_once WP_CONTENT_DIR . '/mu-plugins/layte-object-cache/cache.php';
}
