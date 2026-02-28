package config

// Config is the root configuration structure.
type Config struct {
	Pacing         PacingConfig         `mapstructure:"pacing"`
	Limits         LimitsConfig         `mapstructure:"limits"`
	RateLimits     RateLimitsConfig     `mapstructure:"rate_limits"`
	Backoff        BackoffConfig        `mapstructure:"backoff"`
	Targets        []TargetConfig       `mapstructure:"targets"`
	TargetsFile    string               `mapstructure:"targets_file"`
	TargetDefaults TargetDefaultsConfig `mapstructure:"target_defaults"`
	Metrics        MetricsConfig        `mapstructure:"metrics"`
	Daemon         DaemonConfig         `mapstructure:"daemon"`
}

// TargetDefaultsConfig holds fallback values applied to every target loaded
// from targets_file. Fields left at their zero value fall through to each
// driver's own built-in defaults.
type TargetDefaultsConfig struct {
	Weight    int             `mapstructure:"weight"`
	HTTP      HTTPConfig      `mapstructure:"http"`
	Browser   BrowserConfig   `mapstructure:"browser"`
	DNS       DNSConfig       `mapstructure:"dns"`
	WebSocket WebSocketConfig `mapstructure:"websocket"`
}

// PacingConfig controls how requests are spaced in time.
type PacingConfig struct {
	Mode              string           `mapstructure:"mode"`               // human | rate_limited | scheduled
	RequestsPerMinute float64          `mapstructure:"requests_per_minute"`
	JitterFactor      float64          `mapstructure:"jitter_factor"`
	MinDelayMs        int              `mapstructure:"min_delay_ms"`
	MaxDelayMs        int              `mapstructure:"max_delay_ms"`
	Schedule          []ScheduleEntry  `mapstructure:"schedule"`
}

// ScheduleEntry defines a cron-based active window with its own RPM.
type ScheduleEntry struct {
	Cron               string  `mapstructure:"cron"`
	DurationMinutes    int     `mapstructure:"duration_minutes"`
	RequestsPerMinute  float64 `mapstructure:"requests_per_minute"`
}

// LimitsConfig controls concurrency and resource thresholds.
type LimitsConfig struct {
	MaxWorkers        int     `mapstructure:"max_workers"`
	MaxBrowserWorkers int     `mapstructure:"max_browser_workers"`
	CPUThresholdPct   float64 `mapstructure:"cpu_threshold_pct"`
	MemoryThresholdMB uint64  `mapstructure:"memory_threshold_mb"`
}

// RateLimitsConfig holds global and per-domain rate limits.
type RateLimitsConfig struct {
	DefaultRPS float64           `mapstructure:"default_rps"`
	PerDomain  []DomainRateLimit `mapstructure:"per_domain"`
}

// DomainRateLimit specifies a per-domain requests-per-second limit.
type DomainRateLimit struct {
	Domain string  `mapstructure:"domain"`
	RPS    float64 `mapstructure:"rps"`
}

// BackoffConfig controls retry/backoff behaviour.
type BackoffConfig struct {
	InitialMs   int     `mapstructure:"initial_ms"`
	MaxMs       int     `mapstructure:"max_ms"`
	Multiplier  float64 `mapstructure:"multiplier"`
	MaxAttempts int     `mapstructure:"max_attempts"`
}

// TargetConfig describes a single request target.
type TargetConfig struct {
	URL       string           `mapstructure:"url"`
	Weight    int              `mapstructure:"weight"`
	Type      string           `mapstructure:"type"` // http | browser | dns | websocket
	HTTP      HTTPConfig       `mapstructure:"http"`
	Browser   BrowserConfig    `mapstructure:"browser"`
	DNS       DNSConfig        `mapstructure:"dns"`
	WebSocket WebSocketConfig  `mapstructure:"websocket"`
}

// HTTPConfig holds HTTP-specific target settings.
type HTTPConfig struct {
	Method    string            `mapstructure:"method"`
	Headers   map[string]string `mapstructure:"headers"`
	Body      string            `mapstructure:"body"`
	TimeoutS  int               `mapstructure:"timeout_s"`
}

// BrowserConfig holds headless-browser target settings.
type BrowserConfig struct {
	Scroll           bool   `mapstructure:"scroll"`
	WaitForSelector  string `mapstructure:"wait_for_selector"`
	TimeoutS         int    `mapstructure:"timeout_s"`
}

// DNSConfig holds DNS resolver target settings.
type DNSConfig struct {
	Resolver   string `mapstructure:"resolver"`
	RecordType string `mapstructure:"record_type"`
}

// WebSocketConfig holds WebSocket target settings.
type WebSocketConfig struct {
	DurationS      int      `mapstructure:"duration_s"`
	SendMessages   []string `mapstructure:"send_messages"`
	ExpectMessages int      `mapstructure:"expect_messages"`
}

// MetricsConfig controls Prometheus metrics exposition.
type MetricsConfig struct {
	Enabled        bool `mapstructure:"enabled"`
	PrometheusPort int  `mapstructure:"prometheus_port"`
}

// DaemonConfig holds daemon/process settings.
type DaemonConfig struct {
	PIDFile   string `mapstructure:"pid_file"`
	LogLevel  string `mapstructure:"log_level"`
	LogFormat string `mapstructure:"log_format"`
}
