package table

import "time"

type V22Regression struct {
	Id                string `xorm:"varchar(36) pk"`
	OwnerId           int64  `xorm:"owner_id"`
	CollectorId       int64  `xorm:"collector_id"`
	Kind              string
	CollectorRevision int    `xorm:"collector_revision"`
	BaseVersionId     string `xorm:"base_version_id"`
	TargetVersionId   string `xorm:"target_version_id"`
	BaseHash          string `xorm:"base_hash"`
	TargetHash        string `xorm:"target_hash"`
	Snapshot          string `xorm:"jsonb snapshot"`
	SnapshotHash      string `xorm:"snapshot_hash"`
	Status            string
	Total             int
	Completed         int
	Report            string `xorm:"jsonb report"`
	ErrorCode         string `xorm:"error_code"`
	Lease             string
	DeadlineAt        time.Time `xorm:"deadline_at"`
	IdempotencyKey    string    `xorm:"idempotency_key"`
	Fingerprint       string
	CreatedAt         time.Time  `xorm:"created_at"`
	FinishedAt        *time.Time `xorm:"finished_at"`
}

func (V22Regression) TableName() string { return "v22_regressions" }

type V22VersionRestore struct {
	Id                string `xorm:"varchar(36) pk"`
	OwnerId           int64  `xorm:"owner_id"`
	CollectorId       int64  `xorm:"collector_id"`
	VersionId         string `xorm:"version_id"`
	TargetCollectorId int64  `xorm:"target_collector_id"`
	IdempotencyKey    string `xorm:"idempotency_key"`
	Fingerprint       string
	CreatedAt         time.Time `xorm:"created_at"`
}

func (V22VersionRestore) TableName() string { return "v22_version_restores" }
