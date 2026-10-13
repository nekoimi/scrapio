package co

import (
	"context"
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/collector"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/repo/v22_credential_repo"
	log "github.com/sirupsen/logrus"
	"time"
)

type Worker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func NewWorker() *Worker     { return &Worker{} }
func (*Worker) Name() string { return "coCollectorWorker" }
func (w *Worker) Start(ctx context.Context) error {
	if e := Recover(); e != nil {
		return e
	}
	run, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.done = make(chan struct{})
	browser := bean.PtrFromContext[drission_rod.DrissionRod](ctx)
	cfg := bean.PtrFromContext[config.Config](ctx)
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-run.Done():
				return
			case <-ticker.C:
				o, e := Claim(run)
				if e != nil {
					log.WithError(e).Warn("claim independent collector operation")
					continue
				}
				if o != nil {
					w.execute(run, o, browser, cfg)
				}
			}
		}
	}()
	return nil
}
func (w *Worker) Stop(ctx context.Context) error {
	if w.cancel == nil {
		return nil
	}
	w.cancel()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (w *Worker) execute(parent context.Context, o *Operation, browser *drission_rod.DrissionRod, cfg *config.Config) {
	defer func() {
		if recover() != nil {
			w.finish(o.ID, "failed", map[string]any{}, collector.Fault("WORKER_PANIC", "operation", "执行异常，未重放原操作"))
		}
	}()
	ctx, cancel := context.WithTimeout(parent, 65*time.Second)
	defer cancel()
	if time.Since(o.CreatedAt) > 10*time.Minute {
		w.finish(o.ID, "failed", map[string]any{}, collector.Fault("QUEUE_EXPIRED", "operation", "排队超过10分钟，请核对后创建新测试"))
		return
	}
	if o.Operation == "editorCommand" {
		var p CommandPayload
		if json.Unmarshal([]byte(o.Payload), &p) != nil {
			w.finish(o.ID, "failed", nil, collector.Invalid("command", "动作记录无效"))
			return
		}
		row, e := GetSession(ctx, o.OwnerID, o.ResourceID)
		if e != nil {
			w.finish(o.ID, "failed", nil, e)
			return
		}
		d, e := collector.Decode([]byte(row.Definition))
		if e != nil {
			w.finish(o.ID, "failed", nil, e)
			return
		}
		if browser == nil {
			w.finish(o.ID, "failed", nil, collector.Fault("BROWSER_UNAVAILABLE", "command", "浏览器服务未配置"))
			return
		}
		state, e := browser.GetEditorSession(ctx, row.ID)
		if e == nil && state.PageStateID != p.Command.PageStateID {
			e = collector.Fault("STALE_PAGE_STATE", "command", "页面已变化，请重新点选或验证")
		}
		if e == nil {
			e = collector.CheckOrigin(state.CurrentURL, d.Fetch.AllowedOrigins)
		}
		if e != nil {
			w.finish(o.ID, "failed", nil, e)
			return
		}
		receipt, e := browser.ExecuteEditorCommand(ctx, row.ID, p.CommandID, p.Command)
		if e != nil && !DefiniteRejection(e) {
			query, c := context.WithTimeout(context.Background(), 3*time.Second)
			receipt, e = browser.GetEditorCommand(query, row.ID, p.CommandID)
			c()
			if e != nil {
				e = collector.Fault("OUTCOME_UNCERTAIN", "command", "动作结果未知，查询原回执未取得结果，未重放")
			}
		}
		if e == nil && receipt.Status == "succeeded" {
			e = collector.CheckOrigin(receipt.FinalURL, d.Fetch.AllowedOrigins)
		}
		receiptView := map[string]any{}
		_ = json.Unmarshal([]byte(collector.JSON(receipt)), &receiptView)
		receiptView["command_id"] = p.CommandID
		if e != nil || receipt.Status != "succeeded" {
			if e != nil {
				e = browserError(e, "command")
			}
			if e == nil {
				e = collector.Fault(receipt.ErrorCode, "command", receipt.Error)
				if receipt.Status == "uncertain" || receipt.Status == "running" || receipt.ErrorCode == "" {
					e = collector.Fault("OUTCOME_UNCERTAIN", "command", "动作结果未确认")
				}
			}
			w.finish(o.ID, "failed", receiptView, e)
		} else {
			w.finish(o.ID, "succeeded", receiptView, nil)
		}
		return
	}
	var p TestPayload
	if json.Unmarshal([]byte(o.Payload), &p) != nil {
		w.finish(o.ID, "failed", nil, collector.Invalid("payload", "冻结配置无效"))
		return
	}
	switch o.Operation {
	case "testCollector":
		// Tests return at most 3 pages/100 candidates/60s, regardless of formal budgets.
		d := p.Definition
		originalHash := collector.Hash(d)
		if d.Pagination.MaxPages > 3 {
			d.Pagination.MaxPages = 3
		}
		if d.Pagination.MaxRecords > 100 {
			d.Pagination.MaxRecords = 100
		}
		if d.Pagination.MaxDurationMS > 60000 {
			d.Pagination.MaxDurationMS = 60000
		}
		var browserClient BrowserClient
		if browser != nil {
			browserClient = browser
		}
		src := &Source{Browser: browserClient, Config: cfg, Owner: o.OwnerID, Definition: d, ID: o.ID, Mode: p.Mode, Fixed: p.Snapshots, Journal: func(id string) error {
			s := session(context.Background())
			defer s.Close()
			_, e := s.Exec("UPDATE co_operations SET payload=jsonb_set(payload,'{command_id}',?::jsonb) WHERE id=? AND status='running'", collector.JSON(id), o.ID)
			return e
		}}
		// The interpreter keeps original hashes and budgets, with a test envelope.
		result := collector.Execute(ctx, d, p.Input, p.Mode, src)
		result.DefinitionHash = originalHash
		result.Checkpoint.DefinitionHash = originalHash
		if p.Mode == "offline" {
			result.Comparison = Compare(result.Candidates, p.Expected)
			if result.Comparison["status"] == "different" {
				result.Status = "partial"
				result.Reason = "EXPECTED_OUTPUT_DIFFERENCE"
			}
		}
		projection := map[string]any{}
		_ = json.Unmarshal([]byte(collector.JSON(result)), &projection)
		projection["snapshots"] = result.Snapshots
		projection["test_limits"] = map[string]int{"max_pages": 3, "max_records": 100, "max_duration_ms": 60000}
		status := "succeeded"
		if result.Status == "failed" || result.Status == "awaiting_confirmation" {
			status = "failed"
		}
		var failure error
		if status == "failed" && result.Error != nil {
			failure = result.Error
		}
		w.finish(o.ID, status, projection, failure)
	case "checkPublication":
		evidence, e := GetOperation(ctx, o.OwnerID, p.EvidenceID)
		if e == nil && evidence.ResourceID != o.ResourceID {
			e = ErrNotFound
		}
		var result map[string]any
		if e == nil {
			result, e = CheckEvidence(evidence, p)
		}
		if e == nil {
			var tested TestPayload
			if json.Unmarshal([]byte(evidence.Payload), &tested) != nil {
				e = collector.Invalid("evidence_id", "测试输入证据损坏")
			} else {
				e = CheckAvailability(ctx, o.OwnerID, p.Definition, tested.Input, browser, cfg)
			}
		}
		if e != nil {
			w.finish(o.ID, "failed", map[string]any{"allowed": false}, e)
		} else {
			w.finish(o.ID, "succeeded", result, nil)
		}
	case "createEditorSession":
		if p.Definition.Fetch.Kind != "browser" {
			w.finish(o.ID, "failed", nil, collector.Invalid("fetch.kind", "只有浏览器采集器需要编辑会话"))
			return
		}
		if browser == nil {
			w.finish(o.ID, "failed", nil, collector.Fault("BROWSER_UNAVAILABLE", "session", "浏览器服务未配置，草稿可继续保存"))
			return
		}
		if e := browser.ProbeEditorIsolation(ctx); e != nil {
			w.finish(o.ID, "failed", nil, browserError(e, "session"))
			return
		}
		r, e := collector.Prepare(p.Definition, p.Input)
		if e != nil {
			w.finish(o.ID, "failed", nil, e)
			return
		}
		auth, _, e := v22_credential_repo.Browser(ctx, o.OwnerID, r.URL, p.Definition.Fetch.CredentialRef)
		if e != nil {
			w.finish(o.ID, "failed", nil, collector.Fault("CREDENTIAL_UNAVAILABLE", "session", "凭据不可用"))
			return
		}
		state, e := browser.CreateEditorSession(ctx, drission_rod.EditorSessionInput{Isolated: true, Authorization: auth, SessionID: o.ID, URL: r.URL, TTL: time.Duration(p.TTL) * time.Second, Width: int32(p.Width), Height: int32(p.Height)})
		if e == nil {
			e = collector.CheckOrigin(state.CurrentURL, p.Definition.Fetch.AllowedOrigins)
		}
		if e != nil {
			cleanup, c := context.WithTimeout(context.Background(), 2*time.Second)
			_ = browser.CloseEditorSession(cleanup, o.ID)
			c()
			w.finish(o.ID, "failed", nil, browserError(e, "session"))
			return
		}
		row := &Session{ID: o.ID, OwnerID: o.OwnerID, CollectorID: o.ResourceID, Revision: p.Revision, Definition: collector.JSON(p.Definition), Input: collector.JSON(p.Input), ExpiresAt: state.ExpiresAt, CreatedAt: time.Now()}
		if e = PutSession(ctx, row); e != nil {
			cleanup, c := context.WithTimeout(context.Background(), 2*time.Second)
			_ = browser.CloseEditorSession(cleanup, o.ID)
			c()
			w.finish(o.ID, "failed", nil, e)
			return
		}
		w.finish(o.ID, "succeeded", SessionView(row, state), nil)
	default:
		w.finish(o.ID, "failed", nil, collector.Fault("UNKNOWN_OPERATION", "operation", "未知操作未执行"))
	}
}
func (*Worker) finish(id, status string, value any, e error) {
	if value == nil {
		value = map[string]any{}
	}
	if err := Finish(id, status, value, e); err != nil {
		log.WithError(err).WithField("operation_id", id).Error("persist independent collector operation")
	}
}

type CommandPayload struct {
	CommandID string         `json:"command_id"`
	Command   editor.Command `json:"command"`
}

func SessionView(row *Session, state drission_rod.EditorSessionState) any {
	return map[string]any{"session_id": row.ID, "collector_id": row.CollectorID, "draft_revision": row.Revision, "status": state.Status, "expires_at": row.ExpiresAt, "page_state_id": state.PageStateID, "current_url": state.CurrentURL, "viewport": map[string]int32{"width": state.Width, "height": state.Height}, "frame_url": "/api/v4/sessions/" + row.ID + "/frame", "available": state.Status == "ready", "needs_reopen": state.Status != "ready", "state": state.State}
}
