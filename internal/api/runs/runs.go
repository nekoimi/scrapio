package runs

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/nekoimi/scrapio/internal/api/middleware"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/pkg/error_ext"
	"github.com/nekoimi/scrapio/internal/pkg/request"
	"github.com/nekoimi/scrapio/internal/pkg/respond"
	"github.com/nekoimi/scrapio/internal/repo/audit_repo"
	"github.com/nekoimi/scrapio/internal/repo/task_repo"
)

type PageResponse[T any] struct {
	List  []T   `json:"list"`
	Total int64 `json:"total"`
}

type ListRequest struct {
	ProjectID  int64  `json:"project_id,omitempty"`
	WorkflowID int64  `json:"workflow_id,omitempty"`
	Page       int    `json:"page,omitempty"`
	Size       int    `json:"size,omitempty"`
	PageNum    int    `json:"page_num,omitempty"`
	PageSize   int    `json:"page_size,omitempty"`
	Status     string `json:"status,omitempty"`
	RunID      int64  `json:"run_id,omitempty"`
}
type IDRequest struct {
	ID int64 `json:"id"`
}

func List(w http.ResponseWriter, r *http.Request) {
	input := parseListRequest(r)
	rows, total, err := task_repo.ListRuns(task_repo.RunFilter{Status: input.Status, ProjectID: input.ProjectID, WorkflowID: input.WorkflowID, Page: page(input), Size: size(input)})
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, PageResponse[table.WorkflowRun]{List: rows, Total: total})
}

func Detail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	run := new(table.WorkflowRun)
	has, err := db.Instance().ID(id).Get(run)
	if err != nil {
		respond.Error(w, err)
		return
	}
	if !has {
		respond.Error(w, error_ext.DataNotFoundError)
		return
	}
	limits, err := task_repo.LimitEvents(id, 100)
	if err != nil {
		respond.Error(w, err)
		return
	}
	coverage, err := task_repo.RunCoverage(id)
	if err != nil {
		respond.Error(w, err)
		return
	}
	inspection, err := task_repo.InspectRun(id)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, map[string]any{"run": run, "inspection": inspection, "limit_events": limits, "coverage": coverage})
}

func RunTasks(w http.ResponseWriter, r *http.Request) {
	input := parseListRequest(r)
	if input.RunID <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	rows, total, err := task_repo.ListRunTasks(input.RunID, page(input), size(input))
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, PageResponse[table.CrawlTask]{List: rows, Total: total})
}

func Tasks(w http.ResponseWriter, r *http.Request) {
	input := parseListRequest(r)
	rows, total, err := task_repo.ListTasks(task_repo.TaskFilter{RunID: input.RunID, Status: input.Status, Page: page(input), Size: size(input)})
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, PageResponse[table.CrawlTask]{List: rows, Total: total})
}

func Attempts(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	pageNo, _ := strconv.Atoi(r.URL.Query().Get("page"))
	rows, total, err := task_repo.ListTaskAttempts(id, pageNo, 20)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, PageResponse[table.TaskAttempt]{List: rows, Total: total})
}

func CancelRun(w http.ResponseWriter, r *http.Request)  { mutateID(w, r, task_repo.CancelRun) }
func CancelTask(w http.ResponseWriter, r *http.Request) { mutateID(w, r, task_repo.CancelTask) }
func RetryTask(w http.ResponseWriter, r *http.Request)  { mutateID(w, r, task_repo.RetryTask) }

func Rerun(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil || id <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	run, err := task_repo.RerunRun(id)
	if err != nil {
		respond.Error(w, err)
		return
	}
	_ = audit_repo.Record(middleware.RequestID(r.Context()), "workflow.run_rerun", "workflow_run", &run.Id, map[string]any{"source_run_id": id})
	respond.Ok(w, run)
}

func mutateID(w http.ResponseWriter, r *http.Request, action func(int64) error) {
	id, err := parseID(r)
	if err != nil || id <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	if err := action(id); err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, nil)
}

func parseListRequest(r *http.Request) ListRequest {
	input := ListRequest{}
	if r.Method == http.MethodPost && r.Header.Get("Content-Type") != "" {
		_ = request.Parse(r, &input)
	}
	query := r.URL.Query()
	if input.Page == 0 {
		input.Page, _ = strconv.Atoi(query.Get("page"))
	}
	if input.Size == 0 {
		input.Size, _ = strconv.Atoi(query.Get("size"))
	}
	if input.PageNum == 0 {
		input.PageNum, _ = strconv.Atoi(query.Get("page_num"))
	}
	if input.PageSize == 0 {
		input.PageSize, _ = strconv.Atoi(query.Get("page_size"))
	}
	if input.Status == "" {
		input.Status = query.Get("status")
	}
	if input.RunID == 0 {
		input.RunID, _ = strconv.ParseInt(query.Get("run_id"), 10, 64)
	}
	if input.ProjectID == 0 {
		input.ProjectID, _ = strconv.ParseInt(query.Get("project_id"), 10, 64)
	}
	if input.WorkflowID == 0 {
		input.WorkflowID, _ = strconv.ParseInt(query.Get("workflow_id"), 10, 64)
	}
	return input
}

func parseID(r *http.Request) (int64, error) {
	if raw := r.URL.Query().Get("id"); raw != "" {
		return strconv.ParseInt(raw, 10, 64)
	}
	var input IDRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		return 0, err
	}
	return input.ID, nil
}
func page(v ListRequest) int {
	if v.Page > 0 {
		return v.Page
	}
	if v.PageNum > 0 {
		return v.PageNum
	}
	return 1
}
func size(v ListRequest) int {
	if v.Size > 0 {
		return v.Size
	}
	if v.PageSize > 0 {
		return v.PageSize
	}
	return 20
}
