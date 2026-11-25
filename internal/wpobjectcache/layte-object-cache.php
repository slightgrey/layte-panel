<?php
/**
 * Plugin Name: Layte Object Cache
 * Description: Managed by Layte Panel. Settings → Object Cache: flush the cache, or disable/enable it. Turned on and off for good in Layte Panel.
 *
 * @package Layte_Object_Cache
 */

defined( 'ABSPATH' ) || exit;

const LAYTE_OC_PAGE = 'layte-object-cache';

/** The pause flag (the cache code checks it on every request). */
function layte_oc_flag() {
	return WP_CONTENT_DIR . '/mu-plugins/layte-object-cache/.paused';
}

/** Empty the site's own Redis directly (used when resuming: the current request isn't connected). */
function layte_oc_flush_redis() {
	if ( ! class_exists( 'Redis' ) || ! defined( 'LAYTE_REDIS_SOCKET' ) ) {
		return false;
	}
	try {
		$redis = new Redis();
		if ( ! $redis->connect( LAYTE_REDIS_SOCKET, 0, 1.0 ) ) {
			return false;
		}
		$ok = $redis->flushDB();
		$redis->close();
		return (bool) $ok;
	} catch ( Exception $e ) {
		return false;
	}
}

add_action(
	'admin_menu',
	function () {
		add_options_page( 'Object Cache', 'Object Cache', 'manage_options', LAYTE_OC_PAGE, 'layte_oc_page' );
	}
);

add_action(
	'admin_post_layte_object_cache',
	function () {
		if ( ! current_user_can( 'manage_options' ) ) {
			wp_die( esc_html__( 'You are not allowed to do this.' ), 403 );
		}
		check_admin_referer( 'layte_object_cache' );
		$do     = isset( $_POST['do'] ) ? sanitize_key( wp_unslash( $_POST['do'] ) ) : '';
		$result = 'error';
		switch ( $do ) {
			case 'flush':
				$result = wp_cache_flush() ? 'flushed' : 'error';
				break;
			case 'disable':
				// phpcs:ignore WordPress.WP.AlternativeFunctions.file_system_operations_file_put_contents -- a flag file next to the cache code.
				$result = false !== @file_put_contents( layte_oc_flag(), "paused in WordPress\n" ) ? 'disabled' : 'error';
				break;
			case 'enable':
				// Data in Redis may be stale after a pause: start empty.
				layte_oc_flush_redis();
				$result = ( ! file_exists( layte_oc_flag() ) || @unlink( layte_oc_flag() ) ) ? 'enabled' : 'error'; // phpcs:ignore WordPress.WP.AlternativeFunctions.unlink_unlink
				break;
		}
		wp_safe_redirect( add_query_arg( array( 'page' => LAYTE_OC_PAGE, 'layte-oc' => $result ), admin_url( 'options-general.php' ) ) );
		exit;
	}
);

/** The settings page: status, Flush cache, Disable/Enable. */
function layte_oc_page() {
	if ( ! current_user_can( 'manage_options' ) ) {
		return;
	}
	global $wp_object_cache;
	$paused    = file_exists( layte_oc_flag() );
	$connected = is_object( $wp_object_cache ) && method_exists( $wp_object_cache, 'is_connected' ) && $wp_object_cache->is_connected();
	$messages  = array(
		'flushed'  => 'Cache flushed.',
		'disabled' => 'Object cache disabled. WordPress no longer uses Redis; Layte Panel shows it as paused.',
		'enabled'  => 'Object cache enabled again (it started empty).',
		'error'    => 'That didn\'t work. Check the site\'s PHP error log, or manage the object cache in Layte Panel.',
	);
	$result = isset( $_GET['layte-oc'] ) ? sanitize_key( wp_unslash( $_GET['layte-oc'] ) ) : ''; // phpcs:ignore WordPress.Security.NonceVerification -- only picks a message.
	?>
	<div class="wrap">
		<h1>Object Cache</h1>
		<?php if ( isset( $messages[ $result ] ) ) : ?>
			<div class="notice <?php echo 'error' === $result ? 'notice-error' : 'notice-success'; ?> is-dismissible"><p><?php echo esc_html( $messages[ $result ] ); ?></p></div>
		<?php endif; ?>
		<p>
			<?php if ( $paused ) : ?>
				<strong>Disabled.</strong> WordPress isn't using its object cache. The site works as usual, without the speed-up.
			<?php elseif ( $connected ) : ?>
				<strong>On.</strong> WordPress keeps its database results in this site's own Redis.
			<?php else : ?>
				<strong>Not connected.</strong> Redis isn't reachable right now; WordPress works as usual without it. Check the Object cache card in Layte Panel.
			<?php endif; ?>
		</p>
		<form method="post" action="<?php echo esc_url( admin_url( 'admin-post.php' ) ); ?>" style="display:flex;gap:8px;flex-wrap:wrap">
			<input type="hidden" name="action" value="layte_object_cache" />
			<?php wp_nonce_field( 'layte_object_cache' ); ?>
			<?php if ( ! $paused ) : ?>
				<button type="submit" name="do" value="flush" class="button button-primary" <?php disabled( ! $connected ); ?>>Flush cache</button>
				<button type="submit" name="do" value="disable" class="button">Disable object cache</button>
			<?php else : ?>
				<button type="submit" name="do" value="enable" class="button button-primary">Enable object cache</button>
			<?php endif; ?>
		</form>
		<p class="description">Managed by Layte Panel. To remove the object cache completely, turn it off in Layte Panel.</p>
	</div>
	<?php
}
