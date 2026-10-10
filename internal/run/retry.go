package run

import "errors"

// Retry creates a new live run; it never resumes an interrupted browser session.
type RetryInput struct {
	Scope     string `json:"scope"`
	Confirmed bool   `json:"confirmed"`
}

func (i RetryInput) Validate() error {
	if i.Scope != "full_run" || !i.Confirmed {
		return errors.New("retry requires full_run scope and explicit confirmation of live actions and formal writes")
	}
	return nil
}

type Controls struct {
	Cancel bool     `json:"can_cancel"`
	Retry  bool     `json:"can_retry"`
	Scopes []string `json:"retry_scopes"`
	Reason string   `json:"retry_block_reason"`
}

func RunControls(status string, cancelled bool) Controls {
	c := Controls{Cancel: !Terminal(status) && !cancelled, Scopes: []string{}, Reason: "RUN_NOT_TERMINAL"}
	switch status {
	case "succeeded", "limited", "partial", "failed", "cancelled":
		c.Retry = true
		c.Scopes = []string{"full_run"}
		c.Reason = ""
	}
	return c
}
