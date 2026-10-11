package drission_rod

import (
	"bytes"
	"context"
	"encoding/json"
	"image/jpeg"

	"google.golang.org/grpc"

	pb "github.com/nekoimi/scrapio/internal/drission_rod/grpc"
)

const EditorPreviewProtocol = "editor.preview.v1"
const MaxPreviewFrameBytes = 4 * 1024 * 1024

type PreviewMetadata struct {
	Heartbeat   bool   `json:"heartbeat,omitempty"`
	Protocol    string `json:"protocol"`
	Sequence    int64  `json:"sequence"`
	Mime        string `json:"mime"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	CapturedAt  int64  `json:"captured_at_unix_ms"`
	PreviewOnly bool   `json:"preview_only"`
}

func ValidatePreviewFrame(response *pb.EditorPreviewFrame) (PreviewMetadata, error) {
	var metadata PreviewMetadata
	invalid := &BrowserError{Code: "INVALID_RESPONSE", Message: "invalid preview frame"}
	if response == nil || !response.Success || response.ProtocolVersion != EditorPreviewProtocol || len(response.Image) > MaxPreviewFrameBytes || len(response.MetadataJson) > 4096 {
		return metadata, invalid
	}
	if json.Unmarshal([]byte(response.MetadataJson), &metadata) != nil {
		return metadata, invalid
	}
	if metadata.Heartbeat && metadata.Protocol == EditorPreviewProtocol && len(response.Image) == 0 {
		return metadata, nil
	}
	if len(response.Image) == 0 || metadata.Protocol != EditorPreviewProtocol || !metadata.PreviewOnly || metadata.Mime != "image/jpeg" || metadata.Sequence < 1 || metadata.Sequence > 9007199254740991 || metadata.CapturedAt < 1 {
		return metadata, invalid
	}
	size, err := jpeg.DecodeConfig(bytes.NewReader(response.Image))
	if err != nil || size.Width < 1 || size.Height < 1 || size.Width > 1920 || size.Height > 1080 {
		return metadata, invalid
	}
	metadata.Width, metadata.Height = size.Width, size.Height
	return metadata, nil
}

func (d *DrissionRod) ProbeEditorPreview(ctx context.Context) bool {
	client := d.Client()
	if client == nil {
		return false
	}
	result, err := client.GetEditorSession(ctx, &pb.EditorSessionRequest{ProtocolVersion: EditorPreviewProtocol, SessionId: "protocol-probe"})
	return err == nil && result != nil && result.ErrorCode == "STREAM_SUPPORTED"
}

func (d *DrissionRod) ProbeEditorRecheck(ctx context.Context) bool {
	client := d.Client()
	if client == nil {
		return false
	}
	result, err := client.GetEditorSession(ctx, &pb.EditorSessionRequest{ProtocolVersion: "editor.recheck.v1", SessionId: "protocol-probe"})
	return err == nil && result != nil && result.ErrorCode == "RECHECK_SUPPORTED"
}

func (d *DrissionRod) StreamEditorPreview(ctx context.Context, id string, emit func(PreviewMetadata, []byte) error) error {
	client := d.Client()
	if client == nil {
		return &BrowserError{Code: "BROWSER_UNAVAILABLE"}
	}
	stream, err := client.StreamEditorFrames(ctx, &pb.EditorSessionRequest{ProtocolVersion: EditorPreviewProtocol, SessionId: id}, grpc.MaxCallRecvMsgSize(MaxPreviewFrameBytes+8192))
	if err != nil {
		return classifyRPCError("preview", err)
	}
	var sequence int64
	for {
		response, err := stream.Recv()
		if err != nil {
			return err
		}
		if !response.Success {
			return &BrowserError{Code: response.ErrorCode, Message: "preview unavailable"}
		}
		metadata, err := ValidatePreviewFrame(response)
		if err != nil {
			return err
		}
		if !metadata.Heartbeat && metadata.Sequence <= sequence {
			return &BrowserError{Code: "INVALID_RESPONSE", Message: "non-increasing preview frame"}
		}
		if !metadata.Heartbeat {
			sequence = metadata.Sequence
		}
		if err = emit(metadata, response.Image); err != nil {
			return err
		}
	}
}
