package db

import (
	"bytes"
	"context"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	"xorm.io/xorm"
	xormlog "xorm.io/xorm/log"
)

// Exercise the xorm adapter and core decision without opening a connection.
func TestSQLLoggingSwitch(t *testing.T) {
	e, err := xorm.NewEngine("postgres", "host=127.0.0.1 dbname=sql_log_switch")
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.SetLogger(newXormLogger())
	ctx := context.Background()
	if e.DB().NeedLogSQL(ctx) {
		t.Fatal("SQL logging must be off by default")
	}
	e.ShowSQL()
	if !e.DB().NeedLogSQL(ctx) {
		t.Fatal("ShowSQL() must enable SQL logging")
	}
	private := context.WithValue(ctx, xormlog.SessionShowSQLKey, false)
	if e.DB().NeedLogSQL(private) {
		t.Fatal("session suppression must override the global SQL switch")
	}
	e.ShowSQL(false)
	if e.DB().NeedLogSQL(ctx) {
		t.Fatal("ShowSQL(false) must disable SQL logging")
	}
	e.ShowSQL(true)
	if !e.DB().NeedLogSQL(ctx) {
		t.Fatal("SQL logging must be re-enablable")
	}
}

func TestSQLLoggingDisabledKeepsErrors(t *testing.T) {
	previousOutput, previousLevel := log.StandardLogger().Out, log.GetLevel()
	t.Cleanup(func() { log.SetOutput(previousOutput); log.SetLevel(previousLevel) })
	var output bytes.Buffer
	log.SetOutput(&output)
	log.SetLevel(log.ErrorLevel)
	l := newXormLogger()
	l.ShowSQL(false)
	l.Errorf("database failure: %s", "test error")
	if !strings.Contains(output.String(), "database failure: test error") {
		t.Fatal("disabling SQL must not discard error logs")
	}
}
