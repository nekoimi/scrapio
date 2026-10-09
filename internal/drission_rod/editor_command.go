package drission_rod

import (
	"context"
	"time"

	pb "github.com/nekoimi/scrapio/internal/drission_rod/grpc"
	"github.com/nekoimi/scrapio/internal/editor"
)

type EditorCommandResult struct {
	Status            string          `json:"status"`
	BeforePageStateID string          `json:"before_page_state_id"`
	AfterPageStateID  string          `json:"after_page_state_id"`
	FinalURL          string          `json:"final_url"`
	DurationMS        int64           `json:"duration_ms"`
	ErrorCode         string          `json:"error_code,omitempty"`
	Error             string          `json:"error,omitempty"`
	Locator           *editor.Locator `json:"locator,omitempty"`
	RecordingStatus   string          `json:"recording_status,omitempty"`
	RecordingError    string          `json:"recording_error,omitempty"`
	RecordedRevision  int             `json:"recorded_revision,omitempty"`
}

func (d *DrissionRod) ExecuteEditorCommand(ctx context.Context, sessionID, commandID string, input editor.Command) (EditorCommandResult, error) {
	r := &pb.EditorCommandRequest{ProtocolVersion: EditorProtocolVersion, SessionId: sessionID, CommandId: commandID, RequestId: commandID, PageStateId: input.PageStateID, Type: input.Type, Value: input.Value, TimeoutMs: int32(input.TimeoutMS), Confirmed: input.Confirmed}
	if input.Locator != nil {
		r.LocatorStrategy = input.Locator.Strategy
		r.LocatorExpression = input.Locator.Expression
	}
	if input.Position != nil {
		r.HasPosition = true
		r.X = input.Position.X
		r.Y = input.Position.Y
	}
	return d.editorCommandCall(ctx, r, false)
}
func (d *DrissionRod) GetEditorCommand(ctx context.Context, sessionID, commandID string) (EditorCommandResult, error) {
	return d.editorCommandCall(ctx, &pb.EditorCommandRequest{ProtocolVersion: EditorProtocolVersion, SessionId: sessionID, CommandId: commandID, RequestId: commandID}, true)
}
func (d *DrissionRod) editorCommandCall(ctx context.Context, request *pb.EditorCommandRequest, query bool) (EditorCommandResult, error) {
	client := d.Client()
	if client == nil {
		return EditorCommandResult{}, &BrowserError{Code: "BROWSER_UNAVAILABLE", Message: "browser service unavailable"}
	}
	deadline := 40 * time.Second
	if query {
		deadline = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	var response *pb.EditorCommandResponse
	var err error
	if query {
		response, err = client.GetEditorCommand(ctx, request)
	} else {
		response, err = client.ExecuteEditorCommand(ctx, request)
	}
	if err != nil {
		return EditorCommandResult{}, classifyRPCError(request.RequestId, err)
	}
	if !response.Success {
		return EditorCommandResult{}, &BrowserError{Code: response.ErrorCode, Message: response.Error}
	}
	result := EditorCommandResult{Status: response.Status, BeforePageStateID: response.BeforePageStateId, AfterPageStateID: response.AfterPageStateId, FinalURL: response.FinalUrl, DurationMS: response.DurationMs, ErrorCode: response.ErrorCode, Error: response.Error}
	if response.LocatorExpression != "" {
		result.Locator = &editor.Locator{Strategy: response.LocatorStrategy, Expression: response.LocatorExpression}
	}
	return result, nil
}

func (d *DrissionRod) ProbeEditorCommands(ctx context.Context) error {
	_, err := d.GetEditorCommand(ctx, "protocol-probe", "protocol-probe")
	if e, ok := err.(*BrowserError); ok && (e.Code == "SESSION_GONE" || e.Code == "COMMAND_NOT_FOUND") {
		return nil
	}
	return err
}
