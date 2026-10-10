package publication

import (
	"github.com/nekoimi/scrapio/internal/trial"
	"testing"
	"time"
)

func TestD04CredentialsInvalidateOldTrial(t *testing.T) {
	now := time.Now()
	caps := Capabilities{CredentialUpdatedAt: now}
	if CheckCredentialTrial(caps, now.Add(-time.Second)).Ready {
		t.Fatal("old trial accepted after rotation")
	}
	if !CheckCredentialTrial(caps, now.Add(time.Second)).Ready {
		t.Fatal("new trial rejected")
	}
	if CheckCapabilities(trial.Plan{EntryType: "web", CredentialRef: "${credential:managed}"}, Capabilities{}).Ready {
		t.Fatal("browser authorization capability ignored")
	}
}
