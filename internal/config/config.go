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

func New() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		LogLevel: getEnv("LOG_LEVEL", defaultLogLevel),
		Server: ServerConfig{
			Port: getEnv("PORT", defaultPort),
		},
		Grok: GrokConfig{
			BaseURL:         strings.TrimRight(getEnv("GROK_BASE_URL", defaultBaseURL), "/"),
			CookieFile:      os.Getenv("GROK_COOKIE_FILE"),
			Cookies:         os.Getenv("GROK_COOKIES"),
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
