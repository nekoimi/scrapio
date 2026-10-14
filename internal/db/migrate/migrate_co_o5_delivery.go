package migrate

import "xorm.io/xorm"

type coDeliveries struct{}

func init()                          { registerMigrate(new(coDeliveries)) }
func (*coDeliveries) Version() int64 { return 2026_10_14_004 }
func (*coDeliveries) Desc() string {
	return "independent output delivery outbox and original external receipts"
}
func (*coDeliveries) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
 CREATE TABLE IF NOT EXISTS co_deliveries(id VARCHAR(36) PRIMARY KEY,owner_id BIGINT NOT NULL REFERENCES admin(id),run_id VARCHAR(36) NOT NULL REFERENCES co_runs(id),output_id VARCHAR(36) NOT NULL REFERENCES co_run_outputs(id),binding_id TEXT NOT NULL,plugin_code TEXT NOT NULL,plugin_version TEXT NOT NULL,binding_hash TEXT NOT NULL,idempotency_key TEXT NOT NULL UNIQUE,namespace TEXT NOT NULL CHECK(namespace IN ('formal','trial')),status TEXT NOT NULL CHECK(status IN ('preview','queued','sending','checking','succeeded','failed','awaiting_confirmation','cancelled')),input JSONB NOT NULL,error JSONB NOT NULL DEFAULT '{}',receipt JSONB NOT NULL DEFAULT '{}',external_id TEXT NOT NULL DEFAULT '',attempts INT NOT NULL DEFAULT 0,revision INT NOT NULL DEFAULT 1,lease_token TEXT,lease_until TIMESTAMPTZ,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),UNIQUE(output_id,binding_id));
 ALTER TABLE co_deliveries ADD COLUMN IF NOT EXISTS destination_hash TEXT NOT NULL DEFAULT '';
 CREATE INDEX IF NOT EXISTS idx_co_deliveries_run ON co_deliveries(run_id,created_at DESC,id DESC);
 CREATE INDEX IF NOT EXISTS idx_co_deliveries_due ON co_deliveries(updated_at,id) WHERE namespace='formal' AND status IN ('queued','sending','checking');
 CREATE INDEX IF NOT EXISTS idx_co_deliveries_active_owner ON co_deliveries(owner_id) WHERE namespace='formal' AND status IN ('queued','sending','checking','awaiting_confirmation');
 `)
	return err
}
