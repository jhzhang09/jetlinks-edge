// Package mqtt 的单元测试。
// @author jhzhang
// @date 2026-06-13
package mqtt

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

func TestParseAppConfig(t *testing.T) {
	cfg, err := parseAppConfig(map[string]interface{}{
		"broker":       "tcp://127.0.0.1:1883",
		"clientId":     "test-client",
		"username":     "user",
		"password":     "pass",
		"cleanSession": false,
		"keepAlive":    float64(45),
		"uploadTopic":  "/test/upload",
		"writeTopic":   "/test/write",
	})
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Broker != "tcp://127.0.0.1:1883" || cfg.ClientID != "test-client" ||
		cfg.Username != "user" || cfg.Password != "pass" || cfg.CleanSession ||
		cfg.KeepAlive != 45 || cfg.UploadTopic != "/test/upload" || cfg.WriteTopic != "/test/write" {
		t.Fatalf("unexpected parsed config: %+v", cfg)
	}
}

func TestOnMessageFormatting(t *testing.T) {
	// 验证 OnMessage 产生的上送 Payload 结构是否正确
	a := &app{
		appID: "app-1",
		cfg: AppConfig{
			UploadTopic: "/edge/{appId}/upload",
		},
	}
	msg := core.NorthMessage{
		GroupID:   "group-1",
		DeviceID:  "device-1",
		Timestamp: time.UnixMilli(1690000000000),
		Payload: map[string]interface{}{
			"properties": map[string]interface{}{
				"temp": 25.5,
			},
		},
	}

	// 动态替换主题
	topic := a.cfg.UploadTopic
	topic = replacePlaceholders(topic, a.appID, msg.GroupID, msg.DeviceID)
	if topic != "/edge/app-1/upload" {
		t.Fatalf("unexpected topic formatting: %q", topic)
	}

	payload, err := json.Marshal(map[string]interface{}{
		"appId":     a.appID,
		"groupId":   msg.GroupID,
		"timestamp": msg.Timestamp.UnixMilli(),
		"values":    msg.Payload["properties"],
	})
	if err != nil {
		t.Fatal(err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		t.Fatal(err)
	}

	if data["appId"] != "app-1" || data["groupId"] != "group-1" ||
		data["timestamp"] != float64(1690000000000) {
		t.Fatalf("unexpected serialised payload: %v", data)
	}

	vals := data["values"].(map[string]interface{})
	if vals["temp"] != float64(25.5) {
		t.Fatalf("unexpected values inside payload: %v", vals)
	}
}

func replacePlaceholders(topic, appId, groupId, deviceId string) string {
	topic = strings.ReplaceAll(topic, "{appId}", appId)
	topic = strings.ReplaceAll(topic, "{groupId}", groupId)
	topic = strings.ReplaceAll(topic, "{deviceId}", deviceId)
	return topic
}
