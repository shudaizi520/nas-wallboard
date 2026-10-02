package dashboard

type View struct {
	Width          int        `json:"width"`
	ConnectionTone string     `json:"connection_tone"`
	DataStatus     string     `json:"data_status,omitempty"`
	Uptime         string     `json:"uptime,omitempty"`
	NASPower       string     `json:"nas_power,omitempty"`
	Metrics        []Metric   `json:"metrics"`
	Activities     []Activity `json:"activities"`
}

type Metric struct {
	ID    string `json:"id"`
	Icon  string `json:"icon"`
	Value string `json:"value"`
	Tone  string `json:"tone"`
}

type Activity struct {
	ID          string          `json:"id"`
	Icon        string          `json:"icon"`
	Tone        string          `json:"tone"`
	Title       string          `json:"title"`
	Value       string          `json:"value"`
	Detail      string          `json:"detail"`
	ValueTone   string          `json:"value_tone,omitempty"`
	DetailParts *DetailPartList `json:"detail_parts,omitempty"`
	Progress    *ProgressList   `json:"progress,omitempty"`
}

type ProgressList []int

type DetailPart struct {
	Text  string `json:"text"`
	Color string `json:"color,omitempty"`
}

type DetailPartList []DetailPart
