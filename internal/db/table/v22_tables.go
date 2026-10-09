package table

import "time"

type V22DataTable struct {
	Id             int64 `xorm:"pk autoincr"`
	OwnerId        int64 `xorm:"owner_id"`
	Name           string
	SchemaVersion  int    `xorm:"schema_version"`
	Schema         string `xorm:"jsonb schema"`
	SchemaHash     string `xorm:"schema_hash"`
	IdempotencyKey string `xorm:"idempotency_key"`
	Fingerprint    string
	CreatedAt      time.Time `xorm:"created_at"`
}

func (V22DataTable) TableName() string { return "v22_data_tables" }

type V22OutputCheck struct {
	Id                string `xorm:"varchar(36) pk"`
	OwnerId           int64  `xorm:"owner_id"`
	CollectorId       int64  `xorm:"collector_id"`
	CollectorRevision int    `xorm:"collector_revision"`
	DefinitionHash    string `xorm:"definition_hash"`
	CaptureId         string `xorm:"capture_id"`
	ContentHash       string `xorm:"content_hash"`
	SampleId          string `xorm:"sample_id"`
	SampleRevision    int    `xorm:"sample_revision"`
	TableId           int64  `xorm:"table_id"`
	SchemaVersion     int    `xorm:"schema_version"`
	SchemaHash        string `xorm:"schema_hash"`
	Config            string `xorm:"jsonb config"`
	Result            string `xorm:"jsonb result"`
	Ready             bool
	IdempotencyKey    string `xorm:"idempotency_key"`
	Fingerprint       string
	ExpiresAt         time.Time `xorm:"expires_at"`
	CreatedAt         time.Time `xorm:"created_at"`
}

func (V22OutputCheck) TableName() string { return "v22_output_checks" }

type V22OutputBinding struct {
	CollectorId   int64     `xorm:"collector_id pk"`
	OwnerId       int64     `xorm:"owner_id"`
	TableId       int64     `xorm:"table_id"`
	CheckId       string    `xorm:"check_id"`
	Config        string    `xorm:"jsonb config"`
	BoundRevision int       `xorm:"bound_revision"`
	CreatedAt     time.Time `xorm:"created_at"`
}

func (V22OutputBinding) TableName() string { return "v22_output_bindings" }

// Persistence contracts for B06. B03 previews do not insert any of these rows.
type V22TableRecord struct {
	Id              string `xorm:"varchar(36) pk"`
	TableId         int64  `xorm:"table_id"`
	SchemaVersion   int    `xorm:"schema_version"`
	CanonicalKey    string `xorm:"canonical_key"`
	KeyValues       string `xorm:"jsonb key_values"`
	RecordValues    string `xorm:"jsonb record_values"`
	ValuesHash      string `xorm:"values_hash"`
	Revision        int
	FirstObservedAt time.Time `xorm:"first_observed_at"`
	LastObservedAt  time.Time `xorm:"last_observed_at"`
}

func (V22TableRecord) TableName() string { return "v22_table_records" }

type V22TableObservation struct {
	Id                 string  `xorm:"varchar(36) pk"`
	RecordId           string  `xorm:"record_id"`
	TableId            int64   `xorm:"table_id"`
	SchemaVersion      int     `xorm:"schema_version"`
	CollectorId        *int64  `xorm:"collector_id"`
	RunId              string  `xorm:"run_id"`
	VersionId          string  `xorm:"version_id"`
	DocumentId         string  `xorm:"document_id"`
	CaptureId          *string `xorm:"capture_id"`
	SourceURL          string  `xorm:"source_url"`
	PageRole           string  `xorm:"page_role"`
	ObservedValues     string  `xorm:"jsonb observed_values"`
	ObservedValuesHash string  `xorm:"observed_values_hash"`
	WriteKey           string  `xorm:"write_key"`
	Outcome            string
	ObservedAt         time.Time `xorm:"observed_at"`
}

func (V22TableObservation) TableName() string { return "v22_table_observations" }

type V22TableRecordRevision struct {
	RecordId      string    `xorm:"record_id pk"`
	Revision      int       `xorm:"pk"`
	SchemaVersion int       `xorm:"schema_version"`
	RecordValues  string    `xorm:"jsonb record_values"`
	ValuesHash    string    `xorm:"values_hash"`
	ChangedFields string    `xorm:"jsonb changed_fields"`
	ObservationId string    `xorm:"observation_id"`
	CreatedAt     time.Time `xorm:"created_at"`
}

func (V22TableRecordRevision) TableName() string { return "v22_table_record_revisions" }
