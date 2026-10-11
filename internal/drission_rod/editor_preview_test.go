package drission_rod

import (
	"bytes"
	"encoding/json"
	pb "github.com/nekoimi/scrapio/internal/drission_rod/grpc"
	"image"
	"image/jpeg"
	"testing"
)

func TestPreviewFramesAreBoundedAndNeverAuthoritative(t *testing.T) {
	var jpegBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 2, 1)), nil); err != nil {
		t.Fatal(err)
	}
	meta := PreviewMetadata{Protocol: EditorPreviewProtocol, Sequence: 1, Mime: "image/jpeg", CapturedAt: 1, PreviewOnly: true}
	data, _ := json.Marshal(meta)
	frame := &pb.EditorPreviewFrame{Success: true, ProtocolVersion: EditorPreviewProtocol, MetadataJson: string(data), Image: jpegBytes.Bytes()}
	got, err := ValidatePreviewFrame(frame)
	if err != nil || got.Width != 2 || got.Height != 1 {
		t.Fatalf("valid preview: %+v %v", got, err)
	}
	frame.Image = []byte("not jpeg")
	if _, err := ValidatePreviewFrame(frame); err == nil {
		t.Fatal("accepted corrupt frame")
	}
	frame.Image = jpegBytes.Bytes()
	meta.PreviewOnly = false
	data, _ = json.Marshal(meta)
	frame.MetadataJson = string(data)
	if _, err := ValidatePreviewFrame(frame); err == nil {
		t.Fatal("accepted authoritative-looking preview")
	}
	frame.Image = nil
	frame.MetadataJson = `{"protocol":"editor.preview.v1","heartbeat":true}`
	if got, err := ValidatePreviewFrame(frame); err != nil || !got.Heartbeat {
		t.Fatalf("idle heartbeat: %v", err)
	}
	frame.Image = make([]byte, MaxPreviewFrameBytes+1)
	if _, err := ValidatePreviewFrame(frame); err == nil {
		t.Fatal("accepted oversized preview")
	}
}
