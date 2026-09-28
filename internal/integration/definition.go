package integration

import (
	"context"
	"time"
)

// Config contains only non-secret integration settings.
type Config map[string]any

// Secrets is kept separate so credentials cannot accidentally enter public state.
type Secrets map[string][]byte

type Metadata struct {
	Name           string        `json:"name"`
	Description    string        `json:"description"`
	Icon           string        `json:"icon"`
	Category       string        `json:"category"`
	MinimumRefresh time.Duration `json:"minimum_refresh_ns"`
	SingleInstance bool          `json:"single_instance"`
	Required       bool          `json:"required,omitempty"`
	DiscoveryHints []string      `json:"discovery_hints,omitempty"`
}

type Capability struct {
	ID             string        `json:"id"`
	Label          string        `json:"label"`
	Kind           string        `json:"kind"`
	MinimumRefresh time.Duration `json:"minimum_refresh_ns"`
}

type ProbeStage string

const (
	ProbeStageDNS            ProbeStage = "dns"
	ProbeStageTCP            ProbeStage = "tcp"
	ProbeStageTLS            ProbeStage = "tls"
	ProbeStageAuthentication ProbeStage = "authentication"
	ProbeStageAPIVersion     ProbeStage = "api_version"
	ProbeStagePermission     ProbeStage = "permission"
	ProbeStageFeature        ProbeStage = "feature"
)

type ProbeResult struct {
	Stage        ProbeStage   `json:"stage"`
	OK           bool         `json:"ok"`
	Message      string       `json:"message"`
	Capabilities []Capability `json:"capabilities,omitempty"`
	Checks       []ProbeCheck `json:"checks,omitempty"`
}

type ProbeCheck struct {
	Feature string `json:"feature"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// Collector is intentionally small. Definitions may return a closure-backed
// collector; the lifecycle manager owns cancellation and restart policy.
type Collector interface {
	Run(context.Context) error
}

type Definition interface {
	ID() string
	Metadata() Metadata
	Fields() []Field
	Validate(Config) error
	Test(context.Context, Config, Secrets) ProbeResult
	Capabilities(Config) []Capability
	Collector(Config, Secrets) (Collector, error)
}

type PublicDefinition struct {
	ID           string        `json:"id"`
	Metadata     Metadata      `json:"metadata"`
	Fields       []PublicField `json:"fields"`
	Capabilities []Capability  `json:"capabilities,omitempty"`
}
