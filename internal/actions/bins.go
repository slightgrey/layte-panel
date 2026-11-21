package actions

import "fmt"

// Absolute paths of every program the agent may run (Ubuntu 24.04 layout).
// Keeping them in one place makes the agent's reach easy to audit.
const (
	nginxBin   = "/usr/sbin/nginx"
	mysqlBin   = "/usr/bin/mysql"
	useraddBin = "/usr/sbin/useradd"
	usermodBin = "/usr/sbin/usermod"
	userdelBin = "/usr/sbin/userdel"
)

// phpBin is /usr/bin/phpX.Y; phpFPMBin is /usr/sbin/php-fpmX.Y. The version
// must already be validated against the installed list.
func phpBin(version string) string    { return "/usr/bin/php" + version }
func phpFPMBin(version string) string { return "/usr/sbin/php-fpm" + version }
func phpFPMUnit(version string) string {
	return fmt.Sprintf("php%s-fpm", version)
}
