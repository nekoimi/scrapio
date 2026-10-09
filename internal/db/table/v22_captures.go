package table

import "time"

type V22Capture struct {
	SessionId       string `xorm:"session_id"`
	PageStateId     string `xorm:"page_state_id"`
	BaseURL         string `xorm:"base_url"`
	Id              string `xorm:"varchar(36) pk"`
	OwnerId         int64  `xorm:"owner_id"`
	CollectorId     int64  `xorm:"collector_id"`
	DraftRevision   int    `xorm:"draft_revision"`
	IdempotencyKey  string `xorm:"idempotency_key"`
	Fingerprint     string
	Request         string `xorm:"jsonb request"`
	Source          string
	Format          string
	Status          string
	FinalURL        string `xorm:"final_url"`
	ContentType     string `xorm:"content_type"`
	StatusCode      int    `xorm:"status_code"`
	Content         string
	ContentHash     string    `xorm:"content_hash"`
	ByteCount       int       `xorm:"byte_count"`
	ErrorCode       string    `xorm:"error_code"`
	ErrorStage      string    `xorm:"error_stage"`
	NetworkAccessed bool      `xorm:"network_accessed"`
	CreatedAt       time.Time `xorm:"created_at"`
	DeadlineAt      time.Time `xorm:"deadline_at"`
}

func (V22Capture) TableName() string { return "v22_captures" }
