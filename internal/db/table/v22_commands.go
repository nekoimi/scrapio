package table

import "time"

type V22EditorCommand struct {
	Id             string `xorm:"varchar(36) pk"`
	OwnerId        int64  `xorm:"owner_id"`
	SessionId      string `xorm:"session_id"`
	CollectorId    int64  `xorm:"collector_id"`
	IdempotencyKey string `xorm:"idempotency_key"`
	Fingerprint    string
	Request        string `xorm:"jsonb request"`
	Status         string
	Result         string `xorm:"jsonb result"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeadlineAt     time.Time
}

func (V22EditorCommand) TableName() string { return "v22_editor_commands" }

type V22EditorCheckpoint struct {
	Id            string    `xorm:"varchar(36) pk" json:"checkpoint_id"`
	OwnerId       int64     `xorm:"owner_id" json:"-"`
	CollectorId   int64     `xorm:"collector_id" json:"-"`
	Name          string    `json:"name"`
	DraftRevision int       `xorm:"draft_revision" json:"draft_revision"`
	ThroughStepId string    `xorm:"through_step_id" json:"through_step_id"`
	Snapshot      string    `xorm:"jsonb snapshot" json:"-"`
	CreatedAt     time.Time `json:"created_at"`
}

func (V22EditorCheckpoint) TableName() string { return "v22_editor_checkpoints" }
