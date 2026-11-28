package actions

import "testing"

func TestServiceOps(t *testing.T) {
	tests := []struct {
		name, op string
		ok       bool
	}{
		{"nginx", "reload", true},
		{"nginx", "restart", false}, // nginx gets a reload, not a restart
		{"php8.3-fpm", "reload", true},
		{"layte-fpm-acme-8.3", "reload", true},
		{"layte-redis@acme", "restart", true},
		{"mysql", "restart", true},
		{"ssh", "reload", true},
		{"layte", "restart", false},
		{"layte-agent", "reload", false},
		{"ufw", "reload", false},
		{"php8.3-fpm;id", "reload", false},
		{"layte-fpm-../etc-8.3", "reload", false},
		{"systemd-journald", "restart", false},
	}
	for _, tt := range tests {
		err := ServiceControlParams{Name: tt.name, Op: tt.op}.Validate()
		if (err == nil) != tt.ok {
			t.Errorf("%s %s: err = %v, want ok=%v", tt.op, tt.name, err, tt.ok)
		}
	}
	if op, _ := serviceOp("mysql"); !op.Disruptive {
		t.Error("restarting MySQL isn't marked disruptive")
	}
}
