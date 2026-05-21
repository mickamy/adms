package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mickamy/adms/internal/database"
)

type Config struct {
	Driver         database.Driver
	DSN            string
	AllowedSchemas []string
	AllowedTables  []string
}

// Parse resolves CLI flags and ADMS_* environment variables into a Config.
// Flag values win over environment values when both are set.
func Parse(args []string, stderr io.Writer) (Config, error) {
	fs := flag.NewFlagSet("adms", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	driver := fs.String("driver", os.Getenv("ADMS_DRIVER"), "DB driver (postgres or mysql)")
	dsn := fs.String("dsn", os.Getenv("ADMS_DSN"), "DB connection string")
	allowedSchemas := fs.String("allowed-schemas", os.Getenv("ADMS_ALLOWED_SCHEMAS"),
		"comma-separated schemas to introspect")
	allowedTables := fs.String("allowed-tables", os.Getenv("ADMS_ALLOWED_TABLES"),
		"comma-separated table allowlist")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(stderr)
			fs.Usage()
		}

		return Config{}, fmt.Errorf("parse flags: %w", err)
	}

	if *driver == "" {
		return Config{}, errors.New("--driver is required (or set ADMS_DRIVER)")
	}

	if *dsn == "" {
		return Config{}, errors.New("--dsn is required (or set ADMS_DSN)")
	}

	d, err := parseDriver(*driver)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Driver:         d,
		DSN:            *dsn,
		AllowedSchemas: splitCSV(*allowedSchemas),
		AllowedTables:  splitCSV(*allowedTables),
	}, nil
}

func parseDriver(s string) (database.Driver, error) {
	switch strings.ToLower(s) {
	case "postgres", "postgresql", "pg":
		return database.DriverPostgres, nil
	case "mysql":
		return database.DriverMySQL, nil
	default:
		return "", fmt.Errorf("unknown driver %q (want postgres or mysql)", s)
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))

	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}

	return out
}
