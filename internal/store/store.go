// Package store 提供点组、点位、用户等数据的持久化。
//
// 默认使用 SQLite（零依赖），可切换为 PostgreSQL（多实例共享配置）。
// 所有数据库访问通过 gorm 抽象，便于切换底层。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	// SQLite 驱动使用 glebarez/sqlite（纯 Go 实现）。
	// 原因：gorm.io/driver/sqlite 底层依赖 mattn/go-sqlite3，需要 CGO；
	// 切换为 glebarez/sqlite 后可在 CGO_ENABLED=0 下完成交叉编译，
	// 符合 scripts/build-bundle.sh 的零依赖打包要求。
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/jhzhang09/jetlinks-edge/internal/config"
	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

// User 用户表。
type User struct {
	ID           string    `gorm:"primaryKey;type:varchar(64)" json:"id"`
	Username     string    `gorm:"uniqueIndex;type:varchar(64)" json:"username"`
	PasswordHash string    `gorm:"type:varchar(256)" json:"-"`
	Role         string    `gorm:"type:varchar(32);default:'user'" json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Store 是存储实现，对外暴露 gorm 风格的接口 + core.Store 接口。
type Store struct {
	db        *gorm.DB
	jwtSecret string
	tokenTTL  time.Duration
}

// New 创建存储实例。
func New(cfg config.StorageConfig) (*Store, error) {
	gormCfg := &gorm.Config{
		Logger: logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		}),
	}
	var (
		db  *gorm.DB
		err error
	)
	switch strings.ToLower(cfg.Driver) {
	case "sqlite", "":
		// 自动创建父目录
		dir := filepath.Dir(cfg.DSN)
		if dir != "" && dir != "." {
			_ = os.MkdirAll(dir, 0755)
		}
		db, err = gorm.Open(sqlite.Open(cfg.DSN), gormCfg)
	case "postgres", "postgresql":
		db, err = gorm.Open(postgres.Open(cfg.DSN), gormCfg)
	default:
		return nil, fmt.Errorf("unsupported storage driver: %s", cfg.Driver)
	}
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	return &Store{db: db}, nil
}

// SetAuthConfig 注入 JWT 签名密钥与有效期。
func (s *Store) SetAuthConfig(secret string, tokenTTL time.Duration) {
	s.jwtSecret = secret
	s.tokenTTL = tokenTTL
}

// DB 暴露底层 gorm（用于 Web handler 直接查询）。
func (s *Store) DB() *gorm.DB { return s.db }

// Migrate 自动迁移表结构。
func (s *Store) Migrate() error {
	err := s.db.AutoMigrate(&core.Connection{}, &core.Group{}, &core.Tag{}, &User{}, &core.NorthApp{})
	if err != nil {
		return err
	}
	s.runDataMigration()
	return nil
}

func (s *Store) runDataMigration() {
	if !s.db.Migrator().HasColumn(&core.Group{}, "driver") {
		return
	}
	type TempGroup struct {
		ID         string `gorm:"column:id"`
		Name       string `gorm:"column:name"`
		Driver     string `gorm:"column:driver"`
		ConfigJSON string `gorm:"column:config"`
	}
	var oldGroups []TempGroup
	err := s.db.Raw("SELECT id, name, driver, config FROM groups WHERE driver IS NOT NULL AND driver != '' AND (connection_id IS NULL OR connection_id = '')").Scan(&oldGroups).Error
	if err != nil || len(oldGroups) == 0 {
		return
	}

	for _, og := range oldGroups {
		connID := "conn_" + og.ID
		conn := &core.Connection{
			ID:          connID,
			Name:        og.Name + " 通道",
			Description: "由旧版点组自动迁移创建的物理通道",
			Driver:      og.Driver,
			Enabled:     true,
			ConfigJSON:  og.ConfigJSON,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		var oldConfig map[string]interface{}
		_ = json.Unmarshal([]byte(og.ConfigJSON), &oldConfig)

		newGroupConfig := map[string]interface{}{}
		if oldConfig != nil {
			if uid, ok := oldConfig["unitId"]; ok {
				newGroupConfig["unitId"] = uid
			}
		}
		newGroupConfigJSON := "{}"
		if len(newGroupConfig) > 0 {
			b, _ := json.Marshal(newGroupConfig)
			newGroupConfigJSON = string(b)
		}

		_ = s.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Save(conn).Error; err != nil {
				return err
			}
			return tx.Exec("UPDATE groups SET connection_id = ?, config = ?, driver = '' WHERE id = ?", connID, newGroupConfigJSON, og.ID).Error
		})
	}
}

// SeedDefaultUser 创建默认账号（若不存在）。
func (s *Store) SeedDefaultUser(username, password string) error {
	if username == "" {
		return nil
	}
	var count int64
	s.db.Model(&User{}).Where("username = ?", username).Count(&count)
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.db.Create(&User{
		ID:           username,
		Username:     username,
		PasswordHash: string(hash),
		Role:         "admin",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}).Error
}

// ============ core.Store 实现 ============

func (s *Store) PopulateGroupDriver(g *core.Group) {
	if g == nil || g.ConnectionID == "" {
		return
	}
	var conn core.Connection
	if err := s.db.First(&conn, "id = ?", g.ConnectionID).Error; err == nil {
		g.Driver = conn.Driver
	}
}

func (s *Store) ListGroups(ctx context.Context) ([]*core.Group, error) {
	var gs []core.Group
	if err := s.db.WithContext(ctx).Find(&gs).Error; err != nil {
		return nil, err
	}
	connIDs := make([]string, 0, len(gs))
	for _, g := range gs {
		if g.ConnectionID != "" {
			connIDs = append(connIDs, g.ConnectionID)
		}
	}
	connMap := make(map[string]string)
	if len(connIDs) > 0 {
		var conns []core.Connection
		if err := s.db.WithContext(ctx).Where("id IN ?", connIDs).Find(&conns).Error; err == nil {
			for _, c := range conns {
				connMap[c.ID] = c.Driver
			}
		}
	}
	out := make([]*core.Group, 0, len(gs))
	for i := range gs {
		gs[i].UnmarshalConfig()
		gs[i].Interval = time.Duration(gs[i].IntervalMs) * time.Millisecond
		if drv, ok := connMap[gs[i].ConnectionID]; ok {
			gs[i].Driver = drv
		}
		out = append(out, &gs[i])
	}
	return out, nil
}

func (s *Store) ListEnabledGroups(ctx context.Context) ([]*core.Group, error) {
	var gs []core.Group
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&gs).Error; err != nil {
		return nil, err
	}
	connIDs := make([]string, 0, len(gs))
	for _, g := range gs {
		if g.ConnectionID != "" {
			connIDs = append(connIDs, g.ConnectionID)
		}
	}
	connMap := make(map[string]string)
	if len(connIDs) > 0 {
		var conns []core.Connection
		if err := s.db.WithContext(ctx).Where("id IN ?", connIDs).Find(&conns).Error; err == nil {
			for _, c := range conns {
				connMap[c.ID] = c.Driver
			}
		}
	}
	out := make([]*core.Group, 0, len(gs))
	for i := range gs {
		gs[i].UnmarshalConfig()
		gs[i].Interval = time.Duration(gs[i].IntervalMs) * time.Millisecond
		if drv, ok := connMap[gs[i].ConnectionID]; ok {
			gs[i].Driver = drv
		}
		out = append(out, &gs[i])
	}
	return out, nil
}

func (s *Store) GetGroup(ctx context.Context, id string) (*core.Group, error) {
	var g core.Group
	if err := s.db.WithContext(ctx).First(&g, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	g.UnmarshalConfig()
	g.Interval = time.Duration(g.IntervalMs) * time.Millisecond
	s.PopulateGroupDriver(&g)
	return &g, nil
}

func (s *Store) SaveGroup(ctx context.Context, g *core.Group) error {
	g.MarshalConfig()
	if g.IntervalMs == 0 {
		g.IntervalMs = 1000
	}
	return s.db.WithContext(ctx).Save(g).Error
}

func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", id).Delete(&core.Tag{}).Error; err != nil {
			return err
		}
		return tx.Delete(&core.Group{}, "id = ?", id).Error
	})
}

func (s *Store) SaveTag(ctx context.Context, t *core.Tag) error {
	t.MarshalConfig()
	return s.db.WithContext(ctx).Save(t).Error
}

func (s *Store) DeleteTag(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&core.Tag{}, "id = ?", id).Error
}

func (s *Store) ListTagsByGroup(ctx context.Context, groupID string) ([]*core.Tag, error) {
	var ts []core.Tag
	if err := s.db.WithContext(ctx).Where("group_id = ?", groupID).Find(&ts).Error; err != nil {
		return nil, err
	}
	out := make([]*core.Tag, 0, len(ts))
	for i := range ts {
		ts[i].UnmarshalConfig()
		out = append(out, &ts[i])
	}
	return out, nil
}

func (s *Store) GetTag(ctx context.Context, id string) (*core.Tag, error) {
	var t core.Tag
	if err := s.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	t.UnmarshalConfig()
	return &t, nil
}

// ============ NorthApp 实现 ============

func (s *Store) ListNorthApps(ctx context.Context) ([]*core.NorthApp, error) {
	var ns []core.NorthApp
	if err := s.db.WithContext(ctx).Order("created_at desc").Find(&ns).Error; err != nil {
		return nil, err
	}
	out := make([]*core.NorthApp, 0, len(ns))
	for i := range ns {
		ns[i].UnmarshalConfig()
		out = append(out, &ns[i])
	}
	return out, nil
}

func (s *Store) ListEnabledNorthApps(ctx context.Context) ([]*core.NorthApp, error) {
	var ns []core.NorthApp
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&ns).Error; err != nil {
		return nil, err
	}
	out := make([]*core.NorthApp, 0, len(ns))
	for i := range ns {
		ns[i].UnmarshalConfig()
		out = append(out, &ns[i])
	}
	return out, nil
}

func (s *Store) GetNorthApp(ctx context.Context, id string) (*core.NorthApp, error) {
	var n core.NorthApp
	if err := s.db.WithContext(ctx).First(&n, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	n.UnmarshalConfig()
	return &n, nil
}

func (s *Store) SaveNorthApp(ctx context.Context, n *core.NorthApp) error {
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	n.UpdatedAt = time.Now()
	n.MarshalConfig()
	return s.db.WithContext(ctx).Save(n).Error
}

func (s *Store) DeleteNorthApp(ctx context.Context, id string) error {
	// 解除所有引用此 NorthApp 的 Group 的绑定（支持逗号分隔多北向绑定）
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var groups []core.Group
		if err := tx.Where("north_app_id LIKE ?", "%"+id+"%").Find(&groups).Error; err != nil {
			return err
		}
		for _, g := range groups {
			if core.HasNorthAppID(g.NorthAppID, id) {
				newID := core.RemoveNorthAppID(g.NorthAppID, id)
				if err := tx.Model(&g).Update("north_app_id", newID).Error; err != nil {
					return err
				}
			}
		}
		return tx.Delete(&core.NorthApp{}, "id = ?", id).Error
	})
}

// ============ 鉴权 ============

// Claims JWT 负载。
type Claims struct {
	UserID   string `json:"uid"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Login 登录。
func (s *Store) Login(username, password string) (string, *User, error) {
	var u User
	if err := s.db.Where("username = ?", username).First(&u).Error; err != nil {
		return "", nil, errors.New("invalid username or password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return "", nil, errors.New("invalid username or password")
	}
	ttl := s.tokenTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	claims := Claims{
		UserID:   u.ID,
		Username: u.Username,
		Role:     u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tk := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token, err := tk.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return "", nil, err
	}
	return token, &u, nil
}

// ParseToken 解析 JWT。
func (s *Store) ParseToken(tokenStr string) (*Claims, error) {
	tk, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %s", t.Method.Alg())
		}
		return []byte(s.jwtSecret), nil
	})
	if err != nil {
		return nil, err
	}
	if c, ok := tk.Claims.(*Claims); ok && tk.Valid {
		return c, nil
	}
	return nil, errors.New("invalid token")
}

// ChangePassword 修改用户密码。
// @author jhzhang
// @date 2026-06-13
func (s *Store) ChangePassword(userID, oldPassword, newPassword string) error {
	var u User
	if err := s.db.First(&u, "id = ?", userID).Error; err != nil {
		return errors.New("user not found")
	}
	// 验证旧密码是否匹配
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(oldPassword)); err != nil {
		return errors.New("invalid old password")
	}
	// 生成新密码哈希值
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	u.UpdatedAt = time.Now()
	return s.db.Save(&u).Error
}

// ============ Connection 实现 ============

func (s *Store) ListConnections(ctx context.Context) ([]*core.Connection, error) {
	var conns []core.Connection
	if err := s.db.WithContext(ctx).Order("created_at desc").Find(&conns).Error; err != nil {
		return nil, err
	}
	out := make([]*core.Connection, 0, len(conns))
	for i := range conns {
		conns[i].UnmarshalConfig()
		out = append(out, &conns[i])
	}
	return out, nil
}

func (s *Store) ListEnabledConnections(ctx context.Context) ([]*core.Connection, error) {
	var conns []core.Connection
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&conns).Error; err != nil {
		return nil, err
	}
	out := make([]*core.Connection, 0, len(conns))
	for i := range conns {
		conns[i].UnmarshalConfig()
		out = append(out, &conns[i])
	}
	return out, nil
}

func (s *Store) GetConnection(ctx context.Context, id string) (*core.Connection, error) {
	var conn core.Connection
	if err := s.db.WithContext(ctx).First(&conn, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	conn.UnmarshalConfig()
	return &conn, nil
}

func (s *Store) SaveConnection(ctx context.Context, conn *core.Connection) error {
	if conn.CreatedAt.IsZero() {
		conn.CreatedAt = time.Now()
	}
	conn.UpdatedAt = time.Now()
	conn.MarshalConfig()
	return s.db.WithContext(ctx).Save(conn).Error
}

func (s *Store) DeleteConnection(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var groups []core.Group
		if err := tx.Where("connection_id = ?", id).Find(&groups).Error; err != nil {
			return err
		}
		for _, g := range groups {
			if err := tx.Where("group_id = ?", g.ID).Delete(&core.Tag{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("connection_id = ?", id).Delete(&core.Group{}).Error; err != nil {
			return err
		}
		return tx.Delete(&core.Connection{}, "id = ?", id).Error
	})
}
