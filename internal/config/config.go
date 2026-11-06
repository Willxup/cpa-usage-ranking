// Package config loads the service's startup configuration.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"cpa-usage-ranking/internal/registration"
)

// Config contains only settings used by the current service.
type Config struct {
	HTTPAddr              string
	ShutdownTimeout       time.Duration
	WebDir                string
	SQLitePath            string
	TrustedProxies        []string
	RegistrationIPv6By64  bool
	RegistrationRequests  registration.RequestLimits
	RegistrationCreations registration.CreationLimits
}

// Load reads environment variables. Explicitly empty values are invalid.
func Load() (Config, error) {
	cfg := Config{HTTPAddr: "127.0.0.1:8080", ShutdownTimeout: 10 * time.Second, SQLitePath: "data/ranking.db", RegistrationIPv6By64: true, RegistrationRequests: registration.DefaultRequestLimits(), RegistrationCreations: registration.DefaultCreationLimits()}
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
	if value, ok := os.LookupEnv("RANKING_WEB_DIR"); ok {
		cfg.WebDir = strings.TrimSpace(value)
		if cfg.WebDir == "" {
			return Config{}, fmt.Errorf("RANKING_WEB_DIR must be a non-empty directory path when set")
		}
	}
	if value, ok := os.LookupEnv("RANKING_SQLITE_PATH"); ok {
		cfg.SQLitePath = strings.TrimSpace(value)
		if cfg.SQLitePath == "" {
			return Config{}, fmt.Errorf("RANKING_SQLITE_PATH must be a non-empty file path")
		}
	}
	if value, ok := os.LookupEnv("RANKING_TRUSTED_PROXIES"); ok {
		for _, entry := range strings.Split(value, ",") {
			entry = strings.TrimSpace(entry)
			if net.ParseIP(entry) == nil {
				if _, _, err := net.ParseCIDR(entry); err != nil {
					return Config{}, fmt.Errorf("RANKING_TRUSTED_PROXIES must contain comma-separated IP addresses or CIDRs")
				}
			}
			cfg.TrustedProxies = append(cfg.TrustedProxies, entry)
		}
	}
	if value, ok := os.LookupEnv("RANKING_REGISTRATION_IPV6_64"); ok {
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return Config{}, fmt.Errorf("RANKING_REGISTRATION_IPV6_64 must be a boolean")
		}
		cfg.RegistrationIPv6By64 = parsed
	}
	for _, setting := range []struct {
		key    string
		target *int
	}{
		{"RANKING_REGISTRATION_SOURCE_REQUESTS", &cfg.RegistrationRequests.SourceRequests},
		{"RANKING_REGISTRATION_GLOBAL_REQUESTS", &cfg.RegistrationRequests.GlobalRequests},
		{"RANKING_REGISTRATION_REQUEST_MAX_SOURCES", &cfg.RegistrationRequests.MaxSources},
		{"RANKING_REGISTRATION_HOURLY_CREATIONS", &cfg.RegistrationCreations.Hourly},
		{"RANKING_REGISTRATION_DAILY_CREATIONS", &cfg.RegistrationCreations.Daily},
		{"RANKING_REGISTRATION_CREATION_MAX_SOURCES", &cfg.RegistrationCreations.MaxSources},
	} {
		if err := positiveInt(setting.key, setting.target); err != nil {
			return Config{}, err
		}
	}
	if err := positiveDuration("RANKING_REGISTRATION_REQUEST_WINDOW", &cfg.RegistrationRequests.Window); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func positiveInt(key string, target *int) error {
	if value, ok := os.LookupEnv(key); ok {
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsed <= 0 {
			return fmt.Errorf("%s must be a positive integer", key)
		}
		*target = parsed
	}
	return nil
}

func positiveDuration(key string, target *time.Duration) error {
	if value, ok := os.LookupEnv(key); ok {
		parsed, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil || parsed <= 0 {
			return fmt.Errorf("%s must be a positive duration", key)
		}
		*target = parsed
	}
	return nil
}
