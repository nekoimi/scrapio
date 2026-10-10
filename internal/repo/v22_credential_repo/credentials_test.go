package v22_credential_repo

import (
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/db/table"
	"strings"
	"testing"
	"time"
)

func TestD04MetadataNeverReturnsSecret(t *testing.T) {
	r := &table.V22Credential{Ciphertext: "ciphertext-private", Fingerprint: "secret-fingerprint", CreateFingerprint: "initial-fingerprint", KeyId: "internal-key-id", MaskSelectors: "[]", ExpiresAt: time.Now().Add(time.Hour)}
	b, _ := json.Marshal(metadata(r))
	for _, s := range []string{"ciphertext-private", "secret-fingerprint", "initial-fingerprint", "internal-key-id", "ciphertext", "fingerprint"} {
		if strings.Contains(string(b), s) {
			t.Fatal("private metadata exposed", s)
		}
	}
}
