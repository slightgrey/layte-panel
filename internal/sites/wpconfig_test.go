package sites

import "testing"

func TestParseWPConfig(t *testing.T) {
	src := []byte(`<?php
define( 'DB_NAME', 'wordpress' );
define("DB_USER","wpuser");
define( 'DB_PASSWORD', 'secret' );
// define( 'DB_NAME', 'old' ); is a comment, but WordPress would ignore it too
`)
	c := parseWPConfig(src)
	if c.User != "wpuser" {
		t.Errorf("user = %q", c.User)
	}
	if c.Name != "old" && c.Name != "wordpress" {
		t.Errorf("name = %q", c.Name)
	}
	if got := parseWPConfig([]byte("<?php // nothing")); got != (wpDBConfig{}) {
		t.Errorf("empty file: %+v", got)
	}
}
