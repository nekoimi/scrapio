package table

import "time"

type V22Schedule struct {
	CollectorId  int64 `xorm:"collector_id pk"`
	OwnerId      int64 `xorm:"owner_id"`
	Revision     int
	Enabled      bool
	Cron         string
	Timezone     string
	Overlap      string
	Input        string     `xorm:"jsonb input"`
	NextAt       *time.Time `xorm:"next_at"`
	LastDecision string     `xorm:"last_decision"`
	LastRunId    string     `xorm:"last_run_id"`
	UpdatedAt    time.Time  `xorm:"updated_at"`
}

func (V22Schedule) TableName() string { return "v22_schedules" }

type V22APIKey struct {
	Id          string `xorm:"pk"`
	OwnerId     int64  `xorm:"owner_id"`
	CollectorId int64  `xorm:"collector_id"`
	Name        string
	TokenHash   string     `xorm:"token_hash"`
	Input       string     `xorm:"jsonb input"`
	CreatedAt   time.Time  `xorm:"created_at"`
	RevokedAt   *time.Time `xorm:"revoked_at"`
}

func (V22APIKey) TableName() string { return "v22_api_keys" }
