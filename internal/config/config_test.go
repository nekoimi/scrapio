package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactedMasksSecretsWithoutMutatingSource(t *testing.T) {
	source := &Config{
		JwtSecret: "jwt-secret-value",
		Aria2:     &Aria2Config{Secret: "aria-secret"},
		DB:        &DBConfig{Dsn: "postgres://user:password@db:5432/app"},
		QuickAPI:  &QuickAPIConfig{Token: "quick-token"},
	}
	safe := source.Redacted()
	serialized := safe.String()
	for _, secret := range []string{"jwt-secret-value", "aria-secret", "password", "quick-token"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("redacted config contains secret %q: %s", secret, serialized)
		}
	}
	if source.JwtSecret != "jwt-secret-value" || source.Aria2.Secret != "aria-secret" || source.QuickAPI.Token != "quick-token" {
		t.Fatal("Redacted mutated source config")
	}
}

func TestLoadDefaultsAndEnvironmentOverrides(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "test.yaml")
	if err := os.WriteFile(configPath, []byte("log_level: error\nlog_dir: \""+filepath.ToSlash(t.TempDir())+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", configPath)
	t.Setenv("CRAWLER_WORKER_NUM", "7")
	t.Setenv("LOG_ROTATION_MAX_SIZE_MB", "12")
	t.Setenv("DB_SHOW_SQL", "")
	cfg := Load()
	if cfg.Port != 8093 {
		t.Fatalf("default port = %d; want 8093", cfg.Port)
	}
	if cfg.DB == nil || cfg.DB.ShowSQL {
		t.Fatal("SQL logging should be off by default")
	}
	if cfg.Download != nil || cfg.CloudDriver != nil || cfg.STRM != nil || cfg.Aria2 != nil {
		t.Fatalf("delivery config should not be loaded by the v2 control plane: %+v", cfg)
	}
	if cfg.Crawler == nil || cfg.Crawler.WorkerNum != 7 {
		t.Fatalf("unexpected crawler config: %+v", cfg.Crawler)
	}
	if cfg.LogRotation.MaxSizeMB != 12 || cfg.LogRotation.MaxBackups != 7 || cfg.LogRotation.MaxAgeDays != 7 || !cfg.LogRotation.Compress {
		t.Fatalf("unexpected log rotation config: %+v", cfg.LogRotation)
	}
}

func TestLoadSQLLoggingOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, yamlValue, envValue string
		want                      bool
	}{
		{"yaml_enabled", "true", "", true},
		{"env_enabled", "false", "true", true},
		{"env_disabled", "true", "false", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			content := "log_level: error\nlog_dir: \"" + filepath.ToSlash(t.TempDir()) + "\"\ndb:\n  show_sql: " + tc.yamlValue + "\n"
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CONFIG_FILE", path)
			t.Setenv("DB_SHOW_SQL", tc.envValue)
			cfg := Load()
			if cfg.DB == nil || cfg.DB.ShowSQL != tc.want {
				t.Fatalf("db.show_sql should be %t", tc.want)
			}
		})
	}
}
