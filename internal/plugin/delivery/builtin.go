package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/plugin"
	"github.com/nekoimi/scrapio/internal/repo/resource_repo"
)

const (
	CloudCode = "cloud-driver"
	Aria2Code = "aria2"
)

// RegisterBuiltins adds delivery adapters to the generic plugin registry. The
// adapters are inert when their legacy configuration is absent; collection
// remains fully usable without either external service.
func RegisterBuiltins(registry *plugin.Registry, cfg *config.Config) {
	if registry == nil || cfg == nil {
		return
	}
	if cfg.CloudDriver != nil && strings.TrimSpace(cfg.CloudDriver.BaseURL) != "" {
		_ = registry.Register(NewCloudDriver(*cfg.CloudDriver))
	}
	if cfg.Aria2 != nil && strings.TrimSpace(cfg.Aria2.JsonRpc) != "" {
		_ = registry.Register(NewAria2(*cfg.Aria2))
	}
}

type CloudDriver struct {
	cfg    config.CloudDriverConfig
	client *http.Client
}

func NewCloudDriver(cfg config.CloudDriverConfig) *CloudDriver {
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &CloudDriver{cfg: cfg, client: &http.Client{Timeout: timeout}}
}
func (p *CloudDriver) Code() string               { return CloudCode }
func (p *CloudDriver) SupportsRecordEvents() bool { return true }
func (p *CloudDriver) Capabilities() []string     { return []string{"magnet.offline_add"} }
func (p *CloudDriver) Health(ctx context.Context) error {
	var value map[string]any
	return p.do(ctx, http.MethodGet, "/health", nil, &value)
}
func (p *CloudDriver) Handle(ctx context.Context, task plugin.Task) (any, string, error) {
	rawURL := inputString(task.Input, "url", "link", "optimal_link")
	if rawURL == "" {
		return nil, "", &plugin.PermanentError{Err: errors.New("cloud plugin requires a nonempty scalar input.url")}
	}
	if err := validateDownloadURL(rawURL); err != nil {
		return nil, "", &plugin.PermanentError{Err: err}
	}
	category := inputString(task.Input, "category", "origin")
	if category == "" {
		category = "resource"
	}
	savePath := inputString(task.Input, "save_path")
	request := map[string]any{"url": rawURL, "category": category, "client_task_id": fmt.Sprintf("resource-%d-%s", task.ResourceID, task.EventType), "metadata": map[string]string{"event_type": task.EventType}}
	if task.RecordID > 0 {
		request["client_task_id"] = task.IdempotencyKey
	}
	if savePath != "" {
		request["save_path"] = savePath
	}
	var response struct {
		TaskID         string `json:"task_id"`
		ProviderTaskID string `json:"provider_task_id,omitempty"`
		Status         string `json:"status,omitempty"`
	}
	if err := p.do(ctx, http.MethodPost, p.driverPath("/offline/add"), request, &response); err != nil {
		return nil, "", err
	}
	if response.TaskID == "" {
		return nil, "", errors.New("cloud-driver returned empty task_id")
	}
	return map[string]any{"task_id": response.TaskID, "provider_task_id": response.ProviderTaskID, "status": response.Status}, response.TaskID, nil
}

type cloudTask struct {
	TaskID         string      `json:"task_id"`
	ProviderTaskID string      `json:"provider_task_id,omitempty"`
	Status         string      `json:"status"`
	Name           string      `json:"name,omitempty"`
	Progress       float64     `json:"progress,omitempty"`
	SavePath       string      `json:"save_path,omitempty"`
	SaveDir        *cloudFile  `json:"save_dir,omitempty"`
	ErrorCode      string      `json:"error_code,omitempty"`
	ErrorMessage   string      `json:"error_message,omitempty"`
	Files          []cloudFile `json:"files,omitempty"`
	Warnings       []string    `json:"warnings,omitempty"`
}

type cloudFile struct {
	ID           string         `json:"id,omitempty"`
	FileID       string         `json:"file_id,omitempty"`
	ParentID     string         `json:"parent_id,omitempty"`
	Name         string         `json:"name,omitempty"`
	Path         string         `json:"path,omitempty"`
	RelativePath string         `json:"relative_path,omitempty"`
	IsDir        bool           `json:"is_dir,omitempty"`
	Size         int64          `json:"size,omitempty"`
	Extra        map[string]any `json:"extra,omitempty"`
}

// Poll reads the cloud-driver task state. Provider business failures are
// permanent; transport failures remain retryable by the plugin worker.
func (p *CloudDriver) Poll(ctx context.Context, task plugin.Task, externalID string) (any, bool, error) {
	if strings.TrimSpace(externalID) == "" {
		return nil, false, errors.New("cloud-driver task id is required")
	}
	var response cloudTask
	if err := p.do(ctx, http.MethodGet, p.driverPath("/offline/tasks/")+url.PathEscape(externalID), nil, &response); err != nil {
		return nil, false, err
	}
	if response.TaskID == "" {
		response.TaskID = externalID
	}
	output := cloudTaskOutput(response)
	status := strings.ToLower(strings.TrimSpace(response.Status))
	if isCloudFailureStatus(status) {
		message := response.ErrorMessage
		if message == "" {
			message = "cloud-driver task failed"
		}
		if response.ErrorCode != "" {
			message = response.ErrorCode + ": " + message
		}
		return output, true, &plugin.PermanentError{Err: errors.New(message)}
	}
	return output, isCloudCompleteStatus(status), nil
}

// OnComplete writes the final cloud artifact metadata to the generic resource
// attributes and event stream. It deliberately does not change resource.status.
func (p *CloudDriver) OnComplete(_ context.Context, task plugin.Task, output any) error {
	// Record deliveries keep artifacts on the plugin task. Never mutate the
	// collected record or recursively dispatch record.updated.
	if task.RecordID > 0 {
		return nil
	}
	data, ok := output.(map[string]any)
	if !ok {
		encoded, err := json.Marshal(output)
		if err != nil {
			return err
		}
		data = map[string]any{}
		if err := json.Unmarshal(encoded, &data); err != nil {
			return err
		}
	}
	delivery := map[string]any{"provider": CloudCode, "status": "completed"}
	for _, key := range []string{"task_id", "provider_task_id", "progress", "save_path", "save_dir", "files", "warnings"} {
		if value, exists := data[key]; exists && value != nil {
			delivery[key] = value
		}
	}
	return resource_repo.MergeAttributes(task.ResourceID, map[string]any{"delivery": delivery}, "delivery.completed", "云盘离线任务已完成")
}

func cloudTaskOutput(task cloudTask) map[string]any {
	encoded, _ := json.Marshal(task)
	output := map[string]any{}
	_ = json.Unmarshal(encoded, &output)
	return output
}

func isCloudCompleteStatus(status string) bool {
	switch status {
	case "completed", "complete", "succeeded", "success", "done":
		return true
	default:
		return false
	}
}

func isCloudFailureStatus(status string) bool {
	switch status {
	case "failed", "error", "cancelled", "canceled":
		return true
	default:
		return false
	}
}

type Aria2 struct {
	cfg    config.Aria2Config
	client *http.Client
}

func NewAria2(cfg config.Aria2Config) *Aria2 {
	return &Aria2{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second}}
}
func (p *Aria2) Code() string               { return Aria2Code }
func (p *Aria2) SupportsRecordEvents() bool { return true }
func (p *Aria2) Capabilities() []string     { return []string{"magnet.add_uri"} }
func (p *Aria2) Health(ctx context.Context) error {
	_, err := p.call(ctx, "aria2.getVersion", nil)
	return err
}
func (p *Aria2) Handle(ctx context.Context, task plugin.Task) (any, string, error) {
	rawURL := inputString(task.Input, "url", "link", "optimal_link")
	if rawURL == "" {
		return nil, "", &plugin.PermanentError{Err: errors.New("aria2 plugin requires a nonempty scalar input.url")}
	}
	if err := validateDownloadURL(rawURL); err != nil {
		return nil, "", &plugin.PermanentError{Err: err}
	}
	params := []any{}
	if p.cfg.Secret != "" {
		params = append(params, "token:"+p.cfg.Secret)
	}
	params = append(params, []string{rawURL})
	if options, ok := task.Input["options"].(map[string]any); ok {
		params = append(params, options)
	}
	result, err := p.call(ctx, "aria2.addUri", params)
	if err != nil {
		return nil, "", err
	}
	gid := fmt.Sprint(result)
	if gid == "" {
		return nil, "", errors.New("aria2 returned empty gid")
	}
	return map[string]any{"gid": gid, "url": rawURL}, gid, nil
}

func (p *Aria2) call(ctx context.Context, method string, params []any) (any, error) {
	request := map[string]any{"jsonrpc": "2.0", "id": fmt.Sprintf("plugin-%d", time.Now().UnixNano()), "method": method, "params": params}
	encoded, _ := json.Marshal(request)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.JsonRpc, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aria2 request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("aria2 returned status %d", resp.StatusCode)
	}
	var value struct {
		Result any `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
		return nil, err
	}
	if value.Error != nil {
		return nil, fmt.Errorf("aria2 error %d: %s", value.Error.Code, value.Error.Message)
	}
	return value.Result, nil
}

func (p *CloudDriver) do(ctx context.Context, method, path string, body any, output any) error {
	if strings.TrimSpace(p.cfg.BaseURL) == "" {
		return errors.New("cloud_driver.base_url is not configured")
	}
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(p.cfg.BaseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.cfg.ProfileID != "" {
		req.Header.Set("X-Profile-ID", p.cfg.ProfileID)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("cloud-driver request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cloud-driver returned status %d", resp.StatusCode)
	}
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Msg     string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if envelope.Code != 0 {
		message := envelope.Message
		if message == "" {
			message = envelope.Msg
		}
		return fmt.Errorf("cloud-driver error %d: %s", envelope.Code, message)
	}
	if output != nil && len(envelope.Data) > 0 {
		return json.Unmarshal(envelope.Data, output)
	}
	return nil
}

func (p *CloudDriver) driverPath(path string) string {
	platform := p.cfg.Platform
	if platform == "" {
		platform = "115"
	}
	return "/drivers/" + url.PathEscape(platform) + path
}
func inputString(input map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := input[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func validateDownloadURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" {
		return errors.New("download URL is invalid")
	}
	if !strings.EqualFold(parsed.Scheme, "magnet") && !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("unsupported download URL scheme: %s", parsed.Scheme)
	}
	return nil
}
