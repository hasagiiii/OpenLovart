// Package config loads typed configuration from environment variables and an
// optional `.env` file using viper. Environment variables always take
// precedence over values defined in the file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds the runtime configuration for the backend service.
//
// Field tags use `mapstructure` so viper can bind environment variable names
// (e.g. ENV, HTTP_ADDR) directly onto the struct.
type Config struct {
	// ----- Core runtime -----

	// Env selects environment-specific behavior ("dev" or "prod").
	Env string `mapstructure:"ENV"`

	// HTTPAddr is the listen address passed to the HTTP server (e.g. ":8080").
	HTTPAddr string `mapstructure:"HTTP_ADDR"`

	// LogLevel controls the minimum slog level: "debug" | "info" | "warn" | "error".
	LogLevel string `mapstructure:"LOG_LEVEL"`

	// DatabaseURL is the PostgreSQL DSN consumed by GORM.
	DatabaseURL string `mapstructure:"DATABASE_URL"`

	// FrontendOrigin, when non-empty, enables CORS for that exact origin.
	// In dev with a Next.js rewrite proxy this is typically left blank
	// because the browser sees same-origin traffic.
	FrontendOrigin string `mapstructure:"FRONTEND_ORIGIN"`

	// ----- Auth: JWT (RS256 + JWKS) -----

	// AuthJWTPrivateKeyPath / AuthJWTPublicKeyPath point at PEM files holding
	// the active RSA keypair used to sign and verify access tokens.
	// Both may be empty in dev (auto-generated under backend/.dev-keys/) but
	// must be provided in non-dev environments.
	AuthJWTPrivateKeyPath string `mapstructure:"AUTH_JWT_PRIVATE_KEY_PATH"`
	AuthJWTPublicKeyPath  string `mapstructure:"AUTH_JWT_PUBLIC_KEY_PATH"`

	// AuthOIDCStateSecret is an HMAC secret used to sign the short-lived OIDC
	// state cookie. Independent of the JWT keys. Must be ≥32 bytes.
	AuthOIDCStateSecret string `mapstructure:"AUTH_OIDC_STATE_SECRET"`

	// AuthAccessTTL / AuthRefreshTTL set access- and refresh-cookie lifetimes.
	AuthAccessTTL  time.Duration `mapstructure:"AUTH_ACCESS_TTL"`
	AuthRefreshTTL time.Duration `mapstructure:"AUTH_REFRESH_TTL"`

	// AuthCookieDomain optionally narrows cookies to a specific domain. When
	// empty the browser scopes them to the request host (recommended in dev
	// so __Host- prefix works without TLS).
	AuthCookieDomain string `mapstructure:"AUTH_COOKIE_DOMAIN"`

	// AuthCookieSecure controls the Secure attribute on auth cookies.
	// Must be true in production (which also enables the __Host- prefix).
	AuthCookieSecure bool `mapstructure:"AUTH_COOKIE_SECURE"`

	// ----- Auth: Google OIDC -----

	OIDCGoogleClientID     string `mapstructure:"OIDC_GOOGLE_CLIENT_ID"`
	OIDCGoogleClientSecret string `mapstructure:"OIDC_GOOGLE_CLIENT_SECRET"`
	OIDCGoogleRedirectURL  string `mapstructure:"OIDC_GOOGLE_REDIRECT_URL"`

	// ----- Email (SMTP) -----

	SMTPHost     string `mapstructure:"SMTP_HOST"`
	SMTPPort     int    `mapstructure:"SMTP_PORT"`
	SMTPUsername string `mapstructure:"SMTP_USERNAME"`
	SMTPPassword string `mapstructure:"SMTP_PASSWORD"`
	SMTPFrom     string `mapstructure:"SMTP_FROM"`

	// EmailVerifyBaseURL / PasswordResetBaseURL are the public URLs
	// (frontend) embedded in email templates.
	EmailVerifyBaseURL   string `mapstructure:"EMAIL_VERIFY_BASE_URL"`
	PasswordResetBaseURL string `mapstructure:"PASSWORD_RESET_BASE_URL"`

	// ----- Rate limiting -----

	RateLimitLoginPerMin     int `mapstructure:"RATE_LIMIT_LOGIN_PER_MIN"`
	RateLimitRegisterPerHour int `mapstructure:"RATE_LIMIT_REGISTER_PER_HOUR"`

	// ----- AI generation (chat / image / web search) -----

	// AIChatModel selects the OpenAI-compatible chat model driving the
	// trpc-agent-go agent (default "gpt-5.4-mini").
	AIChatModel string `mapstructure:"AI_CHAT_MODEL"`

	// OpenAIAPIKey / OpenAIBaseURL authenticate the chat model. The
	// trpc-agent-go OpenAI model also reads these from the process
	// environment; we surface them here for explicit wiring and so a
	// missing key is a soft (tool-disabled) rather than fatal condition.
	OpenAIAPIKey  string `mapstructure:"OPENAI_API_KEY"`
	OpenAIBaseURL string `mapstructure:"OPENAI_BASE_URL"`

	// AIImageProvider selects the image-generation backend (default "fal").
	// AIImageModel names the provider model; AIImageAPIKey is the provider
	// credential (the FAL_KEY env var is accepted as a fallback).
	AIImageProvider string `mapstructure:"AI_IMAGE_PROVIDER"`
	AIImageModel    string `mapstructure:"AI_IMAGE_MODEL"`
	AIImageAPIKey   string `mapstructure:"AI_IMAGE_API_KEY"`

	// AISearchProvider selects the web-search backend for the chat agent's
	// `web_search` tool (default "brave"). AISearchAPIKey is the credential;
	// when empty the tool is not registered.
	AISearchProvider string `mapstructure:"AI_SEARCH_PROVIDER"`
	AISearchAPIKey   string `mapstructure:"AI_SEARCH_API_KEY"`
}

// Default values applied when neither the environment nor `.env` provides a value.
const (
	defaultEnv         = "dev"
	defaultHTTPAddr    = ":8080"
	defaultLogLevel    = "info"
	defaultDatabaseURL = "postgres://openlovart:openlovart@localhost:5432/openlovart?sslmode=disable"

	defaultAuthAccessTTL  = 15 * time.Minute
	defaultAuthRefreshTTL = 720 * time.Hour // 30d
	defaultAuthCookieSec  = false           // dev default; must be true in prod

	defaultSMTPHost = "localhost"
	defaultSMTPPort = 1025

	defaultRateLimitLoginPerMin     = 10
	defaultRateLimitRegisterPerHour = 20

	// AI generation defaults. Credentials intentionally have no default:
	// when unset the corresponding feature degrades (chat without a key
	// simply errors at call time; web search without a key is disabled).
	defaultAIChatModel      = "gpt-5.4-mini"
	defaultOpenAIBaseURL    = "https://opentk.ai/v1"
	defaultAIImageProvider  = "fal"
	defaultAISearchProvider = "brave"

	// minOIDCStateSecretBytes is the minimum acceptable length of
	// AUTH_OIDC_STATE_SECRET. 32 bytes matches HMAC-SHA256 input width.
	minOIDCStateSecretBytes = 32
)

// validLogLevels enumerates the slog levels we accept.
var validLogLevels = map[string]struct{}{
	"debug": {},
	"info":  {},
	"warn":  {},
	"error": {},
}

// Load reads configuration from environment variables and an optional
// `backend/.env` file (relative to the current working directory). Environment
// variables override file values. Defaults are applied for any unset key.
//
// Load also calls Validate on the result; callers receive a fully-validated
// Config or an error.
func Load() (*Config, error) {
	v := viper.New()

	// Defaults.
	v.SetDefault("ENV", defaultEnv)
	v.SetDefault("HTTP_ADDR", defaultHTTPAddr)
	v.SetDefault("LOG_LEVEL", defaultLogLevel)
	v.SetDefault("DATABASE_URL", defaultDatabaseURL)

	v.SetDefault("AUTH_ACCESS_TTL", defaultAuthAccessTTL)
	v.SetDefault("AUTH_REFRESH_TTL", defaultAuthRefreshTTL)
	v.SetDefault("AUTH_COOKIE_SECURE", defaultAuthCookieSec)

	v.SetDefault("SMTP_HOST", defaultSMTPHost)
	v.SetDefault("SMTP_PORT", defaultSMTPPort)

	v.SetDefault("RATE_LIMIT_LOGIN_PER_MIN", defaultRateLimitLoginPerMin)
	v.SetDefault("RATE_LIMIT_REGISTER_PER_HOUR", defaultRateLimitRegisterPerHour)

	v.SetDefault("AI_CHAT_MODEL", defaultAIChatModel)
	v.SetDefault("OPENAI_BASE_URL", defaultOpenAIBaseURL)
	v.SetDefault("AI_IMAGE_PROVIDER", defaultAIImageProvider)
	v.SetDefault("AI_SEARCH_PROVIDER", defaultAISearchProvider)

	// Optional .env file. We do not error if the file is absent.
	v.SetConfigName(".env")
	v.SetConfigType("env")
	v.AddConfigPath(".")
	v.AddConfigPath(filepath.Join(".", "backend"))
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, fmt.Errorf("read .env: %w", err)
		}
	}

	// Environment variables (always last so they win over the file).
	v.AutomaticEnv()

	// Credential-style keys have no default value. viper.Unmarshal only
	// considers keys it already knows about (defaults, config file, or an
	// explicit bind), so AutomaticEnv alone is not enough — bind them
	// explicitly so a value present only in the environment is picked up.
	for _, key := range []string{
		"OPENAI_API_KEY",
		"AI_IMAGE_MODEL",
		"AI_IMAGE_API_KEY",
		"AI_SEARCH_API_KEY",
	} {
		_ = v.BindEnv(key)
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Accept the conventional FAL_KEY as a fallback for the image-provider
	// credential when AI_IMAGE_API_KEY is not set.
	if strings.TrimSpace(cfg.AIImageAPIKey) == "" {
		if falKey := strings.TrimSpace(os.Getenv("FAL_KEY")); falKey != "" {
			cfg.AIImageAPIKey = falKey
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// WebSearchEnabled reports whether a web-search credential is configured. When
// false, the chat agent's `web_search` tool must not be registered.
func (c *Config) WebSearchEnabled() bool {
	return strings.TrimSpace(c.AISearchAPIKey) != ""
}

// Validate ensures the config values are well-formed enough to start the
// server. It returns an error describing the first problem found.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return errors.New("config: DATABASE_URL must not be empty")
	}
	if _, ok := validLogLevels[strings.ToLower(c.LogLevel)]; !ok {
		return fmt.Errorf("config: LOG_LEVEL %q is not one of debug|info|warn|error", c.LogLevel)
	}
	if strings.TrimSpace(c.HTTPAddr) == "" {
		return errors.New("config: HTTP_ADDR must not be empty")
	}
	if strings.TrimSpace(c.Env) == "" {
		return errors.New("config: ENV must not be empty")
	}

	if c.AuthAccessTTL <= 0 {
		return errors.New("config: AUTH_ACCESS_TTL must be > 0")
	}
	if c.AuthRefreshTTL <= 0 {
		return errors.New("config: AUTH_REFRESH_TTL must be > 0")
	}
	if c.AuthRefreshTTL <= c.AuthAccessTTL {
		return errors.New("config: AUTH_REFRESH_TTL must be greater than AUTH_ACCESS_TTL")
	}

	// AUTH_OIDC_STATE_SECRET: required in prod; optional in dev (a synthetic
	// secret is acceptable, but if provided it must still meet the minimum).
	stateSecret := strings.TrimSpace(c.AuthOIDCStateSecret)
	if !c.IsDev() && len(stateSecret) < minOIDCStateSecretBytes {
		return fmt.Errorf(
			"config: AUTH_OIDC_STATE_SECRET must be at least %d bytes (got %d)",
			minOIDCStateSecretBytes, len(stateSecret),
		)
	}
	if c.IsDev() && stateSecret != "" && len(stateSecret) < minOIDCStateSecretBytes {
		return fmt.Errorf(
			"config: AUTH_OIDC_STATE_SECRET must be at least %d bytes when set (got %d)",
			minOIDCStateSecretBytes, len(stateSecret),
		)
	}

	// In non-dev, JWT key paths must be present and readable. In dev they are
	// optional (auto-generated on first run by the auth/jwt KeyStore).
	if !c.IsDev() {
		if strings.TrimSpace(c.AuthJWTPrivateKeyPath) == "" {
			return errors.New("config: AUTH_JWT_PRIVATE_KEY_PATH is required when ENV != dev")
		}
		if strings.TrimSpace(c.AuthJWTPublicKeyPath) == "" {
			return errors.New("config: AUTH_JWT_PUBLIC_KEY_PATH is required when ENV != dev")
		}
		if err := assertReadable(c.AuthJWTPrivateKeyPath); err != nil {
			return fmt.Errorf("config: AUTH_JWT_PRIVATE_KEY_PATH: %w", err)
		}
		if err := assertReadable(c.AuthJWTPublicKeyPath); err != nil {
			return fmt.Errorf("config: AUTH_JWT_PUBLIC_KEY_PATH: %w", err)
		}
		if !c.AuthCookieSecure {
			return errors.New("config: AUTH_COOKIE_SECURE must be true when ENV != dev")
		}
	}

	if c.SMTPPort <= 0 || c.SMTPPort > 65535 {
		return fmt.Errorf("config: SMTP_PORT %d is out of range", c.SMTPPort)
	}

	if c.RateLimitLoginPerMin <= 0 {
		return errors.New("config: RATE_LIMIT_LOGIN_PER_MIN must be > 0")
	}
	if c.RateLimitRegisterPerHour <= 0 {
		return errors.New("config: RATE_LIMIT_REGISTER_PER_HOUR must be > 0")
	}

	return nil
}

// IsDev reports whether the configuration targets the development environment.
func (c *Config) IsDev() bool {
	return strings.EqualFold(c.Env, "dev")
}

// assertReadable returns nil iff the supplied path resolves to an existing,
// regular, readable file. Used by Validate for prod JWT key paths.
func assertReadable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %q: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%q is a directory, expected a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %q: %w", path, err)
	}
	_ = f.Close()
	return nil
}
