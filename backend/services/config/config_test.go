package config

import "testing"

func TestLoadUsesDefaultsAndRejectsInvalidNumbers(t *testing.T) {
	t.Setenv("ENV", "development")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("JWT_EXPIRY_HOURS", "")
	t.Setenv("REDIS_DB", "")
	t.Setenv("PORT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Port != "8080" || cfg.JWT.ExpiryHours != 24 || cfg.Redis.DB != 0 {
		t.Fatalf("Load() = %#v; defaults were not applied", cfg)
	}
	if cfg.Database.URL == "" {
		t.Fatal("Load() did not set the development database URL")
	}

	t.Setenv("JWT_EXPIRY_HOURS", "0")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a non-positive JWT expiry")
	}

	t.Setenv("JWT_EXPIRY_HOURS", "24")
	t.Setenv("REDIS_DB", "-1")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a negative Redis database index")
	}
}

func TestLoadRequiresDatabaseURLInProduction(t *testing.T) {
	t.Setenv("ENV", "production")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("JWT_SECRET", "test-secret")
	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded without DATABASE_URL in production")
	}
}

func TestLoadRequiresJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded without JWT_SECRET")
	}
}
