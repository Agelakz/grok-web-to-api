package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Server    ServerConfig
	Grok      GrokConfig
	RateLimit RateLimitConfig
	LogLevel  string
}

type ServerConfig struct {
	Port string
}

type GrokConfig struct {
	// BaseURL of web app (HAR: https://grok.com)
	BaseURL string
	// CookieFile = Netscape cookies.txt from browser export
	CookieFile string
	// Cookies = full Cookie header blob (optional alt to file)
	// Also composed from GROK_SSO / GROK_SSO_RW / ... when set.
	Cookies string
	// Legacy X placeholders (unused by grok.com SSO)
	AuthToken string
	CT0       string

	RefreshInterval int
	MaxRetries      int
}

type RateLimitConfig struct {
	Enabled     bool
	WindowMs    int
	MaxRequests int
}

const (
	defaultPort            = "4982"
	defaultLogLevel        = "info"
	defaultRefreshInterval = 30
	defaultMaxRetries      = 3
	defaultBaseURL         = "https://grok.com"
)

// per-cookie env → Cookie header parts (script-friendly).
// Order matches essentialCookieNames in grok/cookies.go.
var cookieEnvParts = []struct {
	env  string
	name string
}{
	{"GROK_SSO", "sso"},
	{"GROK_SSO_RW", "sso-rw"},
	{"GROK_USER_ID", "x-userid"},
	{"GROK_CF_CLEARANCE", "cf_clearance"},
	{"GROK_CF_BM", "__cf_bm"},
	{"GROK_DEVICE_ID", "grok_device_id"},
}

func New() (*Config, error) {
	_ = godotenv.Load()

	cookies := strings.TrimSpace(os.Getenv("GROK_COOKIES"))
	if cookies == "" {
		cookies = composeCookiesFromEnv()
	}

	cfg := &Config{
		LogLevel: getEnv("LOG_LEVEL", defaultLogLevel),
		Server: ServerConfig{
			Port: getEnv("PORT", defaultPort),
		},
		Grok: GrokConfig{
			BaseURL:         strings.TrimRight(getEnv("GROK_BASE_URL", defaultBaseURL), "/"),
			CookieFile:      os.Getenv("GROK_COOKIE_FILE"),
			Cookies:         cookies,
			AuthToken:       os.Getenv("GROK_AUTH_TOKEN"),
			CT0:             os.Getenv("GROK_CT0"),
			RefreshInterval: getEnvInt("GROK_REFRESH_INTERVAL", defaultRefreshInterval),
			MaxRetries:      getEnvInt("GROK_MAX_RETRIES", defaultMaxRetries),
		},
		RateLimit: RateLimitConfig{
			Enabled:     getEnvBool("RATE_LIMIT_ENABLED", false),
			WindowMs:    getEnvInt("RATE_LIMIT_WINDOW_MS", 60000),
			MaxRequests: getEnvInt("RATE_LIMIT_MAX_REQUESTS", 30),
		},
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// composeCookiesFromEnv builds Cookie header from GROK_SSO / GROK_SSO_RW / ...
// Empty vars skipped. Returns "" if none set.
func composeCookiesFromEnv() string {
	parts := make([]string, 0, len(cookieEnvParts))
	for _, p := range cookieEnvParts {
		v := strings.TrimSpace(os.Getenv(p.env))
		if v == "" {
			continue
		}
		parts = append(parts, p.name+"="+v)
	}
	return strings.Join(parts, "; ")
}

// Validate keeps boot soft: cookies optional so HTTP surface still smokes.
func (c *Config) Validate() error {
	if _, err := strconv.Atoi(c.Server.Port); err != nil {
		return fmt.Errorf("invalid PORT %q", c.Server.Port)
	}
	return nil
}

func (c *Config) HasGrokAuth() bool {
	if strings.TrimSpace(c.Grok.CookieFile) != "" {
		return true
	}
	if strings.TrimSpace(c.Grok.Cookies) != "" {
		return true
	}
	return c.Grok.AuthToken != "" && c.Grok.CT0 != ""
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func getEnvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
