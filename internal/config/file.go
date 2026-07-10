package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

type config struct {
	Driver         string     `toml:"driver"          yaml:"driver"`
	DSN            string     `toml:"dsn"             yaml:"dsn"`
	Listen         string     `toml:"listen"          yaml:"listen"`
	ReadOnly       bool       `toml:"read_only"       yaml:"read_only"`
	AllowedSchemas []string   `toml:"allowed_schemas" yaml:"allowed_schemas"`
	AllowedTables  []string   `toml:"allowed_tables"  yaml:"allowed_tables"`
	Timeout        string     `toml:"timeout"         yaml:"timeout"`
	UI             uiConfig   `toml:"ui"              yaml:"ui"`
	CORSOrigins    []string   `toml:"cors_origins"    yaml:"cors_origins"`
	Auth           authConfig `toml:"auth"            yaml:"auth"`
	LogLevel       string     `toml:"log_level"       yaml:"log_level"`
	DefaultLimit   *int       `toml:"default_limit"   yaml:"default_limit"`
	MaxLimit       *int       `toml:"max_limit"       yaml:"max_limit"`
	MaxBodyBytes   *int64     `toml:"max_body_bytes"  yaml:"max_body_bytes"`
}

type uiConfig struct {
	Enabled bool   `toml:"enabled" yaml:"enabled"`
	Listen  string `toml:"listen"  yaml:"listen"`
}

type authConfig struct {
	Mode   string           `toml:"mode"   yaml:"mode"`
	Static staticAuthConfig `toml:"static" yaml:"static"`
	OIDC   oidcConfig       `toml:"oidc"   yaml:"oidc"`
}

type staticAuthConfig struct {
	TokenEnv string `toml:"token_env" yaml:"token_env"`
}

type oidcConfig struct {
	Issuer     string `toml:"issuer"      yaml:"issuer"`
	Audience   string `toml:"audience"    yaml:"audience"`
	RolesClaim string `toml:"roles_claim" yaml:"roles_claim"`
}

func loadFile(path string) (config, error) {
	// G304 is intentional: path is supplied by the operator via the CLI.
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return config{}, fmt.Errorf("read config: %w", err)
	}

	var c config

	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".yaml", ".yml":
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)

		if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
			return config{}, fmt.Errorf("%s: parse yaml: %w", path, err)
		}

		var extra config
		switch err := dec.Decode(&extra); {
		case err == nil:
			return config{}, fmt.Errorf("%s: parse yaml: multiple YAML documents are not supported", path)
		case !errors.Is(err, io.EOF):
			return config{}, fmt.Errorf("%s: parse yaml: %w", path, err)
		}
	case ".toml":
		meta, err := toml.Decode(string(data), &c)
		if err != nil {
			return config{}, fmt.Errorf("%s: parse toml: %w", path, err)
		}

		if undecoded := meta.Undecoded(); len(undecoded) > 0 {
			keys := make([]string, 0, len(undecoded))
			for _, k := range undecoded {
				keys = append(keys, k.String())
			}

			return config{}, fmt.Errorf("%s: unknown toml key(s): %s", path, strings.Join(keys, ", "))
		}
	default:
		return config{}, fmt.Errorf("%s: unsupported config extension %q (want .yaml, .yml, or .toml)", path, ext)
	}

	expandEnv(&c)
	normalize(&c)

	return c, nil
}

// expandEnv applies os.ExpandEnv to every string-valued field after decoding,
// so env values may contain characters (quotes, backslashes, etc.) that would
// be invalid in raw YAML/TOML.
func expandEnv(c *config) {
	c.Driver = os.ExpandEnv(c.Driver)
	c.DSN = os.ExpandEnv(c.DSN)
	c.Listen = os.ExpandEnv(c.Listen)
	c.Timeout = os.ExpandEnv(c.Timeout)
	c.Auth.Mode = os.ExpandEnv(c.Auth.Mode)
	c.Auth.Static.TokenEnv = os.ExpandEnv(c.Auth.Static.TokenEnv)
	c.Auth.OIDC.Issuer = os.ExpandEnv(c.Auth.OIDC.Issuer)
	c.Auth.OIDC.Audience = os.ExpandEnv(c.Auth.OIDC.Audience)
	c.Auth.OIDC.RolesClaim = os.ExpandEnv(c.Auth.OIDC.RolesClaim)
	c.LogLevel = os.ExpandEnv(c.LogLevel)
	c.UI.Listen = os.ExpandEnv(c.UI.Listen)

	for i, s := range c.AllowedSchemas {
		c.AllowedSchemas[i] = os.ExpandEnv(s)
	}

	for i, s := range c.AllowedTables {
		c.AllowedTables[i] = os.ExpandEnv(s)
	}

	for i, s := range c.CORSOrigins {
		c.CORSOrigins[i] = os.ExpandEnv(s)
	}
}

// normalize trims whitespace from every string field and drops empty entries
// from string-slice fields, so downstream validation and default-application
// only see clean values.
func normalize(c *config) {
	c.Driver = strings.TrimSpace(c.Driver)
	c.DSN = strings.TrimSpace(c.DSN)
	c.Listen = strings.TrimSpace(c.Listen)
	c.Timeout = strings.TrimSpace(c.Timeout)
	c.Auth.Mode = strings.TrimSpace(c.Auth.Mode)
	c.Auth.Static.TokenEnv = strings.TrimSpace(c.Auth.Static.TokenEnv)
	c.Auth.OIDC.Issuer = strings.TrimSpace(c.Auth.OIDC.Issuer)
	c.Auth.OIDC.Audience = strings.TrimSpace(c.Auth.OIDC.Audience)
	c.Auth.OIDC.RolesClaim = strings.TrimSpace(c.Auth.OIDC.RolesClaim)
	c.LogLevel = strings.TrimSpace(c.LogLevel)
	c.UI.Listen = strings.TrimSpace(c.UI.Listen)

	c.AllowedSchemas = trimAndDropEmpty(c.AllowedSchemas)
	c.AllowedTables = trimAndDropEmpty(c.AllowedTables)
	c.CORSOrigins = trimAndDropEmpty(c.CORSOrigins)
}

func trimAndDropEmpty(ss []string) []string {
	if len(ss) == 0 {
		return ss
	}

	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}

	return out
}
