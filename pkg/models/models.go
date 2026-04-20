package models

// Certificate TLS certificate mapping configuration.
type Certificate struct {
	Domain   string `yaml:"domain" json:"domain"`
	CertFile string `yaml:"cert_file" json:"cert_file"`
	KeyFile  string `yaml:"key_file" json:"key_file"`
}

// API route configuration.
type API struct {
	Name          string `yaml:"name" json:"name"`
	Format        string `yaml:"format" json:"format"` // e.g. openai/anthropic/gemini
	Endpoint      string `yaml:"endpoint" json:"endpoint"`
	CustomModelID string `yaml:"custom_model_id" json:"custom_model_id"`
	TargetModelID string `yaml:"target_model_id" json:"target_model_id"`
	StreamMode    string `yaml:"stream_mode" json:"stream_mode"` // "true", "false", or ""
	CustomAPIKey  string `yaml:"custom_api_key" json:"custom_api_key"`
	Active        bool   `yaml:"active" json:"active"`
}

// Server runtime configuration.
type Server struct {
	Port       int  `yaml:"port" json:"port"`
	ManagePort int  `yaml:"manage_port" json:"manage_port"`
	Debug      bool `yaml:"debug" json:"debug"`
}

// Config application configuration.
type Config struct {
	Domain        string          `yaml:"domain" json:"domain"` // legacy compatibility
	Domains       []string        `yaml:"domains" json:"domains"`
	DomainEnabled map[string]bool `yaml:"domain_enabled,omitempty" json:"domain_enabled,omitempty"` // per-domain hosts enable switch
	Certificates  []Certificate   `yaml:"certificates" json:"certificates"`
	APIs          []API           `yaml:"apis" json:"apis"`
	Server        Server          `yaml:"server" json:"server"`
}
