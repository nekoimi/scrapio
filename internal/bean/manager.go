package bean

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
)

// LifecycleManager 负责统一管理模块的启动与关闭
type LifecycleManager struct {
	// context
	ctx context.Context
	// signal
	sigs chan os.Signal
	// 管理的应用列表
	lifecycles []Lifecycle
}

func NewLifecycleManager(ctx context.Context) *LifecycleManager {
	m := &LifecycleManager{
		ctx:        ctx,
		sigs:       make(chan os.Signal, 1),
		lifecycles: make([]Lifecycle, 0),
	}
	signal.Notify(m.sigs, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	return m
}

func (m *LifecycleManager) Register(lifecycle Lifecycle) {
	log.Infof("[Lifecycle] Registry: %s", lifecycle.Name())
	m.lifecycles = append(m.lifecycles, lifecycle)
}

func (m *LifecycleManager) StartAndServe() error {
	// Database migrations must finish before any worker or HTTP endpoint starts.
	for _, lifecycle := range m.lifecycles {
		if lifecycle.Name() == "DB" {
			log.Infof("[Lifecycle] Starting: %s ...", lifecycle.Name())
			if err := lifecycle.Start(m.ctx); err != nil {
				return fmt.Errorf("start %s: %w", lifecycle.Name(), err)
			}
			log.Infof("[Lifecycle] Start %s success!", lifecycle.Name())
		}
	}
	for _, lifecycle := range m.lifecycles {
		if lifecycle.Name() == "DB" {
			continue
		}
		go func(life Lifecycle, ctx context.Context) {
			defer func() {
				if r := recover(); r != nil {
					log.Errorf("[Lifecycle] failed to start %s panic: %v", life.Name(), r)
				}
			}()

			log.Infof("[Lifecycle] Starting: %s ...", life.Name())
			if err := life.Start(ctx); err != nil {
				log.Errorf("[Lifecycle] failed to start %s: %s", life.Name(), err)
				return
			}
			log.Infof("[Lifecycle] Start %s success!", life.Name())
		}(lifecycle, m.ctx)
	}
	m.waitForSignal()
	m.shutdown(30 * time.Second)
	return nil
}

func (m *LifecycleManager) waitForSignal() {
	sig := <-m.sigs
	log.Infof("收到退出信号: %v，正在关闭...", sig)
}

func (m *LifecycleManager) shutdown(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	wait := sync.WaitGroup{}
	for _, lifecycle := range m.lifecycles {
		wait.Add(1)
		go func(life Lifecycle) {
			defer wait.Done()
			log.Infof("[Lifecycle] Stopping: %s ...", life.Name())
			if err := life.Stop(ctx); err != nil {
				log.Errorf("[Lifecycle] Stop error in %s: %v\n", life.Name(), err)
			}
		}(lifecycle)
	}

	wait.Wait()
	log.Infoln("Exit.")
}
