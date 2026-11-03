// Package config loads the service's startup configuration.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains only settings used by the current service.
type Config struct {
	HTTPAddr        string
	ShutdownTimeout time.Duration
}

// Load reads environment variables. Explicitly empty values are invalid.
func Load() (Config, error) {
	cfg := Config{HTTPAddr: "127.0.0.1:8080", ShutdownTimeout: 10 * time.Second}
	if value, ok := os.LookupEnv("RANKING_HTTP_ADDR"); ok {
		cfg.HTTPAddr = strings.TrimSpace(value)
	}
	host, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil || strings.ContainsAny(host, "/\\ \t\r\n") || port == "" {
		return Config{}, fmt.Errorf("RANKING_HTTP_ADDR must be a host:numeric-port address")
	}
	for _, char := range port {
		if char < '0' || char > '9' {
			return Config{}, fmt.Errorf("RANKING_HTTP_ADDR port must contain digits only")
		}
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return Config{}, fmt.Errorf("RANKING_HTTP_ADDR port must be between 0 and 65535")
	}
	if value, ok := os.LookupEnv("RANKING_SHUTDOWN_TIMEOUT"); ok {
		duration, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil || duration <= 0 {
			return Config{}, fmt.Errorf("RANKING_SHUTDOWN_TIMEOUT must be a positive duration")
		}
		cfg.ShutdownTimeout = duration
	}
	return cfg, nil
}
