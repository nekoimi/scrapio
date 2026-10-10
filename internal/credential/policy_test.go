package credential

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestD04AuthenticatedEncryption(t *testing.T) {
	t.Setenv("SCRAPIO_CREDENTIAL_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	a, e := Seal("private-cookie", "owner:1")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := Seal("private-cookie", "owner:1")
	if a == b || strings.Contains(a, "private-cookie") {
		t.Fatal("nonce reuse or plaintext")
	}
	if value, e := Open(a, "owner:1"); e != nil || value != "private-cookie" {
		t.Fatal(e)
	}
	if _, e := Open(a, "owner:2"); e == nil {
		t.Fatal("cross-owner ciphertext accepted")
	}
	t.Setenv("SCRAPIO_CREDENTIAL_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32))))
	if _, e := Open(a, "owner:1"); e == nil {
		t.Fatal("wrong key accepted")
	}
	t.Setenv("SCRAPIO_CREDENTIAL_KEY", "")
	if _, e := Seal("value", "owner"); e == nil {
		t.Fatal("missing key accepted")
	}
}
func TestD04CredentialConstraints(t *testing.T) {
	i := Input{ID: "7c1909fb-2624-4c48-b4e9-434f382e972b", Name: "login", Kind: "browser_cookie", Origin: "https://example.com", Storage: "encrypted", Secret: "sid=private; token=other", ExpiresAt: time.Now().Add(time.Hour)}
	if e := i.Validate(time.Now(), true); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*Input){func(i *Input) { i.Origin = "https://example.com/path" }, func(i *Input) { i.Origin = "http://example.com" }, func(i *Input) { i.Secret = "sid=private\r\nInjected: yes" }, func(i *Input) { i.Secret = "sid=a; sid=b" }, func(i *Input) { i.ExpiresAt = time.Now().Add(-time.Second) }, func(i *Input) { i.Storage = "environment"; i.Env = "DB_PASSWORD"; i.Secret = "" }} {
		x := i
		change(&x)
		if x.Validate(time.Now(), true) == nil {
			t.Fatal("invalid credential accepted", x.Kind, x.Origin)
		}
	}
	if _, e := DefinitionRef([]byte(`{"browser_auth":{"credential_ref":"${credential:7c1909fb-2624-4c48-b4e9-434f382e972b}","password":"literal"}}`)); e == nil {
		t.Fatal("literal browser secret accepted")
	}
	if ID(Ref(i.ID)) != i.ID {
		t.Fatal("reference identity")
	}
}
