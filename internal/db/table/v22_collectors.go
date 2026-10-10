package table

import "time"

// V22Collector is the first isolated persistence model for the new product.
// It intentionally does not reference legacy workflows or datasets.
type V22Collector struct {
	Id                 int64      `json:"id"`
	OwnerId            int64      `xorm:"owner_id" json:"owner_id"`
	Name               string     `json:"name"`
	EntryURL           string     `xorm:"entry_url" json:"entry_url"`
	EntryType          string     `xorm:"entry_type" json:"entry_type"`
	Status             string     `json:"status"`
	Definition         string     `xorm:"jsonb definition" json:"definition"`
	Revision           int        `json:"revision"`
	PublishedVersionId *string    `xorm:"published_version_id" json:"published_version_id,omitempty"`
	ValidatedRevision  int        `xorm:"validated_revision" json:"validated_revision"`
	ValidationSummary  string     `xorm:"jsonb validation_summary" json:"validation_summary"`
	CreatedBy          int64      `xorm:"created_by" json:"created_by"`
	UpdatedBy          int64      `xorm:"updated_by" json:"updated_by"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	ArchivedAt         *time.Time `xorm:"archived_at" json:"archived_at,omitempty"`
}

func (V22Collector) TableName() string { return "v22_collectors" }

type V22IdempotencyKey struct {
	Id         int64     `json:"id"`
	OwnerId    int64     `xorm:"owner_id"`
	Operation  string    `json:"operation"`
	Key        string    `json:"key"`
	ResourceId int64     `xorm:"resource_id"`
	Response   string    `xorm:"jsonb response"`
	CreatedAt  time.Time `json:"created_at"`
}

func (V22IdempotencyKey) TableName() string { return "v22_idempotency_keys" }

type V22BrowserSession struct {
	Id             string     `xorm:"varchar(36) pk" json:"id"`
	OwnerId        int64      `xorm:"owner_id" json:"owner_id"`
	CollectorId    int64      `xorm:"collector_id" json:"collector_id"`
	DraftRevision  int        `xorm:"draft_revision" json:"draft_revision"`
	TargetURL      string     `xorm:"target_url" json:"target_url"`
	Status         string     `json:"status"`
	ExpiresAt      time.Time  `xorm:"expires_at" json:"expires_at"`
	PageStateId    string     `xorm:"page_state_id" json:"page_state_id"`
	CurrentURL     string     `xorm:"current_url" json:"current_url"`
	ViewportWidth  int        `xorm:"viewport_width" json:"viewport_width"`
	ViewportHeight int        `xorm:"viewport_height" json:"viewport_height"`
	CreatedAt      time.Time  `xorm:"created_at" json:"created_at"`
	UpdatedAt      time.Time  `xorm:"updated_at" json:"updated_at"`
	ClosedAt       *time.Time `xorm:"closed_at" json:"closed_at,omitempty"`
}

func (V22BrowserSession) TableName() string { return "v22_browser_sessions" }

type V22BrowserSessionKey struct {
	Id            int64     `json:"id"`
	OwnerId       int64     `xorm:"owner_id" json:"owner_id"`
	Key           string    `json:"key"`
	SessionId     string    `xorm:"session_id" json:"session_id"`
	CollectorId   int64     `xorm:"collector_id" json:"collector_id"`
	DraftRevision int       `xorm:"draft_revision" json:"draft_revision"`
	CreatedAt     time.Time `xorm:"created_at" json:"created_at"`
}

func (V22BrowserSessionKey) TableName() string { return "v22_browser_session_keys" }
