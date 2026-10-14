package migrate

import "xorm.io/xorm"

type coOutputPorts struct{}

func init()                           { registerMigrate(new(coOutputPorts)) }
func (*coOutputPorts) Version() int64 { return 2026_10_14_001 }
func (*coOutputPorts) Desc() string   { return "named output ports and durable pass-through results" }
func (*coOutputPorts) Exec(e *xorm.Engine) error {
	_, err := e.Exec(`
 ALTER TABLE co_run_outputs ADD COLUMN IF NOT EXISTS port TEXT NOT NULL DEFAULT 'records';
 ALTER TABLE co_observations ADD COLUMN IF NOT EXISTS port TEXT NOT NULL DEFAULT 'records';
 ALTER TABLE co_run_outputs DROP CONSTRAINT IF EXISTS co_run_outputs_item_id_output_identity_key;
 CREATE UNIQUE INDEX IF NOT EXISTS idx_co_outputs_port_identity ON co_run_outputs(item_id,port,output_identity);
 `)
	return err
}
