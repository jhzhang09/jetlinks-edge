package core

import (
	"fmt"
	"sort"
)

// ConfigFieldType 描述前端可动态渲染的配置字段类型。
type ConfigFieldType string

const (
	ConfigFieldText     ConfigFieldType = "text"
	ConfigFieldPassword ConfigFieldType = "password"
	ConfigFieldNumber   ConfigFieldType = "number"
	ConfigFieldBoolean  ConfigFieldType = "boolean"
	ConfigFieldSelect   ConfigFieldType = "select"
	ConfigFieldTextarea ConfigFieldType = "textarea"
)

// ConfigOption 是 select 配置字段的可选项。
type ConfigOption struct {
	Label string      `json:"label"`
	Value interface{} `json:"value"`
}

// ConfigField 描述一个插件私有配置字段及其基础校验规则。
type ConfigField struct {
	Key          string          `json:"key"`
	Label        string          `json:"label"`
	Type         ConfigFieldType `json:"type"`
	Required     bool            `json:"required,omitempty"`
	DefaultValue interface{}     `json:"defaultValue,omitempty"`
	Placeholder  string          `json:"placeholder,omitempty"`
	Description  string          `json:"description,omitempty"`
	Min          *float64        `json:"min,omitempty"`
	Max          *float64        `json:"max,omitempty"`
	Step         *float64        `json:"step,omitempty"`
	Options      []ConfigOption  `json:"options,omitempty"`
}

// ExtensionDescriptor 描述一个编译期插件及其动态配置能力。
type ExtensionDescriptor struct {
	Type             string        `json:"type"`
	Name             string        `json:"name"`
	Description      string        `json:"description,omitempty"`
	Version          string        `json:"version"`
	Capabilities     []string      `json:"capabilities,omitempty"`
	ConnectionSchema []ConfigField `json:"connectionSchema,omitempty"`
	TagSchema        []ConfigField `json:"tagSchema,omitempty"`
	ConfigSchema     []ConfigField `json:"configSchema,omitempty"`
}

// ApplyConfigDefaults 使用 Schema 默认值补齐未配置字段，保留 Schema 未声明的兼容字段。
func ApplyConfigDefaults(schema []ConfigField, config map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(config)+len(schema))
	for key, value := range config {
		out[key] = value
	}
	for _, field := range schema {
		if _, exists := out[field.Key]; !exists && field.DefaultValue != nil {
			out[field.Key] = field.DefaultValue
		}
	}
	return out
}

// ValidateConfig 按 Schema 校验插件私有配置。
func ValidateConfig(schema []ConfigField, config map[string]interface{}) error {
	for _, field := range schema {
		value, exists := config[field.Key]
		if !exists || value == nil || isEmptyString(value) {
			if field.Required {
				return fmt.Errorf("config.%s is required", field.Key)
			}
			continue
		}
		switch field.Type {
		case ConfigFieldText, ConfigFieldPassword, ConfigFieldTextarea:
			if _, ok := value.(string); !ok {
				return fmt.Errorf("config.%s must be a string", field.Key)
			}
		case ConfigFieldNumber:
			number, ok := asFloat64(value)
			if !ok {
				return fmt.Errorf("config.%s must be a number", field.Key)
			}
			if field.Min != nil && number < *field.Min {
				return fmt.Errorf("config.%s must be greater than or equal to %v", field.Key, *field.Min)
			}
			if field.Max != nil && number > *field.Max {
				return fmt.Errorf("config.%s must be less than or equal to %v", field.Key, *field.Max)
			}
		case ConfigFieldBoolean:
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("config.%s must be a boolean", field.Key)
			}
		case ConfigFieldSelect:
			if !containsOption(field.Options, value) {
				return fmt.Errorf("config.%s has unsupported value", field.Key)
			}
		default:
			return fmt.Errorf("config.%s has unsupported field type %q", field.Key, field.Type)
		}
	}
	return nil
}

func isEmptyString(value interface{}) bool {
	text, ok := value.(string)
	return ok && text == ""
}

func containsOption(options []ConfigOption, value interface{}) bool {
	for _, option := range options {
		if fmt.Sprint(option.Value) == fmt.Sprint(value) {
			return true
		}
	}
	return false
}

func asFloat64(value interface{}) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), true
	case float32:
		return float64(number), true
	case float64:
		return number, true
	default:
		return 0, false
	}
}

func sortDescriptors(descriptors []ExtensionDescriptor) {
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].Type < descriptors[j].Type
	})
}
