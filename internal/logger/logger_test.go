// Package logger 测试全局日志初始化。
// @author jhzhang
// @date 2026-06-13
package logger

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInit(t *testing.T) {
	// 1. 测试 stdout 初始化
	if err := Init("debug", "stdout"); err != nil {
		t.Fatalf("Init with stdout failed: %v", err)
	}
	Sync()

	// 2. 测试 stderr 初始化
	if err := Init("warn", "stderr"); err != nil {
		t.Fatalf("Init with stderr failed: %v", err)
	}
	Sync()

	// 3. 测试 invalid level (应默认回退到 info)
	if err := Init("invalid-level-name", "stdout"); err != nil {
		t.Fatalf("Init with invalid level failed: %v", err)
	}
	Sync()

	// 4. 测试文件输出初始化
	tmpDir, err := os.MkdirTemp("", "logger-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logPath := filepath.Join(tmpDir, "test-edge.log")
	if err := Init("info", "file:"+logPath); err != nil {
		t.Fatalf("Init with file output failed: %v", err)
	}
	Sync()

	// 验证日志文件是否被创建
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("expected log file to be created at %s, but it does not exist", logPath)
	}
}
