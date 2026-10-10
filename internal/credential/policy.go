// Package credential defines write-only secrets and scoped runtime authorization.
package credential

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("credential unavailable, expired or outside scope")
var ErrInvalid = errors.New("invalid credential input")
var refPattern = regexp.MustCompile(`^\$\{credential:([0-9a-f-]{36})\}$`)
var envPattern = regexp.MustCompile(`^SCRAPIO_SECRET_[A-Z0-9_]{1,80}$`)
var headerPattern = regexp.MustCompile(`(?i)^(Authorization|Cookie|X-API-Key|API-Key|X-Auth-Token)$`)

type Input struct {
	ID               string    `json:"credential_id"`
	ExpectedRevision int       `json:"expected_revision"`
	Name             string    `json:"name"`
	Kind             string    `json:"kind"`
	Origin           string    `json:"origin"`
	Storage          string    `json:"storage"`
	Header           string    `json:"header"`
	Prefix           string    `json:"prefix"`
	Env              string    `json:"env"`
	Secret           string    `json:"secret"`
	ExpiresAt        time.Time `json:"expires_at"`
	MaskSelectors    []string  `json:"mask_selectors"`
}
type Cookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type Authorization struct {
	Origin        string    `json:"origin"`
	Cookies       []Cookie  `json:"cookies"`
	MaskSelectors []string  `json:"mask_selectors"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func Ref(id string) string { return "${credential:" + id + "}" }
func ID(ref string) string {
	m := refPattern.FindStringSubmatch(ref)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}
func Origin(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.Scheme != "https" && u.Scheme != "http" || u.User != nil {
		return "", ErrInvalid
	}
	return u.Scheme + "://" + u.Host, nil
}
func (i Input) Validate(now time.Time, requireSecret bool) error {
	o, e := Origin(i.Origin)
	if e != nil || o != i.Origin || len(i.Name) < 1 || len([]rune(i.Name)) > 80 || i.ExpectedRevision < 0 || !i.ExpiresAt.After(now) || i.ExpiresAt.After(now.Add(366*24*time.Hour)) || len(i.MaskSelectors) > 10 {
		return ErrInvalid
	}
	if i.Kind != "http_header" && i.Kind != "browser_cookie" || i.Storage != "encrypted" && i.Storage != "environment" {
		return ErrInvalid
	}
	if i.Kind == "browser_cookie" && (!strings.HasPrefix(o, "https://") || i.Header != "" || i.Prefix != "") {
		return ErrInvalid
	}
	if i.Kind == "http_header" && (!headerPattern.MatchString(i.Header) || len(i.Prefix) > 128 || strings.ContainsAny(i.Prefix, "\r\n")) {
		return ErrInvalid
	}
	for _, s := range i.MaskSelectors {
		if strings.TrimSpace(s) == "" || len(s) > 256 {
			return ErrInvalid
		}
	}
	if i.Storage == "environment" {
		if !envPattern.MatchString(i.Env) || i.Secret != "" {
			return ErrInvalid
		}
	} else if i.Env != "" || requireSecret && i.Secret == "" {
		return ErrInvalid
	}
	if i.Secret != "" {
		return ValidateSecret(i.Kind, i.Secret)
	}
	return nil
}
func ValidateSecret(kind, secret string) error {
	if secret == "" || len(secret) > 8192 || strings.ContainsAny(secret, "\r\n\x00") {
		return ErrInvalid
	}
	for _, b := range []byte(secret) {
		if b < 32 && b != '\t' || b == 127 {
			return ErrInvalid
		}
	}
	if kind == "browser_cookie" {
		_, e := Cookies(secret)
		return e
	}
	return nil
}
func Cookies(secret string) ([]Cookie, error) {
	r := &http.Request{Header: http.Header{"Cookie": []string{secret}}}
	items := r.Cookies()
	out := []Cookie{}
	seen := map[string]bool{}
	parts := strings.Split(secret, ";")
	if len(items) == 0 || len(items) > 40 || len(items) != len(parts) {
		return nil, ErrInvalid
	}
	for _, c := range items {
		if seen[c.Name] || c.Name == "" || c.Value == "" {
			return nil, ErrInvalid
		}
		seen[c.Name] = true
		out = append(out, Cookie{c.Name, c.Value})
	}
	return out, nil
}
func key() ([]byte, error) {
	k, e := base64.StdEncoding.DecodeString(os.Getenv("SCRAPIO_CREDENTIAL_KEY"))
	if e != nil || len(k) != 32 {
		return nil, ErrUnavailable
	}
	return k, nil
}
func KeyReady() bool { _, e := key(); return e == nil }
func KeyID() string {
	k, e := key()
	if e != nil {
		return ""
	}
	h := sha256.Sum256(k)
	return hex.EncodeToString(h[:8])
}
func Seal(secret, aad string) (string, error) {
	k, e := key()
	if e != nil {
		return "", e
	}
	block, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(block)
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(secret), []byte(aad))), nil
}
func Open(encoded, aad string) (string, error) {
	k, e := key()
	if e != nil {
		return "", e
	}
	b, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil {
		return "", ErrUnavailable
	}
	block, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(block)
	if len(b) < g.NonceSize() {
		return "", ErrUnavailable
	}
	v, e := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], []byte(aad))
	if e != nil {
		return "", ErrUnavailable
	}
	return string(v), nil
}
func Fingerprint(i Input) (string, error) {
	i.ExpectedRevision = 0
	raw, _ := json.Marshal(i)
	k, e := key()
	if i.Storage == "encrypted" && e != nil {
		return "", e
	}
	if i.Storage == "environment" {
		k = []byte("environment-metadata")
	}
	m := hmac.New(sha256.New, k)
	m.Write(raw)
	return hex.EncodeToString(m.Sum(nil)), nil
}

// DefinitionRef accepts references only, never browser secrets embedded in drafts.
func DefinitionRef(raw []byte) (string, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return "", ErrInvalid
	}
	v, ok := root["browser_auth"]
	if !ok {
		return "", nil
	}
	var auth struct {
		Ref string `json:"credential_ref"`
	}
	d := json.NewDecoder(bytes.NewReader(v))
	d.DisallowUnknownFields()
	if d.Decode(&auth) != nil || ID(auth.Ref) == "" {
		return "", ErrInvalid
	}
	return auth.Ref, nil
}
