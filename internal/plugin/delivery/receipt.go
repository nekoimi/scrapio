package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/nekoimi/scrapio/internal/plugin"
	"strings"
)

type rpcError struct {
	Code    int
	Message string
}

func (e *rpcError) Error() string { return fmt.Sprintf("aria2 error %d: %s", e.Code, e.Message) }

func (p *Aria2) DeliveryVersion() string { return plugin.Aria2DeliveryVersion }

// Freeze service identity without persisting endpoint credentials.
func (p *Aria2) DeliveryDestination() string {
	h := sha256.Sum256([]byte(p.cfg.JsonRpc + "\x00" + p.cfg.Secret))
	return hex.EncodeToString(h[:])
}
func (p *Aria2) DeliveryIdentity(t plugin.Task) string {
	h := sha256.Sum256([]byte(t.IdempotencyKey))
	gid := hex.EncodeToString(h[:8])
	if gid == "0000000000000000" {
		return "0000000000000001"
	}
	return gid
}
func (p *Aria2) SubmitDelivery(ctx context.Context, t plugin.Task) (any, string, error) {
	if e := plugin.ValidateAria2Delivery(t.Input); e != nil {
		return nil, "", &plugin.PermanentError{Err: e}
	}
	if strings.TrimSpace(p.cfg.JsonRpc) == "" || t.IdempotencyKey == "" {
		return nil, "", &plugin.PermanentError{Err: errors.New("aria2 endpoint and delivery identity required")}
	}
	gid := p.DeliveryIdentity(t)
	options := map[string]any{"gid": gid}
	if dir, ok := t.Input["dir"]; ok {
		options["dir"] = dir
	}
	params := []any{}
	if p.cfg.Secret != "" {
		params = append(params, "token:"+p.cfg.Secret)
	}
	params = append(params, []string{strings.TrimSpace(t.Input["url"].(string))}, options)
	result, e := p.call(ctx, "aria2.addUri", params)
	if e != nil {
		var rejection *rpcError
		if errors.As(e, &rejection) {
			return nil, gid, &plugin.PermanentError{Err: e}
		}
		return nil, gid, e
	}
	returned, ok := result.(string)
	if !ok || returned != gid {
		return nil, gid, errors.New("aria2 receipt does not match fixed gid")
	}
	return map[string]any{"gid": gid, "receipt": "accepted", "completion": "download_not_confirmed"}, gid, nil
}
func (p *Aria2) QueryDelivery(ctx context.Context, t plugin.Task, external string) (any, string, error) {
	if external == "" || external != p.DeliveryIdentity(t) {
		return nil, "unknown", errors.New("receipt identity mismatch")
	}
	params := []any{}
	if p.cfg.Secret != "" {
		params = append(params, "token:"+p.cfg.Secret)
	}
	params = append(params, external, []string{"gid", "status", "errorCode", "followedBy"})
	result, e := p.call(ctx, "aria2.tellStatus", params)
	if e != nil {
		return nil, "unknown", e
	}
	row, ok := result.(map[string]any)
	if !ok || row["gid"] != external {
		return nil, "unknown", errors.New("aria2 returned invalid original receipt")
	}
	switch row["status"] {
	case "active", "waiting", "paused", "complete":
		return row, "accepted", nil
	case "error", "removed":
		return row, "failed", nil
	default:
		return row, "unknown", nil
	}
}
