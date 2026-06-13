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

func TestTagConfigKeepsLegacyFieldsCompatible(t *testing.T) {
	tag := Tag{Address: "40002", ByteOrder: "BA", Bit: 3}
	tag.MarshalConfig()
	if tag.Config["address"] != "40002" || tag.Config["byteOrder"] != "BA" || tag.Config["bit"] != 3 {
		t.Fatalf("unexpected dynamic config: %#v", tag.Config)
	}

	tag.Config = map[string]interface{}{"address": "40003", "byteOrder": "AB", "bit": float64(4)}
	tag.MarshalConfig()
	if tag.Address != "40003" || tag.ByteOrder != "AB" || tag.Bit != 4 {
		t.Fatalf("legacy fields not updated: %#v", tag)
	}
}
