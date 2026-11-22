package actions

import (
	"context"
	"crypto/rand"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/fileops"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/validate"
)

const mysqldumpBin = "/usr/bin/mysqldump" // MariaDB ships it as a link to mariadb-dump

// Dumps live in the site's tmp/ folder, which nginx doesn't serve. Only plain
// file names there, so the path is safe inside mysql's `source` command.
var dumpPathRe = regexp.MustCompile(`^tmp/[A-Za-z0-9][A-Za-z0-9._-]{0,150}\.sql$`)

// DBDumpParams name a database and the Layte site whose folder holds the dump.
type DBDumpParams struct {
	Database string `json:"database"`
	Domain   string `json:"domain"`
	Username string `json:"username"`
	WebRoot  string `json:"web_root,omitempty"` // adopted sites
	Path     string `json:"path,omitempty"`     // import only: tmp/<file>.sql
}

func (p DBDumpParams) validate() error {
	if err := validate.DBIdent(p.Database); err != nil {
		return err
	}
	if slices.Contains(systemDatabases, p.Database) {
		return fmt.Errorf("%s is a system database", p.Database)
	}
	if err := validDomain(p.Domain); err != nil {
		return err
	}
	if err := validate.SiteUsername(p.Username); err != nil {
		return err
	}
	if p.WebRoot != "" {
		return FSParams{Domain: p.Domain, Username: p.Username, WebRoot: p.WebRoot, Op: "stat", Path: "."}.validate(readOps)
	}
	return nil
}

// DBExportParams / DBImportParams are separate types so each action has
// exactly the fields it needs validated.
type DBExportParams DBDumpParams
type DBImportParams DBDumpParams

func (p DBExportParams) Validate() error {
	if p.Path != "" {
		return fmt.Errorf("export picks its own file name")
	}
	return DBDumpParams(p).validate()
}

func (p DBImportParams) Validate() error {
	if !dumpPathRe.MatchString(p.Path) || strings.Contains(p.Path, "..") {
		return fmt.Errorf("import file must be tmp/<name>.sql")
	}
	return DBDumpParams(p).validate()
}

// DumpFile is the result of an export.
type DumpFile struct {
	Path string `json:"path"` // relative to the site folder
	Size int64  `json:"size"`
}

// tempDBUser creates a MySQL user that can only reach one database, runs fn
// with its credentials, and always drops it again. Dumps are read and written
// by the site's Linux user with this login, so a dump file can't touch other
// databases (USE mysql; fails) and root never writes into the site folder.
func (d Deps) tempDBUser(ctx context.Context, db, grants string, fn func(user, password string) error) error {
	user := "layte_tmp_" + strings.ToLower(rand.Text()[:10])
	password := rand.Text() + rand.Text()
	if err := d.mysql(ctx, fmt.Sprintf("CREATE USER '%s'@'localhost' IDENTIFIED BY '%s';\nGRANT %s ON `%s`.* TO '%s'@'localhost';\n",
		user, password, grants, db, user)); err != nil {
		return fmt.Errorf("create temporary database user: %w", err)
	}
	defer d.mysql(context.WithoutCancel(ctx), fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost';\n", user))
	return fn(user, password)
}

// siteDumpEnv finds the site's data folder (the site folder for Layte sites,
// /home/<user> for adopted ones; same checks as the file manager) and its
// user, and makes sure tmp/ is a real folder.
func (d Deps) siteDumpEnv(ctx context.Context, p DBDumpParams) (root string, as system.Credential, err error) {
	root, as, _, err = d.jail(ctx, FSParams{Domain: p.Domain, Username: p.Username, WebRoot: p.WebRoot, Area: "data"})
	if err != nil {
		return "", as, err
	}
	if fi, err := d.FS.Lstat(root + "/tmp"); err != nil || !fi.IsDir() {
		return "", as, agent.Errorf(agent.CodeNotFound, "%s/tmp is missing or not a folder", root)
	}
	return root, as, nil
}

// dbExport dumps a database to tmp/<db>-<time>.sql in the site folder.
// mysqldump runs as the site user and creates the file itself.
func (d Deps) dbExport(ctx context.Context, _ agent.Actor, p DBExportParams) (any, error) {
	root, as, err := d.siteDumpEnv(ctx, DBDumpParams(p))
	if err != nil {
		return nil, err
	}
	rel := fmt.Sprintf("tmp/%s-%s.sql", p.Database, d.Now().UTC().Format("20060102-150405"))
	if ok, _ := d.FS.Exists(root + "/" + rel); ok {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s already exists; try again in a second", rel)
	}
	// SELECT, SHOW VIEW, TRIGGER and LOCK TABLES are what mysqldump needs for
	// tables, views and triggers. --no-tablespaces avoids needing PROCESS.
	err = d.tempDBUser(ctx, p.Database, "SELECT, SHOW VIEW, TRIGGER, LOCK TABLES", func(user, pw string) error {
		res, err := d.Runner.RunCmd(ctx, system.Cmd{
			Bin: mysqldumpBin,
			Args: []string{"--user=" + user, "--single-transaction", "--no-tablespaces", "--default-character-set=utf8mb4",
				"--result-file=" + root + "/" + rel, p.Database},
			Env: []string{"MYSQL_PWD=" + pw, "HOME=/home/" + p.Username},
			Dir: root,
			As:  &as,
		})
		if err != nil {
			// Remove the partial file as the site user, never as root.
			d.FileOps.Run(context.WithoutCancel(ctx), as, fileops.Request{Op: "delete", Root: root, Path: rel})
			return agent.Errorf("dump_failed", "mysqldump: %s", failure(res, err, 4))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := DumpFile{Path: rel}
	if fi, err := d.FS.Lstat(root + "/" + rel); err == nil {
		out.Size = fi.Size()
	}
	return out, nil
}

// dbImport runs a .sql file from the site's tmp/ folder into the database.
// The mysql client runs as the site user and reads the file itself
// (`source`), so a big dump is streamed instead of held in memory. It stops
// at the first error. LOAD DATA LOCAL is off.
func (d Deps) dbImport(ctx context.Context, _ agent.Actor, p DBImportParams) (any, error) {
	root, as, err := d.siteDumpEnv(ctx, DBDumpParams(p))
	if err != nil {
		return nil, err
	}
	fi, err := d.FS.Lstat(root + "/" + p.Path)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, agent.Errorf(agent.CodeNotFound, "%s not found (or not a regular file)", p.Path)
	}
	err = d.tempDBUser(ctx, p.Database, "ALL PRIVILEGES", func(user, pw string) error {
		res, err := d.Runner.RunCmd(ctx, system.Cmd{
			Bin: mysqlBin,
			Args: []string{"--user=" + user, "--database=" + p.Database, "--default-character-set=utf8mb4",
				"--local-infile=0", "--batch", "--execute=source " + root + "/" + p.Path},
			Env: []string{"MYSQL_PWD=" + pw, "HOME=/home/" + p.Username},
			Dir: root,
			As:  &as,
		})
		if err != nil {
			msg := failure(res, err, 4)
			// Triggers, routines and views with a DEFINER need SUPER/SET_USER_ID
			// (and with binary logging, SUPER for any trigger). The import user
			// deliberately only has rights on this one database.
			if strings.Contains(msg, "SUPER") || strings.Contains(msg, "SET_USER_ID") || strings.Contains(msg, "ERROR 1227") {
				msg += " The file creates triggers, routines or views, which need server-wide rights; Layte imports with a user limited to this database. Remove them (or their DEFINER clauses) from the file and import again."
			}
			return agent.Errorf("import_failed", "mysql: %s", msg)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"imported": p.Path, "size": fi.Size()}, nil
}
