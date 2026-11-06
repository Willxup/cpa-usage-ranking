package test

import (
	"testing"
	"time"

	"cpa-usage-ranking/internal/config"
	"cpa-usage-ranking/internal/registration"
)

func TestRegistrationRequestConfiguration(t *testing.T) {
	clearEnvironment(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RegistrationRequests != registration.DefaultRequestLimits() || cfg.RegistrationCreations != registration.DefaultCreationLimits() || !cfg.RegistrationEnabled {
		t.Fatalf("defaults: %+v", cfg)
	}
	t.Setenv("RANKING_REGISTRATION_SOURCE_REQUESTS", "2")
	t.Setenv("RANKING_REGISTRATION_GLOBAL_REQUESTS", "3")
	t.Setenv("RANKING_REGISTRATION_REQUEST_MAX_SOURCES", "4")
	t.Setenv("RANKING_REGISTRATION_REQUEST_WINDOW", "5s")
	t.Setenv("RANKING_REGISTRATION_HOURLY_CREATIONS", "4")
	t.Setenv("RANKING_REGISTRATION_DAILY_CREATIONS", "12")
	t.Setenv("RANKING_REGISTRATION_CREATION_MAX_SOURCES", "50")
	t.Setenv("RANKING_REGISTRATION_ENABLED", "false")
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := registration.RequestLimits{SourceRequests: 2, GlobalRequests: 3, MaxSources: 4, Window: 5 * time.Second}
	if cfg.RegistrationRequests != want || cfg.RegistrationCreations != (registration.CreationLimits{Hourly: 4, Daily: 12, MaxSources: 50}) || cfg.RegistrationEnabled {
		t.Fatalf("overrides: %+v", cfg)
	}
}
