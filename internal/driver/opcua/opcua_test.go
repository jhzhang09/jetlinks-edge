// Package opcua 测试 OPC UA 南向采集驱动。
// @author jhzhang
// @date 2026-06-08
package opcua

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]interface{}
		wantErr bool
		want    OPCUAConfig
	}{
		{
			name: "missing endpoint",
			input: map[string]interface{}{
				"policy": "None",
			},
			wantErr: true,
		},
		{
			name: "valid anonymous config with defaults",
			input: map[string]interface{}{
				"endpoint": "opc.tcp://127.0.0.1:4840",
			},
			wantErr: false,
			want: OPCUAConfig{
				Endpoint: "opc.tcp://127.0.0.1:4840",
				Policy:   "None",
				Mode:     "None",
				Auth:     "Anonymous",
				Timeout:  10 * time.Second,
			},
		},
		{
			name: "valid userauth config",
			input: map[string]interface{}{
				"endpoint": "opc.tcp://localhost:4840",
				"policy":   "Basic256Sha256",
				"mode":     "SignAndEncrypt",
				"auth":     "Username",
				"username": "admin",
				"password": "password123",
				"timeout":  "5s",
			},
			wantErr: false,
			want: OPCUAConfig{
				Endpoint: "opc.tcp://localhost:4840",
				Policy:   "Basic256Sha256",
				Mode:     "SignAndEncrypt",
				Auth:     "Username",
				Username: "admin",
				Password: "password123",
				Timeout:  5 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseConfig(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if got.Endpoint != tt.want.Endpoint ||
					got.Policy != tt.want.Policy ||
					got.Mode != tt.want.Mode ||
					got.Auth != tt.want.Auth ||
					got.Username != tt.want.Username ||
					got.Password != tt.want.Password ||
					got.Timeout != tt.want.Timeout {
					t.Fatalf("parseConfig() = %+v, want %+v", got, tt.want)
				}
			}
		})
	}
}

func TestGetSecurityPolicy(t *testing.T) {
	tests := []struct {
		policy string
		want   string
	}{
		{"Basic256Sha256", "http://opcfoundation.org/UA/SecurityPolicy#Basic256Sha256"},
		{"Basic128Rsa15", "http://opcfoundation.org/UA/SecurityPolicy#Basic128Rsa15"},
		{"None", "http://opcfoundation.org/UA/SecurityPolicy#None"},
		{"Unknown", "http://opcfoundation.org/UA/SecurityPolicy#None"},
	}

	for _, tt := range tests {
		got := getSecurityPolicy(tt.policy)
		if got != tt.want {
			t.Errorf("getSecurityPolicy(%q) = %q, want %q", tt.policy, got, tt.want)
		}
	}
}

func TestGetMessageSecurityMode(t *testing.T) {
	tests := []struct {
		mode string
		want ua.MessageSecurityMode
	}{
		{"Sign", ua.MessageSecurityModeSign},
		{"SignAndEncrypt", ua.MessageSecurityModeSignAndEncrypt},
		{"None", ua.MessageSecurityModeNone},
		{"Unknown", ua.MessageSecurityModeNone},
	}

	for _, tt := range tests {
		got := getMessageSecurityMode(tt.mode)
		if got != tt.want {
			t.Errorf("getMessageSecurityMode(%q) = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

func TestDescriptor(t *testing.T) {
	desc := Descriptor()
	if desc.Type != DriverName {
		t.Errorf("Descriptor().Type = %q, want %q", desc.Type, DriverName)
	}
	if len(desc.ConnectionSchema) == 0 {
		t.Error("expected non-empty ConnectionSchema")
	}
	if len(desc.TagSchema) == 0 {
		t.Error("expected non-empty TagSchema")
	}
}

func TestNewDriver(t *testing.T) {
	cfg := core.DriverConfig{
		Config: map[string]interface{}{
			"endpoint": "opc.tcp://localhost:4840",
		},
		ReadTimeout:    2 * time.Second,
		ReconnectDelay: 10 * time.Second,
	}
	driver, err := NewDriver(context.Background(), "test-opcua", cfg)
	if err != nil {
		t.Fatalf("NewDriver() failed: %v", err)
	}
	if driver.Name() != "test-opcua" {
		t.Errorf("driver.Name() = %q, want %q", driver.Name(), "test-opcua")
	}

	impl, ok := driver.(*driverImpl)
	if !ok {
		t.Fatal("expected driver to be *driverImpl")
	}
	if impl.cfg.Timeout != 2*time.Second {
		t.Errorf("impl.cfg.Timeout = %v, want %v", impl.cfg.Timeout, 2*time.Second)
	}
	if impl.reconnectDelay != 10*time.Second {
		t.Errorf("impl.reconnectDelay = %v, want %v", impl.reconnectDelay, 10*time.Second)
	}
}

func TestStatus(t *testing.T) {
	d := &driverImpl{
		stats:     map[string]int64{"ok.4": 10},
		lastError: "some error",
		lastTime:  time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
	}

	status := d.Status()
	if status.Connected {
		t.Error("expected Connected to be false because client is nil")
	}
	if status.LastError != "some error" {
		t.Errorf("status.LastError = %q, want %q", status.LastError, "some error")
	}
	if status.Stats["ok.4"] != 10 {
		t.Errorf("status.Stats[\"ok.4\"] = %d, want %d", status.Stats["ok.4"], 10)
	}
}

func TestReadTagsWhileOffline(t *testing.T) {
	cfg := core.DriverConfig{
		Config: map[string]interface{}{
			"endpoint": "opc.tcp://127.0.0.1:12345",
			"timeout":  "50ms",
		},
		ReconnectDelay: 1 * time.Second,
	}
	driver, err := NewDriver(context.Background(), "test-opcua-offline", cfg)
	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}

	tags := []core.Tag{
		{ID: "tag-1", Name: "Temperature", Config: map[string]interface{}{"nodeId": "ns=2;s=Demo.Tag"}},
	}

	start := time.Now()
	values, err := driver.ReadTags(context.Background(), tags)
	if err != nil {
		t.Fatalf("ReadTags returned error: %v", err)
	}
	duration := time.Since(start)

	if len(values) != 1 {
		t.Fatalf("expected 1 result, got %d", len(values))
	}
	if values[0].Quality != core.QualityBad {
		t.Errorf("expected quality to be bad, got %s", values[0].Quality)
	}
	if values[0].Error == "" {
		t.Error("expected non-empty error message")
	}

	if duration > 500*time.Millisecond {
		t.Errorf("ReadTags took too long: %v, expected under 500ms", duration)
	}

	// 立即再次调用 ReadTags，由于重连冷却保护机制，在 cooldown 期应立即得到 delayed 错误
	values2, err := driver.ReadTags(context.Background(), tags)
	if err != nil {
		t.Fatalf("second ReadTags returned error: %v", err)
	}
	if values2[0].Error == "" || !strings.Contains(values2[0].Error, "opcua reconnect delayed") {
		t.Errorf("expected reconnect delayed error, got: %s", values2[0].Error)
	}
}

func TestBrowseReal(t *testing.T) {
	if os.Getenv("RUN_OPCUA_REAL_TEST") != "true" {
		t.Skip("Skipping OPC UA real server integration test; set RUN_OPCUA_REAL_TEST=true to run")
	}
	cfg := core.DriverConfig{
		Config: map[string]interface{}{
			"endpoint": "opc.tcp://172.15.11.225:53530/OPCUA/SimulationServer",
			"timeout":  "5s",
		},
	}
	driver, err := NewDriver(context.Background(), "test-opcua-real", cfg)
	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}

	impl, ok := driver.(*driverImpl)
	if !ok {
		t.Fatal("expected driver to be *driverImpl")
	}

	ctx := context.Background()
	items, err := impl.Browse(ctx, "ns=5;s=StaticVariables")
	if err != nil {
		t.Fatalf("Browse failed: %v", err)
	}

	if len(items) == 0 {
		t.Fatal("expected non-empty items")
	}

	typeMap := map[string]string{}
	for _, item := range items {
		typeMap[item.ID] = item.Type
		t.Logf("item: %s, type: %s", item.ID, item.Type)
	}

	// 验证类型是否正确映射，且没有发生 EOF
	if typeMap["ns=5;s=Boolean"] != "bool" {
		t.Errorf("expected ns=5;s=Boolean type to be 'bool', got %q", typeMap["ns=5;s=Boolean"])
	}
	if typeMap["ns=5;s=Int16"] != "int16" {
		t.Errorf("expected ns=5;s=Int16 type to be 'int16', got %q", typeMap["ns=5;s=Int16"])
	}
	if typeMap["ns=5;s=Double"] != "float64" {
		t.Errorf("expected ns=5;s=Double type to be 'float64', got %q", typeMap["ns=5;s=Double"])
	}
}
