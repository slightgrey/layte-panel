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

// fakeMySQLDump writes a small dump where --result-file says.
func fakeMySQLDump(root system.FS, c system.Cmd) (system.Result, error) {
	var file, db string
	for _, a := range c.Args {
		if f, ok := strings.CutPrefix(a, "--result-file="); ok {
			file = f
		} else if !strings.HasPrefix(a, "-") {
			db = a
		}
	}
	if ok, _ := root.Exists(fakeMySQLDir + "/" + db); !ok {
		return system.Result{Stderr: []byte(fmt.Sprintf("mysqldump: Got error: 1049: Unknown database '%s'", db))},
			&system.ExitError{Bin: c.Bin, ExitCode: 2}
	}
	dump := fmt.Sprintf("-- Fake dump of %s (dev)\nCREATE TABLE `wp_options` (`option_id` bigint);\n", db)
	return system.Result{}, root.WriteFileAtomic(file, []byte(dump), 0o640)
}

func fakeMySQL(root system.FS, c system.Cmd) (system.Result, error) {
	for _, a := range c.Args {
		if f, ok := strings.CutPrefix(a, "--execute=source "); ok {
			if ok, _ := root.Exists(f); !ok {
				return system.Result{Stderr: []byte("Failed to open file '" + f + "', error: 2")}, &system.ExitError{Bin: c.Bin, ExitCode: 1}
			}
			return system.Result{}, nil
		}
	}
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
