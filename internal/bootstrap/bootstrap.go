package bootstrap

import (
	"context"

	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/crawler"
	"github.com/nekoimi/scrapio/internal/crawler/providers/javdb"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/job"
	"github.com/nekoimi/scrapio/internal/plugin"
	"github.com/nekoimi/scrapio/internal/plugin/delivery"
	"github.com/nekoimi/scrapio/internal/server"
	workflowexec "github.com/nekoimi/scrapio/internal/workflow"
)

func BeanLifecycle() *bean.LifecycleManager {
	ctx := bean.ContextWithDefaultRegistry(context.Background())
	// 加载配置
	bean.MustRegisterPtr[config.Config](ctx, config.Load())
	// 数据库
	bean.MustRegister[bean.Lifecycle](ctx, db.NewDBLifecycle())
	// 定时任务
	bean.MustRegister[job.CronScheduler](ctx, job.NewCronScheduler())
	bean.MustRegister[bean.Lifecycle](ctx, job.NewWorkflowScheduler())
	bean.MustRegister[bean.Lifecycle](ctx, job.NewRecordExportWorker())
	bean.MustRegister[bean.Lifecycle](ctx, job.NewDocumentRetentionWorker())
	// drission_rod
	bean.MustRegisterPtr[drission_rod.DrissionRod](ctx, drission_rod.NewDrissionRod())
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
	// 通用声明式工作流执行器与旧 provider worker 分离，按 task_type=workflow 领取任务。
	bean.MustRegister[bean.Lifecycle](ctx, workflowexec.NewWorker())
	// http服务
	bean.MustRegisterPtr[server.Server](ctx, server.NewHttpServer())
	return bean.LifecycleFromContext(ctx)
}
