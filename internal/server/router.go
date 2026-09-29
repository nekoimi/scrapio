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

	// 需要认证的接口
	apiRoute := r.PathPrefix("/api").Subrouter()
	apiRoute.Use(middleware.AuthMiddleware)
	{
		// 登出
		apiRoute.HandleFunc("/auth/logout", auth.Logout)
		v3Api := apiRoute.PathPrefix("/v3").Subrouter()
		v3Api.HandleFunc("/me", v3.Me).Methods("GET")
		v3Api.HandleFunc("/capabilities", v3.Capabilities).Methods("GET")
		v3Api.HandleFunc("/home", v3.Home).Methods("GET")

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
