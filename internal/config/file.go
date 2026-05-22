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
	Driver         string   `toml:"driver"          yaml:"driver"`
	DSN            string   `toml:"dsn"             yaml:"dsn"`
	Listen         string   `toml:"listen"          yaml:"listen"`
	ReadOnly       bool     `toml:"read_only"       yaml:"read_only"`
	AllowedSchemas []string `toml:"allowed_schemas" yaml:"allowed_schemas"`
	AllowedTables  []string `toml:"allowed_tables"  yaml:"allowed_tables"`
	Timeout        string   `toml:"timeout"         yaml:"timeout"`
	UI             uiConfig `toml:"ui"              yaml:"ui"`
	CORSOrigins    []string `toml:"cors_origins"    yaml:"cors_origins"`
	AuthTokenEnv   string   `toml:"auth_token_env"  yaml:"auth_token_env"`
	LogLevel       string   `toml:"log_level"       yaml:"log_level"`
}

type uiConfig struct {
	Enabled bool   `toml:"enabled" yaml:"enabled"`
	Listen  string `toml:"listen"  yaml:"listen"`
}

func loadFile(path string) (config, error) {
	// G304 is intentional: path is supplied by the operator via the CLI.
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return config{}, fmt.Errorf("read config: %w", err)
	}

	expanded := os.ExpandEnv(string(data))

	var c config

	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".yaml", ".yml":
		dec := yaml.NewDecoder(bytes.NewReader([]byte(expanded)))
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
		meta, err := toml.Decode(expanded, &c)
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

	return c, nil
}
