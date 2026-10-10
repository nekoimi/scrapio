package capture

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestD04CookieComponentsRedacted(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"echo":"private-cookie-value"}`))
	}))
	defer s.Close()
	r := Execute(context.Background(), s.Client(), Input{Source: "http", Format: "json", Confirmed: true}, HTTPRequest{Method: "GET", TimeoutMS: 1000}, s.URL, "Cookie", "sid=private-cookie-value", "sid=private-cookie-value")
	if r.Status != "succeeded" || strings.Contains(r.Content, "private-cookie-value") {
		t.Fatal("cookie echo leaked")
	}
}

func TestD04ManagedReferenceDoesNotFallBack(t *testing.T) {
	previous := ResolveManaged
	defer func() { ResolveManaged = previous }()
	ResolveManaged = nil
	if _, _, _, e := Credential(nil, 1, "https://example.com/", "${credential:missing}"); e == nil {
		t.Fatal("missing resolver fell back to public")
	}
	var owner int64
	ResolveManaged = func(ctx context.Context, id int64, target, ref string) (string, string, string, error) {
		owner = id
		return "Authorization", "Bearer private", "private", nil
	}
	h, v, _, e := Credential(nil, 7, "https://example.com/", "${credential:managed}")
	if e != nil || owner != 7 || h != "Authorization" || v != "Bearer private" {
		t.Fatal("managed reference not resolved")
	}
}
