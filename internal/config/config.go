// Package config 提供配置加载与默认值。
//
// 配置文件采用 YAML 格式（也支持 JSON）。所有配置项都可以通过环境变量覆盖，
// 环境变量命名规则：JETLINKS_EDGE_<SECTION>_<KEY>，例如 JETLINKS_EDGE_WEB_ADDR=0.0.0.0:7001。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 是边缘网关的完整配置。
type Config struct {
	Web       WebConfig       `mapstructure:"web"`
	Log       LogConfig       `mapstructure:"log"`
	Storage   StorageConfig   `mapstructure:"storage"`
	Collector CollectorConfig `mapstructure:"collector"`
}

// WebConfig 管理界面相关配置。
type WebConfig struct {
	Addr            string        `mapstructure:"addr"`             // 监听地址，例如 0.0.0.0:7001
	JWTSecret       string        `mapstructure:"jwt_secret"`       // JWT 签名密钥
	TokenTTL        time.Duration `mapstructure:"token_ttl"`        // Token 有效期
	DefaultUser     string        `mapstructure:"default_user"`     // 首次启动创建的默认账号
	DefaultPassword string        `mapstructure:"default_password"` // 首次启动创建的默认密码
	StaticDir       string        `mapstructure:"static_dir"`       // 前端静态文件目录（生产模式嵌入二进制时为空）
}

// LogConfig 日志配置。
type LogConfig struct {
	Level  string `mapstructure:"level"`  // debug/info/warn/error
	Output string `mapstructure:"output"` // stdout/file:/path/to/file
}

// StorageConfig 存储配置。
type StorageConfig struct {
	Driver string `mapstructure:"driver"` // sqlite / postgres
	DSN    string `mapstructure:"dsn"`    // sqlite 文件路径或 postgres 连接串
}

// CollectorConfig 采集器通用配置。
type CollectorConfig struct {
	MaxConcurrency int           `mapstructure:"max_concurrency"` // 最大并发采集任务数
	ReadTimeout    time.Duration `mapstructure:"read_timeout"`    // 单次读点位超时
	WriteTimeout   time.Duration `mapstructure:"write_timeout"`   // 单次写点位超时
	ReconnectDelay time.Duration `mapstructure:"reconnect_delay"` // 断线重连间隔
}

// Load 从指定路径加载配置，配置不存在时使用默认值。
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	// 默认值
	v.SetDefault("web.addr", "0.0.0.0:7001")
	v.SetDefault("web.jwt_secret", "jetlinks-edge-default-secret-change-me")
	v.SetDefault("web.token_ttl", "24h")
	v.SetDefault("web.default_user", "admin")
	v.SetDefault("web.default_password", "admin123")
	v.SetDefault("web.static_dir", "")

	v.SetDefault("log.level", "info")
	v.SetDefault("log.output", "stdout")

	v.SetDefault("storage.driver", "sqlite")
	v.SetDefault("storage.dsn", "data/jetlinks-edge.db")

	v.SetDefault("collector.max_concurrency", 100)
	v.SetDefault("collector.read_timeout", "3s")
	v.SetDefault("collector.write_timeout", "3s")
	v.SetDefault("collector.reconnect_delay", "5s")

	// 环境变量覆盖
	v.SetEnvPrefix("JETLINKS_EDGE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		// 配置不存在时使用默认值而不是直接报错，方便首次启动
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// dsn 相对路径解析：相对可执行文件所在目录（而不是当前工作目录）。
	// 避免 `cd /tmp && ./bin/jetlinks-edge` 时 db 跑到 /tmp/data 下。
	if cfg.Storage.Driver == "sqlite" && !strings.HasPrefix(cfg.Storage.DSN, "/") && !strings.HasPrefix(cfg.Storage.DSN, ":memory:") {
		if exe, err := os.Executable(); err == nil {
			base := filepath.Dir(exe)
			cfg.Storage.DSN = filepath.Join(base, cfg.Storage.DSN)
			_ = os.MkdirAll(filepath.Dir(cfg.Storage.DSN), 0755)
		}
	}

	return cfg, nil
}

// Validate 校验会影响运行稳定性和安全边界的基础配置。
func (c *Config) Validate() error {
	switch {
	case c.Web.Addr == "":
		return fmt.Errorf("web.addr is required")
	case c.Web.JWTSecret == "":
		return fmt.Errorf("web.jwt_secret is required")
	case c.Web.TokenTTL <= 0:
		return fmt.Errorf("web.token_ttl must be greater than zero")
	case c.Storage.DSN == "":
		return fmt.Errorf("storage.dsn is required")
	case c.Collector.MaxConcurrency <= 0:
		return fmt.Errorf("collector.max_concurrency must be greater than zero")
	case c.Collector.ReadTimeout <= 0:
		return fmt.Errorf("collector.read_timeout must be greater than zero")
	case c.Collector.WriteTimeout <= 0:
		return fmt.Errorf("collector.write_timeout must be greater than zero")
	case c.Collector.ReconnectDelay <= 0:
		return fmt.Errorf("collector.reconnect_delay must be greater than zero")
	default:
		return nil
	}
}
