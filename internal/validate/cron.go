package validate

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var (
	cronFieldRe = regexp.MustCompile(`^[0-9a-zA-Z*/,\-]{1,64}$`)
	cronMacros  = []string{"@hourly", "@daily", "@midnight", "@weekly", "@monthly", "@yearly", "@annually", "@reboot"}
)

// CronSchedule accepts five cron fields ("*/5 * * * *") or a macro ("@daily").
// Fields may only contain digits, letters (jan, mon), * / , - so the schedule
// can't spill into the user or command columns of a cron.d line.
func CronSchedule(s string) error {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "@") {
		if slices.Contains(cronMacros, s) {
			return nil
		}
		return fmt.Errorf("unknown schedule %q", s)
	}
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return errors.New("a schedule needs 5 fields: minute hour day month weekday (or @hourly, @daily, …)")
	}
	for _, f := range fields {
		if !cronFieldRe.MatchString(f) {
			return fmt.Errorf("invalid schedule field %q", f)
		}
	}
	return nil
}

// CronCommand is the command a cron job runs (through /bin/sh, as the site's
// own user). One line, no control characters, at most 1000 characters.
func CronCommand(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("command is required")
	}
	if len(s) > 1000 {
		return errors.New("command is too long (max 1000 characters)")
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return errors.New("command must be a single line without control characters")
		}
	}
	return nil
}
