package v3

import (
	"context"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/pkg/request"
	"github.com/nekoimi/scrapio/internal/repo/v22_capture_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_sample_repo"
	"net/http"
	"strconv"
	"time"
)

func PreviewCollector(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id, err := strconv.ParseInt(mux.Vars(r)["collector_id"], 10, 64)
	if err != nil || id < 1 {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "preview", "")
		return
	}
	var input struct {
		ExpectedRevision int    `json:"expected_revision"`
		CaptureID        string `json:"capture_id"`
		StepID           string `json:"step_id"`
		Stage            string `json:"stage"`
		SampleID         string `json:"sample_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if request.Parse(r, &input) != nil || input.ExpectedRevision < 1 || (input.CaptureID == "") == (input.SampleID == "") || len(input.StepID) > 128 {
		fail(w, r, 400, "INVALID_ARGUMENT", "需要 revision 与 capture_id/sample_id 二选一", false, "preview", "")
		return
	}
	var sampleRevision int
	if input.SampleID != "" {
		row, sampleErr := v22_sample_repo.Get(admin.Id, input.SampleID)
		if sampleErr != nil {
			sampleError(w, r, sampleErr)
			return
		}
		if row.CollectorId != id {
			sampleError(w, r, v22_sample_repo.ErrNotFound)
			return
		}
		if input.StepID != "" && input.StepID != row.StepId || input.Stage != "" && input.Stage != row.Stage {
			fail(w, r, 400, "INVALID_ARGUMENT", "样例步骤与角色不可替换", false, "preview", "")
			return
		}
		input.CaptureID, input.StepID, input.Stage = row.CaptureId, row.StepId, row.Stage
		sampleRevision = row.Revision
	}
	if input.StepID == "" {
		fail(w, r, 400, "INVALID_ARGUMENT", "需要 step_id", false, "preview", "")
		return
	}
	if input.Stage == "" {
		input.Stage = "list"
	}
	if input.Stage != "list" && input.Stage != "detail" {
		fail(w, r, 400, "INVALID_ARGUMENT", "stage 必须是 list/detail", false, "preview", "")
		return
	}
	if _, err = uuid.Parse(input.CaptureID); err != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "capture_id 无效", false, "preview", "")
		return
	}
	collector, has, err := v22_collector_repo.Get(admin.Id, id)
	if err != nil {
		captureError(w, r, err)
		return
	}
	if !has || collector.Status == "archived" {
		captureError(w, r, v22_capture_repo.ErrNotFound)
		return
	}
	if collector.Revision != input.ExpectedRevision {
		conflict(w, r, v22_collector_repo.ToDTO(collector))
		return
	}
	snapshot, err := v22_capture_repo.Get(admin.Id, input.CaptureID)
	if err != nil {
		captureError(w, r, err)
		return
	}
	if snapshot.CollectorId != id {
		captureError(w, r, v22_capture_repo.ErrNotFound)
		return
	}
	if snapshot.Status != "succeeded" {
		fail(w, r, 409, "CAPTURE_UNAVAILABLE", "需要成功保存的输入快照", false, "preview", "")
		return
	}
	if snapshot.ContentHash != capture.Hash([]byte(snapshot.Content)) {
		fail(w, r, 409, "CAPTURE_CORRUPT", "快照内容与保存哈希不匹配", false, "preview", "")
		return
	}
	plan, err := extraction.PlanFromDefinition([]byte(collector.Definition), input.StepID)
	if err != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "preview", "config")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	result, err := extraction.Extract(ctx, extraction.Input{Content: snapshot.Content, Format: snapshot.Format, URL: snapshot.FinalURL, BaseURL: snapshot.BaseURL, Stage: input.Stage}, plan)
	if err != nil {
		fail(w, r, 400, "EXTRACTION_FAILED", err.Error(), false, "preview", input.StepID)
		return
	}
	ok(w, r, map[string]any{"collector_id": strconv.FormatInt(id, 10), "revision": collector.Revision, "capture_id": snapshot.Id, "capture_revision": snapshot.DraftRevision, "content_hash": snapshot.ContentHash, "definition_hash": capture.Hash([]byte(collector.Definition)), "source_url": snapshot.FinalURL, "page_state_id": snapshot.PageStateId, "sample_id": input.SampleID, "sample_revision": sampleRevision, "result": result})
}
