package actions

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/slightgrey/layte-panel/internal/system"
)

// The dev MySQL fake keeps one directory per database under /var/lib/mysql
// in the fake root (like the real data dir), so create/drop/list agree.
const fakeMySQLDir = "/var/lib/mysql"

var (
	reCreateDB = regexp.MustCompile("CREATE DATABASE `([a-z0-9_]+)`")
	reDropDB   = regexp.MustCompile("DROP DATABASE IF EXISTS `([a-z0-9_]+)`")
)

func fakeMySQL(root system.FS, c system.Cmd) (system.Result, error) {
	sql := string(c.Stdin)
	switch {
	case reCreateDB.MatchString(sql):
		name := reCreateDB.FindStringSubmatch(sql)[1]
		if ok, _ := root.Exists(fakeMySQLDir + "/" + name); ok {
			return system.Result{Stderr: []byte(fmt.Sprintf("ERROR 1007 (HY000): Can't create database '%s'; database exists", name))},
				&system.ExitError{Bin: c.Bin, ExitCode: 1}
		}
		return system.Result{}, root.MkdirAll(fakeMySQLDir+"/"+name, 0o750)
	case reDropDB.MatchString(sql):
		return system.Result{}, root.RemoveAll(fakeMySQLDir + "/" + reDropDB.FindStringSubmatch(sql)[1])
	case strings.HasPrefix(sql, "SELECT s.schema_name"):
		dirs, _ := root.Glob(fakeMySQLDir + "/*")
		var b strings.Builder
		for _, d := range dirs {
			name := path.Base(d)
			fmt.Fprintf(&b, "%s\t%d\n", name, int64(len(name))*1_300_000)
		}
		return system.Result{Stdout: []byte(b.String())}, nil
	}
	return system.Result{}, nil // ALTER USER, FLUSH, …
}
