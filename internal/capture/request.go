package capture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

const MaxBytes = 1024 * 1024

var sensitiveName = regexp.MustCompile(`(?i)(password|passwd|authorization|cookie|api.?key|access.?token|refresh.?token|secret|^token$)`)
var headerName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)

func DecodeRequest(data []byte) (HTTPRequest, error) {
	var request HTTPRequest
	if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return request, errors.New("http_request must be an object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, errors.New("unsupported HTTP request configuration")
	}
	return request, request.Validate()
}

type HTTPRequest struct {
	Method        string            `json:"method"`
	Query         map[string]string `json:"query,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Body          json.RawMessage   `json:"body,omitempty"`
	CredentialRef string            `json:"credential_ref,omitempty"`
	TimeoutMS     int               `json:"timeout_ms"`
}
type Input struct {
	SessionID        string `json:"session_id,omitempty"`
	PageStateID      string `json:"page_state_id,omitempty"`
	CollectorID      string `json:"collector_id"`
	ExpectedRevision int    `json:"expected_revision"`
	Source           string `json:"source"`
	Format           string `json:"format"`
	Confirmed        bool   `json:"confirmed"`
	Content          string `json:"content,omitempty"`
}
type Result struct {
	BaseURL         string
	NetworkAccessed bool
	Status          string
	Content         string
	FinalURL        string
	ContentType     string
	StatusCode      int
	Bytes           int
	Hash            string
	ErrorCode       string
	ErrorStage      string
}

func ValidateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.Scheme != "http" && u.Scheme != "https" || u.User != nil || len(raw) > 4096 {
		return nil, errors.New("http(s) URL without embedded credentials required")
	}
	for key := range u.Query() {
		if sensitiveName.MatchString(key) {
			return nil, errors.New("secret URL parameters require a credential reference")
		}
	}
	u.Fragment = ""
	return u, nil
}

func (r *HTTPRequest) Validate() error {
	if r.Method == "" {
		r.Method = "GET"
	}
	if r.TimeoutMS == 0 {
		r.TimeoutMS = 10000
	}
	if r.Method != "GET" && r.Method != "POST" || r.TimeoutMS < 100 || r.TimeoutMS > 15000 || len(r.Body) > 64*1024 || len(r.Query) > 30 || len(r.Headers) > 20 {
		return errors.New("GET/POST, bounded request and timeout required")
	}
	if r.Method == "GET" && len(r.Body) > 0 {
		return errors.New("GET body is not supported")
	}
	for key, value := range r.Query {
		if sensitiveName.MatchString(key) || len(key) > 128 || len(value) > 2048 {
			return errors.New("query parameters must be bounded and contain no literal secrets")
		}
	}
	for key, value := range r.Headers {
		if !headerName.MatchString(key) || sensitiveName.MatchString(key) || strings.EqualFold(key, "host") || strings.HasPrefix(strings.ToLower(key), "proxy-") || strings.EqualFold(key, "upgrade") || strings.EqualFold(key, "content-length") || strings.EqualFold(key, "connection") || strings.EqualFold(key, "transfer-encoding") || len(value) > 1024 || strings.ContainsAny(value, "\r\n") {
			return errors.New("header invalid; authentication must use credential_ref")
		}
	}
	if len(r.Body) > 0 {
		value, err := DecodeJSON(string(r.Body))
		if err != nil || containsSecret(value) {
			return errors.New("body must be JSON without literal secret fields")
		}
	}
	if r.CredentialRef != "" && !regexp.MustCompile(`^\$\{(?:secret:[a-zA-Z0-9_-]{1,64}|credential:[0-9a-f-]{36})\}$`).MatchString(r.CredentialRef) {
		return errors.New("credential_ref must use ${secret:name}")
	}
	return nil
}
func containsSecret(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if sensitiveName.MatchString(key) || containsSecret(item) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if containsSecret(item) {
				return true
			}
		}
	}
	return false
}
func (i *Input) Validate() error {
	if i.ExpectedRevision < 1 || i.Source != "http" && i.Source != "offline" && i.Source != "browser" || i.Format != "json" && i.Format != "html" || len(i.Content) > MaxBytes {
		return errors.New("revision, http/offline source, json/html format and bounded content required")
	}
	if i.Source == "browser" {
		if i.Format != "html" || i.SessionID == "" || len(i.SessionID) > 36 || i.PageStateID == "" || len(i.PageStateID) > 256 || i.Content != "" {
			return errors.New("browser snapshot requires an owned session and current page state")
		}
		return nil
	}
	if i.SessionID != "" || i.PageStateID != "" {
		return errors.New("session/page state belong to browser snapshots only")
	}
	if i.Source == "http" && (!i.Confirmed || i.Content != "") {
		return errors.New("HTTP capture requires explicit confirmation")
	}
	if i.Source == "offline" && i.Content == "" {
		return errors.New("offline content is required")
	}
	return nil
}
func Hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func Fingerprint(input Input, request HTTPRequest, target string) string {
	data, _ := json.Marshal([]any{input, request, target})
	return Hash(data)
}
func Journal(input Input, request HTTPRequest, target string) string {
	input.Content = ""
	data, _ := json.Marshal(map[string]any{"input": input, "http_request": request, "target": target})
	return string(data)
}
func IsSensitiveKey(key string) bool { return sensitiveName.MatchString(key) }
