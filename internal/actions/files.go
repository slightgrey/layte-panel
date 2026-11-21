package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/fileops"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/templates"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// FileOps runs a file operation as a given user.
type FileOps interface {
	Run(ctx context.Context, as system.Credential, req fileops.Request) (fileops.Response, error)
}

// helperFileOps re-executes this binary as `layte fsop` under the site
// user's uid/gid. The kernel then enforces that user's permissions.
type helperFileOps struct {
	bin    string
	runner system.Runner
}

func (h helperFileOps) Run(ctx context.Context, as system.Credential, req fileops.Request) (fileops.Response, error) {
	if as.UID == 0 {
		return fileops.Response{}, errors.New("refusing to run file operations as root")
	}
	in, err := json.Marshal(req)
	if err != nil {
		return fileops.Response{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := h.runner.RunCmd(ctx, system.Cmd{Bin: h.bin, Args: []string{"fsop"}, Stdin: in, As: &as})
	if err != nil {
		return fileops.Response{}, fmt.Errorf("file helper: %s", failure(res, err, 3))
	}
	var out fileops.Response
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return fileops.Response{}, fmt.Errorf("file helper: bad response")
	}
	return out, nil
}

// localFileOps runs in-process on the dev fake root (no user switching).
type localFileOps struct{ base string }

func (l localFileOps) Run(_ context.Context, _ system.Credential, req fileops.Request) (fileops.Response, error) {
	req.Root = filepath.Join(l.base, filepath.FromSlash(req.Root))
	return fileops.Do(req), nil
}

// FSParams address a file in a site's folder.
//   - Layte sites: Username is set; the jail is /home/<user>/<domain>, and
//     operations run as that user.
//   - Imported sites: WebRoot is set (read-only); it must be the root of an
//     enabled nginx vhost for Domain, and operations run as the folder's owner
//     (www-data if root owns it).
type FSParams struct {
	Domain   string `json:"domain"`
	Username string `json:"username,omitempty"`
	WebRoot  string `json:"web_root,omitempty"`
	Op       string `json:"op"`
	Path     string `json:"path"`
	To       string `json:"to,omitempty"`
	Offset   int64  `json:"offset,omitempty"`
	Length   int    `json:"length,omitempty"`
	Chunk    []byte `json:"chunk,omitempty"`
	Final    bool   `json:"final,omitempty"`
}

var (
	readOps  = []string{"list", "read", "stat"}
	writeOps = []string{"write", "mkdir", "delete", "rename"}
	// Imported web roots must live in a normal web folder.
	webRootPrefixes = []string{"/var/www/", "/srv/", "/home/", "/opt/", "/usr/share/nginx/"}
)

func (p FSParams) validate(ops []string) error {
	if !slices.Contains(ops, p.Op) {
		return fmt.Errorf("operation %q not allowed here", p.Op)
	}
	if err := validDomain(p.Domain); err != nil {
		return err
	}
	if (p.Username == "") == (p.WebRoot == "") {
		return errors.New("exactly one of username or web_root is required")
	}
	if p.Username != "" {
		if err := validate.SiteUsername(p.Username); err != nil {
			return err
		}
	} else {
		if path.Clean(p.WebRoot) != p.WebRoot || !slices.ContainsFunc(webRootPrefixes, func(pre string) bool { return strings.HasPrefix(p.WebRoot+"/", pre) && p.WebRoot+"/" != pre }) {
			return fmt.Errorf("web root %s is not in a web folder", p.WebRoot)
		}
	}
	if _, err := fileops.CleanPath(p.Path); err != nil {
		return err
	}
	if p.Op == "rename" {
		if _, err := fileops.CleanPath(p.To); err != nil {
			return err
		}
	}
	if len(p.Chunk) > fileops.MaxChunk || p.Offset < 0 {
		return errors.New("invalid chunk")
	}
	return nil
}

// FSReadParams / FSWriteParams are the same fields as two types, so reads
// (fs.read) aren't audited and writes (fs.write) are.
type FSReadParams FSParams
type FSWriteParams FSParams

func (p FSReadParams) Validate() error  { return FSParams(p).validate(readOps) }
func (p FSWriteParams) Validate() error { return FSParams(p).validate(writeOps) }

// jail works out the folder and the user for a request.
func (d Deps) jail(ctx context.Context, p FSParams) (root string, as system.Credential, readOnly bool, err error) {
	if p.Username != "" {
		if ok, _ := d.FS.Exists("/home/" + p.Username + "/" + siteUserMarker); !ok {
			return "", as, false, agent.Errorf(agent.CodeInvalidParams, "%s is not a Layte site user", p.Username)
		}
		root = templates.Site{Domain: p.Domain, User: p.Username}.Dir()
		if fi, err := d.FS.Lstat(root); err != nil || !fi.IsDir() {
			return "", as, false, agent.Errorf(agent.CodeNotFound, "site folder %s not found", root)
		}
		as, err = d.Users.Lookup(p.Username)
		return root, as, false, err
	}

	// Imported: the web root must really belong to this domain in nginx.
	out, err := d.sitesDiscover(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		return "", as, true, err
	}
	found := slices.ContainsFunc(out.([]DiscoveredSite), func(s DiscoveredSite) bool {
		return s.Domain == p.Domain && s.Root == p.WebRoot && !s.Managed
	})
	if !found {
		return "", as, true, agent.Errorf(agent.CodeInvalidParams, "%s is not the web root of %s", p.WebRoot, p.Domain)
	}
	fi, err := d.FS.Lstat(p.WebRoot)
	if err != nil || !fi.IsDir() {
		return "", as, true, agent.Errorf(agent.CodeNotFound, "web root %s not found", p.WebRoot)
	}
	if owner, ok := system.Owner(fi); ok && owner.UID != 0 {
		return p.WebRoot, owner, true, nil
	}
	as, err = d.Users.Lookup(d.webUser())
	return p.WebRoot, as, true, err
}

func (d Deps) fsDo(ctx context.Context, p FSParams) (any, error) {
	root, as, readOnly, err := d.jail(ctx, p)
	if err != nil {
		return nil, err
	}
	resp, err := d.FileOps.Run(ctx, as, fileops.Request{
		Op: p.Op, Root: root, Path: p.Path, To: p.To, Offset: p.Offset, Length: p.Length,
		Chunk: p.Chunk, Final: p.Final, ReadOnly: readOnly,
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, agent.Errorf("fs_error", "%s", resp.Error)
	}
	return resp, nil
}

func (d Deps) fsRead(ctx context.Context, _ agent.Actor, p FSReadParams) (any, error) {
	return d.fsDo(ctx, FSParams(p))
}

func (d Deps) fsWrite(ctx context.Context, _ agent.Actor, p FSWriteParams) (any, error) {
	return d.fsDo(ctx, FSParams(p))
}

// auditFirstChunk: an upload is many fs.write calls; only the first one
// (offset 0, which names the file) goes to the audit log.
func auditFirstChunk(raw json.RawMessage) bool {
	var p struct {
		Op     string `json:"op"`
		Offset int64  `json:"offset"`
	}
	json.Unmarshal(raw, &p)
	return p.Op == "write" && p.Offset > 0
}
