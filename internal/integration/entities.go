package integration

import (
	"context"
	"errors"
	"example.com/nas-wallboard/internal/persist"
)

type EntityChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Unit string `json:"unit,omitempty"`
}
type EntityDiscoverer interface {
	DiscoverEntities(context.Context, Config, Secrets) ([]EntityChoice, error)
}

func (s *Service) DiscoverEntities(ctx context.Context, candidate Candidate, instanceID string) ([]EntityChoice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	definition, ok := s.registry.Definition(candidate.Type)
	if !ok || candidate.Type != "home_assistant" {
		return nil, ErrUnknownIntegration
	}
	var current *persist.Integration
	if instanceID != "" {
		for _, item := range s.state.Snapshot().Integrations {
			if item.ID == instanceID && item.Type == candidate.Type {
				copy := item
				current = &copy
				break
			}
		}
		if current == nil {
			return nil, ErrInstanceNotFound
		}
	}
	// Discovery needs only the server and credentials, not a previously selected fan.
	fields := definition.Fields()
	for i := range fields {
		if fields[i].Kind == FieldEntityID {
			fields[i].Required = false
		}
	}
	if err := ValidateFields(fields, candidate.Config); err != nil {
		return nil, err
	}
	secrets, err := s.effectiveSecrets(definition, candidate.Secrets, current)
	if err != nil {
		return nil, err
	}
	if len(secrets["token"]) == 0 {
		return nil, errors.New("token required")
	}
	discoverer, ok := definition.(EntityDiscoverer)
	if !ok {
		return nil, errors.New("discovery unavailable")
	}
	limited, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return discoverer.DiscoverEntities(limited, candidate.Config, secrets)
}
