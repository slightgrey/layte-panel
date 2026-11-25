<?php
/**
 * Layte Object Cache: WordPress's object cache in the site's own Redis.
 *
 * Managed by Layte Panel. Loaded by wp-content/object-cache.php (the drop-in),
 * before any plugin. Redis is reached on a Unix socket only this site's user
 * can open (LAYTE_REDIS_SOCKET); keys start with LAYTE_REDIS_PREFIX.
 *
 * If Redis can't be reached, or the cache is paused in WordPress (Settings →
 * Object Cache), this works like WordPress's own cache: in memory, for the
 * current request only. A cache problem never takes the site down.
 *
 * @package Layte_Object_Cache
 */

defined( 'ABSPATH' ) || exit;

if ( ! defined( 'LAYTE_OBJECT_CACHE_DIR' ) ) {
	define( 'LAYTE_OBJECT_CACHE_DIR', __DIR__ );
}

/**
 * Is the object cache paused in WordPress? (A flag file next to this one.)
 */
function layte_object_cache_paused() {
	return file_exists( LAYTE_OBJECT_CACHE_DIR . '/.paused' );
}

// phpcs:disable WordPress.NamingConventions -- the API names are WordPress's.

class WP_Object_Cache {

	/** @var array Values for this request: [group][key] => value. */
	public $cache = array();

	/** @var array Groups shared by every site of a multisite network. */
	public $global_groups = array();

	/** @var array Groups kept in memory only, never in Redis. */
	public $non_persistent_groups = array();

	/** @var string Blog prefix (multisite), e.g. "2:". */
	public $blog_prefix = '';

	/** @var bool Whether this is a multisite install. */
	public $multisite = false;

	/** @var int Requests served from memory or Redis (counted like core). */
	public $cache_hits = 0;

	/** @var int Requests that found nothing. */
	public $cache_misses = 0;

	/** @var Redis|null Connection; null when Redis isn't used this request. */
	private $redis = null;

	/** @var string Key prefix of this site. */
	private $prefix = '';

	public function __construct() {
		$this->multisite   = is_multisite();
		$this->blog_prefix = $this->multisite ? get_current_blog_id() . ':' : '';
		$this->prefix      = defined( 'LAYTE_REDIS_PREFIX' ) ? (string) LAYTE_REDIS_PREFIX : '';

		if ( layte_object_cache_paused() || ! class_exists( 'Redis' ) || ! defined( 'LAYTE_REDIS_SOCKET' ) ) {
			return;
		}
		try {
			$redis = new Redis();
			if ( $redis->connect( LAYTE_REDIS_SOCKET, 0, 1.0 ) ) {
				$redis->setOption( Redis::OPT_READ_TIMEOUT, 1.0 );
				$this->redis = $redis;
			}
		} catch ( Exception $e ) {
			$this->redis = null;
		}
	}

	/** Whether values are kept in Redis this request. */
	public function is_connected() {
		return null !== $this->redis;
	}

	/** Redis stopped answering: carry on in memory for the rest of the request. */
	private function lost() {
		$this->redis = null;
	}

	private function persistent( $group ) {
		return null !== $this->redis && ! isset( $this->non_persistent_groups[ $group ] );
	}

	private function group( $group ) {
		return ( null === $group || '' === $group ) ? 'default' : (string) $group;
	}

	private function valid_key( $key ) {
		if ( is_int( $key ) ) {
			return true;
		}
		if ( is_string( $key ) && '' !== trim( $key ) ) {
			return true;
		}
		$type = gettype( $key );
		if ( function_exists( '_doing_it_wrong' ) ) {
			_doing_it_wrong(
				esc_html( __METHOD__ ),
				'string' === $type ? 'Cache key must not be an empty string.' : esc_html( "Cache key must be an integer or a non-empty string, $type given." ),
				'6.1.0'
			);
		}
		return false;
	}

	/** Redis key for a cache key and group. */
	private function rkey( $key, $group ) {
		$blog = isset( $this->global_groups[ $group ] ) ? '' : $this->blog_prefix;
		return $this->prefix . $blog . $group . ':' . $key;
	}

	/** Objects are copied in and out, so callers can't change cached values by reference. */
	private function copy( $value ) {
		return is_object( $value ) ? clone $value : $value;
	}

	private function in_memory( $key, $group ) {
		return isset( $this->cache[ $group ] ) && ( isset( $this->cache[ $group ][ $key ] ) || array_key_exists( $key, $this->cache[ $group ] ) );
	}

	/** Look the key up in memory, then Redis. Returns [found, value]. */
	private function lookup( $key, $group ) {
		if ( $this->in_memory( $key, $group ) ) {
			return array( true, $this->cache[ $group ][ $key ] );
		}
		if ( ! $this->persistent( $group ) ) {
			return array( false, false );
		}
		try {
			$raw = $this->redis->get( $this->rkey( $key, $group ) );
		} catch ( Exception $e ) {
			$this->lost();
			return array( false, false );
		}
		if ( false === $raw ) {
			return array( false, false );
		}
		$value                          = unserialize( $raw ); // phpcs:ignore WordPress.PHP.DiscouragedPHPFunctions -- our own data.
		$this->cache[ $group ][ $key ] = $value;
		return array( true, $value );
	}

	private function store( $key, $data, $group, $expire ) {
		$this->cache[ $group ][ $key ] = $data;
		if ( ! $this->persistent( $group ) ) {
			return true;
		}
		try {
			$raw    = serialize( $data ); // phpcs:ignore WordPress.PHP.DiscouragedPHPFunctions
			$expire = (int) $expire;
			$ok     = $expire > 0 ? $this->redis->setex( $this->rkey( $key, $group ), $expire, $raw ) : $this->redis->set( $this->rkey( $key, $group ), $raw );
			return (bool) $ok;
		} catch ( Exception $e ) {
			$this->lost();
			return true; // kept in memory
		}
	}

	public function add( $key, $data, $group = 'default', $expire = 0 ) {
		if ( function_exists( 'wp_suspend_cache_addition' ) && wp_suspend_cache_addition() ) {
			return false;
		}
		if ( ! $this->valid_key( $key ) ) {
			return false;
		}
		$group = $this->group( $group );
		list( $found ) = $this->lookup( $key, $group );
		if ( $found ) {
			return false;
		}
		return $this->set( $key, $data, $group, $expire );
	}

	public function add_multiple( array $data, $group = '', $expire = 0 ) {
		$out = array();
		foreach ( $data as $key => $value ) {
			$out[ $key ] = $this->add( $key, $value, $group, $expire );
		}
		return $out;
	}

	public function replace( $key, $data, $group = 'default', $expire = 0 ) {
		if ( ! $this->valid_key( $key ) ) {
			return false;
		}
		$group = $this->group( $group );
		list( $found ) = $this->lookup( $key, $group );
		if ( ! $found ) {
			return false;
		}
		return $this->set( $key, $data, $group, $expire );
	}

	public function set( $key, $data, $group = 'default', $expire = 0 ) {
		if ( ! $this->valid_key( $key ) ) {
			return false;
		}
		return $this->store( $key, $this->copy( $data ), $this->group( $group ), $expire );
	}

	public function set_multiple( array $data, $group = '', $expire = 0 ) {
		$out = array();
		foreach ( $data as $key => $value ) {
			$out[ $key ] = $this->set( $key, $value, $group, $expire );
		}
		return $out;
	}

	public function get( $key, $group = 'default', $force = false, &$found = null ) {
		if ( ! $this->valid_key( $key ) ) {
			$found = false;
			return false;
		}
		$group = $this->group( $group );
		if ( $force ) {
			unset( $this->cache[ $group ][ $key ] );
		}
		list( $found, $value ) = $this->lookup( $key, $group );
		if ( $found ) {
			++$this->cache_hits;
			return $this->copy( $value );
		}
		++$this->cache_misses;
		return false;
	}

	public function get_multiple( $keys, $group = 'default', $force = false ) {
		$group  = $this->group( $group );
		$values = array();
		$fetch  = array();
		foreach ( $keys as $key ) {
			if ( ! $this->valid_key( $key ) ) {
				$values[ $key ] = false;
				continue;
			}
			if ( $force ) {
				unset( $this->cache[ $group ][ $key ] );
			}
			if ( $this->in_memory( $key, $group ) ) {
				++$this->cache_hits;
				$values[ $key ] = $this->copy( $this->cache[ $group ][ $key ] );
			} else {
				$values[ $key ] = false;
				$fetch[]        = $key;
			}
		}
		if ( $fetch && $this->persistent( $group ) ) {
			try {
				$raws = $this->redis->mget( array_map( function ( $k ) use ( $group ) {
					return $this->rkey( $k, $group );
				}, $fetch ) );
			} catch ( Exception $e ) {
				$this->lost();
				$raws = array();
			}
			foreach ( $fetch as $i => $key ) {
				if ( isset( $raws[ $i ] ) && false !== $raws[ $i ] ) {
					$value                          = unserialize( $raws[ $i ] ); // phpcs:ignore WordPress.PHP.DiscouragedPHPFunctions
					$this->cache[ $group ][ $key ] = $value;
					$values[ $key ]                 = $this->copy( $value );
					++$this->cache_hits;
				} else {
					++$this->cache_misses;
				}
			}
		} else {
			$this->cache_misses += count( $fetch );
		}
		return $values;
	}

	public function delete( $key, $group = 'default', $deprecated = false ) {
		if ( ! $this->valid_key( $key ) ) {
			return false;
		}
		$group = $this->group( $group );
		list( $found ) = $this->lookup( $key, $group );
		unset( $this->cache[ $group ][ $key ] );
		if ( $this->persistent( $group ) ) {
			try {
				$this->redis->del( $this->rkey( $key, $group ) );
			} catch ( Exception $e ) {
				$this->lost();
			}
		}
		return $found;
	}

	public function delete_multiple( array $keys, $group = '' ) {
		$out = array();
		foreach ( $keys as $key ) {
			$out[ $key ] = $this->delete( $key, $group );
		}
		return $out;
	}

	private function change( $key, $offset, $group, $sign ) {
		if ( ! $this->valid_key( $key ) ) {
			return false;
		}
		$group = $this->group( $group );
		list( $found, $value ) = $this->lookup( $key, $group );
		if ( ! $found ) {
			return false;
		}
		if ( ! is_numeric( $value ) ) {
			$value = 0;
		}
		$value = (int) $value + $sign * (int) $offset;
		if ( $value < 0 ) {
			$value = 0;
		}
		$this->store( $key, $value, $group, 0 );
		return $value;
	}

	public function incr( $key, $offset = 1, $group = 'default' ) {
		return $this->change( $key, $offset, $group, 1 );
	}

	public function decr( $key, $offset = 1, $group = 'default' ) {
		return $this->change( $key, $offset, $group, -1 );
	}

	/** Empties the cache: memory and the site's own Redis (no other site uses it). */
	public function flush() {
		$this->cache = array();
		if ( null === $this->redis ) {
			return true;
		}
		try {
			return (bool) $this->redis->flushDB();
		} catch ( Exception $e ) {
			$this->lost();
			return false;
		}
	}

	public function flush_runtime() {
		$this->cache = array();
		return true;
	}

	public function flush_group( $group ) {
		$group = $this->group( $group );
		unset( $this->cache[ $group ] );
		if ( ! $this->persistent( $group ) ) {
			return true;
		}
		$pattern = $this->rkey( '*', $group );
		try {
			$it = null;
			do {
				$keys = $this->redis->scan( $it, $pattern, 500 );
				if ( $keys ) {
					$this->redis->del( $keys );
				}
			} while ( $it > 0 );
		} catch ( Exception $e ) {
			$this->lost();
			return false;
		}
		return true;
	}

	public function add_global_groups( $groups ) {
		foreach ( (array) $groups as $group ) {
			$this->global_groups[ $group ] = true;
		}
	}

	public function add_non_persistent_groups( $groups ) {
		foreach ( (array) $groups as $group ) {
			$this->non_persistent_groups[ $group ] = true;
		}
	}

	public function switch_to_blog( $blog_id ) {
		$this->blog_prefix = $this->multisite ? (int) $blog_id . ':' : '';
	}

	public function reset() {
		// Deprecated in core: clears non-global groups of the current request.
		foreach ( array_keys( $this->cache ) as $group ) {
			if ( ! isset( $this->global_groups[ $group ] ) ) {
				unset( $this->cache[ $group ] );
			}
		}
	}

	public function close() {
		if ( null !== $this->redis ) {
			try {
				$this->redis->close();
			} catch ( Exception $e ) {
				// nothing to do
			}
		}
		return true;
	}

	public function stats() {
		echo '<p><strong>Cache Hits:</strong> ' . (int) $this->cache_hits . '<br /><strong>Cache Misses:</strong> ' . (int) $this->cache_misses . '</p>';
	}
}

function wp_cache_init() {
	$GLOBALS['wp_object_cache'] = new WP_Object_Cache();
	// Not connected (paused, or Redis unavailable): tell WordPress there's no
	// external cache, so transients stay in the database instead of only in
	// this request's memory.
	if ( ! $GLOBALS['wp_object_cache']->is_connected() && function_exists( 'wp_using_ext_object_cache' ) ) {
		wp_using_ext_object_cache( false );
	}
}

function wp_cache_add( $key, $data, $group = '', $expire = 0 ) {
	global $wp_object_cache;
	return $wp_object_cache->add( $key, $data, $group, (int) $expire );
}

function wp_cache_add_multiple( array $data, $group = '', $expire = 0 ) {
	global $wp_object_cache;
	return $wp_object_cache->add_multiple( $data, $group, $expire );
}

function wp_cache_replace( $key, $data, $group = '', $expire = 0 ) {
	global $wp_object_cache;
	return $wp_object_cache->replace( $key, $data, $group, (int) $expire );
}

function wp_cache_set( $key, $data, $group = '', $expire = 0 ) {
	global $wp_object_cache;
	return $wp_object_cache->set( $key, $data, $group, (int) $expire );
}

function wp_cache_set_multiple( array $data, $group = '', $expire = 0 ) {
	global $wp_object_cache;
	return $wp_object_cache->set_multiple( $data, $group, $expire );
}

function wp_cache_get( $key, $group = '', $force = false, &$found = null ) {
	global $wp_object_cache;
	return $wp_object_cache->get( $key, $group, $force, $found );
}

function wp_cache_get_multiple( $keys, $group = '', $force = false ) {
	global $wp_object_cache;
	return $wp_object_cache->get_multiple( $keys, $group, $force );
}

function wp_cache_delete( $key, $group = '' ) {
	global $wp_object_cache;
	return $wp_object_cache->delete( $key, $group );
}

function wp_cache_delete_multiple( array $keys, $group = '' ) {
	global $wp_object_cache;
	return $wp_object_cache->delete_multiple( $keys, $group );
}

function wp_cache_incr( $key, $offset = 1, $group = '' ) {
	global $wp_object_cache;
	return $wp_object_cache->incr( $key, $offset, $group );
}

function wp_cache_decr( $key, $offset = 1, $group = '' ) {
	global $wp_object_cache;
	return $wp_object_cache->decr( $key, $offset, $group );
}

function wp_cache_flush() {
	global $wp_object_cache;
	return $wp_object_cache->flush();
}

function wp_cache_flush_runtime() {
	global $wp_object_cache;
	return $wp_object_cache->flush_runtime();
}

function wp_cache_flush_group( $group ) {
	global $wp_object_cache;
	return $wp_object_cache->flush_group( $group );
}

function wp_cache_supports( $feature ) {
	switch ( $feature ) {
		case 'add_multiple':
		case 'set_multiple':
		case 'get_multiple':
		case 'delete_multiple':
		case 'flush_runtime':
		case 'flush_group':
			return true;
		default:
			return false;
	}
}

function wp_cache_close() {
	global $wp_object_cache;
	return $wp_object_cache->close();
}

function wp_cache_add_global_groups( $groups ) {
	global $wp_object_cache;
	$wp_object_cache->add_global_groups( $groups );
}

function wp_cache_add_non_persistent_groups( $groups ) {
	global $wp_object_cache;
	$wp_object_cache->add_non_persistent_groups( $groups );
}

function wp_cache_switch_to_blog( $blog_id ) {
	global $wp_object_cache;
	$wp_object_cache->switch_to_blog( $blog_id );
}

function wp_cache_reset() {
	global $wp_object_cache;
	$wp_object_cache->reset();
}
