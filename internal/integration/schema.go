package integration

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

type FieldKind string

const (
	FieldURL      FieldKind = "url"
	FieldText     FieldKind = "text"
	FieldInteger  FieldKind = "integer"
	FieldDuration FieldKind = "duration"
	FieldBoolean  FieldKind = "boolean"
	FieldSelect   FieldKind = "select"
	FieldEntityID FieldKind = "entity_id"
	FieldSecret   FieldKind = "secret"
)

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type Field struct {
	Key         string    `json:"key"`
	Kind        FieldKind `json:"kind"`
	Label       string    `json:"label"`
	Help        string    `json:"help"`
	Advanced    bool      `json:"advanced,omitempty"`
	Placeholder string    `json:"placeholder,omitempty"`
	Required    bool      `json:"required,omitempty"`
	Default     any       `json:"default,omitempty"`
	Options     []Option  `json:"options,omitempty"`
	Minimum     *float64  `json:"minimum,omitempty"`
	Maximum     *float64  `json:"maximum,omitempty"`
	Configured  bool      `json:"-"`
}

// PublicField never contains a secret value or secret default. Configured is
// the only credential state exposed to the browser.
type PublicField struct {
	Key         string    `json:"key"`
	Kind        FieldKind `json:"kind"`
	Label       string    `json:"label"`
	Help        string    `json:"help"`
	Advanced    bool      `json:"advanced,omitempty"`
	Placeholder string    `json:"placeholder,omitempty"`
	Required    bool      `json:"required,omitempty"`
	Default     any       `json:"default,omitempty"`
	Options     []Option  `json:"options,omitempty"`
	Minimum     *float64  `json:"minimum,omitempty"`
	Maximum     *float64  `json:"maximum,omitempty"`
	Configured  bool      `json:"configured,omitempty"`
	Value       string    `json:"-"`
}

var (
	fieldKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	entityIDPattern = regexp.MustCompile(`^[a-z0-9_]+\.[A-Za-z0-9_./-]+$`)
)

func (field Field) public() PublicField {
	result := PublicField{
		Key: field.Key, Kind: field.Kind, Label: field.Label, Help: field.Help, Advanced: field.Advanced,
		Placeholder: field.Placeholder, Required: field.Required, Options: slices.Clone(field.Options),
		Minimum: cloneNumber(field.Minimum), Maximum: cloneNumber(field.Maximum), Configured: field.Configured,
	}
	if field.Kind != FieldSecret {
		result.Default = field.Default
	}
	return result
}

func cloneNumber(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func ValidateFields(fields []Field, config Config) error {
	definitions := make(map[string]Field, len(fields))
	for _, field := range fields {
		definitions[field.Key] = field
	}
	for key, value := range config {
		field, ok := definitions[key]
		if !ok {
			return fmt.Errorf("unknown field %q", key)
		}
		if field.Kind == FieldSecret {
			return fmt.Errorf("secret field %q must not be stored in config", key)
		}
		if err := validateValue(field, value); err != nil {
			return fmt.Errorf("field %q: %w", key, err)
		}
	}
	for _, field := range fields {
		if field.Kind == FieldSecret || !field.Required {
			continue
		}
		value, ok := config[field.Key]
		if !ok || value == nil || (isStringKind(field.Kind) && strings.TrimSpace(value.(string)) == "") {
			return fmt.Errorf("field %q is required", field.Key)
		}
	}
	return nil
}

func validateValue(field Field, value any) error {
	switch field.Kind {
	case FieldURL:
		raw, ok := value.(string)
		if !ok {
			return errors.New("must be a URL")
		}
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || parsed.Host == "" || !slices.Contains([]string{"http", "https", "ws", "wss"}, parsed.Scheme) {
			return errors.New("must be an absolute HTTP or WebSocket URL")
		}
		if parsed.User != nil {
			return errors.New("URL credentials are not allowed")
		}
	case FieldText:
		if _, ok := value.(string); !ok {
			return errors.New("must be text")
		}
	case FieldInteger:
		number, ok := integerValue(value)
		if !ok {
			return errors.New("must be an integer")
		}
		if field.Minimum != nil && number < *field.Minimum {
			return fmt.Errorf("must be at least %v", *field.Minimum)
		}
		if field.Maximum != nil && number > *field.Maximum {
			return fmt.Errorf("must be at most %v", *field.Maximum)
		}
	case FieldDuration:
		raw, ok := value.(string)
		if !ok {
			return errors.New("must be a duration")
		}
		if duration, err := time.ParseDuration(raw); err != nil || duration <= 0 {
			return errors.New("must be a positive duration")
		}
	case FieldBoolean:
		if _, ok := value.(bool); !ok {
			return errors.New("must be true or false")
		}
	case FieldSelect:
		raw, ok := value.(string)
		if !ok {
			return errors.New("must be a selection")
		}
		if !slices.ContainsFunc(field.Options, func(option Option) bool { return option.Value == raw }) {
			return errors.New("is not an allowed option")
		}
	case FieldEntityID:
		raw, ok := value.(string)
		if !ok || !entityIDPattern.MatchString(raw) {
			return errors.New("must be a valid entity ID")
		}
	case FieldSecret:
		return errors.New("secret values must be stored separately")
	default:
		return fmt.Errorf("unsupported field kind %q", field.Kind)
	}
	return nil
}

func integerValue(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case float64:
		return number, !math.IsNaN(number) && !math.IsInf(number, 0) && math.Trunc(number) == number
	default:
		return 0, false
	}
}

func isStringKind(kind FieldKind) bool {
	return kind == FieldURL || kind == FieldText || kind == FieldDuration || kind == FieldSelect || kind == FieldEntityID
}
