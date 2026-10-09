package capture

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nekoimi/scrapio/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRequestBoundaries(t *testing.T) {
	for _, raw := range []string{`{"method":"DELETE"}`, `{"timeout_ms":16000}`, `{"headers":{"Authorization":"secret"}}`, `{"body":{"nested":{"password":"secret"}},"method":"POST"}`, `{"headers":{"Upgrade":"websocket"}}`, `{"unknown":true}`, `null`} {
		if _, err := DecodeRequest([]byte(raw)); err == nil {
			t.Fatalf("accepted unsupported request: %s", raw)
		}
	}
	for _, target := range []string{"file:///tmp/file", "http://user:pass@example.com/", "https://example.com/?access_token=secret"} {
		if _, err := ValidateURL(target); err == nil {
			t.Fatalf("accepted URL: %s", target)
		}
	}
	request, err := DecodeRequest([]byte(`{"method":"POST","body":{"ids":[1,2]},"credential_ref":"${secret:example}"}`))
	if err != nil || request.TimeoutMS != 10000 {
		t.Fatalf("valid request: %+v, %v", request, err)
	}
}

func TestJSONPointerAndPrecision(t *testing.T) {
	plan, err := DecodePlan([]byte(`{"array_pointer":"/a~1b/~0items","max_records":1,"fields":[{"name":"id","pointer":"/id"},{"name":"missing","pointer":"/missing"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := InspectJSON(`{"a/b":{"~items":[{"id":9007199254740993},{"id":2}]}}`, plan)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if !strings.Contains(string(encoded), `9007199254740993`) || !strings.Contains(string(encoded), `"found":false`) || result["truncated"] != true || result["match_count"] != 2 {
		t.Fatalf("unexpected inspection: %s", encoded)
	}
	value, _ := DecodeJSON(`[1]`)
	for _, pointer := range []string{"/01", "/-1", "/-", "/~2"} {
		if _, ok := Resolve(value, pointer); ok {
			t.Fatalf("accepted pointer %s", pointer)
		}
	}
	if value, ok := Resolve(value, ""); !ok || value == nil {
		t.Fatal("root array pointer rejected")
	}
	for _, raw := range []string{`{"array_pointer":null,"max_records":1,"fields":[]}`, `{"array_pointer":"","max_records":1,"fields":[{"name":"id"}]}`, `{"array_pointer":"","max_records":1,"fields":[],"unknown":true}`, `{"array_pointer":"","max_records":1,"fields":[{"name":"id","pointer":"","type":"integer"}]}`} {
		if _, err := DecodePlan([]byte(raw)); err == nil {
			t.Fatal("unknown rule silently ignored")
		}
	}
}

func TestSnapshotRedactionAndOffline(t *testing.T) {
	secret := `quoted"\secret`
	data, _ := json.Marshal(map[string]any{"id": json.Number("9007199254740993"), "echo": secret, "nested": []any{map[string]any{"password": "hidden"}, secret}})
	content, err := Sanitize(string(data), "json", secret)
	if err != nil || !json.Valid([]byte(content)) || strings.Contains(content, "hidden") || strings.Contains(content, "quoted") || !strings.Contains(content, "9007199254740993") {
		t.Fatalf("bad redaction: %s %v", content, err)
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("offline accessed network"); return nil, nil })}
	result := Execute(context.Background(), client, Input{Source: "offline", Format: "html", Content: `<input value="hidden"><textarea>hidden</textarea><script>hidden</script><p>visible</p>`}, HTTPRequest{}, "https://example.com/", "", "", "")
	if result.NetworkAccessed || result.Status != "succeeded" || strings.Contains(result.Content, "hidden") || !strings.Contains(result.Content, "visible") || result.Hash != Hash([]byte(result.Content)) {
		t.Fatalf("bad offline snapshot: %+v", result)
	}
	if _, err = DecodeJSON(strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65)); err == nil {
		t.Fatal("unbounded JSON nesting accepted")
	}
}

func TestHTTPExecutionBoundaries(t *testing.T) {
	request := HTTPRequest{Method: "POST", TimeoutMS: 1000, Query: map[string]string{"q": "hello"}, Body: json.RawMessage(`{"id":1}`)}
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.Query().Get("q") != "hello" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("saved request not executed")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.example/"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	input := Input{Source: "http", Format: "json"}
	result := Execute(context.Background(), client, input, request, "https://example.com/", "Authorization", "Bearer secret", "secret")
	if calls != 1 || result.StatusCode != 302 || result.ErrorCode != "HTTP_STATUS_ERROR" {
		t.Fatalf("POST redirected: %+v, %d", result, calls)
	}
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, errors.New("transport lost") })
	result = Execute(context.Background(), client, input, request, "https://example.com/", "", "", "")
	if result.Status != "uncertain" || !result.NetworkAccessed {
		t.Fatalf("lost POST outcome reported incorrectly: %+v", result)
	}
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", MaxBytes+1))), Request: r}, nil
	})
	result = Execute(context.Background(), client, input, request, "https://example.com/", "", "", "")
	if result.ErrorCode != "RESPONSE_TOO_LARGE" || result.Content != "" {
		t.Fatal("oversize response saved")
	}
}

func TestCredentialScopeAndPrivateNetwork(t *testing.T) {
	t.Setenv("SCRAPIO_TEST_HTTP_SECRET", "secret-value")
	cfg := &config.HTTPEntryConfig{Credentials: map[string]config.HTTPEntryCredential{"test": {Env: "SCRAPIO_TEST_HTTP_SECRET", OwnerIDs: []int64{1}, Origins: []string{"https://example.com"}, Prefix: "Bearer "}}}
	header, value, _, err := Credential(cfg, 1, "https://example.com/path", "${secret:test}")
	if err != nil || header != "Authorization" || value != "Bearer secret-value" {
		t.Fatal("valid credential scope rejected")
	}
	for _, target := range []string{"https://other.example/", "http://example.com/"} {
		if _, _, _, err = Credential(cfg, 1, target, "${secret:test}"); err == nil {
			t.Fatal("credential escaped origin scope")
		}
	}
	if _, _, _, err = Credential(cfg, 2, "https://example.com/", "${secret:test}"); err == nil {
		t.Fatal("credential escaped owner scope")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := NewClient(false).Transport.(*http.Transport)
	if conn, err := transport.DialContext(ctx, "tcp", "127.0.0.1:1"); err == nil {
		conn.Close()
		t.Fatal("private destination accepted")
	}
}
