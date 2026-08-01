package core

import (
	"context"
	"testing"
)

func floatPointer(value float64) *float64 {
	return &value
}

func TestApplyConfigDefaultsPreservesUnknownFields(t *testing.T) {
	config := ApplyConfigDefaults([]ConfigField{
		{Key: "port", Type: ConfigFieldNumber, DefaultValue: 502},
	}, map[string]interface{}{"custom": "kept"})

	if config["port"] != 502 || config["custom"] != "kept" {
		t.Fatalf("unexpected config: %#v", config)
	}
}

func TestValidateConfig(t *testing.T) {
	schema := []ConfigField{
		{Key: "host", Type: ConfigFieldText, Required: true},
		{Key: "port", Type: ConfigFieldNumber, Min: floatPointer(1), Max: floatPointer(65535)},
		{Key: "mode", Type: ConfigFieldSelect, Options: []ConfigOption{{Label: "TCP", Value: "tcp"}}},
	}
	if err := ValidateConfig(schema, map[string]interface{}{"host": "127.0.0.1", "port": float64(502), "mode": "tcp"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfig(schema, map[string]interface{}{"host": "127.0.0.1", "port": float64(70000), "mode": "tcp"}); err == nil {
		t.Fatal("expected port validation error")
	}
	if err := ValidateConfig(schema, map[string]interface{}{"host": map[string]interface{}{}, "mode": "tcp"}); err == nil {
		t.Fatal("expected composite value validation error")
	}
}

func TestRegistryReturnsExtensionDescriptor(t *testing.T) {
	registry := NewDriverRegistry()
	registry.RegisterExtension(ExtensionDescriptor{
		Type:    "test-driver",
		Name:    "Test Driver",
		Version: "1.0.0",
	}, func(_ context.Context, _ string, _ DriverConfig) (SouthDriver, error) {
		return nil, nil
	})

	descriptors := registry.Descriptors()
	if len(descriptors) != 1 || descriptors[0].Type != "test-driver" {
		t.Fatalf("unexpected descriptors: %#v", descriptors)
	}
}

type lifecycleOnlyDriver struct{}

func (lifecycleOnlyDriver) Name() string                  { return "event-driver" }
func (lifecycleOnlyDriver) Connect(context.Context) error { return nil }
func (lifecycleOnlyDriver) Disconnect() error             { return nil }
func (lifecycleOnlyDriver) Status() DriverStatus          { return DriverStatus{Connected: true} }

func TestRegistrySupportsLifecycleOnlyDriverWithoutBreakingLegacyCreate(t *testing.T) {
	registry := NewDriverRegistry()
	registry.RegisterLifecycleExtension(ExtensionDescriptor{
		Type: "event-driver", Name: "Event Driver", Version: "1.0.0",
	}, func(context.Context, string, DriverConfig) (DriverLifecycle, error) {
		return lifecycleOnlyDriver{}, nil
	})
	driver, err := registry.CreateLifecycle(context.Background(), "event-driver", DriverConfig{})
	if err != nil || driver.Name() != "event-driver" {
		t.Fatalf("create lifecycle driver: driver=%#v err=%v", driver, err)
	}
	if _, err := registry.Create(context.Background(), "event-driver", DriverConfig{}); err == nil {
		t.Fatal("legacy Create must reject a driver without polling capabilities")
	}
}

func TestTagConfigKeepsLegacyFieldsCompatible(t *testing.T) {
	tag := Tag{Address: "40002", ByteOrder: "BA", Bit: 3}
	if err := tag.MarshalConfig(); err != nil {
		t.Fatal(err)
	}
	if tag.Config["address"] != "40002" || tag.Config["byteOrder"] != "BA" || tag.Config["bit"] != 3 {
		t.Fatalf("unexpected dynamic config: %#v", tag.Config)
	}

	tag.Config = map[string]interface{}{"address": "40003", "byteOrder": "AB", "bit": float64(4)}
	if err := tag.MarshalConfig(); err != nil {
		t.Fatal(err)
	}
	if tag.Address != "40003" || tag.ByteOrder != "AB" || tag.Bit != 4 {
		t.Fatalf("legacy fields not updated: %#v", tag)
	}
}

func TestSensitiveConfigMergeAndRedaction(t *testing.T) {
	schema := []ConfigField{
		{Key: "username", Type: ConfigFieldText},
		{Key: "password", Type: ConfigFieldPassword},
	}
	existing := map[string]interface{}{"username": "edge", "password": "secret"}
	merged := MergeSensitiveConfig(schema, map[string]interface{}{
		"username": "edge-2",
		"password": MaskedSecret,
	}, existing)
	if merged["password"] != "secret" || merged["username"] != "edge-2" {
		t.Fatalf("unexpected merged config: %#v", merged)
	}
	redacted := RedactSensitiveConfig(schema, existing)
	if redacted["password"] != MaskedSecret || existing["password"] != "secret" {
		t.Fatalf("redaction must not mutate source: redacted=%#v source=%#v", redacted, existing)
	}
	if err := ValidateConfig(schema, map[string]interface{}{"password": MaskedSecret}); err == nil {
		t.Fatal("masked password placeholder must not be accepted as an actual secret")
	}
	invalid := MergeSensitiveConfig(schema, map[string]interface{}{"password": 123}, existing)
	if err := ValidateConfig(schema, invalid); err == nil {
		t.Fatal("invalid password type must not be replaced by the existing secret")
	}
}

func TestMarshalConfigReturnsEncodingError(t *testing.T) {
	group := Group{Config: map[string]interface{}{"invalid": func() {}}}
	if err := group.MarshalConfig(); err == nil {
		t.Fatal("expected unsupported config value to fail encoding")
	}
}
