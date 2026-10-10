package server

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/ai"
	"github.com/nekoimi/scrapio/internal/api/audit"
	"github.com/nekoimi/scrapio/internal/api/auth"
	"github.com/nekoimi/scrapio/internal/api/crawler"
	"github.com/nekoimi/scrapio/internal/api/dashboard"
	"github.com/nekoimi/scrapio/internal/api/datasets"
	"github.com/nekoimi/scrapio/internal/api/documents"
	"github.com/nekoimi/scrapio/internal/api/magnets"
	"github.com/nekoimi/scrapio/internal/api/middleware"
	"github.com/nekoimi/scrapio/internal/api/ops"
	"github.com/nekoimi/scrapio/internal/api/plugins"
	"github.com/nekoimi/scrapio/internal/api/records"
	"github.com/nekoimi/scrapio/internal/api/resources"
	"github.com/nekoimi/scrapio/internal/api/runs"
	"github.com/nekoimi/scrapio/internal/api/settings"
	"github.com/nekoimi/scrapio/internal/api/sources"
	"github.com/nekoimi/scrapio/internal/api/ui"
	"github.com/nekoimi/scrapio/internal/api/user"
	"github.com/nekoimi/scrapio/internal/api/v3"
	"github.com/nekoimi/scrapio/internal/api/workflows"
	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/config"
	crawlercore "github.com/nekoimi/scrapio/internal/crawler"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/job"
	pluginruntime "github.com/nekoimi/scrapio/internal/plugin"
	log "github.com/sirupsen/logrus"
)

const uiDir = "/workspace/ui"

func newRouter(ctx context.Context, cfg *config.Config) *mux.Router {
	r := mux.NewRouter()
	cronScheduler := bean.FromContext[job.CronScheduler](ctx)
	crawlerEngine := bean.PtrFromContext[crawlercore.Engine](ctx)
	crawlerManager := bean.PtrFromContext[crawlercore.Manager](ctx)
	pluginRegistry := bean.PtrFromContext[pluginruntime.Registry](ctx)
	pluginWorker := bean.PtrFromContext[pluginruntime.Worker](ctx)
	browserService := bean.PtrFromContext[drission_rod.DrissionRod](ctx)

	r.Use(middleware.CORSMiddleware)
	r.Use(mux.CORSMethodMiddleware(r))
	r.Use(middleware.RequestIDMiddleware)
	r.Use(middleware.MetricsMiddleware)
	r.Use(middleware.LoggingMiddleware)

	r.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}).Methods("GET")

	// 无需认证的接口必须在受保护的 /api 子路由之前注册。
	r.HandleFunc("/api/auth/login", auth.Login)
	r.HandleFunc("/api/v3/collectors/{collector_id}/api-runs", v3.APIRun).Methods("POST")

	// 需要认证的接口
	apiRoute := r.PathPrefix("/api").Subrouter()
	apiRoute.Use(middleware.AuthMiddleware)
	{
		// 登出
		apiRoute.HandleFunc("/auth/logout", auth.Logout)
		v3Api := apiRoute.PathPrefix("/v3").Subrouter()
		v3Api.HandleFunc("/me", v3.Me).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/quality", v3.CollectorQuality).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/quality-policy", v3.QualityPolicy).Methods("GET", "PUT")
		v3Api.HandleFunc("/issues", v3.QualityIssues).Methods("GET")
		v3Api.HandleFunc("/issues/{issue_id}", v3.QualityIssue).Methods("GET")
		v3Api.HandleFunc("/issues/{issue_id}/resolve", v3.QualityIssue).Methods("POST")
		v3Api.HandleFunc("/credentials",v3.Credentials).Methods("GET","POST")
 v3Api.HandleFunc("/credentials/{credential_id}",v3.CredentialItem).Methods("GET","PATCH","DELETE")
 v3Api.HandleFunc("/credentials/{credential_id}/checks",v3.CredentialCheck(browserService)).Methods("POST")
 v3Api.HandleFunc("/capabilities", v3.CapabilitiesWithBrowser(browserService)).Methods("GET")
		v3Api.HandleFunc("/home", v3.Home).Methods("GET")
		v3Api.HandleFunc("/collectors/health", v3.CollectorHealthList).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/health", v3.CollectorHealth).Methods("GET")
		v3Api.HandleFunc("/runs/{run_id}/repair-context", v3.PrepareRepair).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/repair-drafts", v3.CreateRepairDraft).Methods("POST")
		v3Api.HandleFunc("/repair-drafts/by-key", v3.GetRepairDraft).Methods("GET")
		v3Api.HandleFunc("/repair-drafts/{repair_id}", v3.GetRepairDraft).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/regressions", v3.CreateRegression("regression")).Methods("POST")
		v3Api.HandleFunc("/collectors/{collector_id}/regressions", v3.ListRegressions).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/version-comparisons", v3.CreateRegression("comparison")).Methods("POST")
		v3Api.HandleFunc("/regressions/by-key", v3.GetRegression).Methods("GET")
		v3Api.HandleFunc("/regressions/{job_id}", v3.GetRegression).Methods("GET")
		v3Api.HandleFunc("/version-comparisons/{job_id}", v3.GetRegression).Methods("GET")
		v3Api.HandleFunc("/regressions/{job_id}/cancel", v3.MutateRegression).Methods("POST")
		v3Api.HandleFunc("/regressions/{job_id}", v3.MutateRegression).Methods("DELETE")
		v3Api.HandleFunc("/collectors/{collector_id}/version-restores", v3.RestoreVersion).Methods("POST")
		v3Api.HandleFunc("/version-restores/by-key", v3.GetVersionRestore).Methods("GET")
		v3Api.HandleFunc("/captures", v3.CreateCapture(cfg)).Methods("POST")
		v3Api.HandleFunc("/captures/by-key", v3.GetCaptureByKey).Methods("GET")
		v3Api.HandleFunc("/captures/{capture_id}", v3.GetCapture).Methods("GET")
		v3Api.HandleFunc("/captures/{capture_id}/json-checks", v3.CheckCaptureJSON).Methods("POST")
		v3Api.HandleFunc("/collectors", v3.CreateCollector).Methods("POST")
		v3Api.HandleFunc("/collectors/{collector_id}/previews", v3.PreviewCollector).Methods("POST")
		v3Api.HandleFunc("/tables", v3.CreateDataTable).Methods("POST")
		v3Api.HandleFunc("/tables", v3.ListDataTables).Methods("GET")
		v3Api.HandleFunc("/tables/{table_id}", v3.GetDataTable).Methods("GET")
		v3Api.HandleFunc("/tables/{table_id}/schema", v3.GetDataTable).Methods("GET")
		v3Api.HandleFunc("/tables/{table_id}/records", v3.TableRecords).Methods("GET")
		v3Api.HandleFunc("/tables/{table_id}/views", v3.DataViews).Methods("GET")
		v3Api.HandleFunc("/tables/{table_id}/views", v3.SaveDataView).Methods("POST")
		v3Api.HandleFunc("/views/{view_id}", v3.UpdateDataView).Methods("PATCH")
		v3Api.HandleFunc("/views/{view_id}", v3.DeleteDataView).Methods("DELETE")
		v3Api.HandleFunc("/tables/{table_id}/exports", v3.CreateDataExport).Methods("POST")
		v3Api.HandleFunc("/tables/{table_id}/exports", v3.ListDataExports).Methods("GET")
		v3Api.HandleFunc("/exports/by-key", v3.GetDataExport).Methods("GET")
		v3Api.HandleFunc("/exports/{export_id}", v3.GetDataExport).Methods("GET")
		v3Api.HandleFunc("/exports/{export_id}/cancel", v3.CancelDataExport).Methods("POST")
		v3Api.HandleFunc("/exports/{export_id}/download", v3.DownloadDataExport).Methods("GET")
		v3Api.HandleFunc("/tables/{table_id}/statistics", v3.TableStatistics).Methods("GET")
		v3Api.HandleFunc("/records/{record_id}", v3.RecordDetail).Methods("GET")
		v3Api.HandleFunc("/records/{record_id}/observations", v3.RecordObservations).Methods("GET")
		v3Api.HandleFunc("/records/{record_id}/revisions", v3.RecordRevisions).Methods("GET")
		v3Api.HandleFunc("/runs/{run_id}/pages", v3.RunPages).Methods("GET")
		v3Api.HandleFunc("/runs/{run_id}/checkpoints", v3.RunCheckpoints).Methods("GET")
		v3Api.HandleFunc("/runs/{run_id}/attempts", v3.RunAttempts).Methods("GET")
		v3Api.HandleFunc("/pages/{page_id}", v3.PageDetail).Methods("GET")
		v3Api.HandleFunc("/pages/{page_id}/attempts", v3.PageAttempts).Methods("GET")
		v3Api.HandleFunc("/documents/{document_id}", v3.DocumentAsset).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/output-checks", v3.CreateOutputCheck).Methods("POST")
		v3Api.HandleFunc("/collectors/{collector_id}/output-checks/by-key", v3.GetOutputCheck).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/output-checks/{check_id}", v3.GetOutputCheck).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/output", v3.ConfirmOutput).Methods("PUT")
		v3Api.HandleFunc("/collectors/{collector_id}/trials", v3.CreateTrial).Methods("POST")
		v3Api.HandleFunc("/collectors/{collector_id}/trials", v3.ListTrials).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/publish-checks", v3.CreatePublishCheck(browserService, cfg)).Methods("POST")
		v3Api.HandleFunc("/collectors/{collector_id}/publish-checks/by-key", v3.GetPublishCheck).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/publish-checks/{check_id}", v3.GetPublishCheck).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/versions", v3.PublishVersion(browserService, cfg)).Methods("POST")
		v3Api.HandleFunc("/collectors/{collector_id}/versions", v3.ListVersions).Methods("GET")
		v3Api.HandleFunc("/versions/by-key", v3.GetVersion).Methods("GET")
		v3Api.HandleFunc("/versions/{version_id}", v3.GetVersion).Methods("GET")
		v3Api.HandleFunc("/versions/{version_id}/evidence", v3.VersionEvidence).Methods("GET")
		v3Api.HandleFunc("/trials/by-key", v3.GetTrial).Methods("GET")
		v3Api.HandleFunc("/trials/{trial_id}", v3.GetTrial).Methods("GET")
		v3Api.HandleFunc("/trials/{trial_id}", v3.DeleteTrial).Methods("DELETE")
		v3Api.HandleFunc("/trials/{trial_id}/results", v3.TrialResults).Methods("GET")
		v3Api.HandleFunc("/trials/{trial_id}/events", v3.TrialEvents).Methods("GET")
		v3Api.HandleFunc("/trials/{trial_id}/cancel", v3.CancelTrial).Methods("POST")
		v3Api.HandleFunc("/collectors/{collector_id}/runs", v3.CreateRun(browserService, cfg)).Methods("POST")
		v3Api.HandleFunc("/collectors/{collector_id}/schedule", v3.GetSchedule).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/schedule", v3.SaveSchedule).Methods("PUT")
		v3Api.HandleFunc("/collectors/{collector_id}/schedule/events", v3.ScheduleEvents).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/api-keys", v3.ListAPIKeys).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/api-keys", v3.CreateAPIKey).Methods("POST")
		v3Api.HandleFunc("/collectors/{collector_id}/api-keys/{key_id}", v3.RevokeAPIKey).Methods("DELETE")
		v3Api.HandleFunc("/runs/statistics", v3.RunStatistics).Methods("GET")
		v3Api.HandleFunc("/runs", v3.ListRuns).Methods("GET")
		v3Api.HandleFunc("/runs/by-key", v3.GetRun).Methods("GET")
		v3Api.HandleFunc("/runs/{run_id}", v3.GetRun).Methods("GET")
		v3Api.HandleFunc("/runs/{run_id}/results", v3.RunResults).Methods("GET")
		v3Api.HandleFunc("/runs/{run_id}/events", v3.RunEvents).Methods("GET")
		v3Api.HandleFunc("/runs/{run_id}/cancel", v3.CancelRun).Methods("POST")
		v3Api.HandleFunc("/runs/{run_id}/retries", v3.RetryRun(browserService, cfg)).Methods("POST")
		v3Api.HandleFunc("/runs/{run_id}/diagnostics", v3.RunDiagnostics).Methods("GET")
		v3Api.HandleFunc("/runs/{run_id}/coverage", v3.RunCoverage).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/samples", v3.CreateSample(browserService)).Methods("POST")
		v3Api.HandleFunc("/collectors/{collector_id}/samples", v3.ListSamples).Methods("GET")
		v3Api.HandleFunc("/samples/by-key", v3.GetSampleByKey).Methods("GET")
		v3Api.HandleFunc("/samples/{sample_id}", v3.GetSample).Methods("GET")
		v3Api.HandleFunc("/samples/{sample_id}", v3.UpdateSample).Methods("PATCH")
		v3Api.HandleFunc("/samples/{sample_id}", v3.DeleteSample).Methods("DELETE")
		v3Api.HandleFunc("/samples/{sample_id}/screenshot", v3.GetSampleScreenshot).Methods("GET")
		v3Api.HandleFunc("/samples/{sample_id}/checks", v3.CheckSample).Methods("POST")
		v3Api.HandleFunc("/samples/{sample_id}/checks", v3.ListSampleChecks).Methods("GET")
		v3Api.HandleFunc("/samples/{sample_id}/checks/{check_id}", v3.GetSampleCheck).Methods("GET")
		v3Api.HandleFunc("/browser-sessions/{session_id}/captures", v3.CaptureBrowserPage(browserService)).Methods("POST")
		v3Api.HandleFunc("/collectors", v3.ListCollectors).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/copies", v3.CopyCollector).Methods("POST")
		v3Api.HandleFunc("/collectors/{collector_id}/draft", v3.GetCollectorDraft).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/draft", v3.UpdateCollectorDraft).Methods("PUT")
		v3Api.HandleFunc("/collectors/{collector_id}/validate", v3.ValidateCollectorDraft).Methods("POST")
		v3Api.HandleFunc("/browser-sessions", v3.CreateBrowserSession(browserService)).Methods("POST")
		v3Api.HandleFunc("/browser-sessions/{session_id}", v3.GetBrowserSession(browserService)).Methods("GET")
		v3Api.HandleFunc("/browser-sessions/{session_id}", v3.CloseBrowserSession(browserService)).Methods("DELETE")
		v3Api.HandleFunc("/browser-sessions/{session_id}/heartbeat", v3.HeartbeatBrowserSession(browserService)).Methods("POST")
		v3Api.HandleFunc("/browser-sessions/{session_id}/resume", v3.ResumeBrowserSession(browserService)).Methods("POST")
		v3Api.HandleFunc("/browser-sessions/{session_id}/events", v3.BrowserSessionEvents(browserService)).Methods("GET")
		v3Api.HandleFunc("/browser-sessions/{session_id}/frame", v3.GetBrowserSessionFrame(browserService)).Methods("GET")
		v3Api.HandleFunc("/browser-sessions/{session_id}/inspect", v3.InspectBrowserPage(browserService, "inspect")).Methods("POST")
		v3Api.HandleFunc("/browser-sessions/{session_id}/dom", v3.InspectBrowserPage(browserService, "dom")).Methods("GET")
		v3Api.HandleFunc("/browser-sessions/{session_id}/locator-checks", v3.InspectBrowserPage(browserService, "locator-check")).Methods("POST")
		v3Api.HandleFunc("/browser-sessions/{session_id}/record-previews", v3.InspectBrowserPage(browserService, "record-preview")).Methods("POST")
		v3Api.HandleFunc("/browser-sessions/{session_id}/commands", v3.CreateBrowserCommand(browserService)).Methods("POST")
		v3Api.HandleFunc("/browser-sessions/{session_id}/commands", v3.ListBrowserCommands).Methods("GET")
		v3Api.HandleFunc("/browser-sessions/{session_id}/commands/{command_id}", v3.GetBrowserCommand(browserService)).Methods("GET")
		v3Api.HandleFunc("/collectors/{collector_id}/checkpoints", v3.CollectorCheckpoints).Methods("GET", "POST")
		v3Api.HandleFunc("/collectors/{collector_id}/checkpoints/{checkpoint_id}", v3.CollectorCheckpoints).Methods("DELETE")

		v3Api.HandleFunc("/collectors/{collector_id}/archive", v3.ArchiveCollector).Methods("POST")

		v1Api := apiRoute.PathPrefix("/v1").Subrouter()
		{
			v1Api.HandleFunc("/dashboard/summary", dashboard.Summary).Methods("GET")
			v1Api.HandleFunc("/settings", settings.List(cfg)).Methods("GET")
			v1Api.HandleFunc("/settings/testDrissionRod", settings.TestDrissionRod(cfg)).Methods("POST")
			v1Api.HandleFunc("/ops/health", ops.Health(cfg)).Methods("GET")
			v1Api.HandleFunc("/ops/jobs", ops.Jobs(cronScheduler)).Methods("GET")
			v1Api.HandleFunc("/ops/version", ops.Version).Methods("GET")

			// 获取当前用户信息
			v1Api.HandleFunc("/me", user.Me)
			// 修改当前用户密码
			v1Api.HandleFunc("/me/changePwd", user.ChangePassword)
			v1Api.HandleFunc("/crawler/submit/javdb", crawlerapi.SubmitJavDB).Methods("POST")
			v1Api.HandleFunc("/crawler/submit/javdbPage", crawlerapi.SubmitJavDBPage).Methods("POST")
			v1Api.HandleFunc("/crawler/status", crawlerapi.Status(crawlerEngine)).Methods("GET")
			v1Api.HandleFunc("/crawler/providers", crawlerapi.Providers(crawlerManager)).Methods("GET")
			v1Api.HandleFunc("/crawler/run", crawlerapi.Run(crawlerManager)).Methods("POST")
			// 磁力链接管理
			v1Api.HandleFunc("/magnets/list", magnets.List).Methods("GET", "POST")
			v1Api.HandleFunc("/magnets/statusOptions", magnets.StatusOptions).Methods("GET")
			v1Api.HandleFunc("/magnets/sourceOptions", magnets.SourceOptions).Methods("GET")
			v1Api.HandleFunc("/magnets/detail", magnets.Detail(cfg)).Methods("GET")
			v1Api.HandleFunc("/magnets/create", magnets.Create).Methods("POST")
			v1Api.HandleFunc("/magnets/update", magnets.Update).Methods("POST")
			v1Api.HandleFunc("/magnets/delete", magnets.Delete).Methods("POST")
			v1Api.HandleFunc("/magnets/markStatus", magnets.MarkStatus).Methods("POST")
		}

		v2Api := apiRoute.PathPrefix("/v2").Subrouter()
		{
			v2Api.HandleFunc("/projects/list", datasets.ProjectList).Methods("GET")
			v2Api.HandleFunc("/projects/health", datasets.ProjectHealth).Methods("GET")
			v2Api.HandleFunc("/workflows/quality", datasets.WorkflowQuality).Methods("GET")
			v2Api.HandleFunc("/workflows/quality/save", datasets.SaveWorkflowQuality).Methods("POST")
			v2Api.HandleFunc("/projects/create", datasets.ProjectCreate).Methods("POST")
			v2Api.HandleFunc("/projects/update", datasets.ProjectUpdate).Methods("POST")
			v2Api.HandleFunc("/datasets/list", datasets.List).Methods("GET")
			v2Api.HandleFunc("/datasets/detail", datasets.Detail).Methods("GET")
			v2Api.HandleFunc("/datasets/create", datasets.Create).Methods("POST")
			v2Api.HandleFunc("/datasets/schema/update", datasets.UpdateSchema).Methods("POST")
			v2Api.HandleFunc("/records/list", records.List).Methods("GET")
			v2Api.HandleFunc("/records/coverage", records.Coverage).Methods("GET")
			v2Api.HandleFunc("/records/views", records.Views).Methods("GET")
			v2Api.HandleFunc("/records/views/save", records.SaveView).Methods("POST")
			v2Api.HandleFunc("/records/views/delete", records.DeleteView).Methods("POST")
			v2Api.HandleFunc("/records/export/direct", records.ExportDirect).Methods("POST")
			v2Api.HandleFunc("/records/export/create", records.ExportCreate).Methods("POST")
			v2Api.HandleFunc("/records/export/status", records.ExportStatus).Methods("GET")
			v2Api.HandleFunc("/records/export/download", records.ExportDownload).Methods("GET")
			v2Api.HandleFunc("/records/detail", records.Detail).Methods("GET")
			v2Api.HandleFunc("/records/create", records.Create).Methods("POST")
			v2Api.HandleFunc("/records/legacy/reconcile", records.ReconcileLegacy).Methods("POST")
			v2Api.HandleFunc("/workflows/list", workflows.List).Methods("GET", "POST")
			v2Api.HandleFunc("/workflows/templates", workflows.Templates).Methods("GET")
			v2Api.HandleFunc("/workflows/detail", workflows.Detail).Methods("GET", "POST")
			v2Api.HandleFunc("/workflows/owner/update", workflows.UpdateOwner).Methods("POST")
			v2Api.HandleFunc("/workflows/create", workflows.Create).Methods("POST")
			v2Api.HandleFunc("/workflows/versions/create", workflows.CreateVersion).Methods("POST")
			v2Api.HandleFunc("/workflows/versions/validate", workflows.Validate).Methods("POST")
			v2Api.HandleFunc("/workflows/versions/publish", workflows.Publish).Methods("POST")
			v2Api.HandleFunc("/workflows/ai/prepare", workflows.AIPrepare(cfg)).Methods("GET")
			v2Api.HandleFunc("/workflows/ai/suggest", workflows.AISuggest(cfg, ai.AssistClient{})).Methods("POST")
			v2Api.HandleFunc("/workflows/ai/list", workflows.AIList).Methods("GET")
			v2Api.HandleFunc("/workflows/ai/review", workflows.AIReview).Methods("POST")
			v2Api.HandleFunc("/workflows/samples/create", workflows.SaveSample(browserService)).Methods("POST")
			v2Api.HandleFunc("/workflows/samples/list", workflows.ListSamples).Methods("GET")
			v2Api.HandleFunc("/workflows/samples/delete", workflows.DeleteSample).Methods("POST")
			v2Api.HandleFunc("/workflows/samples/preview", workflows.PreviewSample).Methods("POST")
			v2Api.HandleFunc("/workflows/versions/check-samples", workflows.CheckSamples).Methods("POST")
			v2Api.HandleFunc("/workflows/versions/rollback", workflows.Rollback).Methods("POST")
			v2Api.HandleFunc("/workflows/versions/compare-samples", workflows.CompareSamples).Methods("POST")
			v2Api.HandleFunc("/workflows/stop", workflows.Stop).Methods("POST")
			v2Api.HandleFunc("/workflows/run", workflows.Run).Methods("POST")
			v2Api.HandleFunc("/workflows/run/api", workflows.APITrigger).Methods("POST")
			v2Api.HandleFunc("/workflows/schedule", workflows.ScheduleDetail).Methods("GET")
			v2Api.HandleFunc("/workflows/schedule/save", workflows.ScheduleSave).Methods("POST")
			v2Api.HandleFunc("/sources/list", sources.List).Methods("GET")
			v2Api.HandleFunc("/sources/create", sources.Create).Methods("POST")
			v2Api.HandleFunc("/sources/update", sources.Update).Methods("POST")
			v2Api.HandleFunc("/sources/toggle", sources.Toggle).Methods("POST")
			v2Api.HandleFunc("/documents/list", documents.List).Methods("GET")
			v2Api.HandleFunc("/documents/retention/preview", documents.RetentionPreview).Methods("GET")
			v2Api.HandleFunc("/documents/detail", documents.Detail).Methods("GET")
			v2Api.HandleFunc("/audit/list", audit.List).Methods("GET")
			v2Api.HandleFunc("/workflows/versions/diff", workflows.Diff).Methods("POST")
			v2Api.HandleFunc("/workflows/test-extract", workflows.TestExtract).Methods("POST")
			v2Api.HandleFunc("/documents/replay", workflows.Replay).Methods("POST")
			v2Api.HandleFunc("/documents/replay-diff", workflows.ReplayDiff).Methods("POST")
			v2Api.HandleFunc("/resources/list", resources.List).Methods("GET", "POST")
			v2Api.HandleFunc("/resources/detail", resources.Detail).Methods("GET")
			v2Api.HandleFunc("/resources/statusOptions", resources.StatusOptions).Methods("GET")
			v2Api.HandleFunc("/resources/sourceOptions", resources.SourceOptions).Methods("GET")
			v2Api.HandleFunc("/resources/create", resources.Create).Methods("POST")
			v2Api.HandleFunc("/resources/update", resources.Update).Methods("POST")
			v2Api.HandleFunc("/resources/delete", resources.Delete).Methods("POST")
			v2Api.HandleFunc("/resources/markStatus", resources.MarkStatus).Methods("POST")
			v2Api.HandleFunc("/runs/list", runs.List).Methods("GET", "POST")
			v2Api.HandleFunc("/runs/detail", runs.Detail).Methods("GET", "POST")
			v2Api.HandleFunc("/runs/tasks", runs.RunTasks).Methods("GET")
			v2Api.HandleFunc("/runs/cancel", runs.CancelRun).Methods("POST")
			v2Api.HandleFunc("/runs/rerun", runs.Rerun).Methods("POST")
			v2Api.HandleFunc("/tasks/list", runs.Tasks).Methods("GET", "POST")
			v2Api.HandleFunc("/tasks/attempts", runs.Attempts).Methods("GET", "POST")
			v2Api.HandleFunc("/tasks/cancel", runs.CancelTask).Methods("POST")
			v2Api.HandleFunc("/tasks/retry", runs.RetryTask).Methods("POST")
			v2Api.HandleFunc("/observability/metrics", ops.Metrics(crawlerEngine)).Methods("GET")
			v2Api.HandleFunc("/plugins/overview", plugins.Overview(pluginRegistry, pluginWorker)).Methods("GET")
			v2Api.HandleFunc("/plugins/record-capabilities", plugins.RecordCapabilities(pluginRegistry)).Methods("GET")
			v2Api.HandleFunc("/plugins/subscriptions", plugins.Subscriptions).Methods("GET")
			v2Api.HandleFunc("/plugins/subscriptions/save", plugins.SaveSubscription(pluginRegistry)).Methods("POST")
			v2Api.HandleFunc("/plugins/subscriptions/delete", plugins.DeleteSubscription).Methods("POST")
			v2Api.HandleFunc("/plugins/tasks", plugins.List).Methods("GET")
			v2Api.HandleFunc("/plugins/tasks/detail", plugins.Detail).Methods("GET")
			v2Api.HandleFunc("/plugins/tasks/cancel", plugins.Cancel).Methods("POST")
			v2Api.HandleFunc("/plugins/tasks/retry", plugins.Retry).Methods("POST")
		}
	}

	// 静态资源
	// The current Vue bundle uses hash history. Keep /app deep links usable
	// until the new application can be served with its own history fallback.
	r.HandleFunc("/app", appEntry).Methods("GET")
	r.PathPrefix("/app/").HandlerFunc(appEntry).Methods("GET")
	r.PathPrefix("/").Handler(ui.AdminUI(uiDir))

	debugRoute(r)

	return r
}

func appEntry(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/app" || path == "/app/" {
		path = "/app/home"
	}
	target := "/#" + path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func debugRoute(r *mux.Router) {
	r.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		path, _ := route.GetPathTemplate()
		log.Debugf("Route: %s", path)
		return nil
	})
}
