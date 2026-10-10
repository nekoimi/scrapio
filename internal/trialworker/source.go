package trialworker

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/trial"
)

type source struct {
	browser  *drission_rod.DrissionRod
	cfg      *config.Config
	ownerID  int64
	plan     trial.Plan
	input    trial.Input
	id       string
	state    drission_rod.EditorSessionState
	document trial.FixedDocument
}

func (s *source) Start(ctx context.Context) error {
	if !trial.Allowed(s.plan.URL, s.input.Origins) {
		return errors.New("ORIGIN_OUT_OF_SCOPE")
	}
	if s.plan.EntryType == "json" {
		var cfg *config.HTTPEntryConfig
		allow := false
		if s.cfg != nil {
			cfg = s.cfg.HTTPEntry
			if cfg != nil {
				allow = cfg.AllowPrivateNetwork
			}
		}
		header, value, secret, err := capture.Credential(cfg, s.ownerID, s.plan.URL, s.plan.Request.CredentialRef)
		if err != nil {
			return errors.New("CREDENTIAL_UNAVAILABLE")
		}
		result := capture.Execute(ctx, capture.NewClient(allow), capture.Input{Source: "http", Format: "json", Confirmed: true}, s.plan.Request, s.plan.URL, header, value, secret)
		if result.Status != "succeeded" {
			return errors.New(result.ErrorCode)
		}
		if !trial.Allowed(result.FinalURL, s.input.Origins) {
			return errors.New("ORIGIN_OUT_OF_SCOPE")
		}
		s.document = trial.FixedDocument{Content: result.Content, Hash: result.Hash, URL: result.FinalURL, Format: "json"}
		return nil
	}
	if s.browser == nil {
		return errors.New("BROWSER_UNAVAILABLE")
	}
	state, err := s.browser.CreateEditorSession(ctx, drission_rod.EditorSessionInput{SessionID: s.id, URL: s.plan.URL, TTL: 3 * time.Minute, Width: 1280, Height: 800})
	if err != nil {
		return browserFailure(err)
	}
	s.state = state
	return s.checkState()
}
func (s *source) checkState() error {
	if s.state.Status != "ready" || s.state.PageStateID == "" {
		return errors.New("TRIAL_SESSION_NOT_READY")
	}
	if !trial.Allowed(s.state.CurrentURL, s.input.Origins) {
		return errors.New("ORIGIN_OUT_OF_SCOPE")
	}
	return nil
}
func (s *source) Snapshot(ctx context.Context) (trial.FixedDocument, error) {
	if s.plan.EntryType == "json" {
		return s.document, ctx.Err()
	}
	if err := s.checkState(); err != nil {
		return trial.FixedDocument{}, err
	}
	raw, err := s.browser.InspectEditorPage(ctx, s.id, uuid.NewString(), "snapshot", editor.Inspection{PageStateID: s.state.PageStateID})
	if err != nil {
		return trial.FixedDocument{}, browserFailure(err)
	}
	var snapshot struct {
		HTML    string `json:"html"`
		URL     string `json:"current_url"`
		BaseURL string `json:"base_url"`
	}
	if json.Unmarshal(raw, &snapshot) != nil || snapshot.HTML == "" || len(snapshot.HTML) > capture.MaxBytes {
		return trial.FixedDocument{}, errors.New("INVALID_SNAPSHOT")
	}
	if !trial.Allowed(snapshot.URL, s.input.Origins) {
		return trial.FixedDocument{}, errors.New("ORIGIN_OUT_OF_SCOPE")
	}
	if _, err = capture.ValidateURL(snapshot.BaseURL); err != nil {
		return trial.FixedDocument{}, errors.New("INVALID_BASE_URL")
	}
	content, err := capture.Sanitize(snapshot.HTML, "html", "")
	if err != nil {
		return trial.FixedDocument{}, errors.New("INVALID_SNAPSHOT")
	}
	return trial.FixedDocument{Content: content, Hash: capture.Hash([]byte(content)), URL: snapshot.URL, BaseURL: snapshot.BaseURL, Format: "html"}, nil
}
func (s *source) Action(ctx context.Context, cmd editor.Command) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkState(); err != nil {
		return err
	}
	if cmd.Type == "navigate" && !trial.Allowed(cmd.Value, s.input.Origins) {
		return errors.New("ORIGIN_OUT_OF_SCOPE")
	}
	cmd.PageStateID = s.state.PageStateID
	cmd.ExpectedRevision = s.input.ExpectedRevision
	cmd.Confirmed = true
	cmd.Record = false
	if err := cmd.Validate(); err != nil {
		return err
	}
	result, err := s.browser.ExecuteEditorCommand(ctx, s.id, uuid.NewString(), cmd)
	if err != nil {
		return browserFailure(err)
	}
	if result.Status != "succeeded" {
		code := result.ErrorCode
		if code == "" {
			code = "ACTION_OUTCOME_UNCERTAIN"
		}
		return errors.New(code)
	}
	s.state.PageStateID = result.AfterPageStateID
	s.state.CurrentURL = result.FinalURL
	return s.checkState()
}
func (s *source) Close() {
	if s.plan.EntryType != "web" || s.browser == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.browser.CloseEditorSession(ctx, s.id)
}
func browserFailure(err error) error {
	var e *drission_rod.BrowserError
	if errors.As(err, &e) && e.Code != "" {
		return errors.New(e.Code)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.New("BROWSER_RPC_FAILED")
}
