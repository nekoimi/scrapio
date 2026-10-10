package v22_schedule_repo

import (
	"strings"
	"testing"
)

func TestScheduleCredentialHeader(t *testing.T) {
	token := "sap_" + strings.Repeat("a", 64)
	if got, err := Bearer("Bearer " + token); err != nil || got != token {
		t.Fatal("valid token rejected", err)
	}
	for _, header := range []string{"", token, "Bearer jwt", "Bearer " + token + " ", "Bearer sap_" + strings.Repeat("g", 64)} {
		if _, err := Bearer(header); err == nil {
			t.Error("accepted malformed credential", header)
		}
	}
	if len(TokenHash(token)) != 64 || TokenHash(token) == TokenHash(token+"b") {
		t.Fatal("credential hash mismatch")
	}
}
