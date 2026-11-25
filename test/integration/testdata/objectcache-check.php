<?php
/**
 * Checks Layte's object cache against what WordPress expects of wp_cache_*.
 * Run with WP-CLI as the site user:  wp eval-file objectcache-check.php <phase>
 *
 * Phases: "api" (one request), "set" then "get" (across requests),
 * "expire-set" then "expire-get" (2 s TTL). Prints "OK" or the failures.
 */

$phase = isset( $args[0] ) ? $args[0] : 'api';
$fail  = array();
$ok    = function ( $cond, $what ) use ( &$fail ) {
	if ( ! $cond ) {
		$fail[] = $what;
	}
};

global $wp_object_cache;
$ok( $wp_object_cache instanceof WP_Object_Cache && method_exists( $wp_object_cache, 'is_connected' ), 'Layte cache class not loaded' );
$ok( method_exists( $wp_object_cache, 'is_connected' ) && $wp_object_cache->is_connected(), 'not connected to Redis' );
$ok( wp_using_ext_object_cache(), 'WordPress does not see an external object cache' );

/** Is the key really in Redis (not just in memory)? */
$in_redis = function ( $key, $group ) {
	$r = new Redis();
	$r->connect( LAYTE_REDIS_SOCKET );
	return (bool) $r->exists( LAYTE_REDIS_PREFIX . $group . ':' . $key );
};

switch ( $phase ) {
	case 'api':
		$g = 'layte_it';
		wp_cache_flush_group( $g );

		// set/get: scalars, arrays, false, null; found.
		$ok( wp_cache_set( 'k1', 'v1', $g ), 'set' );
		$ok( 'v1' === wp_cache_get( 'k1', $g ), 'get after set' );
		$ok( wp_cache_set( 'arr', array( 'a' => 1 ), $g ) && array( 'a' => 1 ) === wp_cache_get( 'arr', $g ), 'array' );
		wp_cache_set( 'f', false, $g );
		$found = null;
		$ok( false === wp_cache_get( 'f', $g, false, $found ) && true === $found, 'stored false is found' );
		wp_cache_get( 'missing', $g, false, $found );
		$ok( false === $found, 'missing is not found' );
		wp_cache_set( 'n', null, $g );
		$ok( null === wp_cache_get( 'n', $g, true, $found ) && true === $found, 'stored null (from Redis) is found' );
		$ok( 'v1' === wp_cache_get( 'k1', $g, true ), 'force reads from Redis' );
		$ok( $in_redis( 'k1', $g ), 'value is in Redis' );

		// Objects are copies.
		$o    = new stdClass();
		$o->v = 1;
		wp_cache_set( 'obj', $o, $g );
		$o->v = 2;
		$got  = wp_cache_get( 'obj', $g );
		$ok( 1 === $got->v, 'set stores a copy' );
		$got->v = 3;
		$ok( 1 === wp_cache_get( 'obj', $g )->v, 'get returns a copy' );

		// add / replace / delete.
		$ok( false === wp_cache_add( 'k1', 'x', $g ), 'add on existing fails' );
		$ok( true === wp_cache_add( 'k2', 'x', $g ), 'add on new works' );
		$ok( false === wp_cache_replace( 'nope', 'x', $g ), 'replace missing fails' );
		$ok( true === wp_cache_replace( 'k2', 'y', $g ) && 'y' === wp_cache_get( 'k2', $g, true ), 'replace existing' );
		$ok( true === wp_cache_delete( 'k2', $g ) && false === wp_cache_get( 'k2', $g, true ), 'delete existing' );
		$ok( false === wp_cache_delete( 'k2', $g ), 'delete missing returns false' );

		// incr / decr.
		wp_cache_set( 'c', 5, $g );
		$ok( 7 === wp_cache_incr( 'c', 2, $g ), 'incr' );
		$ok( 0 === wp_cache_decr( 'c', 10, $g ), 'decr stops at 0' );
		$ok( false === wp_cache_incr( 'nope', 1, $g ), 'incr missing fails' );
		$ok( 0 === wp_cache_get( 'c', $g, true ), 'incr result stored' );

		// *_multiple.
		$ok( array( 'm1' => true, 'm2' => true ) === wp_cache_set_multiple( array( 'm1' => 1, 'm2' => 2 ), $g ), 'set_multiple' );
		wp_cache_flush_runtime();
		$ok( array( 'm1' => 1, 'm2' => 2, 'm3' => false ) === wp_cache_get_multiple( array( 'm1', 'm2', 'm3' ), $g ), 'get_multiple from Redis' );
		$ok( array( 'm1' => false, 'm4' => true ) === wp_cache_add_multiple( array( 'm1' => 9, 'm4' => 4 ), $g ), 'add_multiple' );
		$ok( array( 'm1' => true, 'm9' => false ) === wp_cache_delete_multiple( array( 'm1', 'm9' ), $g ), 'delete_multiple' );

		// Keys: integers work, empty strings don't.
		$ok( wp_cache_set( 42, 'int', $g ) && 'int' === wp_cache_get( 42, $g, true ), 'integer key' );
		$ok( false === @wp_cache_set( '', 'x', $g ), 'empty key refused' );

		// Groups: default group, flush_group, non-persistent groups.
		wp_cache_set( 'dk', 'd' );
		$ok( 'd' === wp_cache_get( 'dk', 'default' ), 'empty group means default' );
		wp_cache_set( 'other', 'o', 'layte_it_other' );
		$ok( wp_cache_flush_group( $g ), 'flush_group' );
		$ok( false === wp_cache_get( 'k1', $g, true ), 'flush_group emptied the group' );
		$ok( 'o' === wp_cache_get( 'other', 'layte_it_other', true ), 'flush_group kept other groups' );
		wp_cache_add_non_persistent_groups( array( 'layte_it_np' ) );
		wp_cache_set( 'np', 'v', 'layte_it_np' );
		$ok( 'v' === wp_cache_get( 'np', 'layte_it_np' ), 'non-persistent in memory' );
		$ok( ! $in_redis( 'np', 'layte_it_np' ), 'non-persistent not in Redis' );

		// Supports.
		foreach ( array( 'add_multiple', 'set_multiple', 'get_multiple', 'delete_multiple', 'flush_runtime', 'flush_group' ) as $f ) {
			$ok( wp_cache_supports( $f ), "supports $f" );
		}

		// WordPress itself: options and transients through the cache.
		set_transient( 'layte_it_t', 'tv', 60 );
		$ok( 'tv' === get_transient( 'layte_it_t' ), 'transient' );
		$ok( $in_redis( 'layte_it_t', 'transient' ), 'transient kept in Redis' );
		$ok( '' !== get_option( 'siteurl' ), 'options load' );

		// flush empties Redis.
		$ok( wp_cache_flush() && false === wp_cache_get( 'other', 'layte_it_other', true ), 'flush' );
		break;

	case 'plugin':
		// The settings page (mu-plugin) loads, handles its form, and renders.
		$ok( function_exists( 'layte_oc_page' ), 'settings plugin not loaded' );
		$ok( false !== has_action( 'admin_post_layte_object_cache' ), 'form handler not registered' );
		$ok( layte_oc_flush_redis(), 'flush from the settings page' );
		$admins = get_users( array( 'role' => 'administrator', 'number' => 1 ) );
		if ( $admins ) {
			wp_set_current_user( $admins[0]->ID );
			ob_start();
			layte_oc_page();
			$html = ob_get_clean();
			$ok( false !== strpos( $html, 'Flush cache' ) && false !== strpos( $html, 'Disable object cache' ) && false !== strpos( $html, '<strong>On.</strong>' ), 'settings page renders (On, Flush cache, Disable)' );
		} else {
			$fail[] = 'no administrator to render the page as';
		}
		break;
	case 'set':
		wp_cache_set( 'persist', 'across requests', 'layte_it' );
		break;
	case 'get':
		$ok( 'across requests' === wp_cache_get( 'persist', 'layte_it' ), 'value persists across requests' );
		break;
	case 'expire-set':
		wp_cache_set( 'ttl', 'soon gone', 'layte_it', 2 );
		$ok( 'soon gone' === wp_cache_get( 'ttl', 'layte_it', true ), 'ttl value readable' );
		break;
	case 'expire-get':
		$ok( false === wp_cache_get( 'ttl', 'layte_it' ), 'expired after its TTL' );
		break;
	case 'paused':
		// Paused in WordPress: no Redis, and transients go to the database.
		$fail = array_values( array_filter( $fail, function ( $f ) {
			return 'not connected to Redis' !== $f && 'WordPress does not see an external object cache' !== $f;
		} ) );
		$ok( ! $wp_object_cache->is_connected(), 'paused but connected' );
		$ok( ! wp_using_ext_object_cache(), 'paused but WordPress uses the external cache' );
		set_transient( 'layte_it_pt', 'db', 60 );
		$ok( 'db' === get_option( '_transient_layte_it_pt' ), 'transient stored in the database while paused' );
		delete_transient( 'layte_it_pt' );
		break;
}

echo $fail ? 'FAIL: ' . implode( '; ', $fail ) : 'OK';
