package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultsMatchDesign(t *testing.T) {
	want := Config{
		DataDir:                    DefaultDataDir(),
		ArtifactsRoot:              filepath.Join(DefaultDataDir(), "artifacts"),
		Kubeconfig:                 "",
		RetentionDays:              3,
		SweepInterval:              time.Hour,
		StabilizationWindow:        10 * time.Minute,
		StabilizationCheckInterval: 30 * time.Second,
		SchedulingGrace:            60 * time.Second,
		ProbeGrace:                 60 * time.Second,
		StuckAfter:                 10 * time.Minute,
		LogTailLines:               50,
		LogMaxBytes:                262144,
		CaptureWorkersPerCluster:   4,
		CaptureQueueSize:           1024,
		EarlyCaptureDebounce:       60 * time.Second,
		APIListen:                  "127.0.0.1:7770",
		APIStreamThrottle:          time.Second,
		APIListLimit:               500,
		AttentionWindow:            24 * time.Hour,
	}
	got := Default()
	if got != want {
		t.Fatalf("Default() = %+v\nwant        %+v", got, want)
	}
	if p := got.DBPath(); p != filepath.Join(DefaultDataDir(), "idios.db") {
		t.Fatalf("DBPath = %q", p)
	}
}

func TestDataDirFollowsTheOS(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	cases := []struct {
		name string
		goos string
		env  map[string]string
		want string
	}{
		{"darwin", "darwin", map[string]string{"HOME": "/Users/h"}, "/Users/h/Library/Application Support/idios"},
		{"linux without xdg", "linux", map[string]string{"HOME": "/home/h"}, "/home/h/.local/share/idios"},
		{"linux with xdg", "linux", map[string]string{"HOME": "/home/h", "XDG_DATA_HOME": "/data"}, "/data/idios"},
		{"windows", "windows", map[string]string{"LocalAppData": `C:\Users\h\AppData\Local`}, filepath.Join(`C:\Users\h\AppData\Local`, "idios")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dataDir(c.goos, env(c.env)); got != filepath.FromSlash(c.want) {
				t.Fatalf("dataDir = %q, want %q", got, filepath.FromSlash(c.want))
			}
		})
	}
}

func TestLoad(t *testing.T) {
	write := func(t *testing.T, body string) string {
		p := filepath.Join(t.TempDir(), "idios.toml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	overlay := Default()
	overlay.DataDir = "/var/lib/idios"
	overlay.ArtifactsRoot = filepath.Join("/var/lib/idios", "artifacts")
	overlay.RetentionDays = 7
	overlay.SweepInterval = 30 * time.Minute
	overlay.Kubeconfig = "/tmp/kc"

	explicitRoot := Default()
	explicitRoot.DataDir = "/d"
	explicitRoot.ArtifactsRoot = "/elsewhere"

	cases := []struct {
		name string
		path string
		want Config
		ok   bool
	}{
		{"missing file gives defaults", filepath.Join(t.TempDir(), "absent.toml"), Default(), true},
		{"file overlays and derives artifacts_root", write(t, `
data_dir = "/var/lib/idios"
retention_days = 7
sweep_interval = "30m"
kubeconfig = "/tmp/kc"
`), overlay, true},
		{"explicit artifacts_root wins", write(t, "data_dir = \"/d\"\nartifacts_root = \"/elsewhere\"\n"), explicitRoot, true},
		{"unknown key rejected", write(t, "retention_dayz = 3\n"), Config{}, false},
	}
	for _, c := range cases {
		got, err := Load(c.path)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("%s: Load = %+v\nwant %+v", c.name, got, c.want)
		}
	}
}

func TestValidateRejectsZeroOrNegative(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"empty data_dir", func(c *Config) { c.DataDir = "" }},
		{"retention 0", func(c *Config) { c.RetentionDays = 0 }},
		{"sweep interval 0", func(c *Config) { c.SweepInterval = 0 }},
		{"stabilization window 0", func(c *Config) { c.StabilizationWindow = 0 }},
		{"check interval 0", func(c *Config) { c.StabilizationCheckInterval = 0 }},
		{"tail lines 0", func(c *Config) { c.LogTailLines = 0 }},
		{"max bytes 0", func(c *Config) { c.LogMaxBytes = 0 }},
		{"workers 0", func(c *Config) { c.CaptureWorkersPerCluster = 0 }},
		{"queue 0", func(c *Config) { c.CaptureQueueSize = 0 }},
		{"scheduling grace negative", func(c *Config) { c.SchedulingGrace = -time.Second }},
		{"probe grace negative", func(c *Config) { c.ProbeGrace = -time.Second }},
		{"stuck after 0", func(c *Config) { c.StuckAfter = 0 }},
		{"debounce negative", func(c *Config) { c.EarlyCaptureDebounce = -time.Second }},
		{"stream throttle negative", func(c *Config) { c.APIStreamThrottle = -time.Second }},
		{"list limit 0", func(c *Config) { c.APIListLimit = 0 }},
		{"attention window negative", func(c *Config) { c.AttentionWindow = -time.Second }},
	}
	if err := Default().Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	for _, c := range cases {
		cfg := Default()
		c.mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: Validate accepted", c.name)
		}
	}
}

func TestValidateRejectsNonLoopbackAPIListen(t *testing.T) {
	cases := []struct {
		listen string
		ok     bool
	}{
		{"127.0.0.1:7770", true},
		{"localhost:7770", true},
		{"[::1]:7770", true},
		{"0.0.0.0:7770", false},
		{":7770", false},
		{"192.168.1.2:7770", false},
		{"127.0.0.1", false},
		{"", false},
	}
	for _, c := range cases {
		cfg := Default()
		cfg.APIListen = c.listen
		err := cfg.Validate()
		mentions := err != nil && strings.Contains(err.Error(), "api_listen")
		if mentions == c.ok {
			t.Errorf("api_listen %q: Validate = %v, want api_listen error = %v", c.listen, err, !c.ok)
		}
	}
}
