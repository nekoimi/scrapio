package drission_rod

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"

	pb "github.com/nekoimi/scrapio/internal/drission_rod/grpc"
	"github.com/nekoimi/scrapio/internal/editor"
)

func TestEditorStateCaptureContract(t *testing.T) {
	meta := editor.State{Protocol: editor.StateProtocol, PageStateID: "page-1", DocumentID: strings.Repeat("a", 64), ValidationStateID: strings.Repeat("b", 64), FrameStateID: strings.Repeat("c", 64), Viewport: editor.Size{Width: 640, Height: 400}, PixelRatio: 2, Image: &editor.Size{Width: 2, Height: 1}, FrameSequence: 1, CapturedAtUnixMS: 1}
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(meta)
	r := &pb.EditorSessionResponse{Success: true, ProtocolVersion: EditorProtocolVersion, PageStateId: "page-1", StateJson: string(encoded), Screenshot: picture.Bytes()}
	state, err := editorSessionResponse(r)
	if err != nil || state.State == nil || state.State.Image.Width != 2 {
		t.Fatalf("valid frame: %v", err)
	}
	r.PageStateId = "page-2"
	if _, err := editorSessionResponse(r); err == nil {
		t.Fatal("accepted mismatched page")
	}
	r.PageStateId = "page-1"
	r.Screenshot = []byte("invalid PNG")
	if _, err := editorSessionResponse(r); err == nil {
		t.Fatal("accepted bad capture")
	}
	r.Screenshot = picture.Bytes()
	meta.Image.Width = 3
	encoded, _ = json.Marshal(meta)
	r.StateJson = string(encoded)
	if _, err := editorSessionResponse(r); err == nil {
		t.Fatal("accepted mismatched image dimensions")
	}
	r.StateJson = "null"
	if _, err := editorSessionResponse(r); err == nil {
		t.Fatal("accepted null metadata")
	}
	r.StateJson = ""
	state, err = editorSessionResponse(r)
	if err != nil || state.State != nil {
		t.Fatalf("legacy frame should retain strict page-only contract: %v", err)
	}
}
