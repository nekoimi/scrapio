package table

import "time"

type V22Trial struct {
	Id                string `xorm:"varchar(36) pk"`
	OwnerId           int64  `xorm:"owner_id"`
	CollectorId       int64  `xorm:"collector_id"`
	CollectorRevision int    `xorm:"collector_revision"`
	Definition        string `xorm:"jsonb definition"`
	DefinitionHash    string `xorm:"definition_hash"`
	EntryType         string `xorm:"entry_type"`
	Input             string `xorm:"jsonb input"`
	FixedInputs       string `xorm:"jsonb fixed_inputs"`
	OutputSchema      string `xorm:"jsonb output_schema"`
	Status            string
	CancelRequested   bool   `xorm:"cancel_requested"`
	SessionId         string `xorm:"session_id"`
	CurrentStep       string `xorm:"current_step"`
	CurrentStage      string `xorm:"current_stage"`
	EventSeq          int64  `xorm:"event_seq"`
	Summary           string `xorm:"jsonb summary"`
	IdempotencyKey    string `xorm:"idempotency_key"`
	Fingerprint       string
	CreatedAt         time.Time  `xorm:"created_at"`
	StartedAt         *time.Time `xorm:"started_at"`
	FinishedAt        *time.Time `xorm:"finished_at"`
}

func (V22Trial) TableName() string { return "v22_trials" }

type V22TrialDocument struct {
	Id       string `xorm:"varchar(36) pk"`
	TrialId  string `xorm:"trial_id"`
	Sequence int64
	Content  string
	Result   string `xorm:"jsonb result"`
}

func (V22TrialDocument) TableName() string { return "v22_trial_documents" }

type V22TrialEvent struct {
	TrialId   string    `xorm:"trial_id pk"`
	Sequence  int64     `xorm:"pk"`
	Payload   string    `xorm:"jsonb payload"`
	CreatedAt time.Time `xorm:"created_at"`
}

func (V22TrialEvent) TableName() string { return "v22_trial_events" }
