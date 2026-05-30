package config

import "testing"

func TestLoad_defaults(t *testing.T) {
	t.Setenv("EVE_SESSION_SECRET", "test-secret")
	t.Setenv("EVE_ADMIN_INITIAL_PASSWORD", "admin-pw")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr default = %q, want :8080", cfg.Addr)
	}
	if cfg.DBPath != "/data/eve.db" {
		t.Errorf("DBPath default = %q, want /data/eve.db", cfg.DBPath)
	}
	if cfg.UsersDir != "/data/users" {
		t.Errorf("UsersDir default = %q, want /data/users", cfg.UsersDir)
	}
}

func TestLoad_overrides_and_required(t *testing.T) {
	t.Setenv("EVE_ADDR", ":9000")
	t.Setenv("EVE_SESSION_SECRET", "")
	t.Setenv("EVE_ADMIN_INITIAL_PASSWORD", "admin-pw")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when EVE_SESSION_SECRET is empty")
	}
}
