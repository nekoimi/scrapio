package table

import "time"

// Source identifies a stable collection origin. Code is the machine-readable
// identifier and must remain stable when the display name changes.
type Source struct {
	Id        int64     `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	Config    string    `xorm:"jsonb" json:"config"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Source) TableName() string { return "sources" }

type Workflow struct {
	Id                 int64     `json:"id"`
	ProjectId          *int64    `xorm:"project_id" json:"project_id,omitempty"`
	DatasetId          *int64    `xorm:"dataset_id" json:"dataset_id,omitempty"`
	SourceId           int64     `xorm:"source_id" json:"source_id"`
	Code               string    `json:"code"`
	Name               string    `json:"name"`
	OwnerName          string    `xorm:"owner_name" json:"owner_name"`
	ResourceType       string    `xorm:"resource_type" json:"resource_type"`
	Enabled            bool      `json:"enabled"`
	PublishedVersionId *int64    `xorm:"published_version_id" json:"published_version_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (Workflow) TableName() string { return "workflows" }

type WorkflowVersion struct {
	Id            int64      `json:"id"`
	WorkflowId    int64      `xorm:"workflow_id" json:"workflow_id"`
	Version       int        `json:"version"`
	Status        string     `json:"status"`
	Definition    string     `xorm:"jsonb" json:"definition"`
	ChangeSummary string     `xorm:"change_summary" json:"change_summary"`
	CreatedBy     *int64     `xorm:"created_by" json:"created_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	PublishedAt   *time.Time `xorm:"published_at" json:"published_at,omitempty"`
}

func (WorkflowVersion) TableName() string { return "workflow_versions" }

// WorkflowSample is an immutable content snapshot tied to one draft version.
type WorkflowSample struct {
	Id                int64     `json:"id"`
	WorkflowVersionId int64     `xorm:"workflow_version_id" json:"workflow_version_id"`
	Source            string    `json:"source"`
	PageRole          string    `xorm:"page_role" json:"page_role"`
	DocumentId        *int64    `xorm:"document_id" json:"document_id,omitempty"`
	PageURL           string    `xorm:"page_url text" json:"page_url"`
	ContentType       string    `xorm:"content_type" json:"content_type"`
	Content           string    `xorm:"text" json:"content,omitempty"`
	ContentHash       string    `xorm:"content_hash" json:"content_hash"`
	Note              string    `xorm:"text" json:"note"`
	ExpectedOutcome   string    `xorm:"expected_outcome" json:"expected_outcome"`
	ExpectedError     string    `xorm:"text expected_error" json:"expected_error,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

func (WorkflowSample) TableName() string { return "workflow_samples" }

type WorkflowRun struct {
	Id                int64      `json:"id"`
	WorkflowId        int64      `xorm:"workflow_id" json:"workflow_id"`
	WorkflowVersionId int64      `xorm:"workflow_version_id" json:"workflow_version_id"`
	TriggerType       string     `xorm:"trigger_type" json:"trigger_type"`
	Status            string     `json:"status"`
	Input             string     `xorm:"jsonb" json:"input"`
	Summary           string     `xorm:"jsonb" json:"summary"`
	Budget            string     `xorm:"<- jsonb" json:"budget"`
	StartedAt         *time.Time `xorm:"started_at" json:"started_at,omitempty"`
	FinishedAt        *time.Time `xorm:"finished_at" json:"finished_at,omitempty"`
	CreatedBy         *int64     `xorm:"created_by" json:"created_by,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

func (WorkflowRun) TableName() string { return "workflow_runs" }

type CrawlTask struct {
	Id               int64      `json:"id"`
	RunId            int64      `xorm:"run_id" json:"run_id"`
	ParentTaskId     *int64     `xorm:"parent_task_id" json:"parent_task_id,omitempty"`
	StepName         string     `xorm:"step_name" json:"step_name"`
	TaskType         string     `xorm:"task_type" json:"task_type"`
	Input            string     `xorm:"jsonb" json:"input"`
	Depth            int        `json:"depth"`
	PageReserved     bool       `xorm:"page_reserved" json:"page_reserved"`
	PageHost         string     `xorm:"page_host" json:"page_host"`
	Status           string     `json:"status"`
	AttemptCount     int        `xorm:"attempt_count" json:"attempt_count"`
	MaxAttempts      int        `xorm:"max_attempts" json:"max_attempts"`
	NextRetryAt      *time.Time `xorm:"next_retry_at" json:"next_retry_at,omitempty"`
	LeaseOwner       string     `xorm:"lease_owner" json:"lease_owner,omitempty"`
	LeaseUntil       *time.Time `xorm:"lease_until" json:"lease_until,omitempty"`
	OutputDocumentId *int64     `xorm:"output_document_id" json:"output_document_id,omitempty"`
	ErrorMessage     string     `xorm:"text error_message" json:"error_message,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	FinishedAt       *time.Time `xorm:"finished_at" json:"finished_at,omitempty"`
}

func (CrawlTask) TableName() string { return "crawl_tasks" }

type TaskAttempt struct {
	Id               int64      `json:"id"`
	TaskId           int64      `xorm:"task_id" json:"task_id"`
	AttemptNo        int        `xorm:"attempt_no" json:"attempt_no"`
	WorkerId         string     `xorm:"worker_id" json:"worker_id,omitempty"`
	Status           string     `json:"status"`
	StartedAt        *time.Time `xorm:"started_at" json:"started_at,omitempty"`
	FinishedAt       *time.Time `xorm:"finished_at" json:"finished_at,omitempty"`
	DurationMs       int64      `json:"duration_ms"`
	RequestSnapshot  string     `xorm:"jsonb" json:"request_snapshot"`
	ResponseSnapshot string     `xorm:"jsonb" json:"response_snapshot"`
	ErrorMessage     string     `xorm:"text error_message" json:"error_message,omitempty"`
}

func (TaskAttempt) TableName() string { return "task_attempts" }

type Document struct {
	Id           int64     `json:"id"`
	TaskId       *int64    `xorm:"task_id" json:"task_id,omitempty"`
	DocumentType string    `xorm:"document_type" json:"document_type"`
	Content      string    `xorm:"text" json:"content,omitempty"`
	ContentHash  string    `xorm:"content_hash" json:"content_hash,omitempty"`
	ContentSize  int64     `json:"content_size"`
	Metadata     string    `xorm:"jsonb" json:"metadata"`
	CreatedAt    time.Time `json:"created_at"`
}

func (Document) TableName() string { return "documents" }

type DocumentAsset struct {
	Id          int64     `json:"id"`
	DocumentId  int64     `xorm:"document_id" json:"document_id"`
	AssetType   string    `xorm:"asset_type" json:"asset_type"`
	ContentType string    `xorm:"content_type" json:"content_type"`
	Content     []byte    `xorm:"blob" json:"content,omitempty"`
	ContentHash string    `xorm:"content_hash" json:"content_hash"`
	ContentSize int64     `xorm:"content_size" json:"content_size"`
	Metadata    string    `xorm:"jsonb" json:"metadata"`
	CreatedAt   time.Time `json:"created_at"`
}

func (DocumentAsset) TableName() string { return "document_assets" }

type AuditLog struct {
	Id           int64     `json:"id"`
	RequestID    string    `xorm:"request_id" json:"request_id"`
	ActorID      *int64    `xorm:"actor_id" json:"actor_id,omitempty"`
	Action       string    `json:"action"`
	ResourceType string    `xorm:"resource_type" json:"resource_type"`
	ResourceID   *int64    `xorm:"resource_id" json:"resource_id,omitempty"`
	Details      string    `xorm:"jsonb" json:"details"`
	CreatedAt    time.Time `json:"created_at"`
}

func (AuditLog) TableName() string { return "audit_logs" }

type Resource struct {
	Id           int64     `json:"id"`
	ResourceType string    `xorm:"resource_type" json:"resource_type"`
	SourceId     int64     `xorm:"source_id" json:"source_id"`
	SourceURL    string    `xorm:"source_url text" json:"source_url,omitempty"`
	CanonicalKey string    `xorm:"canonical_key" json:"canonical_key"`
	Title        string    `xorm:"text" json:"title,omitempty"`
	Status       string    `json:"status"`
	Attributes   string    `xorm:"jsonb" json:"attributes"`
	FirstSeenAt  time.Time `xorm:"first_seen_at" json:"first_seen_at"`
	LastSeenAt   time.Time `xorm:"last_seen_at" json:"last_seen_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (Resource) TableName() string { return "resources" }

type ResourceLink struct {
	Id         int64     `json:"id"`
	ResourceId int64     `xorm:"resource_id" json:"resource_id"`
	LinkType   string    `xorm:"link_type" json:"link_type"`
	Link       string    `xorm:"text" json:"link"`
	Name       string    `json:"name,omitempty"`
	Priority   int       `json:"priority"`
	IsOptimal  bool      `xorm:"is_optimal" json:"is_optimal"`
	Metadata   string    `xorm:"jsonb" json:"metadata"`
	CreatedAt  time.Time `json:"created_at"`
}

func (ResourceLink) TableName() string { return "resource_links" }

type ResourceEvent struct {
	Id         int64     `json:"id"`
	ResourceId int64     `xorm:"resource_id" json:"resource_id"`
	EventType  string    `xorm:"event_type" json:"event_type"`
	Message    string    `xorm:"text" json:"message,omitempty"`
	Data       string    `xorm:"jsonb" json:"data"`
	CreatedAt  time.Time `json:"created_at"`
}

func (ResourceEvent) TableName() string { return "resource_events" }

type PluginTask struct {
	Id             int64      `json:"id"`
	ResourceId     int64      `xorm:"resource_id" json:"resource_id"`
	RecordId       *int64     `xorm:"record_id" json:"record_id,omitempty"`
	ObservationId  *int64     `xorm:"observation_id" json:"observation_id,omitempty"`
	DatasetId      *int64     `xorm:"dataset_id" json:"dataset_id,omitempty"`
	WorkflowId     *int64     `xorm:"workflow_id" json:"workflow_id,omitempty"`
	SubscriptionId *int64     `xorm:"subscription_id" json:"subscription_id,omitempty"`
	EventType      string     `xorm:"event_type" json:"event_type"`
	PluginCode     string     `xorm:"plugin_code" json:"plugin_code"`
	IdempotencyKey string     `xorm:"idempotency_key" json:"idempotency_key"`
	Status         string     `json:"status"`
	AttemptCount   int        `xorm:"attempt_count" json:"attempt_count"`
	MaxAttempts    int        `xorm:"max_attempts" json:"max_attempts"`
	NextRetryAt    *time.Time `xorm:"next_retry_at" json:"next_retry_at,omitempty"`
	LeaseOwner     string     `xorm:"lease_owner" json:"lease_owner,omitempty"`
	LeaseUntil     *time.Time `xorm:"lease_until" json:"lease_until,omitempty"`
	ExternalID     string     `xorm:"external_id" json:"external_id,omitempty"`
	Input          string     `xorm:"jsonb" json:"input"`
	Output         string     `xorm:"jsonb" json:"output"`
	ErrorMessage   string     `xorm:"text error_message" json:"error_message,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	FinishedAt     *time.Time `xorm:"finished_at" json:"finished_at,omitempty"`
}

func (PluginTask) TableName() string { return "plugin_tasks" }

type PluginSubscription struct {
	Id         int64     `json:"id"`
	DatasetId  int64     `xorm:"dataset_id" json:"dataset_id"`
	WorkflowId *int64    `xorm:"workflow_id" json:"workflow_id,omitempty"`
	PluginCode string    `xorm:"plugin_code" json:"plugin_code"`
	EventType  string    `xorm:"event_type" json:"event_type"`
	URLField   string    `xorm:"url_field" json:"url_field"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (PluginSubscription) TableName() string { return "plugin_subscriptions" }

type AIExtraction struct {
	Id                int64     `json:"id"`
	CacheKey          string    `xorm:"cache_key" json:"cache_key"`
	DocumentId        *int64    `xorm:"document_id" json:"document_id,omitempty"`
	WorkflowVersionId *int64    `xorm:"workflow_version_id" json:"workflow_version_id,omitempty"`
	Model             string    `json:"model"`
	Result            string    `xorm:"jsonb" json:"result"`
	Confidence        float64   `json:"confidence"`
	ReviewStatus      string    `xorm:"review_status" json:"review_status"`
	InputTokens       int       `xorm:"input_tokens" json:"input_tokens"`
	OutputTokens      int       `xorm:"output_tokens" json:"output_tokens"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (AIExtraction) TableName() string { return "ai_extractions" }

// LegacyDownloadState keeps historical delivery fields queryable without
// making them part of the v2 resource lifecycle.
type LegacyDownloadState struct {
	ResourceId          int64      `xorm:"resource_id pk" json:"resource_id"`
	FollowedBy          string     `xorm:"followed_by" json:"followed_by,omitempty"`
	OldStatus           uint8      `xorm:"old_status" json:"old_status"`
	PostProcessDone     bool       `xorm:"post_process_done" json:"post_process_done"`
	PlayFileId          string     `xorm:"play_file_id" json:"play_file_id,omitempty"`
	PlayFilePath        string     `xorm:"play_file_path text" json:"play_file_path,omitempty"`
	PlayFileSize        int64      `json:"play_file_size"`
	StrmPath            string     `xorm:"strm_path text" json:"strm_path,omitempty"`
	DownloadError       string     `xorm:"download_error text" json:"download_error,omitempty"`
	DownloadRetryCount  int        `xorm:"download_retry_count" json:"download_retry_count"`
	LastSubmitAt        *time.Time `xorm:"last_submit_at" json:"last_submit_at,omitempty"`
	DownloadCompletedAt *time.Time `xorm:"download_completed_at" json:"download_completed_at,omitempty"`
	MigratedAt          time.Time  `json:"migrated_at"`
}

func (LegacyDownloadState) TableName() string { return "legacy_download_states" }
