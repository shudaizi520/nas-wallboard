package integration

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"unicode"
)

var safeIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

type Registry struct{ definitions map[string]Definition }

func NewRegistry() *Registry { return &Registry{definitions: map[string]Definition{}} }

func (registry *Registry) Register(definition Definition) error {
	if definition == nil {
		return errors.New("definition is nil")
	}
	if registry.definitions == nil {
		registry.definitions = map[string]Definition{}
	}
	id := definition.ID()
	if !safeIDPattern.MatchString(id) {
		return fmt.Errorf("unsafe integration ID %q", id)
	}
	if _, exists := registry.definitions[id]; exists {
		return fmt.Errorf("integration %q is already registered", id)
	}
	if err := validateDefinition(definition); err != nil {
		return fmt.Errorf("integration %q: %w", id, err)
	}
	registry.definitions[id] = definition
	return nil
}

func (registry *Registry) Definition(id string) (Definition, bool) {
	definition, ok := registry.definitions[id]
	return definition, ok
}

func (registry *Registry) Catalog() []PublicDefinition {
	ids := make([]string, 0, len(registry.definitions))
	for id := range registry.definitions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]PublicDefinition, 0, len(ids))
	for _, id := range ids {
		definition := registry.definitions[id]
		fields := definition.Fields()
		publicFields := make([]PublicField, len(fields))
		for index, field := range fields {
			publicFields[index] = field.public()
		}
		result = append(result, PublicDefinition{
			ID: id, Metadata: cloneMetadata(definition.Metadata()), Fields: publicFields,
			Capabilities: slices.Clone(definition.Capabilities(Config{})),
		})
	}
	return result
}

func validateDefinition(definition Definition) error {
	metadata := definition.Metadata()
	if !containsHan(metadata.Name) || !containsHan(metadata.Description) {
		return errors.New("name and description must contain Chinese guidance")
	}
	if metadata.Icon == "" || !safeIDPattern.MatchString(metadata.Icon) {
		return errors.New("icon must be a safe ID")
	}
	if metadata.Category == "" || !containsHan(metadata.Category) {
		return errors.New("category must be Chinese")
	}
	if metadata.MinimumRefresh <= 0 {
		return errors.New("minimum refresh must be declared")
	}
	if !metadata.SingleInstance {
		return errors.New("the first public release permits one instance per type")
	}
	fields := definition.Fields()
	if len(fields) == 0 {
		return errors.New("at least one field is required")
	}
	seen := map[string]struct{}{}
	for _, field := range fields {
		if !fieldKeyPattern.MatchString(field.Key) {
			return fmt.Errorf("unsafe field key %q", field.Key)
		}
		if _, exists := seen[field.Key]; exists {
			return fmt.Errorf("duplicate field %q", field.Key)
		}
		seen[field.Key] = struct{}{}
		if !containsHan(field.Label) || !containsHan(field.Help) {
			return fmt.Errorf("field %q needs Chinese label and help", field.Key)
		}
		if !slices.Contains([]FieldKind{FieldURL, FieldText, FieldInteger, FieldDuration, FieldBoolean, FieldSelect, FieldEntityID, FieldSecret}, field.Kind) {
			return fmt.Errorf("field %q has unsupported kind", field.Key)
		}
		if field.Kind == FieldSelect {
			if len(field.Options) == 0 {
				return fmt.Errorf("field %q has no options", field.Key)
			}
			for _, option := range field.Options {
				if option.Value == "" || !containsHan(option.Label) {
					return fmt.Errorf("field %q has an invalid option", field.Key)
				}
			}
		}
		if field.Kind == FieldSecret && field.Default != nil {
			return fmt.Errorf("secret field %q cannot have a default", field.Key)
		}
		if field.Default != nil {
			if err := validateValue(field, field.Default); err != nil {
				return fmt.Errorf("field %q has invalid default: %w", field.Key, err)
			}
		}
	}
	return nil
}

func containsHan(value string) bool {
	for _, character := range value {
		if unicode.Is(unicode.Han, character) {
			return true
		}
	}
	return false
}

func cloneMetadata(metadata Metadata) Metadata {
	metadata.DiscoveryHints = slices.Clone(metadata.DiscoveryHints)
	return metadata
}
