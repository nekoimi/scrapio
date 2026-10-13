package bootstrap

import (
	"context"

	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/co"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/crawler"
	"github.com/nekoimi/scrapio/internal/crawler/providers/javdb"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/exportworker"
	"github.com/nekoimi/scrapio/internal/job"
	"github.com/nekoimi/scrapio/internal/plugin"
	"github.com/nekoimi/scrapio/internal/plugin/delivery"
	"github.com/nekoimi/scrapio/internal/qualityworker"
	"github.com/nekoimi/scrapio/internal/regressionworker"
	"github.com/nekoimi/scrapio/internal/repo/v22_credential_repo"
	"github.com/nekoimi/scrapio/internal/runworker"
	"github.com/nekoimi/scrapio/internal/scheduleworker"
	"github.com/nekoimi/scrapio/internal/server"
	"github.com/nekoimi/scrapio/internal/trialworker"
	workflowexec "github.com/nekoimi/scrapio/internal/workflow"
)

func BeanLifecycle() *bean.LifecycleManager {
	ctx := bean.ContextWithDefaultRegistry(context.Background())
	capture.ResolveManaged = v22_credential_repo.HTTP
	// 加载配置
	bean.MustRegisterPtr[config.Config](ctx, config.Load())
	// 数据库
	bean.MustRegister[bean.Lifecycle](ctx, db.NewDBLifecycle())
	// 定时任务
	bean.MustRegister[job.CronScheduler](ctx, job.NewCronScheduler())
	// drission_rod
	bean.MustRegisterPtr[drission_rod.DrissionRod](ctx, drission_rod.NewDrissionRod())
	registerControlWorkers(ctx)
	pluginRegistry := plugin.NewRegistry()
	delivery.RegisterBuiltins(pluginRegistry, bean.PtrFromContext[config.Config](ctx))
	bean.MustRegisterPtr[plugin.Registry](ctx, pluginRegistry)
	bean.MustRegisterPtr[plugin.Worker](ctx, plugin.NewWorker(pluginRegistry))
	// 任务管理器
	crawlerManager := crawler.NewCrawlerManager(ctx)
	crawlerManager.Register(javdb.NewJavDBCrawler())
	//crawlerManager.Register(sehuatang.NewSeHuaTangCrawler())
	bean.MustRegisterPtr[crawler.Manager](ctx, crawlerManager)
	// 任务处理引擎
	bean.MustRegisterPtr[crawler.Engine](ctx, crawler.NewCrawlerEngine())
	// http服务
	bean.MustRegisterPtr[server.Server](ctx, server.NewHttpServer())
	return bean.LifecycleFromContext(ctx)
}

func registerControlWorkers(ctx context.Context) {
	bean.MustRegisterPtr[co.Worker](ctx, co.NewWorker())
	// Registry keys are types. Each worker needs its own concrete key; registering
	// them all as bean.Lifecycle replaces the lookup and only starts the first one.
	bean.MustRegisterPtr[job.WorkflowScheduler](ctx, job.NewWorkflowScheduler())
	bean.MustRegisterPtr[job.RecordExportWorker](ctx, job.NewRecordExportWorker())
	bean.MustRegisterPtr[job.DocumentRetentionWorker](ctx, job.NewDocumentRetentionWorker())
	bean.MustRegisterPtr[trialworker.Worker](ctx, trialworker.New())
	bean.MustRegisterPtr[runworker.Worker](ctx, runworker.New())
	bean.MustRegisterPtr[scheduleworker.Worker](ctx, scheduleworker.New())
	bean.MustRegisterPtr[exportworker.Worker](ctx, exportworker.New())
	bean.MustRegisterPtr[regressionworker.Worker](ctx, regressionworker.New())
	bean.MustRegisterPtr[qualityworker.Worker](ctx, qualityworker.New())
	// 通用声明式工作流执行器与旧 provider worker 分离，按 task_type=workflow 领取任务。
	bean.MustRegisterPtr[workflowexec.Worker](ctx, workflowexec.NewWorker())
}
