package capture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/credential"
	"golang.org/x/net/html"
)

func NewClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{DisableKeepAlives: true, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 32 * 1024}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errors.New("no destination address")
		}
		for _, ip := range ips {
			if !allowPrivate && (!ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast()) {
				return nil, errors.New("private destination not enabled")
			}
		}
		// Dial the checked IP, preserving hostname TLS validation. No proxy or
		// second DNS lookup can change the destination after validation.
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
	return &http.Client{Transport: transport}
}

// ResolveManaged is installed by bootstrap; standalone/offline callers fail closed.
var ResolveManaged func(context.Context, int64, string, string) (string, string, string, error)

func Credential(cfg *config.HTTPEntryConfig, ownerID int64, target, ref string) (string, string, string, error) {
	if strings.HasPrefix(ref, "${credential:") {
		if ResolveManaged == nil {
			return "", "", "", errors.New("managed credential unavailable")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return ResolveManaged(ctx, ownerID, target, ref)
	}
	if ref == "" {
		return "", "", "", nil
	}
	if cfg == nil {
		return "", "", "", errors.New("credential unavailable")
	}
	name := strings.TrimSuffix(strings.TrimPrefix(ref, "${secret:"), "}")
	entry, ok := cfg.Credentials[name]
	if !ok {
		return "", "", "", errors.New("credential unavailable")
	}
	u, err := ValidateURL(target)
	if err != nil {
		return "", "", "", err
	}
	allowedOwner, allowedOrigin := false, false
	for _, id := range entry.OwnerIDs {
		if id == ownerID {
			allowedOwner = true
		}
	}
	for _, origin := range entry.Origins {
		if origin == u.Scheme+"://"+u.Host {
			allowedOrigin = true
		}
	}
	secret := os.Getenv(entry.Env)
	header := entry.Header
	if header == "" {
		header = "Authorization"
	}
	if !allowedOwner || !allowedOrigin || secret == "" || len(secret) > 8192 || len(entry.Prefix) > 128 || strings.ContainsAny(header+entry.Prefix+secret, "\r\n") || !headerName.MatchString(header) || !sensitiveName.MatchString(header) || strings.HasPrefix(strings.ToLower(header), "proxy-") {
		return "", "", "", errors.New("credential missing or outside user/origin scope")
	}
	return header, entry.Prefix + secret, secret, nil
}

func Execute(ctx context.Context, client *http.Client, input Input, request HTTPRequest, target, authHeader, authValue, secret string) Result {
	result := Result{Status: "failed", FinalURL: target}
	if input.Source == "offline" {
		content, err := Sanitize(input.Content, input.Format, "")
		if err != nil {
			result.ErrorCode = "INVALID_CONTENT"
			result.ErrorStage = "parse"
			return result
		}
		result.Status = "succeeded"
		result.Content = content
		result.ContentType = map[string]string{"json": "application/json", "html": "text/html"}[input.Format]
		result.Bytes = len(input.Content)
		result.Hash = Hash([]byte(content))
		return result
	}
	u, err := ValidateURL(target)
	if err != nil {
		result.ErrorCode = "INVALID_URL"
		result.ErrorStage = "validate"
		return result
	}
	query := u.Query()
	for key, value := range request.Query {
		query.Set(key, value)
	}
	u.RawQuery = query.Encode()
	result.FinalURL = u.String()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(request.TimeoutMS)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, request.Method, u.String(), bytes.NewReader(request.Body))
	if err != nil {
		result.ErrorCode = "INVALID_REQUEST"
		result.ErrorStage = "validate"
		return result
	}
	for key, value := range request.Headers {
		req.Header.Set(key, value)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", map[string]string{"json": "application/json", "html": "text/html"}[input.Format])
	}
	if len(request.Body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if authHeader != "" {
		req.Header.Set(authHeader, authValue)
	}
	copyClient := *client
	copyClient.CheckRedirect = func(next *http.Request, prior []*http.Request) error {
		if request.Method == "POST" || authHeader != "" || len(prior) > 3 || next.URL.Scheme != u.Scheme || next.URL.Host != u.Host {
			return http.ErrUseLastResponse
		}
		_, err := ValidateURL(next.URL.String())
		return err
	}
	result.NetworkAccessed = true
	response, err := copyClient.Do(req)
	if err != nil {
		result.ErrorCode = "HTTP_REQUEST_FAILED"
		result.ErrorStage = "fetch"
		if request.Method == "POST" {
			result.Status = "uncertain"
			result.ErrorCode = "OUTCOME_UNCERTAIN"
		}
		return result
	}
	defer response.Body.Close()
	result.FinalURL = response.Request.URL.String()
	result.StatusCode = response.StatusCode
	result.ContentType = response.Header.Get("Content-Type")
	secrets := []string{secret}
	if strings.EqualFold(authHeader, "Cookie") {
		if cookies, e := credential.Cookies(secret); e == nil {
			for _, cookie := range cookies {
				secrets = append(secrets, cookie.Value)
			}
		}
	}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		result.FinalURL = strings.ReplaceAll(result.FinalURL, secret, "[REDACTED]")
		result.ContentType = strings.ReplaceAll(result.ContentType, secret, "[REDACTED]")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.ErrorCode = "HTTP_STATUS_ERROR"
		result.ErrorStage = "fetch"
		return result
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxBytes+1))
	result.Bytes = len(data)
	if len(data) > MaxBytes {
		result.ErrorCode = "RESPONSE_TOO_LARGE"
		result.ErrorStage = "read"
		return result
	}
	if err != nil {
		result.ErrorCode = "RESPONSE_READ_FAILED"
		result.ErrorStage = "read"
		return result
	}
	content, err := Sanitize(string(data), input.Format, secret)
	for _, part := range secrets {
		if err == nil && part != "" && part != secret {
			content, err = Sanitize(content, input.Format, part)
		}
	}
	if err != nil {
		result.ErrorCode = "INVALID_CONTENT"
		result.ErrorStage = "parse"
		return result
	}
	result.Status = "succeeded"
	result.Content = content
	result.Hash = Hash([]byte(content))
	return result
}

func Sanitize(content, format, secret string) (string, error) {
	if format == "json" {
		value, err := DecodeJSON(content)
		if err != nil {
			return "", err
		}
		value = redact(value, secret)
		data, err := json.Marshal(value)
		if err != nil || len(data) > MaxBytes {
			return "", errors.New("normalized snapshot exceeds limit")
		}
		return string(data), nil
	}
	if secret != "" {
		content = strings.ReplaceAll(content, secret, "[REDACTED]")
	}
	root, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return "", err
	}
	count := 0
	var clean func(*html.Node, int) error
	clean = func(node *html.Node, depth int) error {
		count++
		if count > 100000 || depth > 256 {
			return errors.New("HTML nesting or node budget exceeded")
		}
		attrs := node.Attr[:0]
		for _, attr := range node.Attr {
			if sensitiveName.MatchString(attr.Key) || strings.EqualFold(attr.Key, "value") && (node.Data == "input" || node.Data == "textarea") {
				continue
			}
			attrs = append(attrs, attr)
		}
		node.Attr = attrs
		for child := node.FirstChild; child != nil; {
			next := child.NextSibling
			if child.Type == html.ElementNode && (child.Data == "script" || child.Data == "style") || node.Data == "textarea" {
				node.RemoveChild(child)
			} else {
				if err := clean(child, depth+1); err != nil {
					return err
				}
			}
			child = next
		}
		return nil
	}
	if err := clean(root, 0); err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err = html.Render(&out, root); err != nil || out.Len() > MaxBytes {
		return "", errors.New("snapshot exceeds limit")
	}
	return out.String(), nil
}

func DecodeJSON(content string) (any, error) {
	if len(content) > MaxBytes || !json.Valid([]byte(content)) {
		return nil, errors.New("bounded valid JSON required")
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	count := 0
	var check func(any, int) bool
	check = func(value any, depth int) bool {
		count++
		if depth > 64 || count > 100000 {
			return false
		}
		switch v := value.(type) {
		case map[string]any:
			for _, item := range v {
				if !check(item, depth+1) {
					return false
				}
			}
		case []any:
			for _, item := range v {
				if !check(item, depth+1) {
					return false
				}
			}
		}
		return true
	}
	if !check(value, 0) {
		return nil, errors.New("JSON nesting or node budget exceeded")
	}
	return value, nil
}
func redact(value any, secret string) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if sensitiveName.MatchString(key) {
				v[key] = "[REDACTED]"
			} else {
				v[key] = redact(item, secret)
			}
		}
	case []any:
		for index, item := range v {
			v[index] = redact(item, secret)
		}
	case string:
		if secret != "" {
			return strings.ReplaceAll(v, secret, "[REDACTED]")
		}
	}
	return value
}
