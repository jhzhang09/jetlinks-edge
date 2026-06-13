// Package store 提供持久化数据库存储的单元测试。
// @author jhzhang
// @date 2026-06-13
package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jhzhang09/jetlinks-edge/internal/config"
	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

func TestStore(t *testing.T) {
	// 创建临时测试目录及 SQLite 数据库
	tmpDir, err := os.MkdirTemp("", "store-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "subfolder", "edge.db") // 顺便测试自动创建父目录逻辑
	cfg := config.StorageConfig{
		Driver: "sqlite",
		DSN:    dbPath,
	}

	// 1. 初始化测试
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// 测试不支持的驱动
	_, err = New(config.StorageConfig{Driver: "invalid"})
	if err == nil {
		t.Error("expected error when using invalid storage driver")
	}

	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// 2. 测试用户登录和 JWT 鉴权
	s.SetAuthConfig("test-jwt-secret-key-12345", time.Hour)

	// Seed 默认用户
	if err := s.SeedDefaultUser("admin", "admin123"); err != nil {
		t.Fatalf("SeedDefaultUser failed: %v", err)
	}

	// 幂等性测试（重复 Seed）
	if err := s.SeedDefaultUser("admin", "admin123"); err != nil {
		t.Fatalf("SeedDefaultUser duplicate failed: %v", err)
	}

	// 登录成功
	token, u, err := s.Login("admin", "admin123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if token == "" || u == nil || u.Username != "admin" {
		t.Errorf("invalid login result: token=%q, user=%+v", token, u)
	}

	// 登录失败：错误密码
	_, _, err = s.Login("admin", "wrongpassword")
	if err == nil {
		t.Error("expected login with wrong password to fail")
	}

	// 登录失败：不存在用户
	_, _, err = s.Login("nonexistent", "password")
	if err == nil {
		t.Error("expected login with nonexistent user to fail")
	}

	// 解析 Token
	claims, err := s.ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken failed: %v", err)
	}
	if claims.Username != "admin" {
		t.Errorf("expected claim username to be 'admin', got %q", claims.Username)
	}

	// 解析无效 Token
	_, err = s.ParseToken("invalid-token-string")
	if err == nil {
		t.Error("expected ParseToken with invalid string to fail")
	}

	// 测试修改密码：成功修改
	err = s.ChangePassword(u.ID, "admin123", "newsecret123")
	if err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}

	// 错误旧密码修改失败
	err = s.ChangePassword(u.ID, "wrong-old-pwd", "anotherpwd")
	if err == nil {
		t.Error("expected ChangePassword with wrong old password to fail")
	}

	// 用新密码登录
	tokenNew, uNew, err := s.Login("admin", "newsecret123")
	if err != nil {
		t.Fatalf("Login with new password failed: %v", err)
	}
	if tokenNew == "" || uNew == nil {
		t.Error("expected login with new password to succeed")
	}

	// 用旧密码登录失败
	_, _, err = s.Login("admin", "admin123")
	if err == nil {
		t.Error("expected login with old password after change to fail")
	}

	// 获取 DB 实例
	if db := s.DB(); db == nil {
		t.Error("expected DB() to return non-nil gorm.DB")
	}

	ctx := context.Background()

	// 3. 测试 NorthApp CRUD
	app := &core.NorthApp{
		ID:      "north-1",
		Name:    "North App 1",
		Type:    "jetlinks",
		Enabled: true,
		Config:  map[string]interface{}{"host": "localhost"},
	}

	if err := s.SaveNorthApp(ctx, app); err != nil {
		t.Fatalf("SaveNorthApp failed: %v", err)
	}

	appGet, err := s.GetNorthApp(ctx, "north-1")
	if err != nil {
		t.Fatalf("GetNorthApp failed: %v", err)
	}
	if appGet == nil || appGet.Name != "North App 1" {
		t.Errorf("GetNorthApp returned unexpected result: %+v", appGet)
	}

	apps, err := s.ListNorthApps(ctx)
	if err != nil {
		t.Fatalf("ListNorthApps failed: %v", err)
	}
	if len(apps) != 1 || apps[0].ID != "north-1" {
		t.Errorf("ListNorthApps returned unexpected result: %+v", apps)
	}

	enabledApps, err := s.ListEnabledNorthApps(ctx)
	if err != nil {
		t.Fatalf("ListEnabledNorthApps failed: %v", err)
	}
	if len(enabledApps) != 1 || enabledApps[0].ID != "north-1" {
		t.Errorf("ListEnabledNorthApps returned unexpected result: %+v", enabledApps)
	}

	// 4. 测试 Group CRUD 及其关联关系
	g := &core.Group{
		ID:         "group-1",
		Name:       "Group 1",
		Enabled:    true,
		IntervalMs: 1000,
		NorthAppID: "north-1",
		Config:     map[string]interface{}{"slaveId": byte(1)},
	}

	if err := s.SaveGroup(ctx, g); err != nil {
		t.Fatalf("SaveGroup failed: %v", err)
	}

	gGet, err := s.GetGroup(ctx, "group-1")
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}
	if gGet == nil || gGet.IntervalMs != 1000 || gGet.NorthAppID != "north-1" {
		t.Errorf("GetGroup returned unexpected result: %+v", gGet)
	}

	// 测试列表
	groups, err := s.ListEnabledGroups(ctx)
	if err != nil {
		t.Fatalf("ListEnabledGroups failed: %v", err)
	}
	if len(groups) != 1 || groups[0].ID != "group-1" {
		t.Errorf("ListEnabledGroups returned unexpected size/content: %+v", groups)
	}

	// 5. 测试 Tag CRUD
	t1 := &core.Tag{
		ID:      "tag-1",
		GroupID: "group-1",
		Name:    "Tag 1",
		Address: "40001",
		Config:  map[string]interface{}{"format": "int16"},
	}

	if err := s.SaveTag(ctx, t1); err != nil {
		t.Fatalf("SaveTag failed: %v", err)
	}

	tGet, err := s.GetTag(ctx, "tag-1")
	if err != nil {
		t.Fatalf("GetTag failed: %v", err)
	}
	if tGet == nil || tGet.Address != "40001" {
		t.Errorf("GetTag returned unexpected result: %+v", tGet)
	}

	tags, err := s.ListTagsByGroup(ctx, "group-1")
	if err != nil {
		t.Fatalf("ListTagsByGroup failed: %v", err)
	}
	if len(tags) != 1 || tags[0].ID != "tag-1" {
		t.Errorf("ListTagsByGroup returned unexpected size/content: %+v", tags)
	}

	// 6. 测试删除级联和解绑
	// 删除 NorthApp 会导致 Group 的 north_app_id 被置空
	if err := s.DeleteNorthApp(ctx, "north-1"); err != nil {
		t.Fatalf("DeleteNorthApp failed: %v", err)
	}

	gAfterDelete, err := s.GetGroup(ctx, "group-1")
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}
	if gAfterDelete == nil || gAfterDelete.NorthAppID != "" {
		t.Errorf("expected north_app_id to be cleared after deleting app, got %q", gAfterDelete.NorthAppID)
	}

	// 删除 Group 会级联删除对应 Tag
	if err := s.DeleteGroup(ctx, "group-1"); err != nil {
		t.Fatalf("DeleteGroup failed: %v", err)
	}

	gDeleted, err := s.GetGroup(ctx, "group-1")
	if err != nil {
		t.Fatalf("GetGroup for deleted group failed: %v", err)
	}
	if gDeleted != nil {
		t.Error("expected group to be deleted")
	}

	tDeleted, err := s.GetTag(ctx, "tag-1")
	if err != nil {
		t.Fatalf("GetTag for deleted tag failed: %v", err)
	}
	if tDeleted != nil {
		t.Error("expected tag to be cascade-deleted with group")
	}

	// 7. 额外测试未找到资源
	gNotFound, err := s.GetGroup(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetGroup for nonexistent expected nil error, got %v", err)
	}
	if gNotFound != nil {
		t.Error("expected nil for nonexistent group")
	}

	tNotFound, err := s.GetTag(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetTag for nonexistent expected nil error, got %v", err)
	}
	if tNotFound != nil {
		t.Error("expected nil for nonexistent tag")
	}

	appNotFound, err := s.GetNorthApp(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetNorthApp for nonexistent expected nil error, got %v", err)
	}
	if appNotFound != nil {
		t.Error("expected nil for nonexistent NorthApp")
	}
}
