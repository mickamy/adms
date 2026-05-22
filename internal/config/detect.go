package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var ErrNoConfigFound = errors.New("no config file found")

var candidateNames = []string{"adms.yaml", "adms.yml", "adms.toml"}

func Detect(dir string) (string, error) {
	for _, name := range candidateNames {
		path := filepath.Join(dir, name)

		info, err := os.Stat(path)
		if err == nil {
			if !info.IsDir() {
				return path, nil
			}

			continue
		}

		if errors.Is(err, fs.ErrNotExist) {
			continue
		}

		return "", fmt.Errorf("stat %s: %w", path, err)
	}

	return "", ErrNoConfigFound
}
