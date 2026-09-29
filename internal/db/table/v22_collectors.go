package table

import "time"

// V22Collector is the first isolated persistence model for the new product.
// It intentionally does not reference legacy workflows or datasets.
type V22Collector struct {
	Id         int64      `json:"id"`
	OwnerId    int64      `xorm:"owner_id" json:"owner_id"`
	Name       string     `json:"name"`
	EntryURL   string     `xorm:"entry_url" json:"entry_url"`
	EntryType  string     `xorm:"entry_type" json:"entry_type"`
	Status     string     `json:"status"`
	Definition string     `xorm:"jsonb definition" json:"definition"`
	Revision   int        `json:"revision"`
	CreatedBy  int64      `xorm:"created_by" json:"created_by"`
	UpdatedBy  int64      `xorm:"updated_by" json:"updated_by"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ArchivedAt *time.Time `xorm:"archived_at" json:"archived_at,omitempty"`
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
