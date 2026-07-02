package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mickamy/adms/internal/database"
)

const (
	DefaultListen       = ":7777"
	DefaultUIListen     = ":7778"
	DefaultTimeout      = 30 * time.Second
	DefaultLogLevel     = "info"
	DefaultLimit        = 100
	DefaultMaxLimit     = 1000
	DefaultMaxBodyBytes = int64(10 << 20) // 10 MiB
)

type Config struct {
	Driver         database.Driver
	DSN            string
	Listen         string
	ReadOnly       bool
	AllowedSchemas []string
	AllowedTables  []string
	Timeout        time.Duration
	UI             UIConfig
	CORSOrigins    []string
	Auth           Auth
	LogLevel       string
	DefaultLimit   int
	MaxLimit       int
	MaxBodyBytes   int64
}

type UIConfig struct {
	Enabled bool
	Listen  string
}

// AuthMode selects how the server authenticates requests.
type AuthMode string

const (
	AuthModeNone   AuthMode = "none"
	AuthModeStatic AuthMode = "static"
	AuthModeOIDC   AuthMode = "oidc"
)

// Auth holds the resolved authentication settings. Token is populated by the
// cli from the TokenEnv environment variable when Mode is static, and is empty
// otherwise.
type Auth struct {
	Mode     AuthMode
	TokenEnv string
	Token    string
	OIDC     OIDC
}

// OIDC holds the settings for validating OIDC/JWT bearer tokens. The JWKS is
// fetched from the issuer's discovery document, so no key material lives in the
// config.
type OIDC struct {
	Issuer     string
	Audience   string
	RolesClaim string
}

func Load(path string) (Config, error) {
	c, err := loadFile(path)
	if err != nil {
		return Config{}, err
	}

	cfg, err := buildConfig(c)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}

	return cfg, nil
}

func buildConfig(c config) (Config, error) {
	if c.Driver == "" {
		return Config{}, errors.New("driver is required")
	}

	drv, err := parseDriver(c.Driver)
	if err != nil {
		return Config{}, err
	}

	if c.DSN == "" {
		return Config{}, errors.New("dsn is required")
	}

	timeout := DefaultTimeout
	if c.Timeout != "" {
		d, err := time.ParseDuration(c.Timeout)
		if err != nil {
			return Config{}, fmt.Errorf("invalid timeout %q: %w", c.Timeout, err)
		}

		if d <= 0 {
			return Config{}, fmt.Errorf("invalid timeout %q: must be positive", c.Timeout)
		}

		timeout = d
	}

	listen := c.Listen
	if listen == "" {
		listen = DefaultListen
	}

	uiListen := c.UI.Listen
	if uiListen == "" {
		uiListen = DefaultUIListen
	}

	logLevel := c.LogLevel
	if logLevel == "" {
		logLevel = DefaultLogLevel
	}

	logLevel = strings.ToLower(logLevel)
	switch logLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, fmt.Errorf("invalid log_level %q (want debug, info, warn, or error)", c.LogLevel)
	}

	defaultLimit := DefaultLimit
	if c.DefaultLimit != nil {
		defaultLimit = *c.DefaultLimit
	}

	maxLimit := DefaultMaxLimit
	if c.MaxLimit != nil {
		maxLimit = *c.MaxLimit
	}

	if defaultLimit <= 0 {
		return Config{}, fmt.Errorf("invalid default_limit %d: must be positive", defaultLimit)
	}

	if maxLimit <= 0 {
		return Config{}, fmt.Errorf("invalid max_limit %d: must be positive", maxLimit)
	}

	if defaultLimit > maxLimit {
		return Config{}, fmt.Errorf("default_limit (%d) must not exceed max_limit (%d)", defaultLimit, maxLimit)
	}

	maxBodyBytes := DefaultMaxBodyBytes
	if c.MaxBodyBytes != nil {
		maxBodyBytes = *c.MaxBodyBytes
	}

	if maxBodyBytes <= 0 {
		return Config{}, fmt.Errorf("invalid max_body_bytes %d: must be positive", maxBodyBytes)
	}

	auth, err := buildAuth(c.Auth)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Driver:         drv,
		DSN:            c.DSN,
		Listen:         listen,
		ReadOnly:       c.ReadOnly,
		AllowedSchemas: c.AllowedSchemas,
		AllowedTables:  c.AllowedTables,
		Timeout:        timeout,
		UI: UIConfig{
			Enabled: c.UI.Enabled,
			Listen:  uiListen,
		},
		CORSOrigins:  c.CORSOrigins,
		Auth:         auth,
		LogLevel:     logLevel,
		DefaultLimit: defaultLimit,
		MaxLimit:     maxLimit,
		MaxBodyBytes: maxBodyBytes,
	}, nil
}

// buildAuth validates the auth block and returns the resolved settings. The
// static token itself is not read here; the cli resolves TokenEnv into Token
// at startup so the token value never lives in the config file.
func buildAuth(a authConfig) (Auth, error) {
	mode := AuthMode(strings.ToLower(a.Mode))
	if a.Mode == "" {
		mode = AuthModeNone
	}

	switch mode {
	case AuthModeNone, AuthModeStatic, AuthModeOIDC:
	default:
		return Auth{}, fmt.Errorf("invalid auth.mode %q (want none, static, or oidc)", a.Mode)
	}

	if mode == AuthModeStatic && a.Static.TokenEnv == "" {
		return Auth{}, errors.New("auth.static.token_env is required when auth.mode is static")
	}

	if mode == AuthModeOIDC {
		if a.OIDC.Issuer == "" {
			return Auth{}, errors.New("auth.oidc.issuer is required when auth.mode is oidc")
		}

		if a.OIDC.Audience == "" {
			return Auth{}, errors.New("auth.oidc.audience is required when auth.mode is oidc")
		}
	}

	return Auth{
		Mode:     mode,
		TokenEnv: a.Static.TokenEnv,
		OIDC: OIDC{
			Issuer:     a.OIDC.Issuer,
			Audience:   a.OIDC.Audience,
			RolesClaim: a.OIDC.RolesClaim,
		},
	}, nil
}

func parseDriver(s string) (database.Driver, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "postgres", "postgresql", "pg":
		return database.DriverPostgres, nil
	case "mysql":
		return database.DriverMySQL, nil
	default:
		return "", fmt.Errorf("unknown driver %q (want postgres or mysql)", s)
	}
}
