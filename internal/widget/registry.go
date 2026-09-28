package widget

import (
	"errors"
	"regexp"
	"sort"

	"example.com/nas-wallboard/internal/integration"
)

var safeID = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)
var safeInstanceID = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)

type Registry struct{ definitions map[string]Definition }

func NewRegistry() *Registry { return &Registry{definitions: map[string]Definition{}} }
func (registry *Registry) Register(definition Definition) error {
	if !safeID.MatchString(definition.ID) || definition.Label == "" {
		return errors.New("invalid widget definition")
	}
	if definition.Placement != PlacementMetric && definition.Placement != PlacementActivity {
		return errors.New("invalid widget placement")
	}
	if definition.Visibility != VisibilityAlways && definition.Visibility != VisibilityNonEmpty && definition.Visibility != VisibilityWarningOnly {
		return errors.New("invalid widget visibility")
	}
	if _, ok := registry.definitions[definition.ID]; ok {
		return errors.New("duplicate widget definition")
	}
	definition.Defaults = cloneMap(definition.Defaults)
	definition.Fields = append([]integration.Field(nil), definition.Fields...)
	registry.definitions[definition.ID] = definition
	return nil
}
func (registry *Registry) Definition(id string) (Definition, bool) {
	value, ok := registry.definitions[id]
	value.Defaults = cloneMap(value.Defaults)
	value.Fields = append([]integration.Field(nil), value.Fields...)
	return value, ok
}
func (registry *Registry) Catalog(capabilities map[string]bool) []Definition {
	ids := make([]string, 0, len(registry.definitions))
	for id := range registry.definitions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := []Definition{}
	for _, id := range ids {
		value := registry.definitions[id]
		if value.IntegrationType != "" && !capabilities[value.IntegrationType] {
			continue
		}
		value.Defaults = cloneMap(value.Defaults)
		value.Fields = append([]integration.Field(nil), value.Fields...)
		result = append(result, value)
	}
	return result
}
