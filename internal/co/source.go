package co

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/collector"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/repo/v22_credential_repo"
	"net/http"
	"time"
)

type BrowserClient interface {
	ProbeEditorIsolation(context.Context) error
	CreateEditorSession(context.Context, drission_rod.EditorSessionInput) (drission_rod.EditorSessionState, error)
	GetEditorSession(context.Context, string) (drission_rod.EditorSessionState, error)
	CloseEditorSession(context.Context, string) error
	InspectEditorPage(context.Context, string, string, string, editor.Inspection) (json.RawMessage, error)
	ExecuteEditorCommand(context.Context, string, string, editor.Command) (drission_rod.EditorCommandResult, error)
	GetEditorCommand(context.Context, string, string) (drission_rod.EditorCommandResult, error)
}
type Source struct {
	Browser    BrowserClient
	HTTPClient *http.Client
	Config     *config.Config
	Owner      int64
	Definition collector.Definition
	ID         string
	State      drission_rod.EditorSessionState
	Mode       string
	Fixed      []collector.Document
	document   collector.Document
	sequence   int
	Journal    func(string) error
	Uncertain  bool
}

func (s *Source) Open(ctx context.Context, r collector.Request) (collector.Document, error) {
	if e := collector.CheckOrigin(r.URL, s.Definition.Fetch.AllowedOrigins); e != nil {
		return collector.Document{}, e
	}
	format := "html"
	if s.Definition.Fetch.Kind == "http_json" {
		format = "json"
	}
	if s.Mode == "offline" {
		for _, d := range s.Fixed {
			if d.URL == r.URL && d.Format == format {
				return d, nil
			}
		}
		return collector.Document{}, collector.Fault("OFFLINE_PAGE_MISSING", "fetch", "样例未覆盖此地址，不能当作已完成获取")
	}
	if s.Definition.Fetch.Kind != "browser" {
		var cfg *config.HTTPEntryConfig
		allow := false
		if s.Config != nil {
			cfg = s.Config.HTTPEntry
		}
		if cfg != nil {
			allow = cfg.AllowPrivateNetwork
		}
		header, value, secret, e := capture.Credential(cfg, s.Owner, r.URL, r.HTTP.CredentialRef)
		if e != nil {
			return collector.Document{}, collector.Fault("CREDENTIAL_UNAVAILABLE", "fetch", "凭据不可用、过期或超出访问范围")
		}
		client := s.HTTPClient
		if client == nil {
			client = capture.NewClient(allow)
		}
		v := capture.Execute(ctx, client, capture.Input{Source: "http", Format: format, Confirmed: true}, r.HTTP, r.URL, header, value, secret)
		if v.Status != "succeeded" {
			return collector.Document{}, collector.Fault(v.ErrorCode, "fetch", "HTTP获取失败，请检查地址、状态码或请求参数")
		}
		if e = collector.CheckOrigin(v.FinalURL, s.Definition.Fetch.AllowedOrigins); e != nil {
			return collector.Document{}, e
		}
		s.document = collector.Document{URL: v.FinalURL, Format: format, Content: v.Content}
		return s.document, nil
	}
	if s.Browser == nil {
		return collector.Document{}, collector.Fault("BROWSER_UNAVAILABLE", "fetch", "请配置scrapio-browser，草稿可以继续保存")
	}
	if s.State.SessionID == "" {
		if e := s.Browser.ProbeEditorIsolation(ctx); e != nil {
			return collector.Document{}, browserError(e, "fetch")
		}
		authorization, _, e := v22_credential_repo.Browser(ctx, s.Owner, r.URL, s.Definition.Fetch.CredentialRef)
		if e != nil {
			return collector.Document{}, collector.Fault("CREDENTIAL_UNAVAILABLE", "fetch", "浏览器凭据不可用")
		}
		if s.ID == "" {
			s.ID = uuid.NewString()
		}
		state, e := s.Browser.CreateEditorSession(ctx, drission_rod.EditorSessionInput{Isolated: true, Authorization: authorization, SessionID: s.ID, URL: r.URL, TTL: 10 * time.Minute, Width: 1280, Height: 800})
		if e != nil {
			return collector.Document{}, browserError(e, "fetch")
		}
		s.State = state
	} else {
		if e := s.Action(ctx, collector.Action{ID: "page_open", Kind: "navigate", TimeoutMS: 10000, ReplayClass: "receipt_required"}, r.URL); e != nil {
			return collector.Document{}, e
		}
	}
	return s.Snapshot(ctx)
}
func browserError(e error, step string) error {
	if _, ok := e.(*collector.Error); ok {
		return e
	}
	if b, ok := e.(*drission_rod.BrowserError); ok {
		return collector.Fault(b.Code, step, b.Message)
	}
	return collector.Fault("BROWSER_UNAVAILABLE", step, "浏览器操作失败，请检查服务连接")
}
func (s *Source) Snapshot(ctx context.Context) (collector.Document, error) {
	if s.Definition.Fetch.Kind != "browser" {
		return s.document, nil
	}
	expected := s.State.CurrentURL
	for attempt := 0; attempt < 3; attempt++ {
		state, e := s.Browser.GetEditorSession(ctx, s.ID)
		if e != nil {
			return collector.Document{}, browserError(e, "snapshot")
		}
		if state.Status != "ready" || state.SessionID != s.ID {
			return collector.Document{}, collector.Fault("SESSION_NOT_READY", "snapshot", "会话尚未就绪")
		}
		if e = collector.CheckOrigin(state.CurrentURL, s.Definition.Fetch.AllowedOrigins); e != nil {
			return collector.Document{}, e
		}
		if state.CurrentURL != expected {
			return collector.Document{}, collector.Fault("PAGE_STATE_CHANGED", "snapshot", "页面地址发生变化，请检查导航路径")
		}
		s.State = state
		raw, e := s.Browser.InspectEditorPage(ctx, s.ID, uuid.NewString(), "snapshot", editor.Inspection{PageStateID: state.PageStateID})
		if e != nil {
			if b, ok := e.(*drission_rod.BrowserError); ok && b.Code == "STALE_PAGE_STATE" {
				continue
			}
			return collector.Document{}, browserError(e, "snapshot")
		}
		var v struct {
			HTML string `json:"html"`
			URL  string `json:"current_url"`
		}
		if json.Unmarshal(raw, &v) != nil || v.URL != state.CurrentURL || v.HTML == "" || len(v.HTML) > capture.MaxBytes {
			return collector.Document{}, collector.Fault("INVALID_SNAPSHOT", "snapshot", "快照无效或与页面状态不匹配")
		}
		content, e := capture.Sanitize(v.HTML, "html", "")
		if e != nil {
			return collector.Document{}, collector.Fault("INVALID_SNAPSHOT", "snapshot", "快照解析失败")
		}
		return collector.Document{URL: v.URL, Format: "html", Content: content, PageStateID: state.PageStateID}, nil
	}
	return collector.Document{}, collector.Fault("PAGE_NOT_STABLE", "snapshot", "页面连续变化，请等待稳定后重新验证")
}
func (s *Source) Action(ctx context.Context, a collector.Action, value string) error {
	if s.Mode == "offline" {
		return collector.Fault("OFFLINE_ACTION_UNCOVERED", a.ID, "离线样例不能执行网页动作")
	}
	state, e := s.Browser.GetEditorSession(ctx, s.ID)
	if e != nil {
		return browserError(e, a.ID)
	}
	if state.SessionID != s.ID || state.Status != "ready" {
		return collector.Fault("SESSION_NOT_READY", a.ID, "会话状态与动作不匹配，请重新打开网页")
	}
	if e = collector.CheckOrigin(state.CurrentURL, s.Definition.Fetch.AllowedOrigins); e != nil {
		return e
	}
	s.State = state
	cmd := editor.Command{PageStateID: state.PageStateID, ExpectedRevision: 1, Type: a.Kind, Value: value, TimeoutMS: a.TimeoutMS, Confirmed: true}
	if a.Locator != nil {
		cmd.Locator = &editor.Locator{Strategy: a.Locator.Strategy, Expression: a.Locator.Expression}
	}
	if e = cmd.Validate(); e != nil {
		return collector.Invalid("actions."+a.ID, e.Error())
	}
	if a.Kind == "navigate" {
		if e = collector.CheckOrigin(value, s.Definition.Fetch.AllowedOrigins); e != nil {
			return e
		}
	}
	s.sequence++
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s:%s:%d", s.ID, a.ID, s.sequence))).String()
	if s.Journal != nil {
		if e = s.Journal(id); e != nil {
			return collector.Fault("COMMAND_JOURNAL_FAILED", a.ID, "无法记录动作身份，尚未执行动作")
		}
	}
	result, e := s.Browser.ExecuteEditorCommand(ctx, s.ID, id, cmd)
	if e != nil {
		if DefiniteRejection(e) {
			return browserError(e, a.ID)
		}
		query, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		result, e = s.Browser.GetEditorCommand(query, s.ID, id)
		if e != nil {
			s.Uncertain = true
			return collector.Fault("OUTCOME_UNCERTAIN", a.ID, "动作结果未知，已查询原回执且未重放；请人工确认，command_id="+id)
		}
	}
	if result.Status != "succeeded" {
		code := result.ErrorCode
		if result.Status == "uncertain" || result.Status == "running" || code == "" {
			code = "OUTCOME_UNCERTAIN"
			s.Uncertain = true
		}
		return collector.Fault(code, a.ID, "网页动作未完成，请检查原回执，command_id="+id)
	}
	s.State.CurrentURL = result.FinalURL
	s.State.PageStateID = result.AfterPageStateID
	return collector.CheckOrigin(result.FinalURL, s.Definition.Fetch.AllowedOrigins)
}

func DefiniteRejection(e error) bool {
	b, ok := e.(*drission_rod.BrowserError)
	if !ok {
		return false
	}
	switch b.Code {
	case "INVALID_ARGUMENT", "INVALID_LOCATOR", "STALE_PAGE_STATE", "STALE_FRAME", "SESSION_GONE", "SESSION_EXPIRED", "SESSION_NOT_FOUND", "COMMAND_CONFLICT":
		return true
	}
	return false
}
func (s *Source) Close() {
	if s.Uncertain {
		return
	} // Preserve original receipt until bounded session TTL expires.
	if s.ID != "" && s.Browser != nil && s.Mode != "offline" && s.Definition.Fetch.Kind == "browser" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.Browser.CloseEditorSession(ctx, s.ID)
	}
}
