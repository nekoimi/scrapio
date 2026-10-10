package table

import "time"

type V22Run struct {
	Id                  string  `xorm:"varchar(36) pk"`
	OwnerId             int64   `xorm:"owner_id"`
	CollectorId         int64   `xorm:"collector_id"`
	VersionId           string  `xorm:"version_id"`
	VersionNumber       int     `xorm:"version_number"`
	CollectorRevision   int     `xorm:"collector_revision"`
	Definition          string  `xorm:"jsonb definition"`
	DefinitionHash      string  `xorm:"definition_hash"`
	PublicationContract string  `xorm:"publication_contract"`
	InterpreterVersion  string  `xorm:"interpreter_version"`
	EntryType           string  `xorm:"entry_type"`
	OutputSchema        string  `xorm:"jsonb output_schema"`
	Input               string  `xorm:"jsonb input"`
	TriggerSource       string  `xorm:"trigger_source"`
	RetryOf             *string `xorm:"retry_of"`
	RetryScope          string  `xorm:"retry_scope"`
	Status              string
	Attempt             int
	LeaseToken          string     `xorm:"lease_token"`
	LeaseUntil          *time.Time `xorm:"lease_until"`
	CancelRequested     bool       `xorm:"cancel_requested"`
	SessionId           string     `xorm:"session_id"`
	CurrentStep         string     `xorm:"current_step"`
	CurrentStage        string     `xorm:"current_stage"`
	EventSeq            int64      `xorm:"event_seq"`
	Summary             string     `xorm:"jsonb summary"`
	IdempotencyKey      string     `xorm:"idempotency_key"`
	Fingerprint         string
	CreatedAt           time.Time  `xorm:"created_at"`
	StartedAt           *time.Time `xorm:"started_at"`
	FinishedAt          *time.Time `xorm:"finished_at"`
}

func (V22Run) TableName() string { return "v22_runs" }

type V22RunDocument struct {
	Id       string `xorm:"varchar(36) pk"`
	RunId    string `xorm:"run_id"`
	Attempt  int
	Sequence int64
	Content  string
	Result   string `xorm:"jsonb result"`
}

func (V22RunDocument) TableName() string { return "v22_run_documents" }

type V22RunEvent struct {
	RunId     string `xorm:"run_id pk"`
	Sequence  int64  `xorm:"pk"`
	Attempt   int
	Payload   string    `xorm:"jsonb payload"`
	CreatedAt time.Time `xorm:"created_at"`
}

func (V22RunEvent) TableName() string { return "v22_run_events" }
