package v3

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/repo/v22_run_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_version_repo"
	runmodel "github.com/nekoimi/scrapio/internal/run"
)

func RetryRun(browser *drission_rod.DrissionRod, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parent, has := loadRun(w, r)
		if !has {
			return
		}
		var input runmodel.RetryInput
		if parseSampleBody(w, r, &input) != nil || input.Validate() != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", "需确认按原版本、原范围和原预算完整重跑及正式写入", false, "run", "")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len([]rune(key)) > 128 {
			runError(w, r, v22_run_repo.ErrInvalid)
			return
		}
		// Recover a known request before live capability/schema checks, even if those changed.
		prior, err := v22_run_repo.Get(ctx, parent.OwnerId, "", key)
		if err == nil {
			if prior.RetryOf == nil || *prior.RetryOf != parent.Id || prior.RetryScope != input.Scope {
				runError(w, r, v22_run_repo.ErrConflict)
				return
			}
			writeJSON(w, 202, map[string]any{"data": runDTO(prior), "request_id": w.Header().Get("X-Request-ID")})
			return
		}
		if !errors.Is(err, v22_run_repo.ErrNotFound) {
			runError(w, r, err)
			return
		}
		if !runmodel.RunControls(parent.Status, parent.CancelRequested).Retry {
			runError(w, r, v22_run_repo.ErrConflict)
			return
		}
		version, err := v22_version_repo.Get(parent.OwnerId, parent.VersionId, "")
		if err != nil {
			runError(w, r, err)
			return
		}
		plan, _, err := v22_run_repo.VersionPlan(version)
		if err != nil {
			runError(w, r, err)
			return
		}
		caps := definitionCapabilities(ctx, parent.OwnerId, version.EntryType, plan.URL, version.Definition, browser, cfg)
		row, err := v22_run_repo.Retry(ctx, parent.OwnerId, parent.Id, key, input, caps)
		if err != nil {
			runError(w, r, err)
			return
		}
		w.Header().Set("Location", "/api/v3/runs/"+row.Id)
		writeJSON(w, 202, map[string]any{"data": runDTO(row), "request_id": w.Header().Get("X-Request-ID")})
	}
}

func RunStatistics(w http.ResponseWriter, r *http.Request) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	collector := int64(0)
	if raw := r.URL.Query().Get("collector_id"); raw != "" {
		var err error
		collector, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || collector < 1 {
			runError(w, r, v22_run_repo.ErrInvalid)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	counts, err := v22_run_repo.Statistics(ctx, admin.Id, collector)
	if err != nil {
		runError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"counts": counts, "scope": "retained_history"})
}
