package table

import "time"

type V22DataSnapshot struct {
	Id            string `xorm:"pk"`
	OwnerId       int64  `xorm:"owner_id"`
	TableId       int64  `xorm:"table_id"`
	Query         string `xorm:"jsonb query"`
	QueryHash     string `xorm:"query_hash"`
	Schema        string `xorm:"jsonb schema"`
	SchemaVersion int    `xorm:"schema_version"`
	Rows          string `xorm:"jsonb rows"`
	Count         int
	CapturedAt    time.Time `xorm:"captured_at"`
	ExpiresAt     time.Time `xorm:"expires_at"`
}

func (V22DataSnapshot) TableName() string { return "v22_data_snapshots" }

type V22DataView struct {
	Id        string `xorm:"pk"`
	OwnerId   int64  `xorm:"owner_id"`
	TableId   int64  `xorm:"table_id"`
	Name      string
	Revision  int
	Query     string    `xorm:"jsonb query"`
	CreatedAt time.Time `xorm:"created_at"`
	UpdatedAt time.Time `xorm:"updated_at"`
}

func (V22DataView) TableName() string { return "v22_data_views" }

type V22Export struct {
	Id             string `xorm:"pk"`
	OwnerId        int64  `xorm:"owner_id"`
	TableId        int64  `xorm:"table_id"`
	SnapshotId     string `xorm:"snapshot_id"`
	Format         string
	Status         string
	Progress       int
	RowCount       int       `xorm:"row_count"`
	Query          string    `xorm:"jsonb query"`
	Schema         string    `xorm:"jsonb schema"`
	CapturedAt     time.Time `xorm:"captured_at"`
	IdempotencyKey string    `xorm:"idempotency_key"`
	Fingerprint    string
	LeaseToken     string     `xorm:"lease_token"`
	LeaseUntil     *time.Time `xorm:"lease_until"`
	ErrorCode      string     `xorm:"error_code"`
	File           []byte     `xorm:"bytea file"`
	FileHash       string     `xorm:"file_hash"`
	FileBytes      int        `xorm:"file_bytes"`
	CreatedAt      time.Time  `xorm:"created_at"`
	FinishedAt     *time.Time `xorm:"finished_at"`
	ExpiresAt      time.Time  `xorm:"expires_at"`
}

func (V22Export) TableName() string { return "v22_exports" }
