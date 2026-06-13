package jetlinksmqtt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

func TestHandleCommandUsesInjectedExecutor(t *testing.T) {
	var received core.NorthCommand
	a := &app{
		cfg: AppConfig{ProductID: "gateway-product", DeviceID: "gateway-device"},
		commandExecutor: func(_ context.Context, cmd core.NorthCommand) (core.NorthCommandReply, error) {
			received = cmd
			return core.NorthCommandReply{ID: cmd.ID, Code: 0}, nil
		},
	}
	cmd := core.NorthCommand{ID: "message-1", GroupID: "group-1", Type: "read-property"}

	a.handleCommand(context.Background(), cmd)

	if received.ID != cmd.ID || received.GroupID != cmd.GroupID {
		t.Fatalf("executor received %+v, want %+v", received, cmd)
	}
}

func TestDecodeCommandSupportsOfficialReadAndWritePayloads(t *testing.T) {
	read, err := decodeCommand("child-1", "properties/read", []byte(`{"messageId":"r1","properties":["temperature"]}`))
	if err != nil {
		t.Fatal(err)
	}
	properties, ok := read.Payload["properties"].([]string)
	if !ok || len(properties) != 1 || properties[0] != "temperature" {
		t.Fatalf("unexpected read properties: %#v", read.Payload["properties"])
	}

	write, err := decodeCommand("child-1", "properties/write", []byte(`{"messageId":"w1","properties":{"setpoint":55}}`))
	if err != nil {
		t.Fatal(err)
	}
	values, ok := write.Payload["properties"].(map[string]interface{})
	if !ok || values["setpoint"] != float64(55) {
		t.Fatalf("unexpected write properties: %#v", write.Payload["properties"])
	}
}

func TestParseAppConfigKeepsNumericAndBooleanOptions(t *testing.T) {
	cfg, err := parseAppConfig(map[string]interface{}{
		"broker":         "tcp://127.0.0.1:1883",
		"productId":      "gateway-product",
		"deviceId":       "gateway-device",
		"username":       "token",
		"password":       "secret",
		"cleanSession":   false,
		"keepAlive":      float64(60),
		"timestampDelta": float64(240),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CleanSession || cfg.KeepAlive != 60 || cfg.TimestampDelta != 240 {
		t.Fatalf("unexpected parsed config: %+v", cfg)
	}
}

func TestBoundDevicesSnapshotIncludesSouthDeviceStatus(t *testing.T) {
	a := &app{
		groups: map[string]*core.Group{
			deviceKey("product-1", "device-1"): {
				ID:      "group-1",
				Name:    "OPC UA",
				Driver:  "opc-ua",
				Enabled: true,
				Device:  core.DeviceConfig{ProductID: "product-1", DeviceID: "device-1"},
				Tags:    []core.Tag{{ID: "tag-1"}},
			},
		},
		statusProvider: func(groupID string) (core.DriverStatus, bool) {
			if groupID != "group-1" {
				return core.DriverStatus{}, false
			}
			return core.DriverStatus{Connected: true}, true
		},
	}

	boundDevices, info := a.boundDevicesSnapshot()
	if len(boundDevices) != 1 || boundDevices[0] != "device-1" {
		t.Fatalf("boundDevices = %#v, want [device-1]", boundDevices)
	}
	if len(info) != 1 || info[0].Status != southDeviceStatusOnline {
		t.Fatalf("boundDevicesInfo = %+v, want status %q", info, southDeviceStatusOnline)
	}

	payload, err := json.Marshal(info[0])
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]interface{}
	if err := json.Unmarshal(payload, &encoded); err != nil {
		t.Fatal(err)
	}
	if encoded["status"] != southDeviceStatusOnline {
		t.Fatalf("encoded status = %#v, want %q", encoded["status"], southDeviceStatusOnline)
	}
}
