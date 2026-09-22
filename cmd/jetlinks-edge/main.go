// Package main 是 JetLinks Edge 边缘网关的入口程序。
//
// 边缘网关主要职责：
//  1. 通过南向驱动（南向插件）从现场设备采集数据（Modbus TCP / OPC UA）
//  2. 通过北向上送（北向应用）把数据推送到 JetLinks 或通用 MQTT Broker
//  3. 接收来自平台的控制指令并下发到南向设备
//  4. 提供 Web 管理界面配置南向连接、采集组、点位、北向应用和插件
//
// 设计原则：
//   - 协议无关：所有南向驱动实现统一的 SouthDriver 接口
//   - 平台无关：所有北向应用实现统一的 NorthApp 接口
//   - 配置驱动：南向连接、采集组、点位、北向应用全部从数据库加载，运行时可热更新
//   - 单二进制：所有功能打包为单个可执行文件，零外部运行时依赖（除 SQLite）
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jhzhang09/jetlinks-edge/internal/config"
	"github.com/jhzhang09/jetlinks-edge/internal/core"
	"github.com/jhzhang09/jetlinks-edge/internal/driver/modbus"
	"github.com/jhzhang09/jetlinks-edge/internal/driver/opcua"
	"github.com/jhzhang09/jetlinks-edge/internal/externalplugin"
	"github.com/jhzhang09/jetlinks-edge/internal/logger"
	"github.com/jhzhang09/jetlinks-edge/internal/northbound/jetlinksmqtt"
	"github.com/jhzhang09/jetlinks-edge/internal/northbound/mqtt"
	"github.com/jhzhang09/jetlinks-edge/internal/store"
	"github.com/jhzhang09/jetlinks-edge/internal/web"
	"go.uber.org/zap"
)

// version 由编译时通过 -ldflags "-X main.version=xxx" 注入。
// 本地开发时默认 "dev"，CI/Release 构建时自动从 git tag 取值。
var version = "dev"

// @title           JetLinks Edge API
// @version         0.5.0
// @description     JetLinks 边缘网关管理 API
// @BasePath        /api
func main() {
	cfgFile := flag.String("c", "config.yaml", "配置文件路径")
	showVersion := flag.Bool("v", false, "打印版本号并退出")
	flag.Parse()

	if *showVersion {
		fmt.Printf("jetlinks-edge %s\n", version)
		os.Exit(0)
	}

	// 1. 加载配置
	cfg, err := config.Load(*cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config failed: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "invalid config: %v\n", err)
		os.Exit(1)
	}

	// 2. 初始化日志
	if err := logger.Init(cfg.Log.Level, cfg.Log.Output); err != nil {
		fmt.Fprintf(os.Stderr, "init logger failed: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	zap.L().Info("jetlinks-edge starting",
		zap.String("version", version),
		zap.String("config", *cfgFile),
		zap.String("webAddr", cfg.Web.Addr),
	)
	if cfg.Web.JWTSecret == "jetlinks-edge-default-secret-change-me" ||
		cfg.Web.DefaultPassword == "admin123" {
		zap.L().Warn("default web credentials are enabled; change them before production use")
	}

	// 3. 初始化存储
	st, err := store.New(cfg.Storage)
	if err != nil {
		zap.L().Fatal("init store failed", zap.Error(err))
	}
	if err := st.Migrate(); err != nil {
		zap.L().Fatal("migrate store failed", zap.Error(err))
	}
	if err := st.SeedDefaultUser(cfg.Web.DefaultUser, cfg.Web.DefaultPassword); err != nil {
		zap.L().Fatal("seed default user failed", zap.Error(err))
	}

	// 4. 构建核心调度器
	driverRegistry := core.NewDriverRegistry()
	// 注册内置南向驱动，后续插件继续通过注册表扩展。
	modbus.Register(driverRegistry)
	opcua.Register(driverRegistry)

	northRegistry := core.NewNorthRegistry()
	// 注册内置北向应用。
	jetlinksmqtt.Register(northRegistry)
	mqtt.Register(northRegistry)

	var pluginManager *externalplugin.Manager
	if cfg.Plugins.Enabled {
		pluginManager = externalplugin.NewManager(cfg.Plugins.Directory, driverRegistry, northRegistry)
		if _, err := pluginManager.Reload(); err != nil {
			zap.L().Fatal("load external plugins failed", zap.Error(err))
		}
	}

	runner := core.NewRunner(driverRegistry, northRegistry, st, core.RunnerOptions{
		MaxConcurrency: cfg.Collector.MaxConcurrency,
		ReadTimeout:    cfg.Collector.ReadTimeout,
		WriteTimeout:   cfg.Collector.WriteTimeout,
		ReconnectDelay: cfg.Collector.ReconnectDelay,
	})

	// 5. 启动核心：加载北向应用、南向连接和采集组
	if err := runner.Start(context.Background()); err != nil {
		zap.L().Fatal("runner start failed", zap.Error(err))
	}
	pluginWatchCtx, stopPluginWatch := context.WithCancel(context.Background())
	defer stopPluginWatch()
	if pluginManager != nil {
		err := pluginManager.Watch(pluginWatchCtx, func(changes externalplugin.ChangeSet) error {
			return runner.ReconcileExtensionTypes(
				pluginWatchCtx,
				changes.DriverTypes,
				changes.NorthTypes,
				changes.RemovedDriverTypes,
				changes.RemovedNorthTypes,
			)
		}, func(err error) {
			zap.L().Error("external plugin hot reload failed", zap.Error(err))
		})
		if err != nil {
			zap.L().Fatal("watch external plugins failed", zap.Error(err))
		}
	}

	// 6. 启动 Web 管理服务
	webServer := web.New(cfg, st, runner, pluginManager)
	go func() {
		if err := webServer.Start(cfg.Web.Addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			zap.L().Fatal("web server failed", zap.Error(err))
		}
	}()

	zap.L().Info("jetlinks-edge started",
		zap.String("web", "http://"+cfg.Web.Addr),
		zap.String("docs", "http://"+cfg.Web.Addr+"/swagger/index.html"),
	)

	// 7. 等待退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	zap.L().Info("shutting down...")
	stopPluginWatch()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	webServer.Shutdown(ctx)
	runner.Stop()
}
