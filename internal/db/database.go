package db

import (
	"context"
	"fmt"
	"sync"

	_ "github.com/lib/pq"
	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/db/migrate"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/pkg/util"
	log "github.com/sirupsen/logrus"
	"xorm.io/xorm"
)

var (
	engine     *xorm.Engine
	engineOnce sync.Once
	initErr    error
)

func NewDBLifecycle() bean.Lifecycle {
	return bean.NewLifecycle("DB", func(ctx context.Context) error {
		cfg := bean.PtrFromContext[config.Config](ctx)
		// 初始化数据库
		return initialize(cfg.DB)
	}, func(ctx context.Context) error {
		if engine == nil {
			return nil
		}
		return engine.Close()
	})
}

// 初始化数据库操作
func initialize(cfg *config.DBConfig) error {
	engineOnce.Do(func() {
		log.Debugf("连接数据库")
		var candidate *xorm.Engine
		candidate, initErr = xorm.NewEngine(Postgres.String(), cfg.Dsn)
		if initErr != nil {
			return
		}
		defer func() {
			if initErr != nil {
				_ = candidate.Close()
			}
		}()

		// 初始化设置
		candidate.SetLogger(newXormLogger())
		candidate.ShowSQL(cfg.ShowSQL)
		// 连接池设置
		candidate.SetMaxIdleConns(8)
		// Result writers hold a task-row lock while using another connection.
		candidate.SetMaxOpenConns(24)

		if initErr = candidate.Ping(); initErr != nil {
			return
		}

		if initErr = migrateWithLock(candidate); initErr != nil {
			return
		}
		engine = candidate
	})
	return initErr
}

// Serialize migrations across control-plane instances sharing a database.
func migrateWithLock(e *xorm.Engine) error {
	s := e.NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	if _, err := s.QueryString("SELECT pg_advisory_xact_lock(20260925, 1)"); err != nil {
		_ = s.Rollback()
		return fmt.Errorf("获取数据库迁移锁: %w", err)
	}
	if err := initMigrates(e); err != nil {
		_ = s.Rollback()
		return err
	}
	if err := runMigrates(e); err != nil {
		_ = s.Rollback()
		return err
	}
	return s.Commit()
}

// Instance 获取数据库操作实例
func Instance() *xorm.Engine {
	return engine
}

// 初始化数据迁移
func initMigrates(e *xorm.Engine) error {
	mg := new(table.Migrates)
	if exist, err := e.IsTableExist(mg); err != nil {
		log.Errorf("数据表检查失败: %s", err.Error())
		return err
	} else if !exist {
		err := e.CreateTables(mg)
		if err != nil {
			log.Errorf("数据表初始化失败: %s", err.Error())
			return err
		}
	}
	// 修复 success 列类型：旧版 xorm 可能将 bool 映射为 smallint
	if _, err := e.Exec(`DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'migrates' AND column_name = 'success' AND data_type = 'smallint'
  ) THEN
    ALTER TABLE migrates ALTER COLUMN success TYPE boolean USING (success != 0);
  END IF;
END $$`); err != nil {
		return fmt.Errorf("修复 migrates 表 success 列类型: %w", err)
	}
	return nil
}

// 初始化数据表迁移
func runMigrates(e *xorm.Engine) error {
	migrates := migrate.GetAll()
	util.Sort[migrate.Migrate](migrates, func(a migrate.Migrate, b migrate.Migrate) bool {
		return a.Version() < b.Version()
	})
	log.Debugln("数据表迁移执行...")
	for _, m := range migrates {
		var record table.Migrates
		if exists, err := e.Where("version = ?", m.Version()).Get(&record); err != nil {
			return fmt.Errorf("检查迁移 %d: %w", m.Version(), err)
		} else if exists && record.Success {
			continue
		}

		log.Infof("数据表迁移: %d, %s , 执行...", m.Version(), m.Desc())
		migrationErr := m.Exec(e)
		if migrationErr != nil {
			if record.Id > 0 {
				_, _ = e.ID(record.Id).Cols("success", "message").Update(&table.Migrates{Success: false, Message: migrationErr.Error()})
			} else {
				_, _ = e.InsertOne(&table.Migrates{Version: m.Version(), Success: false, Message: migrationErr.Error()})
			}
			return fmt.Errorf("执行迁移 %d (%s): %w", m.Version(), m.Desc(), migrationErr)
		}
		var recordErr error
		if record.Id > 0 {
			_, recordErr = e.ID(record.Id).Cols("success", "message").Update(&table.Migrates{Success: true, Message: "ok"})
		} else {
			_, recordErr = e.InsertOne(&table.Migrates{Version: m.Version(), Success: true, Message: "ok"})
		}
		if recordErr != nil {
			return fmt.Errorf("记录迁移 %d 成功状态: %w", m.Version(), recordErr)
		}
	}
	log.Infoln("数据表迁移执行完毕")
	return nil
}
