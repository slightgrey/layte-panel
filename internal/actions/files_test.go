package actions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/fileops"
	"github.com/slightgrey/layte-panel/internal/system"
)

func TestHelperFileOpsRunsAsUser(t *testing.T) {
	fr := &system.FakeRunner{Handle: func(c system.Cmd) (system.Result, error) {
		return system.Result{Stdout: []byte(`{"size":3}`)}, nil
	}}
	h := helperFileOps{bin: "/usr/local/bin/layte", runner: fr}
	res, err := h.Run(ctx, system.Credential{UID: 1001, GID: 1001}, fileops.Request{Op: "list", Root: "/home/acme/a.test"})
	if err != nil || res.Size != 3 {
		t.Fatalf("%v %v", res, err)
	}
	c := fr.Cmds[0]
	var req fileops.Request
	json.Unmarshal(c.Stdin, &req)
	if c.Bin != "/usr/local/bin/layte" || strings.Join(c.Args, " ") != "fsop" || c.As == nil || c.As.UID != 1001 || req.Root != "/home/acme/a.test" {
		t.Errorf("cmd = %+v", c)
	}
	if _, err := h.Run(ctx, system.Credential{UID: 0}, fileops.Request{Op: "list"}); err == nil {
		t.Error("ran file operations as root")
	}
}

func TestFSJail(t *testing.T) {
	d := devDeps(t)
	// Not a Layte user → refused.
	d.FS.MkdirAll("/home/bob/x.test", 0o750)
	_, err := d.fsRead(ctx, agent.Actor{}, FSReadParams{Domain: "x.test", Username: "bob", Op: "list"})
	if err == nil {
		t.Error("browsed a non-Layte user's folder")
	}
	// Imported: only the web root nginx really uses for that domain.
	if _, err := d.fsRead(ctx, agent.Actor{}, FSReadParams{Domain: "blog.example.com", WebRoot: "/var/www/blog", Op: "list"}); err != nil {
		t.Errorf("imported web root refused: %v", err)
	}
	if _, err := d.fsRead(ctx, agent.Actor{}, FSReadParams{Domain: "blog.example.com", WebRoot: "/var/www/shop/public", Op: "list"}); err == nil {
		t.Error("accepted another site's web root")
	}
	// Imported sites are read-only even if a write request gets through.
	if _, err := d.fsWrite(ctx, agent.Actor{}, FSWriteParams{Domain: "blog.example.com", WebRoot: "/var/www/blog", Op: "delete", Path: "wp-config.php"}); err == nil {
		t.Error("wrote to an imported site")
	}
	for _, bad := range []FSReadParams{
		{Domain: "a.test", WebRoot: "/", Op: "list"},
		{Domain: "a.test", WebRoot: "/etc", Op: "list"},
		{Domain: "a.test", WebRoot: "/var/www", Op: "list"},
		{Domain: "a.test", Username: "acme", WebRoot: "/var/www/x", Op: "list"},
		{Domain: "a.test", Username: "acme", Op: "delete"},
		{Domain: "a.test", Username: "acme", Op: "list", Path: "../x"},
	} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}
