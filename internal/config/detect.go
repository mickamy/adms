package config

import (
	"errors"
	"os"
	"path/filepath"
)

var ErrNoConfigFound = errors.New("no config file found")

var candidateNames = []string{"adms.yaml", "adms.yml", "adms.toml"}

func Detect(dir string) (string, error) {
	for _, name := range candidateNames {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}

	return "", ErrNoConfigFound
}
