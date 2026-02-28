package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// Load reads the YAML config at path, applies defaults, and validates.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	if cfg.TargetsFile != "" {
		if err := loadTargetsFile(&cfg); err != nil {
			return nil, fmt.Errorf("targets_file: %w", err)
		}
	}

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("pacing.mode", "human")
	v.SetDefault("pacing.requests_per_minute", 20.0)
	v.SetDefault("pacing.jitter_factor", 0.4)
	v.SetDefault("pacing.min_delay_ms", 800)
	v.SetDefault("pacing.max_delay_ms", 8000)

	v.SetDefault("limits.max_workers", 4)
	v.SetDefault("limits.max_browser_workers", 1)
	v.SetDefault("limits.cpu_threshold_pct", 60.0)
	v.SetDefault("limits.memory_threshold_mb", 512)

	v.SetDefault("rate_limits.default_rps", 0.5)

	v.SetDefault("backoff.initial_ms", 1000)
	v.SetDefault("backoff.max_ms", 120000)
	v.SetDefault("backoff.multiplier", 2.0)
	v.SetDefault("backoff.max_attempts", 3)

	v.SetDefault("metrics.enabled", false)
	v.SetDefault("metrics.prometheus_port", 9090)

	v.SetDefault("daemon.pid_file", "/tmp/sendit.pid")
	v.SetDefault("daemon.log_level", "info")
	v.SetDefault("daemon.log_format", "text")

	// target_defaults: applied to every target loaded from targets_file.
	v.SetDefault("target_defaults.weight", 1)
	v.SetDefault("target_defaults.http.method", "GET")
	v.SetDefault("target_defaults.http.timeout_s", 15)
	v.SetDefault("target_defaults.browser.timeout_s", 30)
	v.SetDefault("target_defaults.dns.resolver", "8.8.8.8:53")
	v.SetDefault("target_defaults.dns.record_type", "A")
	v.SetDefault("target_defaults.websocket.duration_s", 30)
}

// loadTargetsFile reads the file at cfg.TargetsFile and appends a TargetConfig
// for each entry to cfg.Targets, applying cfg.TargetDefaults for all fields
// not specified in the file.
//
// File format — one entry per line:
//
//	<url> <type> [weight]
//
// Lines beginning with '#' and blank lines are ignored. Weight defaults to
// target_defaults.weight when omitted.
func loadTargetsFile(cfg *Config) error {
	f, err := os.Open(cfg.TargetsFile)
	if err != nil {
		return fmt.Errorf("opening %q: %w", cfg.TargetsFile, err)
	}
	defer f.Close()

	d := cfg.TargetDefaults
	validTypes := map[string]bool{"http": true, "browser": true, "dns": true, "websocket": true}

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			return fmt.Errorf("line %d: expected \"<url> <type> [weight]\", got %q", lineNum, line)
		}

		url := fields[0]
		typ := strings.ToLower(fields[1])

		if !validTypes[typ] {
			return fmt.Errorf("line %d: unknown type %q (must be http|browser|dns|websocket)", lineNum, typ)
		}

		weight := d.Weight
		if len(fields) >= 3 {
			w, err := strconv.Atoi(fields[2])
			if err != nil || w <= 0 {
				return fmt.Errorf("line %d: invalid weight %q (must be a positive integer)", lineNum, fields[2])
			}
			weight = w
		}
		if weight <= 0 {
			weight = 1
		}

		cfg.Targets = append(cfg.Targets, TargetConfig{
			URL:       url,
			Weight:    weight,
			Type:      typ,
			HTTP:      d.HTTP,
			Browser:   d.Browser,
			DNS:       d.DNS,
			WebSocket: d.WebSocket,
		})
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading %q: %w", cfg.TargetsFile, err)
	}
	return nil
}

func validate(cfg *Config) error {
	var errs []string

	validModes := map[string]bool{"human": true, "rate_limited": true, "scheduled": true}
	if !validModes[cfg.Pacing.Mode] {
		errs = append(errs, fmt.Sprintf("pacing.mode must be one of human|rate_limited|scheduled, got %q", cfg.Pacing.Mode))
	}

	if cfg.Pacing.RequestsPerMinute <= 0 {
		errs = append(errs, "pacing.requests_per_minute must be > 0")
	}

	if cfg.Pacing.JitterFactor < 0 || cfg.Pacing.JitterFactor > 1 {
		errs = append(errs, "pacing.jitter_factor must be in [0, 1]")
	}

	if cfg.Pacing.MinDelayMs < 0 {
		errs = append(errs, "pacing.min_delay_ms must be >= 0")
	}

	if cfg.Pacing.MaxDelayMs < cfg.Pacing.MinDelayMs {
		errs = append(errs, "pacing.max_delay_ms must be >= min_delay_ms")
	}

	if cfg.Pacing.Mode == "scheduled" && len(cfg.Pacing.Schedule) == 0 {
		errs = append(errs, "pacing.schedule must have at least one entry when mode is scheduled")
	}

	if cfg.Limits.MaxWorkers <= 0 {
		errs = append(errs, "limits.max_workers must be > 0")
	}

	if cfg.Limits.MaxBrowserWorkers <= 0 {
		errs = append(errs, "limits.max_browser_workers must be > 0")
	}

	if cfg.Limits.CPUThresholdPct <= 0 || cfg.Limits.CPUThresholdPct > 100 {
		errs = append(errs, "limits.cpu_threshold_pct must be in (0, 100]")
	}

	if cfg.RateLimits.DefaultRPS <= 0 {
		errs = append(errs, "rate_limits.default_rps must be > 0")
	}

	if cfg.Backoff.InitialMs <= 0 {
		errs = append(errs, "backoff.initial_ms must be > 0")
	}

	if cfg.Backoff.MaxMs < cfg.Backoff.InitialMs {
		errs = append(errs, "backoff.max_ms must be >= initial_ms")
	}

	if cfg.Backoff.Multiplier <= 1 {
		errs = append(errs, "backoff.multiplier must be > 1")
	}

	if cfg.Backoff.MaxAttempts <= 0 {
		errs = append(errs, "backoff.max_attempts must be > 0")
	}

	if len(cfg.Targets) == 0 {
		errs = append(errs, "targets must have at least one entry (via 'targets' in config or 'targets_file')")
	}

	validTypes := map[string]bool{"http": true, "browser": true, "dns": true, "websocket": true}
	for i, t := range cfg.Targets {
		if t.URL == "" {
			errs = append(errs, fmt.Sprintf("targets[%d].url must not be empty", i))
		}
		if t.Weight <= 0 {
			errs = append(errs, fmt.Sprintf("targets[%d].weight must be > 0", i))
		}
		if !validTypes[t.Type] {
			errs = append(errs, fmt.Sprintf("targets[%d].type must be one of http|browser|dns|websocket, got %q", i, t.Type))
		}
	}

	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[cfg.Daemon.LogLevel] {
		errs = append(errs, fmt.Sprintf("daemon.log_level must be one of debug|info|warn|error, got %q", cfg.Daemon.LogLevel))
	}

	validLogFormats := map[string]bool{"text": true, "json": true}
	if !validLogFormats[cfg.Daemon.LogFormat] {
		errs = append(errs, fmt.Sprintf("daemon.log_format must be text|json, got %q", cfg.Daemon.LogFormat))
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
