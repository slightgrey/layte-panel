package phpini

import "testing"

func TestValidate(t *testing.T) {
	ok := []Settings{
		{},
		{MemoryLimit: "128M", UploadMax: "8M", MaxExecutionTime: 60, MaxInputVars: 3000},
		{MemoryLimit: "1G", UploadMax: "2G", MaxExecutionTime: 3600, MaxInputVars: 100000},
		{MemoryLimit: "300M"}, // custom
	}
	for _, s := range ok {
		if err := s.Validate(); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
	bad := []Settings{
		{MemoryLimit: "256"},
		{MemoryLimit: "256MB"},
		{MemoryLimit: "-1"},
		{MemoryLimit: "16M"},
		{MemoryLimit: "8G"},
		{MemoryLimit: "256M\nphp_admin_value[open_basedir] = /"},
		{UploadMax: "0M"},
		{UploadMax: "3G"},
		{UploadMax: "64 M"},
		{MaxExecutionTime: -1},
		{MaxExecutionTime: 3601},
		{MaxInputVars: 50},
		{MaxInputVars: 100001},
	}
	for _, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("%+v: accepted", s)
		}
	}
}

func TestEffective(t *testing.T) {
	var s Settings
	if s.Memory() != "256M" || s.Upload() != "64M" || s.NginxBodySize() != "64m" {
		t.Errorf("defaults: %s %s %s", s.Memory(), s.Upload(), s.NginxBodySize())
	}
	s = Settings{MemoryLimit: " 512m ", UploadMax: "1g"}.Normalize()
	if s.Memory() != "512M" || s.Upload() != "1G" || s.NginxBodySize() != "1g" {
		t.Errorf("normalized: %+v", s)
	}
}
