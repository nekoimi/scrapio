package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/spf13/viper"
	"xorm.io/xorm"
)

func TestV22BrowserSessionsMigrationDevDatabase(t *testing.T) {
	if os.Getenv("SCRAPIO_RUN_DEV_DB_MIGRATION_TEST") != "1" {
		t.Skip("set SCRAPIO_RUN_DEV_DB_MIGRATION_TEST=1 to use config/dev.yaml")
	}
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	config := viper.New()
	config.SetConfigFile(filepath.Join(root, "config", "dev.yaml"))
	if err := config.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	engine, err := xorm.NewEngine("postgres", config.GetString("db.dsn"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	engine.SetMaxOpenConns(1)
	engine.SetMaxIdleConns(1)
	if err := engine.Ping(); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("scrapio_a03_test_%d", time.Now().UnixNano())
	if _, err := engine.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer engine.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
	if _, err := engine.Exec("SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Exec("CREATE TABLE admin (id BIGINT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		for _, migration := range []Migrate{new(v22CollectorDrafts), new(v22CollectorValidation), new(v22BrowserSessions)} {
			if err := migration.Exec(engine); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, table := range []string{"v22_browser_sessions", "v22_browser_session_keys"} {
		if _, err := engine.Query("SELECT 1 FROM " + table + " LIMIT 0"); err != nil {
			t.Fatalf("migration did not create %q: %v", table, err)
		}
	}
}
