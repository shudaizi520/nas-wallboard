package integration

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

type contractDefinition struct {
	id       string
	metadata Metadata
	fields   []Field
}

func (d contractDefinition) ID() string                   { return d.id }
func (d contractDefinition) Metadata() Metadata           { return d.metadata }
func (d contractDefinition) Fields() []Field              { return d.fields }
func (d contractDefinition) Validate(config Config) error { return ValidateFields(d.fields, config) }
func (d contractDefinition) Test(context.Context, Config, Secrets) ProbeResult {
	return ProbeResult{Stage: ProbeStageFeature, OK: true}
}
func (d contractDefinition) Capabilities(Config) []Capability             { return nil }
func (d contractDefinition) Collector(Config, Secrets) (Collector, error) { return nil, nil }

func validDefinition(id, name string) contractDefinition {
	return contractDefinition{
		id: id,
		metadata: Metadata{
			Name: name, Description: "用于显示测试数据。", Icon: "app", Category: "服务",
			MinimumRefresh: 15 * time.Second, SingleInstance: true,
		},
		fields: []Field{{Key: "url", Kind: FieldURL, Label: "服务地址", Help: "填写局域网中的服务地址。", Required: true}},
	}
}

func TestRegistryRejectsUnsafeAndDuplicateDefinitions(t *testing.T) {
	registry := NewRegistry()
	for _, id := range []string{"Plex", "../plex", "plex-token", "_plex", ""} {
		if err := registry.Register(validDefinition(id, "媒体服务")); err == nil {
			t.Errorf("Register(%q) unexpectedly succeeded", id)
		}
	}
	if err := registry.Register(validDefinition("plex", "媒体服务")); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(validDefinition("plex", "另一个服务")); err == nil {
		t.Fatal("duplicate registration unexpectedly succeeded")
	}
}

func TestRegistryCatalogIsDeterministicAndRedactsSecretFields(t *testing.T) {
	registry := NewRegistry()
	weather := validDefinition("qweather", "和风天气")
	weather.fields = append(weather.fields, Field{
		Key: "api_key", Kind: FieldSecret, Label: "API 密钥", Help: "在和风天气控制台创建只读密钥。",
		Required: true, Configured: true,
	})
	if err := registry.Register(weather); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(validDefinition("plex", "Plex 媒体")); err != nil {
		t.Fatal(err)
	}

	catalog := registry.Catalog()
	if got := []string{catalog[0].ID, catalog[1].ID}; !reflect.DeepEqual(got, []string{"plex", "qweather"}) {
		t.Fatalf("catalog order = %#v", got)
	}
	secret := catalog[1].Fields[1]
	if secret.Kind != FieldSecret || !secret.Configured {
		t.Fatalf("public secret schema = %#v", secret)
	}
	rendered := strings.ToLower(strings.TrimSpace(secret.Value))
	if rendered != "" || secret.Default != nil {
		t.Fatalf("secret leaked through public schema: %#v", secret)
	}
	weather.fields[1].Configured = false
	if !catalog[1].Fields[1].Configured {
		t.Fatal("catalog was not copied defensively")
	}
}

func TestRegistryRequiresChineseGuidanceSafeDefaultsAndRefreshPolicy(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*contractDefinition)
	}{
		{"English metadata", func(d *contractDefinition) { d.metadata.Name, d.metadata.Description = "Plex", "Media sessions" }},
		{"missing help", func(d *contractDefinition) { d.fields[0].Help = "" }},
		{"bad default", func(d *contractDefinition) { d.fields[0].Default = 42 }},
		{"secret default", func(d *contractDefinition) {
			d.fields[0] = Field{Key: "token", Kind: FieldSecret, Label: "访问令牌", Help: "填写服务生成的只读令牌。", Default: "leak"}
		}},
		{"missing refresh", func(d *contractDefinition) { d.metadata.MinimumRefresh = 0 }},
		{"multiple instances", func(d *contractDefinition) { d.metadata.SingleInstance = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := validDefinition("example", "示例服务")
			test.mutate(&definition)
			if err := NewRegistry().Register(definition); err == nil {
				t.Fatal("invalid definition unexpectedly succeeded")
			}
		})
	}
}

func TestValidateFieldsRejectsUnknownValuesCredentialsAndInvalidKinds(t *testing.T) {
	fields := []Field{
		{Key: "url", Kind: FieldURL, Label: "服务地址", Help: "填写服务地址。", Required: true},
		{Key: "count", Kind: FieldInteger, Label: "数量", Help: "填写显示数量。", Default: 3, Minimum: number(1), Maximum: number(10)},
		{Key: "refresh", Kind: FieldDuration, Label: "刷新间隔", Help: "填写刷新间隔。", Default: "15s"},
		{Key: "enabled", Kind: FieldBoolean, Label: "启用功能", Help: "控制是否启用。", Default: true},
		{Key: "mode", Kind: FieldSelect, Label: "显示模式", Help: "选择显示模式。", Default: "brief", Options: []Option{{Value: "brief", Label: "简洁"}}},
		{Key: "entity", Kind: FieldEntityID, Label: "实体编号", Help: "填写 Home Assistant 实体编号。", Default: "fan.room"},
		{Key: "name", Kind: FieldText, Label: "显示名称", Help: "填写显示名称。", Default: "客厅"},
		{Key: "token", Kind: FieldSecret, Label: "访问令牌", Help: "填写只读访问令牌。"},
	}
	valid := Config{"url": "http://nas.local:32400", "count": 4, "refresh": "30s", "enabled": true, "mode": "brief", "entity": "fan.living_room", "name": "客厅"}
	if err := ValidateFields(fields, valid); err != nil {
		t.Fatal(err)
	}
	for name, config := range map[string]Config{
		"unknown":     cloneConfigWith(valid, "extra", true),
		"credentials": cloneConfigWith(valid, "url", "http://user:pass@nas.local"),
		"range":       cloneConfigWith(valid, "count", 30),
		"duration":    cloneConfigWith(valid, "refresh", "soon"),
		"select":      cloneConfigWith(valid, "mode", "verbose"),
		"entity":      cloneConfigWith(valid, "entity", "living room"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateFields(fields, config); err == nil {
				t.Fatal("invalid config unexpectedly succeeded")
			}
		})
	}
}

func number(value float64) *float64 { return &value }

func cloneConfigWith(source Config, key string, value any) Config {
	result := Config{}
	for current, item := range source {
		result[current] = item
	}
	result[key] = value
	return result
}
