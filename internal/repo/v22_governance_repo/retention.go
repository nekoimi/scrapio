package v22_governance_repo

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/governance"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"xorm.io/xorm"
)

func Policy(s *xorm.Session, owner int64) (governance.Policy, error) {
	rows, err := s.QueryString("SELECT revision::text,capture_days::text,asset_limit_mib::text FROM v22_retention_settings WHERE owner_id=?", owner)
	p := governance.DefaultPolicy()
	if len(rows) > 0 {
		p.Revision = int(number(rows, "revision"))
		p.CaptureDays = int(number(rows, "capture_days"))
		p.AssetLimitMiB = int(number(rows, "asset_limit_mib"))
	}
	return p, err
}
func assetBytes(s *xorm.Session, owner int64) (int64, int64, error) {
	rows, err := s.QueryString(`SELECT
 (SELECT COALESCE(sum(octet_length(content)),0) FROM v22_captures WHERE owner_id=?) +
 (SELECT COALESCE(sum(octet_length(screenshot)),0) FROM v22_samples WHERE owner_id=?) AS bytes,
 (SELECT count(*) FROM v22_captures WHERE owner_id=? AND status IN ('running','uncertain') AND deadline_at>CURRENT_TIMESTAMP) * ? AS reserved`, owner, owner, owner, capture.MaxBytes)
	return number(rows, "bytes"), number(rows, "reserved"), err
}

// All capture reservations and sample screenshots share this owner lock.
// Protected evidence counts toward the budget; it is never evicted to make space.
func ReserveAssets(s *xorm.Session, owner int64, additional int64) error {
	if additional < 0 {
		return ErrInvalid
	}
	if err := idempotency.Lock(s, owner, "governance.assets", "owner"); err != nil {
		return err
	}
	p, err := Policy(s, owner)
	if err != nil {
		return err
	}
	used, reserved, err := assetBytes(s, owner)
	if err != nil {
		return err
	}
	if used+reserved+additional > int64(p.AssetLimitMiB)*1024*1024 {
		return ErrCapacity
	}
	return nil
}

// A late response may outlive its reservation. Recheck its replacement under
// the pool lock instead of assuming an expired reservation is still available.
func CompleteAssets(s *xorm.Session, owner int64, id string, size int64) error {
	if err := idempotency.Lock(s, owner, "governance.assets", "owner"); err != nil {
		return err
	}
	rows, err := s.QueryString("SELECT CASE WHEN status IN ('running','uncertain') AND deadline_at>CURRENT_TIMESTAMP THEN ? ELSE 0 END AS own_reserved FROM v22_captures WHERE id=? AND owner_id=?", capture.MaxBytes, id, owner)
	if err != nil {
		return err
	}
	used, reserved, err := assetBytes(s, owner)
	if err != nil {
		return err
	}
	p, err := Policy(s, owner)
	if err != nil {
		return err
	}
	if used+reserved-number(rows, "own_reserved")+size > int64(p.AssetLimitMiB)*1024*1024 {
		return ErrCapacity
	}
	return nil
}
func Settings(ctx context.Context, owner int64) (map[string]any, error) {
	s, err := begin(ctx)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	defer s.Rollback()
	p, err := Policy(s, owner)
	if err != nil {
		return nil, err
	}
	used, reserved, err := assetBytes(s, owner)
	if err != nil {
		return nil, err
	}
	rows, err := s.QueryString(`SELECT
 (SELECT count(*) FROM v22_samples WHERE owner_id=?)::text AS samples,
 (SELECT count(*) FROM v22_samples WHERE owner_id=? AND protected)::text AS protected_samples,
 (SELECT count(*) FROM v22_data_tables WHERE owner_id=?)::text AS tables,
 (SELECT count(*) FROM v22_credentials WHERE owner_id=?)::text AS credentials,
 (SELECT count(*) FROM v22_captures WHERE owner_id=?)::text AS captures,
 (SELECT COALESCE(sum(octet_length(rows::text)),0) FROM v22_data_snapshots WHERE owner_id=?)::text AS snapshot_bytes,
 (SELECT COALESCE(sum(octet_length(file)),0) FROM v22_exports WHERE owner_id=?)::text AS export_bytes,
 (SELECT count(*) FROM v22_versions WHERE owner_id=?)::text AS versions,
 (SELECT count(*) FROM v22_runs WHERE owner_id=?)::text AS runs`, owner, owner, owner, owner, owner, owner, owner, owner, owner)
	if err != nil {
		return nil, err
	}
	return map[string]any{"policy": p, "asset_bytes": used, "reserved_bytes": reserved, "usage": rows[0], "limits": map[string]int{"tables": 100, "samples_per_collector": 100, "credentials": 100, "active_runs": 3, "run_history": 1000, "active_regressions": 2, "regression_history": 100, "active_exports": 3, "valid_exports": 20, "export_history": 200, "snapshot_rows": 10000, "snapshot_mib": 16}, "fixed_retention": map[string]int{"snapshot_minutes": 30, "export_file_hours": 24, "export_metadata_days": 7, "schema_previews_per_table": 20, "cleanup_previews": 20}, "scope": "capture content + sample screenshots; formal data and frozen version/trial/regression evidence are retained separately"}, nil
}
func SavePolicy(ctx context.Context, owner int64, input governance.Policy) (governance.Policy, error) {
	if !input.Valid() {
		return input, ErrInvalid
	}
	s, err := begin(ctx)
	if err != nil {
		return input, err
	}
	defer s.Close()
	defer s.Rollback()
	if err = idempotency.Lock(s, owner, "governance.assets", "owner"); err != nil {
		return input, err
	}
	p, err := Policy(s, owner)
	if err != nil {
		return input, err
	}
	if p.Revision != input.Revision {
		return input, ErrConflict
	}
	used, reserved, err := assetBytes(s, owner)
	if err != nil {
		return input, err
	}
	if used+reserved > int64(input.AssetLimitMiB)*1024*1024 {
		return input, ErrCapacity
	}
	input.Revision++
	if _, err = s.Exec(`INSERT INTO v22_retention_settings(owner_id,revision,capture_days,asset_limit_mib) VALUES(?,?,?,?) ON CONFLICT(owner_id) DO UPDATE SET revision=EXCLUDED.revision,capture_days=EXCLUDED.capture_days,asset_limit_mib=EXCLUDED.asset_limit_mib,updated_at=clock_timestamp()`, owner, input.Revision, input.CaptureDays, input.AssetLimitMiB); err != nil {
		return input, err
	}
	if err = Record(s, owner, "retention.update", strconv.FormatInt(owner, 10), governance.SafeDetails{Revision: input.Revision}); err != nil {
		return input, err
	}
	return input, s.Commit()
}

// Keep every sample (not only manually protected ones), output check, observation
// and repair reference. Capture tombstones keep request keys/hash/identity, so an
// old request cannot silently make a new network call after retention cleanup.
const unpinnedCapture = `NOT EXISTS(SELECT 1 FROM v22_samples s WHERE s.capture_id=c.id)
 AND NOT EXISTS(SELECT 1 FROM v22_output_checks o WHERE o.capture_id=c.id)
 AND NOT EXISTS(SELECT 1 FROM v22_table_observations o WHERE o.capture_id=c.id)
 AND NOT EXISTS(SELECT 1 FROM v22_repair_drafts r WHERE r.capture_id=c.id)
 AND NOT EXISTS(SELECT 1 FROM v22_trial_capture_pins t WHERE t.capture_id=c.id)`

type CleanupPreview struct {
	CheckID        string              `json:"check_id"`
	PolicyRevision int                 `json:"policy_revision"`
	Items          []map[string]string `json:"items"`
	Bytes          int64               `json:"bytes"`
	More           bool                `json:"has_more"`
	ExpiresAt      time.Time           `json:"expires_at"`
	Applied        bool                `json:"applied"`
}

func candidates(s *xorm.Session, owner int64, p governance.Policy) ([]map[string]string, error) {
	rows, err := s.QueryString(`SELECT * FROM (
 SELECT 'capture' AS kind,c.id,octet_length(c.content)::text AS bytes,c.content_hash AS hash FROM v22_captures c WHERE c.owner_id=? AND c.status IN ('succeeded','failed') AND octet_length(c.content)>0 AND c.created_at<clock_timestamp()-make_interval(days=>?) AND `+unpinnedCapture+`
 UNION ALL SELECT 'snapshot',p.id,octet_length(p.rows::text)::text,p.query_hash FROM v22_data_snapshots p WHERE p.owner_id=? AND p.expires_at<=clock_timestamp() AND NOT EXISTS(SELECT 1 FROM v22_exports e WHERE e.snapshot_id=p.id)
 UNION ALL SELECT 'export',e.id,octet_length(e.file)::text,e.file_hash FROM v22_exports e WHERE e.owner_id=? AND e.expires_at<=clock_timestamp() AND e.status NOT IN ('queued','running') AND octet_length(e.file)>0
 ) assets ORDER BY kind,id LIMIT 201`, owner, p.CaptureDays, owner, owner)
	if rows == nil {
		rows = []map[string]string{}
	}
	return rows, err
}
func CreateCleanupCheck(ctx context.Context, owner int64) (*CleanupPreview, error) {
	s, err := begin(ctx)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	defer s.Rollback()
	if err = idempotency.Lock(s, owner, "governance.assets", "owner"); err != nil {
		return nil, err
	}
	p, err := Policy(s, owner)
	if err != nil {
		return nil, err
	}
	items, err := candidates(s, owner, p)
	if err != nil {
		return nil, err
	}
	result := &CleanupPreview{CheckID: uuid.NewString(), PolicyRevision: p.Revision, Items: items, More: len(items) > 200, ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}
	if result.More {
		result.Items = items[:200]
	}
	for _, item := range result.Items {
		v, _ := strconv.ParseInt(item["bytes"], 10, 64)
		result.Bytes += v
	}
	raw, _ := json.Marshal(result)
	if _, err = s.Exec("INSERT INTO v22_cleanup_checks(id,owner_id,policy_revision,candidates,expires_at) VALUES(?,?,?,?::jsonb,?)", result.CheckID, owner, p.Revision, string(raw), result.ExpiresAt); err != nil {
		return nil, err
	}
	if _, err = s.Exec("DELETE FROM v22_cleanup_checks WHERE id IN (SELECT id FROM v22_cleanup_checks WHERE owner_id=? AND result IS NULL ORDER BY created_at DESC,id DESC OFFSET 20)", owner); err != nil {
		return nil, err
	}
	return result, s.Commit()
}
func ApplyCleanup(ctx context.Context, owner int64, id string, confirmed bool) (*CleanupPreview, error) {
	if !confirmed {
		return nil, ErrInvalid
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrInvalid
	}
	s, err := begin(ctx)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	defer s.Rollback()
	if err = idempotency.Lock(s, owner, "governance.assets", "owner"); err != nil {
		return nil, err
	}
	checks, err := s.QueryString("SELECT candidates::text,result::text,expires_at>clock_timestamp() AS live FROM v22_cleanup_checks WHERE id=? AND owner_id=? FOR UPDATE", id, owner)
	if err != nil {
		return nil, err
	}
	if len(checks) == 0 {
		return nil, ErrNotFound
	}
	result := new(CleanupPreview)
	if checks[0]["result"] != "" && checks[0]["result"] != "null" {
		err = json.Unmarshal([]byte(checks[0]["result"]), result)
		return result, err
	}
	if checks[0]["live"] != "true" {
		return nil, ErrConflict
	}
	if err = json.Unmarshal([]byte(checks[0]["candidates"]), result); err != nil {
		return nil, err
	}
	p, err := Policy(s, owner)
	if err != nil {
		return nil, err
	}
	if p.Revision != result.PolicyRevision {
		return nil, ErrConflict
	}
	counts := governance.SafeDetails{Bytes: result.Bytes}
	// Lock first, then recheck pins in a NEW statement. A concurrent sample/check
	// creation holds SHARE on capture; PostgreSQL waits for it before rechecking.
	for _, item := range result.Items {
		var affected int64
		switch item["kind"] {
		case "capture":
			if _, err = s.QueryString("SELECT id FROM v22_captures WHERE owner_id=? AND id=? FOR UPDATE", owner, item["id"]); err != nil {
				return nil, err
			}
			res, e := s.Exec(`UPDATE v22_captures c SET content='',byte_count=0,status='failed',error_code='INPUT_EXPIRED',error_stage='retention' WHERE c.owner_id=? AND c.id=? AND c.content_hash=? AND octet_length(c.content)::text=? AND c.status IN ('succeeded','failed') AND c.created_at<clock_timestamp()-make_interval(days=>?) AND `+unpinnedCapture, owner, item["id"], item["hash"], item["bytes"], p.CaptureDays)
			if e != nil {
				return nil, e
			}
			affected, err = res.RowsAffected()
			counts.Captures++
		case "snapshot":
			if _, err = s.QueryString("SELECT id FROM v22_data_snapshots WHERE owner_id=? AND id=? FOR UPDATE", owner, item["id"]); err != nil {
				return nil, err
			}
			res, e := s.Exec(`DELETE FROM v22_data_snapshots p WHERE p.owner_id=? AND p.id=? AND p.query_hash=? AND p.expires_at<=clock_timestamp() AND NOT EXISTS(SELECT 1 FROM v22_exports e WHERE e.snapshot_id=p.id)`, owner, item["id"], item["hash"])
			if e != nil {
				return nil, e
			}
			affected, err = res.RowsAffected()
			counts.Snapshots++
		case "export":
			res, e := s.Exec("UPDATE v22_exports SET file=NULL,status='expired' WHERE owner_id=? AND id=? AND file_hash=? AND octet_length(file)::text=? AND expires_at<=clock_timestamp() AND status NOT IN ('queued','running')", owner, item["id"], item["hash"], item["bytes"])
			if e != nil {
				return nil, e
			}
			affected, err = res.RowsAffected()
			counts.Exports++
		default:
			return nil, ErrInvalid
		}
		if err != nil {
			return nil, err
		}
		if affected != 1 {
			return nil, ErrConflict
		}
	}
	result.Applied = true
	raw, _ := json.Marshal(result)
	if _, err = s.Exec("UPDATE v22_cleanup_checks SET result=?::jsonb WHERE id=?", string(raw), id); err != nil {
		return nil, err
	}
	if err = Record(s, owner, "retention.cleanup", id, counts); err != nil {
		return nil, err
	}
	return result, s.Commit()
}
