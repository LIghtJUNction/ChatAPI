package service

type ConfigSchema struct {
	Fields           []ConfigFieldSchema `json:"fields"`
	AllowUnknownKeys bool                `json:"allow_unknown_keys"`
	ReservedPrefixes []string            `json:"reserved_prefixes,omitempty"`
}

type ConfigFieldSchema struct {
	Key            string         `json:"key"`
	ValueType      string         `json:"value_type"`
	DefaultValue   any            `json:"default_value,omitempty"`
	Public         bool           `json:"public"`
	AdminWriteOnly bool           `json:"admin_write_only"`
	ReadOnly       bool           `json:"read_only,omitempty"`
	Description    string         `json:"description,omitempty"`
	Validation     map[string]any `json:"validation,omitempty"`
}
