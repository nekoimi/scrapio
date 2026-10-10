package table

import "time"

type V22RepairDraft struct {
	Id                string  `xorm:"varchar(36) pk"`
	OwnerId           int64   `xorm:"owner_id"`
	SourceCollectorId int64   `xorm:"source_collector_id"`
	TargetCollectorId int64   `xorm:"target_collector_id"`
	RunId             string  `xorm:"run_id"`
	CaptureId         *string `xorm:"capture_id"`
	IdempotencyKey    string  `xorm:"idempotency_key"`
	Fingerprint       string
	Context           string    `xorm:"jsonb context"`
	CreatedAt         time.Time `xorm:"created_at"`
}

func (V22RepairDraft) TableName() string { return "v22_repair_drafts" }
