package drission_rod

import (
	"context"
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/credential"
	"time"

	"github.com/google/uuid"
	pb "github.com/nekoimi/scrapio/internal/drission_rod/grpc"
)

const EditorProtocolVersion = "editor.v1"

type EditorSessionInput struct {
	Authorization credential.Authorization
	SessionID     string
	URL           string
	TTL           time.Duration
	Width         int32
	Height        int32
}

type EditorSessionState struct {
	SessionID   string
	Status      string
	ExpiresAt   time.Time
	PageStateID string
	CurrentURL  string
	Width       int32
	Height      int32
	Screenshot  []byte
}

func (d *DrissionRod) CreateEditorSession(ctx context.Context, input EditorSessionInput) (EditorSessionState, error) {
	if input.SessionID == "" {
		input.SessionID = uuid.NewString()
	}
	if input.TTL <= 0 {
		input.TTL = 2 * time.Minute
	}
	request := &pb.EditorSessionCreateRequest{
		ProtocolVersion: EditorProtocolVersion,
		RequestId:       uuid.NewString(),
		SessionId:       input.SessionID,
		Url:             input.URL,
		TtlSeconds:      int32(input.TTL.Seconds()),
		ViewportWidth:   input.Width,
		ViewportHeight:  input.Height,
	}
	if input.Authorization.Origin != "" {
		request.ProtocolVersion = "editor.auth.v1"
		raw, e := json.Marshal(input.Authorization)
		if e != nil {
			return EditorSessionState{}, e
		}
		request.AuthorizationJson = string(raw)
	}
	client := d.Client()
	if client == nil {
		return EditorSessionState{}, &BrowserError{Code: "BROWSER_UNAVAILABLE", Retryable: true, Message: "browser client is not connected"}
	}
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	response, err := client.CreateEditorSession(callCtx, request)
	if err != nil {
		return EditorSessionState{}, classifyRPCError(request.RequestId, err)
	}
	return editorSessionResponse(response)
}

func (d *DrissionRod) GetEditorSession(ctx context.Context, sessionID string) (EditorSessionState, error) {
	return d.editorSessionCall(ctx, sessionID, "get", 0, "")
}

func (d *DrissionRod) HeartbeatEditorSession(ctx context.Context, sessionID string, ttl time.Duration) (EditorSessionState, error) {
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	return d.editorSessionCall(ctx, sessionID, "heartbeat", int32(ttl.Seconds()), "")
}

func (d *DrissionRod) CloseEditorSession(ctx context.Context, sessionID string) error {
	_, err := d.editorSessionCall(ctx, sessionID, "close", 0, "")
	return err
}

func (d *DrissionRod) FrameEditorSession(ctx context.Context, sessionID, pageStateID string) (EditorSessionState, error) {
	return d.editorSessionCall(ctx, sessionID, "frame", 0, pageStateID)
}

func (d *DrissionRod) editorSessionCall(ctx context.Context, sessionID, action string, ttlSeconds int32, pageStateID string) (EditorSessionState, error) {
	request := &pb.EditorSessionRequest{ProtocolVersion: EditorProtocolVersion, RequestId: uuid.NewString(), SessionId: sessionID, TtlSeconds: ttlSeconds, PageStateId: pageStateID}
	client := d.Client()
	if client == nil {
		return EditorSessionState{}, &BrowserError{Code: "BROWSER_UNAVAILABLE", Retryable: true, Message: "browser client is not connected"}
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var response *pb.EditorSessionResponse
	var err error
	switch action {
	case "get":
		response, err = client.GetEditorSession(callCtx, request)
	case "heartbeat":
		response, err = client.HeartbeatEditorSession(callCtx, request)
	case "close":
		response, err = client.CloseEditorSession(callCtx, request)
	case "frame":
		response, err = client.FrameEditorSession(callCtx, request)
	}
	if err != nil {
		return EditorSessionState{}, classifyRPCError(request.RequestId, err)
	}
	return editorSessionResponse(response)
}

func editorSessionResponse(response *pb.EditorSessionResponse) (EditorSessionState, error) {
	if !response.Success {
		code := response.ErrorCode
		if code == "" {
			code = "EDITOR_SESSION_FAILED"
		}
		return EditorSessionState{}, &BrowserError{Code: code, RequestID: response.RequestId, Message: response.Error, Retryable: code == "BROWSER_UNAVAILABLE" || code == "CAPACITY_REACHED"}
	}
	return EditorSessionState{
		SessionID: response.SessionId, Status: response.Status,
		ExpiresAt: time.UnixMilli(response.ExpiresAtUnixMs), PageStateID: response.PageStateId,
		CurrentURL: response.CurrentUrl, Width: response.ViewportWidth, Height: response.ViewportHeight,
		Screenshot: response.Screenshot,
	}, nil
}

// ProbeEditor checks editor.v1 without allocating a tab. Legacy Health alone
// does not establish support for the editor RPCs or browser startup readiness.
func (d *DrissionRod) ProbeEditor(ctx context.Context) error {
	_, err := d.GetEditorSession(ctx, "protocol-probe")
	if browserErr, ok := err.(*BrowserError); ok && (browserErr.Code == "SESSION_NOT_FOUND" || browserErr.Code == "SESSION_EXPIRED") {
		return nil
	}
	if err == nil {
		return &BrowserError{Code: "EDITOR_PROTOCOL_INVALID", Message: "unexpected editor probe response"}
	}
	return err
}

// An older browser rejects editor.auth.v1 instead of ignoring authorization.
func (d *DrissionRod) ProbeEditorAuthorization(ctx context.Context) error {
	client := d.Client()
	if client == nil {
		return &BrowserError{Code: "BROWSER_UNAVAILABLE"}
	}
	call, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	response, e := client.GetEditorSession(call, &pb.EditorSessionRequest{ProtocolVersion: "editor.auth.v1", SessionId: "protocol-probe"})
	if e != nil {
		return e
	}
	if response.ErrorCode != "AUTHORIZATION_SUPPORTED" {
		return &BrowserError{Code: "AUTHORIZATION_UNAVAILABLE"}
	}
	return nil
}
