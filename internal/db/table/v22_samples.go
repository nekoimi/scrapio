package table

import "time"

type V22Sample struct {
	Id               string `xorm:"varchar(36) pk"`
	OwnerId          int64  `xorm:"owner_id"`
	CollectorId      int64  `xorm:"collector_id"`
	CaptureId        string `xorm:"capture_id"`
	Name             string
	Stage            string
	StepId           string `xorm:"step_id"`
	Kind             string
	Revision         int
	SavedRevision    int    `xorm:"saved_revision"`
	DefinitionHash   string `xorm:"definition_hash"`
	Expected         string `xorm:"jsonb expected"`
	ExpectedHash     string `xorm:"expected_hash"`
	Actions          string `xorm:"jsonb actions"`
	ActionsTruncated bool   `xorm:"actions_truncated"`
	Protected        bool
	Screenshot       []byte `xorm:"bytea screenshot"`
	ScreenshotHash   string `xorm:"screenshot_hash"`
	Masks            string `xorm:"jsonb masks"`
	ScreenshotPolicy string `xorm:"screenshot_policy"`
	IdempotencyKey   string `xorm:"idempotency_key"`
	Fingerprint      string
	CreatedAt        time.Time `xorm:"created_at"`
	UpdatedAt        time.Time `xorm:"updated_at"`
}

func (V22Sample) TableName() string { return "v22_samples" }

type V22SampleCheck struct {
	Id                 string `xorm:"varchar(36) pk"`
	OwnerId            int64  `xorm:"owner_id"`
	SampleId           string `xorm:"sample_id"`
	SampleRevision     int    `xorm:"sample_revision"`
	CollectorRevision  int    `xorm:"collector_revision"`
	DefinitionHash     string `xorm:"definition_hash"`
	Definition         string `xorm:"jsonb definition"`
	ContentHash        string `xorm:"content_hash"`
	ExpectedHash       string `xorm:"expected_hash"`
	Expected           string `xorm:"jsonb expected"`
	InterpreterVersion string `xorm:"interpreter_version"`
	Status             string
	Result             string `xorm:"jsonb result"`
	Comparison         string `xorm:"jsonb comparison"`
	ErrorCode          string `xorm:"error_code"`
	ErrorMessage       string `xorm:"error_message"`
	IdempotencyKey     string `xorm:"idempotency_key"`
	Fingerprint        string
	CreatedAt          time.Time `xorm:"created_at"`
}

func (V22SampleCheck) TableName() string { return "v22_sample_checks" }
