// Package web 提供边缘网关的 HTTP 管理 API。
//
// 路由（统一前缀 /api/v1）：
//
//	POST   /auth/login                    登录获取 JWT
//	GET    /drivers                       列出南向驱动
//	GET    /north-apps                    列出北向应用
//
//	GET    /groups                        列出点组
//	POST   /groups                        新建点组
//	GET    /groups/:id                    点组详情
//	PUT    /groups/:id                    更新点组
//	DELETE /groups/:id                    删除点组
//	POST   /groups/:id/reload             热重启采集
//
//	GET    /groups/:id/tags               列出点位
//	POST   /groups/:id/tags               新建点位
//	PUT    /tags/:id                      更新点位
//	DELETE /tags/:id                      删除点位
//	POST   /tags/:id/read                 主动读
//	POST   /tags/:id/write                主动写
//
//	GET    /status                        运行时状态
package web

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/jhzhang09/jetlinks-edge/internal/config"
	"github.com/jhzhang09/jetlinks-edge/internal/core"
	"github.com/jhzhang09/jetlinks-edge/internal/store"
	"github.com/jhzhang09/jetlinks-edge/internal/web/handler"
	"github.com/jhzhang09/jetlinks-edge/internal/web/middleware"
	webassets "github.com/jhzhang09/jetlinks-edge/web"
)

// Server HTTP 服务。
type Server struct {
	cfg    *config.Config
	store  *store.Store
	runner *core.Runner
	engine *gin.Engine
	server *http.Server
}

// New 创建 Web 服务。
func New(cfg *config.Config, st *store.Store, runner *core.Runner) *Server {
	st.SetAuthConfig(cfg.Web.JWTSecret, cfg.Web.TokenTTL)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.ZapLogger(zap.L()))
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"*"},
		ExposeHeaders:    []string{"Content-Length", "Authorization"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	// 创建 handler
	authH := handler.NewAuthHandler(st, cfg)
	driverH := handler.NewDriverHandler(runner)
	northAppH := handler.NewNorthAppHandler(runner)
	groupH := handler.NewGroupHandler(runner, st)
	tagH := handler.NewTagHandler(runner, st)
	statusH := handler.NewStatusHandler(runner, st)
	connectionH := handler.NewConnectionHandler(runner, st)

	// 路由
	api := r.Group("/api/v1")
	{
		// 公共
		api.POST("/auth/login", authH.Login)

		// 需要鉴权
		auth := api.Group("")
		auth.Use(middleware.JWTAuth(st))
		{
			auth.GET("/auth/me", authH.Me)
			auth.POST("/auth/password", authH.ChangePassword)

			auth.GET("/drivers", driverH.ListDrivers)
			auth.GET("/extensions/drivers", driverH.ListDriverExtensions)
			auth.GET("/extensions/north-apps", driverH.ListNorthExtensions)

			// 物理连接通道：独立 CRUD
			auth.GET("/connections", connectionH.List)
			auth.GET("/connections/:id", connectionH.Get)
			auth.POST("/connections", connectionH.Create)
			auth.PUT("/connections/:id", connectionH.Update)
			auth.DELETE("/connections/:id", connectionH.Delete)
			auth.GET("/connections/drivers", connectionH.Drivers)

			// 北向应用：独立 CRUD
			auth.GET("/north-apps", northAppH.List)
			auth.GET("/north-apps/:id", northAppH.Get)
			auth.POST("/north-apps", northAppH.Create)
			auth.PUT("/north-apps/:id", northAppH.Update)
			auth.DELETE("/north-apps/:id", northAppH.Delete)
			auth.POST("/north-apps/:id/reload", northAppH.Reload)

			auth.GET("/groups", groupH.List)
			auth.POST("/groups", groupH.Create)
			auth.GET("/groups/:id", groupH.Get)
			auth.PUT("/groups/:id", groupH.Update)
			auth.DELETE("/groups/:id", groupH.Delete)
			auth.POST("/groups/:id/reload", groupH.Reload)
			auth.GET("/groups/:id/opcua/browse", groupH.BrowseOPCUA)

			auth.GET("/groups/:id/tags", tagH.ListByGroup)
			auth.POST("/groups/:id/tags", tagH.Create)
			auth.PUT("/tags/:id", tagH.Update)
			auth.DELETE("/tags/:id", tagH.Delete)
			auth.POST("/tags/:id/read", tagH.Read)
			auth.POST("/tags/:id/write", tagH.Write)
			auth.GET("/groups/:id/values", tagH.LastValues)

			auth.GET("/status", statusH.Status)
			auth.GET("/operations", statusH.Operations)
		}
	}

	// 健康检查
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// 静态前端提供逻辑
	if cfg.Web.StaticDir != "" {
		// 1. 开发模式：使用外部磁盘路径提供静态文件
		abs, _ := filepath.Abs(cfg.Web.StaticDir)
		if _, err := os.Stat(abs); err == nil {
			r.Static("/assets", filepath.Join(abs, "assets"))
			r.StaticFile("/", filepath.Join(abs, "index.html"))
			r.StaticFile("/index.html", filepath.Join(abs, "index.html"))
			r.NoRoute(func(c *gin.Context) {
				// SPA 兜底
				if strings.HasPrefix(c.Request.URL.Path, "/api") {
					c.JSON(404, gin.H{"error": "not found"})
					return
				}
				c.File(filepath.Join(abs, "index.html"))
			})
		}
	} else {
		// 2. 生产模式：使用 embed 嵌入的静态资源提供服务（最佳实践）
		assetsFS, err := fs.Sub(webassets.DistFS, "dist/assets")
		if err == nil {
			// 将 /assets 静态路由映射到嵌入文件系统的 dist/assets 子目录
			r.StaticFS("/assets", http.FS(assetsFS))
		}

		// 统一处理 index.html 的提供，直接读取其字节流输出，防止 http.FileServer 产生 URL 美化重定向循环
		serveIndex := func(c *gin.Context) {
			data, err := fs.ReadFile(webassets.DistFS, "dist/index.html")
			if err != nil {
				c.String(http.StatusInternalServerError, "Internal Server Error")
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", data)
		}

		// 挂载首页路由
		r.GET("/", serveIndex)
		r.GET("/index.html", serveIndex)

		// 针对单页应用 (SPA) 进行 NoRoute 兜底处理
		r.NoRoute(func(c *gin.Context) {
			if strings.HasPrefix(c.Request.URL.Path, "/api") {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			serveIndex(c)
		})
	}

	return &Server{
		cfg:    cfg,
		store:  st,
		runner: runner,
		engine: r,
	}
}

// Engine 暴露 gin engine（用于测试）。
func (s *Server) Engine() *gin.Engine { return s.engine }

// Start 启动 HTTP 服务。
func (s *Server) Start(addr string) error {
	s.server = &http.Server{
		Addr:              addr,
		Handler:           s.engine,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	zap.L().Info("web server listening", zap.String("addr", addr))
	return s.server.ListenAndServe()
}

// Shutdown 优雅关闭。
func (s *Server) Shutdown(ctx context.Context) {
	if s.server == nil {
		return
	}
	_ = s.server.Shutdown(ctx)
}
