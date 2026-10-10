package table

import "time"

type V22QualityPolicy struct {
	CollectorId int64 `xorm:"collector_id pk"`
	OwnerId     int64 `xorm:"owner_id"`
	Revision    int
	Policy      string    `xorm:"jsonb policy"`
	Baseline    string    `xorm:"jsonb baseline"`
	UpdatedAt   time.Time `xorm:"updated_at"`
}

func (V22QualityPolicy) TableName() string { return "v22_quality_policies" }

type V22QualityEvaluation struct {
	RunId          string    `xorm:"run_id pk"`
	OwnerId        int64     `xorm:"owner_id"`
	CollectorId    int64     `xorm:"collector_id"`
	PolicyRevision int       `xorm:"policy_revision"`
	Facts          string    `xorm:"jsonb facts"`
	Result         string    `xorm:"jsonb result"`
	CreatedAt      time.Time `xorm:"created_at"`
}

func (V22QualityEvaluation) TableName() string { return "v22_quality_evaluations" }

type V22QualityIssue struct {
	Id             string `xorm:"varchar(36) pk"`
	OwnerId        int64  `xorm:"owner_id"`
	CollectorId    int64  `xorm:"collector_id"`
	Code           string
	Category       string
	Message        string
	Status         string
	Revision       int
	Occurrences    int
	RecoveryStreak int        `xorm:"recovery_streak"`
	FirstRunId     string     `xorm:"first_run_id"`
	LastRunId      string     `xorm:"last_run_id"`
	RecoveryRunId  string     `xorm:"recovery_run_id"`
	Evidence       string     `xorm:"jsonb evidence"`
	CreatedAt      time.Time  `xorm:"created_at"`
	UpdatedAt      time.Time  `xorm:"updated_at"`
	ResolvedAt     *time.Time `xorm:"resolved_at"`
}

func (V22QualityIssue) TableName() string { return "v22_quality_issues" }
