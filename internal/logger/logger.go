// Package logger 提供 zap 日志的全局初始化与同步。
package logger

import (
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var globalLogger *zap.Logger

// Init 初始化全局 zap logger。
// level: debug/info/warn/error；output: stdout / stderr / file:./xxx.log
func Init(level, output string) error {
	lvl := zapcore.InfoLevel
	if err := lvl.UnmarshalText([]byte(strings.ToLower(level))); err != nil {
		lvl = zapcore.InfoLevel
	}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder

	var ws zapcore.WriteSyncer
	switch {
	case strings.HasPrefix(output, "file:"):
		path := strings.TrimPrefix(output, "file:")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		ws = zapcore.AddSync(f)
	case output == "stderr":
		ws = zapcore.AddSync(os.Stderr)
	default:
		ws = zapcore.AddSync(os.Stdout)
	}

	core := zapcore.NewCore(zapcore.NewConsoleEncoder(encCfg), ws, lvl)
	globalLogger = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	zap.ReplaceGlobals(globalLogger)
	return nil
}

// Sync 刷新缓冲。
func Sync() {
	if globalLogger != nil {
		_ = globalLogger.Sync()
	}
}
