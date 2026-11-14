package config

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cpa-usage-ranking/internal/admin"
)

func loadAdmin(cfg *Config) error {
	path, set := os.LookupEnv("RANKING_ADMIN_CONFIG_FILE")
	if set {
		if strings.TrimSpace(path) == "" {
			return errors.New("RANKING_ADMIN_CONFIG_FILE must be a non-empty path")
		}
		file, err := os.Open(path)
		if err != nil {
			return errors.New("cannot open administrator configuration file")
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
			return errors.New("administrator configuration must be a small regular JSON file")
		}
		// Group-read is useful for a service identity; never allow public access or group writes.
		if info.Mode().Perm()&0037 != 0 {
			return errors.New("administrator configuration permissions must be 0600 or 0640 (or stricter)")
		}
		if cfg.WebDir != "" {
			root, _ := filepath.Abs(cfg.WebDir)
			if resolved, err := filepath.EvalSymlinks(root); err == nil {
				root = resolved
			}
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				return errors.New("cannot resolve administrator configuration file")
			}
			target, _ = filepath.Abs(target)
			relative, err := filepath.Rel(root, target)
			if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative) {
				return errors.New("administrator configuration must be outside the frontend directory")
			}
		}
		decoder := json.NewDecoder(io.LimitReader(file, 8193))
		decoder.DisallowUnknownFields()
		var owner admin.Owner
		if decoder.Decode(&owner) != nil || decoder.Decode(new(any)) != io.EOF || !admin.ValidCredentials(owner.Username, owner.Password) {
			return errors.New("invalid administrator configuration: use a 3-64 character ASCII username and a 12-72 byte password")
		}
		cfg.AdminOwner = &owner
	}
	if value, ok := os.LookupEnv("RANKING_ADMIN_ALLOW_INSECURE_LOCAL_HTTP"); ok {
		allow, err := strconv.ParseBool(value)
		if err != nil {
			return errors.New("RANKING_ADMIN_ALLOW_INSECURE_LOCAL_HTTP must be a boolean")
		}
		cfg.AdminAllowLocalHTTP = allow
	}
	if cfg.AdminAllowLocalHTTP {
		host, _, _ := net.SplitHostPort(cfg.HTTPAddr)
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return errors.New("insecure administrator HTTP requires a literal loopback listen address")
		}
	}
	return nil
}
