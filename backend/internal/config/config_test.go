package config

import "testing"

func TestLoad_defaults(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "test-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr default = %q, want :8080", cfg.Addr)
	}
	if cfg.DBPath != "/data/spark.db" {
		t.Errorf("DBPath default = %q, want /data/spark.db", cfg.DBPath)
	}
	if cfg.UsersDir != "/data/users" {
		t.Errorf("UsersDir default = %q, want /data/users", cfg.UsersDir)
	}
}

func TestLoad_overrides_and_required(t *testing.T) {
	t.Setenv("SPARK_ADDR", ":9000")
	t.Setenv("SPARK_SESSION_SECRET", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when SPARK_SESSION_SECRET is empty")
	}
}

func TestLoad_defaultsDoNotRequireAdminPassword(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "test-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.AdminInitialPassword != "" {
		t.Fatalf("AdminInitialPassword = %q, want empty legacy field", cfg.AdminInitialPassword)
	}
}

func TestLoad_oidcSettings(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "test-secret")
	t.Setenv("SPARK_PUBLIC_URL", "https://spark.example.com")
	t.Setenv("SPARK_OIDC_ISSUER", "https://auth.example.com/application/o/spark/")
	t.Setenv("SPARK_OIDC_CLIENT_ID", "spark-client")
	t.Setenv("SPARK_OIDC_CLIENT_SECRET", "spark-secret")
	t.Setenv("SPARK_OIDC_REDIRECT_URL", "https://spark.example.com/api/auth/callback")
	t.Setenv("SPARK_OIDC_POST_LOGOUT_REDIRECT_URL", "https://spark.example.com/")
	t.Setenv("SPARK_OIDC_ADMIN_GROUP", "spark-admins")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.OIDC.Issuer != "https://auth.example.com/application/o/spark/" {
		t.Fatalf("OIDC issuer = %q", cfg.OIDC.Issuer)
	}
	if cfg.OIDC.AdminGroup != "spark-admins" {
		t.Fatalf("OIDC admin group = %q", cfg.OIDC.AdminGroup)
	}
}
