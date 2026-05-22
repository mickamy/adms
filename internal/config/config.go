package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mickamy/adms/internal/database"
)

const (
	DefaultListen   = ":7777"
	DefaultUIListen = ":7778"
	DefaultTimeout  = 30 * time.Second
	DefaultLogLevel = "info"
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
	AuthTokenEnv   string
	LogLevel       string
}

type UIConfig struct {
	Enabled bool
	Listen  string
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
	if strings.TrimSpace(c.Driver) == "" {
		return Config{}, errors.New("driver is required")
	}

	drv, err := parseDriver(c.Driver)
	if err != nil {
		return Config{}, err
	}

	if strings.TrimSpace(c.DSN) == "" {
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
		AuthTokenEnv: c.AuthTokenEnv,
		LogLevel:     logLevel,
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
