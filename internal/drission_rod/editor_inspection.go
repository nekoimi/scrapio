package drission_rod

import (
	"context"
	"encoding/json"
	"time"

	pb "github.com/nekoimi/scrapio/internal/drission_rod/grpc"
	"github.com/nekoimi/scrapio/internal/editor"
)

func (d *DrissionRod) InspectEditorPage(ctx context.Context, sessionID, requestID, operation string, input editor.Inspection) (json.RawMessage, error) {
	client := d.Client()
	if client == nil {
		return nil, &BrowserError{Code: "BROWSER_UNAVAILABLE", Message: "browser unavailable"}
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	response, err := client.InspectEditorPage(ctx, &pb.EditorInspectionRequest{ProtocolVersion: EditorProtocolVersion, RequestId: requestID, SessionId: sessionID, PageStateId: input.PageStateID, Operation: operation, InputJson: string(data)})
	if err != nil {
		return nil, classifyRPCError(requestID, err)
	}
	if !response.Success {
		return nil, &BrowserError{Code: response.ErrorCode, Message: response.Error}
	}
	var result struct {
		PageStateID string `json:"page_state_id"`
	}
	limit := 1024 * 1024
	if operation == "snapshot" {
		limit = 2 * 1024 * 1024
	}
	if len(response.ResultJson) > limit || json.Unmarshal([]byte(response.ResultJson), &result) != nil || result.PageStateID != input.PageStateID {
		return nil, &BrowserError{Code: "INVALID_RESPONSE", Message: "invalid or stale inspection response"}
	}
	return json.RawMessage(response.ResultJson), nil
}

func (d *DrissionRod) ProbeEditorInspection(ctx context.Context) error {
	_, err := d.InspectEditorPage(ctx, "protocol-probe", "protocol-probe", "dom", editor.Inspection{PageStateID: "protocol-probe", Limit: 1})
	if e, ok := err.(*BrowserError); ok && e.Code == "SESSION_GONE" {
		return nil
	}
	return err
}

func (d *DrissionRod) ProbeEditorRecords(ctx context.Context) error {
	_, err := d.InspectEditorPage(ctx, "protocol-probe", "protocol-probe", "record-preview", editor.Inspection{PageStateID: "protocol-probe"})
	if e, ok := err.(*BrowserError); ok && e.Code == "RECORD_PREVIEW_SUPPORTED" {
		return nil
	}
	return err
}

func (d *DrissionRod) ProbeEditorSnapshot(ctx context.Context) error {
	_, err := d.InspectEditorPage(ctx, "protocol-probe", "protocol-probe", "snapshot", editor.Inspection{PageStateID: "protocol-probe"})
	if e, ok := err.(*BrowserError); ok && e.Code == "SNAPSHOT_SUPPORTED" {
		return nil
	}
	return err
}
