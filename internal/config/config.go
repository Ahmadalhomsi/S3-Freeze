// Package config loads runtime configuration from environment variables.
package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Addr          string
	DataDir       string
	MasterKey     [32]byte
	SecureCookies bool
}

func Load() (*Config, error) {
	c := &Config{
		Addr:          ":" + env("PORT", "8080"),
		DataDir:       env("DATA_DIR", "./data"),
		SecureCookies: env("SECURE_COOKIES", "false") == "true",
	}
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	key := os.Getenv("MASTER_KEY")
	if key == "" {
		var err error
		if key, err = loadOrCreateKeyFile(filepath.Join(c.DataDir, ".master_key")); err != nil {
			return nil, err
		}
	}
	c.MasterKey = sha256.Sum256([]byte(key))
	return c, nil
}

// loadOrCreateKeyFile keeps a generated master key next to the database so the
// app works without configuration. Setting MASTER_KEY explicitly is preferred.
func loadOrCreateKeyFile(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(b)), nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read master key file: %w", err)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	key := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		return "", fmt.Errorf("write master key file: %w", err)
	}
	slog.Warn("MASTER_KEY not set; generated one", "path", path)
	return key, nil
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
