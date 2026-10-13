package drission_rod

import (
	"context"
	"github.com/nekoimi/scrapio/internal/credential"
	pb "github.com/nekoimi/scrapio/internal/drission_rod/grpc"
	"google.golang.org/grpc"
	"testing"
)

type coBrowserStub struct {
	pb.PageFetchServiceClient
	supported bool
	created   *pb.EditorSessionCreateRequest
}

func (c *coBrowserStub) GetEditorSession(_ context.Context, r *pb.EditorSessionRequest, _ ...grpc.CallOption) (*pb.EditorSessionResponse, error) {
	code := "UNSUPPORTED_PROTOCOL"
	if c.supported && r.ProtocolVersion == "editor.isolated.v1" {
		code = "ISOLATION_SUPPORTED"
	}
	return &pb.EditorSessionResponse{ErrorCode: code}, nil
}
func (c *coBrowserStub) CreateEditorSession(_ context.Context, r *pb.EditorSessionCreateRequest, _ ...grpc.CallOption) (*pb.EditorSessionResponse, error) {
	c.created = r
	return &pb.EditorSessionResponse{Success: true, SessionId: r.SessionId, Status: "ready", PageStateId: "page", CurrentUrl: r.Url}, nil
}
func TestCOIsolationNegotiationDoesNotAllocateTab(t *testing.T) {
	client := &coBrowserStub{}
	d := &DrissionRod{client: client}
	if d.ProbeEditorIsolation(context.Background()) == nil {
		t.Fatal("legacy service accepted")
	}
	if client.created != nil {
		t.Fatal("probe allocated tab")
	}
	client.supported = true
	if e := d.ProbeEditorIsolation(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e := d.CreateEditorSession(context.Background(), EditorSessionInput{Isolated: true, URL: "https://example.test", Authorization: credential.Authorization{Origin: "https://example.test"}}); e != nil {
		t.Fatal(e)
	}
	if client.created.ProtocolVersion != "editor.isolated.v1" || client.created.AuthorizationJson == "" {
		t.Fatal("lost isolation/authorization")
	}
}
