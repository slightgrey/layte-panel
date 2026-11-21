package actions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// systemDatabases can never be created over or dropped.
var systemDatabases = []string{"mysql", "sys", "information_schema", "performance_schema"}

type DBCreateParams struct {
	Name     string `json:"name"`
	User     string `json:"user"`
	Password string `json:"password"` // redacted in the audit log by key name
}

func (p DBCreateParams) Validate() error {
	if err := validate.DBIdent(p.Name); err != nil {
		return err
	}
	if err := validate.DBIdent(p.User); err != nil {
		return err
	}
	if slices.Contains(systemDatabases, p.Name) || p.User == "root" {
		return fmt.Errorf("%s is reserved", p.Name)
	}
	return validate.Password(p.Password)
}

// dbCreate creates a database and a user with full rights on it. Works with
// MySQL 8 and MariaDB (same SQL). The SQL, including the password, is sent on
// stdin, so it never shows up in the process list. Names and the password are
// strictly validated, so no escaping is needed.
func (d Deps) dbCreate(ctx context.Context, _ agent.Actor, p DBCreateParams) (any, error) {
	engine, err := d.dbEngine(ctx)
	if err != nil {
		return nil, err
	}
	sql := fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;\n"+
		"CREATE USER '%s'@'localhost' IDENTIFIED BY '%s';\n"+
		"GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';\n"+
		"FLUSH PRIVILEGES;\n",
		p.Name, p.User, p.Password, p.Name, p.User)
	if err := d.mysql(ctx, sql); err != nil {
		if strings.Contains(err.Error(), "exists") {
			return nil, agent.Errorf(agent.CodeInvalidParams, "database or user already exists: %v", err)
		}
		return nil, err
	}
	return map[string]string{"engine": engine}, nil
}

type DBDropParams struct {
	Name string `json:"name"`
	User string `json:"user"`
}

func (p DBDropParams) Validate() error {
	if err := validate.DBIdent(p.Name); err != nil {
		return err
	}
	if err := validate.DBIdent(p.User); err != nil {
		return err
	}
	if slices.Contains(systemDatabases, p.Name) || p.User == "root" {
		return fmt.Errorf("%s is reserved", p.Name)
	}
	return nil
}

// dbDrop removes a database and its user (idempotent).
func (d Deps) dbDrop(ctx context.Context, _ agent.Actor, p DBDropParams) (any, error) {
	sql := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;\nDROP USER IF EXISTS '%s'@'localhost';\n", p.Name, p.User)
	if err := d.mysql(ctx, sql); err != nil {
		return nil, err
	}
	return map[string]bool{"dropped": true}, nil
}

// DBInfo is one database on the server.
type DBInfo struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"` // bytes (data + indexes)
	System bool   `json:"system"`
}

// dbList lists every database with its size (read-only).
func (d Deps) dbList(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	engine, err := d.dbEngine(ctx)
	if err != nil {
		return nil, err
	}
	out, err := d.mysqlQuery(ctx, "SELECT s.schema_name, COALESCE(SUM(t.data_length + t.index_length), 0)\n"+
		"FROM information_schema.schemata s LEFT JOIN information_schema.tables t ON t.table_schema = s.schema_name\n"+
		"GROUP BY s.schema_name ORDER BY s.schema_name;\n")
	if err != nil {
		return nil, err
	}
	dbs := []DBInfo{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		name, size, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		var n int64
		fmt.Sscan(size, &n)
		dbs = append(dbs, DBInfo{Name: name, Size: n, System: slices.Contains(systemDatabases, name)})
	}
	return map[string]any{"engine": engine, "databases": dbs}, nil
}

type DBPasswordParams struct {
	User     string `json:"user"`
	Password string `json:"password"` // redacted in audit
}

func (p DBPasswordParams) Validate() error {
	if err := validate.DBIdent(p.User); err != nil {
		return err
	}
	if p.User == "root" {
		return fmt.Errorf("refusing to change root")
	}
	return validate.Password(p.Password)
}

// dbPasswordReset sets a new password for a database user.
func (d Deps) dbPasswordReset(ctx context.Context, _ agent.Actor, p DBPasswordParams) (any, error) {
	if err := d.mysql(ctx, fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s';\n", p.User, p.Password)); err != nil {
		return nil, err
	}
	return map[string]bool{"changed": true}, nil
}

// dbEngine reports "mysql" or "mariadb", or an error if neither is installed.
func (d Deps) dbEngine(ctx context.Context) (string, error) {
	v := d.probe(ctx, "mysql", mysqlBin, []string{"--version"}, parseMySQLVersion)
	if !v.Installed {
		return "", agent.Errorf(agent.CodeNotFound, "no MySQL or MariaDB client found at %s", mysqlBin)
	}
	return v.Name, nil
}

// mysql runs SQL as root over the local socket (Ubuntu's default auth for root).
func (d Deps) mysql(ctx context.Context, sql string) error {
	_, err := d.mysqlQuery(ctx, sql)
	return err
}

// mysqlQuery is mysql returning the (tab-separated) output.
func (d Deps) mysqlQuery(ctx context.Context, sql string) (string, error) {
	res, err := d.Runner.RunCmd(ctx, system.Cmd{
		Bin:   mysqlBin,
		Args:  []string{"--batch", "--skip-column-names", "--user=root"},
		Stdin: []byte(sql),
		Env:   []string{"HOME=/root"},
	})
	if err != nil {
		var ee *system.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("mysql: %s", strings.TrimSpace(string(res.Stderr)))
		}
		return "", err
	}
	return string(res.Stdout), nil
}
