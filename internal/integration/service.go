package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"example.com/nas-wallboard/internal/persist"
)

const MaskedSecret = "********"

var (
	ErrUnknownIntegration  = errors.New("unknown integration")
	ErrInstanceNotFound    = errors.New("integration instance not found")
	ErrAlreadyConfigured   = errors.New("integration type is already configured")
	ErrProbeFailed         = errors.New("integration connection test failed")
	ErrRequiredIntegration = errors.New("required integration cannot be disabled or removed")
)

type Candidate struct {
	Type    string
	Config  Config
	Secrets Secrets
}

type InstanceView struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Enabled bool            `json:"enabled"`
	Config  Config          `json:"config"`
	Secrets map[string]bool `json:"secrets"`
}

type DiscoveredApp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Discovery struct {
	Apps []DiscoveredApp `json:"apps"`
}

type CatalogEntry struct {
	PublicDefinition
	Detected   bool `json:"detected"`
	Configured bool `json:"configured"`
}

type ServiceOptions struct {
	ProbeTimeout time.Duration
	OnChange     func(context.Context, []persist.Integration, []persist.Integration) error
	Initialize   func(*persist.State, persist.Integration) error
}

type Service struct {
	mu         sync.Mutex
	registry   *Registry
	state      *persist.Store
	secrets    *persist.SecretStore
	timeout    time.Duration
	onChange   func(context.Context, []persist.Integration, []persist.Integration) error
	initialize func(*persist.State, persist.Integration) error
}

func NewService(registry *Registry, state *persist.Store, secrets *persist.SecretStore, options ServiceOptions) *Service {
	timeout := options.ProbeTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Service{registry: registry, state: state, secrets: secrets, timeout: timeout, onChange: options.OnChange, initialize: options.Initialize}
}

func (service *Service) Catalog(discovery Discovery) []CatalogEntry {
	service.mu.Lock()
	defer service.mu.Unlock()
	public := service.registry.Catalog()
	instances := service.state.Snapshot().Integrations
	result := make([]CatalogEntry, len(public))
	for index, definition := range public {
		entry := CatalogEntry{PublicDefinition: definition, Detected: discoveryMatches(definition.Metadata.DiscoveryHints, discovery.Apps)}
		for _, instance := range instances {
			if instance.Type != definition.ID {
				continue
			}
			entry.Configured = true
			for fieldIndex := range entry.Fields {
				if entry.Fields[fieldIndex].Kind == FieldSecret {
					_, entry.Fields[fieldIndex].Configured = instance.SecretRefs[entry.Fields[fieldIndex].Key]
				}
			}
			break
		}
		result[index] = entry
	}
	return result
}

func (service *Service) Instances() []InstanceView {
	service.mu.Lock()
	defer service.mu.Unlock()
	return publicInstances(service.state.Snapshot().Integrations)
}

func (service *Service) ValidateRestored(store *persist.Store, secrets *persist.SecretStore) error {
	validator := NewService(service.registry, store, secrets, ServiceOptions{})
	ids, types := map[string]bool{}, map[string]bool{}
	for _, item := range store.Snapshot().Integrations {
		if item.ID == "" || ids[item.ID] || types[item.Type] {
			return errors.New("duplicate restored integration")
		}
		ids[item.ID], types[item.Type] = true, true
		supplied := Secrets{}
		for key, ref := range item.SecretRefs {
			value, err := secrets.Read(ref)
			if err != nil {
				return err
			}
			supplied[key] = value
		}
		if _, err := validator.validateCandidate(Candidate{Type: item.Type, Config: Config(item.Config), Secrets: supplied}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) TestCandidate(ctx context.Context, candidate Candidate) (ProbeResult, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	definition, err := service.validateCandidate(candidate, nil)
	if err != nil {
		return ProbeResult{}, err
	}
	effective, err := service.effectiveSecrets(definition, candidate.Secrets, nil)
	if err != nil {
		return ProbeResult{}, err
	}
	return service.probe(ctx, definition, candidate.Config, effective)
}

func (service *Service) Create(ctx context.Context, candidate Candidate) (InstanceView, ProbeResult, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	before := service.state.Snapshot().Integrations
	for _, instance := range before {
		if instance.Type == candidate.Type {
			return InstanceView{}, ProbeResult{}, ErrAlreadyConfigured
		}
	}
	definition, err := service.validateCandidate(candidate, nil)
	if err != nil {
		return InstanceView{}, ProbeResult{}, err
	}
	instanceID, err := randomInstanceID()
	if err != nil {
		return InstanceView{}, ProbeResult{}, err
	}
	return service.saveCandidate(ctx, definition, instanceID, nil, candidate, before)
}

func (service *Service) Update(ctx context.Context, instanceID string, candidate Candidate) (InstanceView, ProbeResult, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	before := service.state.Snapshot().Integrations
	current, ok := findIntegration(before, instanceID)
	if !ok {
		return InstanceView{}, ProbeResult{}, ErrInstanceNotFound
	}
	if candidate.Type == "" {
		candidate.Type = current.Type
	}
	if candidate.Type != current.Type {
		return InstanceView{}, ProbeResult{}, errors.New("integration type cannot be changed")
	}
	definition, err := service.validateCandidate(candidate, &current)
	if err != nil {
		return InstanceView{}, ProbeResult{}, err
	}
	return service.saveCandidate(ctx, definition, instanceID, &current, candidate, before)
}

func (service *Service) Enable(ctx context.Context, instanceID string) error {
	return service.setEnabled(ctx, instanceID, true)
}
func (service *Service) Disable(ctx context.Context, instanceID string) error {
	return service.setEnabled(ctx, instanceID, false)
}

func (service *Service) setEnabled(ctx context.Context, instanceID string, enabled bool) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	before := service.state.Snapshot().Integrations
	current, ok := findIntegration(before, instanceID)
	if !ok {
		return ErrInstanceNotFound
	}
	definition, _ := service.registry.Definition(current.Type)
	if !enabled && definition.Metadata().Required {
		return ErrRequiredIntegration
	}
	if current.Enabled == enabled {
		return nil
	}
	if err := service.state.Update(func(state *persist.State) error {
		for index := range state.Integrations {
			if state.Integrations[index].ID == instanceID {
				state.Integrations[index].Enabled = enabled
				return nil
			}
		}
		return ErrInstanceNotFound
	}); err != nil {
		return err
	}
	after := service.state.Snapshot().Integrations
	return service.notify(ctx, before, after)
}

func (service *Service) Remove(ctx context.Context, instanceID string) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	before := service.state.Snapshot().Integrations
	current, ok := findIntegration(before, instanceID)
	if !ok {
		return ErrInstanceNotFound
	}
	definition, _ := service.registry.Definition(current.Type)
	if definition.Metadata().Required {
		return ErrRequiredIntegration
	}
	if err := service.state.Update(func(state *persist.State) error {
		state.Integrations = slices.DeleteFunc(state.Integrations, func(item persist.Integration) bool { return item.ID == instanceID })
		return nil
	}); err != nil {
		return err
	}
	after := service.state.Snapshot().Integrations
	if err := service.collect(after); err != nil {
		return err
	}
	return service.notify(ctx, before, after)
}

func (service *Service) saveCandidate(ctx context.Context, definition Definition, instanceID string, current *persist.Integration, candidate Candidate, before []persist.Integration) (InstanceView, ProbeResult, error) {
	effective, err := service.effectiveSecrets(definition, candidate.Secrets, current)
	if err != nil {
		return InstanceView{}, ProbeResult{}, err
	}
	refs := map[string]persist.SecretRef{}
	if current != nil {
		for key, ref := range current.SecretRefs {
			refs[key] = ref
		}
	}
	discards := []func() error{}
	committed := false
	defer func() {
		if committed {
			return
		}
		for _, discard := range discards {
			_ = discard()
		}
	}()
	for _, field := range definition.Fields() {
		if field.Kind != FieldSecret {
			continue
		}
		value, supplied := candidate.Secrets[field.Key]
		if !supplied || string(value) == MaskedSecret {
			continue
		}
		ref, discard, stageErr := service.secrets.Stage(instanceID, field.Key, value)
		if stageErr != nil {
			return InstanceView{}, ProbeResult{}, stageErr
		}
		refs[field.Key] = ref
		discards = append(discards, discard)
	}
	probe, err := service.probe(ctx, definition, candidate.Config, effective)
	if err != nil {
		return InstanceView{}, probe, err
	}
	next := persist.Integration{ID: instanceID, Type: candidate.Type, Enabled: true, Config: cloneConfig(candidate.Config), SecretRefs: refs}
	if current != nil {
		next.Enabled = current.Enabled
	}
	if err := service.state.Update(func(state *persist.State) error {
		if current == nil {
			state.Integrations = append(state.Integrations, next)
			if service.initialize != nil {
				return service.initialize(state, next)
			}
			return nil
		}
		for index := range state.Integrations {
			if state.Integrations[index].ID == instanceID {
				state.Integrations[index] = next
				return nil
			}
		}
		return ErrInstanceNotFound
	}); err != nil {
		return InstanceView{}, probe, err
	}
	committed = true
	after := service.state.Snapshot().Integrations
	if err := service.collect(after); err != nil {
		return InstanceView{}, probe, err
	}
	if err := service.notify(ctx, before, after); err != nil {
		return publicInstance(next), probe, err
	}
	return publicInstance(next), probe, nil
}

func (service *Service) validateCandidate(candidate Candidate, current *persist.Integration) (Definition, error) {
	definition, ok := service.registry.Definition(candidate.Type)
	if !ok {
		return nil, ErrUnknownIntegration
	}
	if err := definition.Validate(candidate.Config); err != nil {
		return nil, err
	}
	knownSecrets := map[string]Field{}
	for _, field := range definition.Fields() {
		if field.Kind == FieldSecret {
			knownSecrets[field.Key] = field
		}
	}
	for key := range candidate.Secrets {
		if _, ok := knownSecrets[key]; !ok {
			return nil, fmt.Errorf("unknown secret field %q", key)
		}
	}
	for key, field := range knownSecrets {
		value, supplied := candidate.Secrets[key]
		configured := false
		if current != nil {
			_, configured = current.SecretRefs[key]
		}
		if supplied && string(value) == MaskedSecret && !configured {
			return nil, fmt.Errorf("secret field %q is not configured", key)
		}
		if field.Required && (!supplied || len(value) == 0) && !configured {
			return nil, fmt.Errorf("secret field %q is required", key)
		}
	}
	return definition, nil
}

func (service *Service) effectiveSecrets(definition Definition, supplied Secrets, current *persist.Integration) (Secrets, error) {
	result := Secrets{}
	for _, field := range definition.Fields() {
		if field.Kind != FieldSecret {
			continue
		}
		value, ok := supplied[field.Key]
		if ok && string(value) != MaskedSecret {
			result[field.Key] = slices.Clone(value)
			continue
		}
		if current != nil {
			if ref, configured := current.SecretRefs[field.Key]; configured {
				stored, err := service.secrets.Read(ref)
				if err != nil {
					return nil, fmt.Errorf("read secret %q: %w", field.Key, err)
				}
				result[field.Key] = stored
			}
		}
	}
	return result, nil
}

func (service *Service) probe(ctx context.Context, definition Definition, config Config, secrets Secrets) (ProbeResult, error) {
	probeCtx, cancel := context.WithTimeout(ctx, service.timeout)
	defer cancel()
	result := definition.Test(probeCtx, cloneConfig(config), cloneSecrets(secrets))
	if probeCtx.Err() != nil && ctx.Err() == nil {
		if result.Stage == "" {
			result.Stage = ProbeStageTCP
		}
		if result.Message == "" {
			result.Message = "连接测试超时"
		}
		result.OK = false
	}
	if !result.OK {
		return result, ErrProbeFailed
	}
	return result, nil
}

func (service *Service) collect(integrations []persist.Integration) error {
	live := map[persist.SecretRef]struct{}{}
	for _, instance := range integrations {
		for _, ref := range instance.SecretRefs {
			live[ref] = struct{}{}
		}
	}
	return service.secrets.Collect(live)
}

func (service *Service) notify(ctx context.Context, before, after []persist.Integration) error {
	if service.onChange == nil {
		return nil
	}
	return service.onChange(ctx, before, after)
}

func publicInstances(values []persist.Integration) []InstanceView {
	result := make([]InstanceView, len(values))
	for index, value := range values {
		result[index] = publicInstance(value)
	}
	return result
}

func publicInstance(value persist.Integration) InstanceView {
	configured := map[string]bool{}
	for key := range value.SecretRefs {
		configured[key] = true
	}
	return InstanceView{ID: value.ID, Type: value.Type, Enabled: value.Enabled, Config: cloneConfig(value.Config), Secrets: configured}
}

func findIntegration(values []persist.Integration, id string) (persist.Integration, bool) {
	for _, value := range values {
		if value.ID == id {
			return value, true
		}
	}
	return persist.Integration{}, false
}

func cloneConfig(value Config) Config {
	result := Config{}
	for key, item := range value {
		result[key] = item
	}
	return result
}

func cloneSecrets(value Secrets) Secrets {
	result := Secrets{}
	for key, item := range value {
		result[key] = slices.Clone(item)
	}
	return result
}

func discoveryMatches(hints []string, apps []DiscoveredApp) bool {
	for _, app := range apps {
		candidate := strings.ToLower(app.ID + " " + app.Name)
		for _, hint := range hints {
			if strings.Contains(candidate, strings.ToLower(hint)) {
				return true
			}
		}
	}
	return false
}

func randomInstanceID() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "integration-" + hex.EncodeToString(value), nil
}
