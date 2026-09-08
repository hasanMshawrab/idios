// Package config holds process-level settings. Clusters and watched
// namespaces are rows in the database, not settings.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/BurntSushi/toml"
)

// Config holds process-level settings loaded from TOML and defaults.
type Config struct {
	DataDir                    string        `toml:"data_dir"`
	ArtifactsRoot              string        `toml:"artifacts_root"`
	Kubeconfig                 string        `toml:"kubeconfig"`
	RetentionDays              int           `toml:"retention_days"`
	SweepInterval              time.Duration `toml:"sweep_interval"`
	StabilizationWindow        time.Duration `toml:"stabilization_window"`
	StabilizationCheckInterval time.Duration `toml:"stabilization_check_interval"`
	SchedulingGrace            time.Duration `toml:"scheduling_grace"`
	ProbeGrace                 time.Duration `toml:"probe_grace"`
	StuckAfter                 time.Duration `toml:"stuck_after"`
	LogTailLines               int           `toml:"log_tail_lines"`
	LogMaxBytes                int64         `toml:"log_max_bytes"`
	CaptureWorkersPerCluster   int           `toml:"capture_workers_per_cluster"`
	CaptureQueueSize           int           `toml:"capture_queue_size"`
	EarlyCaptureDebounce       time.Duration `toml:"early_capture_debounce"`
	APIListen                  string        `toml:"api_listen"`
	APIStreamThrottle          time.Duration `toml:"api_stream_throttle"`
	APIListLimit               int           `toml:"api_list_limit"`
	AttentionWindow            time.Duration `toml:"attention_window"`
}

// DefaultDataDir is the per-user data directory of this OS, where the
// database, artifacts, log and config file live unless configured.
func DefaultDataDir() string {
	return dataDir(runtime.GOOS, os.Getenv)
}

func dataDir(goos string, getenv func(string) string) string {
	switch goos {
	case "darwin":
		return filepath.Join(getenv("HOME"), "Library", "Application Support", "idios")
	case "windows":
		return filepath.Join(getenv("LocalAppData"), "idios")
	default:
		if x := getenv("XDG_DATA_HOME"); x != "" {
			return filepath.Join(x, "idios")
		}
		return filepath.Join(getenv("HOME"), ".local", "share", "idios")
	}
}

// Default returns the release configuration. Kubeconfig empty means the
// usual resolution: KUBECONFIG, then the home directory.
func Default() Config {
	c := Config{
		DataDir:                    DefaultDataDir(),
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
	c.deriveArtifactsRoot()
	return c
}

// Load returns Default overlaid with the TOML file at path. A missing file
// is not an error; an unknown key is, so a typo cannot silently keep a
// default.
func Load(path string) (Config, error) {
	c := Default()
	c.ArtifactsRoot = ""
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c.deriveArtifactsRoot()
		return c, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	md, err := toml.Decode(string(data), &c)
	if err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Config{}, fmt.Errorf("config %s: unknown keys %v", path, undecoded)
	}
	c.deriveArtifactsRoot()
	return c, nil
}

func (c *Config) deriveArtifactsRoot() {
	if c.ArtifactsRoot == "" && c.DataDir != "" {
		c.ArtifactsRoot = filepath.Join(c.DataDir, "artifacts")
	}
}

// DBPath returns the SQLite database path under DataDir.
func (c Config) DBPath() string {
	return filepath.Join(c.DataDir, "idios.db")
}

// The daemon has no authentication, so a listener anywhere but loopback
// would expose logs that contain secrets.
func validateAPIListen(listen string) []error {
	if listen == "" {
		return []error{errors.New("api_listen must not be empty")}
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return []error{fmt.Errorf("api_listen %q must be host:port", listen)}
	}
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return nil
	}
	return []error{fmt.Errorf("api_listen %q must bind 127.0.0.1, localhost or ::1", listen)}
}

// Validate reports an error for every field that would hang or panic a
// ticker, channel, or worker pool at runtime.
func (c Config) Validate() error {
	var errs []error
	if c.DataDir == "" {
		errs = append(errs, errors.New("data_dir must not be empty"))
	}
	if c.RetentionDays < 1 {
		errs = append(errs, errors.New("retention_days must be >= 1"))
	}
	if c.SweepInterval <= 0 {
		errs = append(errs, errors.New("sweep_interval must be > 0"))
	}
	if c.StabilizationWindow <= 0 {
		errs = append(errs, errors.New("stabilization_window must be > 0"))
	}
	if c.StabilizationCheckInterval <= 0 {
		errs = append(errs, errors.New("stabilization_check_interval must be > 0"))
	}
	// Zero is a setting, not a mistake: an incident the instant scheduling
	// fails.
	if c.SchedulingGrace < 0 {
		errs = append(errs, errors.New("scheduling_grace must be >= 0"))
	}
	// Zero is a setting here too: an incident on the first readiness miss.
	if c.ProbeGrace < 0 {
		errs = append(errs, errors.New("probe_grace must be >= 0"))
	}
	// Zero is a mistake, not a setting: it puts the edge at now and makes
	// every pod that is merely starting a stuck incident.
	if c.StuckAfter <= 0 {
		errs = append(errs, errors.New("stuck_after must be > 0"))
	}
	if c.LogTailLines < 1 {
		errs = append(errs, errors.New("log_tail_lines must be >= 1"))
	}
	if c.LogMaxBytes < 1 {
		errs = append(errs, errors.New("log_max_bytes must be >= 1"))
	}
	if c.CaptureWorkersPerCluster < 1 {
		errs = append(errs, errors.New("capture_workers_per_cluster must be >= 1"))
	}
	if c.CaptureQueueSize < 1 {
		errs = append(errs, errors.New("capture_queue_size must be >= 1"))
	}
	if c.EarlyCaptureDebounce < 0 {
		errs = append(errs, errors.New("early_capture_debounce must be >= 0"))
	}
	errs = append(errs, validateAPIListen(c.APIListen)...)
	if c.APIStreamThrottle < 0 {
		errs = append(errs, errors.New("api_stream_throttle must be >= 0"))
	}
	if c.APIListLimit < 1 {
		errs = append(errs, errors.New("api_list_limit must be >= 1"))
	}
	// Zero is a setting, not a mistake: attention is then exactly the open set.
	if c.AttentionWindow < 0 {
		errs = append(errs, errors.New("attention_window must be >= 0"))
	}
	return errors.Join(errs...)
}
