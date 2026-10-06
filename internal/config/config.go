// Package config loads runtime configuration from environment variables.
package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Addr          string
	DataDir       string
	BackupDir     string
	MasterKey     [32]byte
	SecureCookies bool

	// Optional bootstrap credentials: create the admin account on first start
	// (or reset its password when AdminReset is set). Never used for login.
	AdminUsername string
	AdminPassword string
	AdminReset    bool

	// Proxies whose X-Forwarded-For / X-Forwarded-Proto headers are trusted.
	TrustedProxies []*net.IPNet
}

func Load() (*Config, error) {
	c := &Config{
		Addr:          ":" + env("PORT", "8080"),
		DataDir:       env("DATA_DIR", "./data"),
		SecureCookies: env("SECURE_COOKIES", "false") == "true",
		AdminUsername: strings.TrimSpace(env("ADMIN_USERNAME", "admin")),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		AdminReset:    env("ADMIN_RESET", "false") == "true",
	}
	proxies, err := ParseCIDRs(env("TRUSTED_PROXIES", defaultTrustedProxies))
	if err != nil {
		return nil, fmt.Errorf("TRUSTED_PROXIES: %w", err)
	}
	c.TrustedProxies = proxies
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	// Destination of the built-in "Server disk" storage; mount a volume here.
	c.BackupDir = env("BACKUP_DIR", filepath.Join(c.DataDir, "backups"))
	if abs, err := filepath.Abs(c.BackupDir); err == nil {
		c.BackupDir = abs
	}
	if err := os.MkdirAll(c.BackupDir, 0o755); err != nil {
		return nil, fmt.Errorf("create backup dir: %w", err)
	}

	key := os.Getenv("MASTER_KEY")
	if key == "" {
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

// Loopback and private ranges: reverse proxies such as Coolify's Traefik reach
// the container from the Docker network.
const defaultTrustedProxies = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"

func ParseCIDRs(list string) ([]*net.IPNet, error) {
	if strings.TrimSpace(list) == "none" {
		return nil, nil
	}
	var nets []*net.IPNet
	for _, s := range strings.Split(list, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			if strings.Contains(s, ":") {
				s += "/128"
			} else {
				s += "/32"
			}
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, err
		}
		nets = append(nets, n)
	}
	return nets, nil
}
