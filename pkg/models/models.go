package models

// Certificate 配置
type Certificate struct {
	Domain   string `yaml:"domain" json:"domain"`
	CertFile string `yaml:"cert_file" json:"cert_file"`
	KeyFile  string `yaml:"key_file" json:"key_file"`
}

// API 配置结构
type API struct {
	Name          string `yaml:"name" json:"name"`
	Format        string `yaml:"format" json:"format"` // 例如: "openai", "anthropic", "gemini"等，如果为空则默认为 "openai"
	Endpoint      string `yaml:"endpoint" json:"endpoint"`
	CustomModelID string `yaml:"custom_model_id" json:"custom_model_id"`
	TargetModelID string `yaml:"target_model_id" json:"target_model_id"`
	StreamMode    string `yaml:"stream_mode" json:"stream_mode"` // "true", "false", or null
	CustomAPIKey  string `yaml:"custom_api_key" json:"custom_api_key"`
	Active        bool   `yaml:"active" json:"active"`
}

// Server 配置结构
type Server struct {
	Port       int  `yaml:"port" json:"port"`
	ManagePort int  `yaml:"manage_port" json:"manage_port"` // HTML 管理页面端口，默认: 8080
	Debug      bool `yaml:"debug" json:"debug"`
}

// Config 完整配置结构
type Config struct {
	Domain       string        `yaml:"domain" json:"domain"`             // 默认或兼容用途的主域名
	Domains      []string      `yaml:"domains" json:"domains"`           // 多域名支持
	Certificates []Certificate `yaml:"certificates" json:"certificates"` // 多证书支持
	APIs         []API         `yaml:"apis" json:"apis"`
	Server       Server        `yaml:"server" json:"server"`
}
