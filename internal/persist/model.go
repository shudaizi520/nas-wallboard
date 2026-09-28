package persist

const CurrentSchemaVersion = 1

type State struct {
	SchemaVersion int             `json:"schema_version"`
	SetupComplete bool            `json:"setup_complete"`
	Server        ServerSettings  `json:"server"`
	Integrations  []Integration   `json:"integrations"`
	Widgets       []Widget        `json:"widgets"`
	Legacy        *LegacyMetadata `json:"legacy,omitempty"`
}

type ServerSettings struct {
	Listen             string  `json:"listen,omitempty"`
	ReadTimeout        string  `json:"read_timeout,omitempty"`
	WriteTimeout       string  `json:"write_timeout,omitempty"`
	Title              string  `json:"title,omitempty"`
	Language           string  `json:"language,omitempty"`
	Timezone           string  `json:"timezone,omitempty"`
	Theme              string  `json:"theme,omitempty"`
	BackgroundStrength float64 `json:"background_strength,omitempty"`
	Width              int     `json:"width,omitempty"`
}

type Integration struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"`
	Enabled    bool                 `json:"enabled"`
	Config     map[string]any       `json:"config,omitempty"`
	SecretRefs map[string]SecretRef `json:"secret_refs,omitempty"`
}

type SecretRef struct {
	InstanceID string `json:"instance_id"`
	Key        string `json:"key"`
	Revision   string `json:"revision"`
}

type Widget struct {
	ID            string         `json:"id"`
	DefinitionID  string         `json:"definition_id"`
	IntegrationID string         `json:"integration_id,omitempty"`
	Enabled       bool           `json:"enabled"`
	Order         int            `json:"order"`
	Config        map[string]any `json:"config,omitempty"`
}

type LegacyMetadata struct {
	SourceFingerprint string   `json:"source_fingerprint,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
}

func newState() State {
	return State{
		SchemaVersion: CurrentSchemaVersion,
		Integrations:  []Integration{},
		Widgets:       []Widget{},
	}
}
