package table

import "time"

type V22PublishCheck struct {
	Id                string `xorm:"varchar(36) pk"`
	OwnerId           int64  `xorm:"owner_id"`
	CollectorId       int64  `xorm:"collector_id"`
	CollectorRevision int    `xorm:"collector_revision"`
	TrialId           string `xorm:"trial_id"`
	DefinitionHash    string `xorm:"definition_hash"`
	ManifestHash      string `xorm:"manifest_hash"`
	CapabilityHash    string `xorm:"capability_hash"`
	AcceptLimited     bool   `xorm:"accept_limited"`
	Ready             bool
	Result            string `xorm:"jsonb result"`
	Manifest          string `xorm:"jsonb manifest"`
	Capabilities      string `xorm:"jsonb capabilities"`
	Evidence          string `xorm:"jsonb evidence"`
	IdempotencyKey    string `xorm:"idempotency_key"`
	Fingerprint       string
	ExpiresAt         time.Time `xorm:"expires_at"`
	CreatedAt         time.Time `xorm:"created_at"`
}

func (V22PublishCheck) TableName() string { return "v22_publish_checks" }

type V22Version struct {
	Id                 string `xorm:"varchar(36) pk"`
	OwnerId            int64  `xorm:"owner_id"`
	CollectorId        int64  `xorm:"collector_id"`
	Number             int
	CollectorRevision  int `xorm:"collector_revision"`
	Name               string
	EntryType          string `xorm:"entry_type"`
	Definition         string `xorm:"jsonb definition"`
	DefinitionHash     string `xorm:"definition_hash"`
	OutputSchema       string `xorm:"jsonb output_schema"`
	RuntimeConfig      string `xorm:"jsonb runtime_config"`
	DifferenceReview   string `xorm:"jsonb difference_review"`
	CheckId            string `xorm:"check_id"`
	TrialId            string `xorm:"trial_id"`
	OutputCheckId      string `xorm:"output_check_id"`
	ContractVersion    string `xorm:"contract_version"`
	InterpreterVersion string `xorm:"interpreter_version"`
	CapabilityHash     string `xorm:"capability_hash"`
	ManifestHash       string `xorm:"manifest_hash"`
	Note               string
	IdempotencyKey     string `xorm:"idempotency_key"`
	Fingerprint        string
	PublishedBy        int64     `xorm:"published_by"`
	CreatedAt          time.Time `xorm:"created_at"`
}

func (V22Version) TableName() string { return "v22_versions" }
