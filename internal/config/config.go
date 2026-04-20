package config

import (
	"fmt"
	"os"
	"strings"
	"trae-proxy-go/pkg/models"

	"gopkg.in/yaml.v3"
)

// LoadConfig loads configuration from file.
func LoadConfig(configPath string) (*models.Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg models.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if err := normalizeConfig(&cfg); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	return &cfg, nil
}

// SaveConfig stores configuration to file.
func SaveConfig(cfg *models.Config, configPath string) error {
	if err := normalizeConfig(cfg); err != nil {
		return fmt.Errorf("配置验证失败: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}

func normalizeConfig(cfg *models.Config) error {
	if cfg.Server.ManagePort == 0 {
		cfg.Server.ManagePort = 8080
	}

	if cfg.DomainEnabled == nil {
		cfg.DomainEnabled = make(map[string]bool)
	}

	if len(cfg.Domains) == 0 && strings.TrimSpace(cfg.Domain) != "" {
		cfg.Domains = []string{strings.TrimSpace(cfg.Domain)}
	}

	if len(cfg.Domains) == 0 {
		cfg.Domain = ""
	} else {
		cfg.Domain = strings.TrimSpace(cfg.Domains[0])
	}

	seen := make(map[string]struct{}, len(cfg.Domains))
	normalizedDomains := make([]string, 0, len(cfg.Domains))
	for _, domain := range cfg.Domains {
		d := strings.TrimSpace(domain)
		if d == "" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		normalizedDomains = append(normalizedDomains, d)
		if _, ok := cfg.DomainEnabled[d]; !ok {
			cfg.DomainEnabled[d] = true
		}
	}
	cfg.Domains = normalizedDomains

	// Remove stale domain flags.
	for d := range cfg.DomainEnabled {
		if _, ok := seen[strings.TrimSpace(d)]; !ok {
			delete(cfg.DomainEnabled, d)
		}
	}

	for i := range cfg.APIs {
		if cfg.APIs[i].Format == "" {
			cfg.APIs[i].Format = "openai"
		}
	}

	return validateConfig(cfg)
}

// validateConfig validates configuration.
func validateConfig(cfg *models.Config) error {
	if len(cfg.Domains) == 0 && len(cfg.Certificates) == 0 {
		return fmt.Errorf("至少需要配置一个域名或证书")
	}

	if len(cfg.APIs) == 0 {
		return fmt.Errorf("至少需要配置一个API")
	}

	for i, api := range cfg.APIs {
		if strings.TrimSpace(api.Name) == "" {
			return fmt.Errorf("API配置[%d]的名称不能为空", i)
		}
		if strings.TrimSpace(api.Endpoint) == "" {
			return fmt.Errorf("API配置[%d]的endpoint不能为空", i)
		}
		if strings.TrimSpace(api.CustomModelID) == "" {
			return fmt.Errorf("API配置[%d]的custom_model_id不能为空", i)
		}
		if strings.TrimSpace(api.TargetModelID) == "" {
			return fmt.Errorf("API配置[%d]的target_model_id不能为空", i)
		}
	}

	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("服务端口必须在1-65535之间")
	}

	return nil
}

// GetDefaultConfig returns default configuration.
func GetDefaultConfig() *models.Config {
	return &models.Config{
		Domain:  "api.openai.com",
		Domains: []string{"api.openai.com"},
		DomainEnabled: map[string]bool{
			"api.openai.com": true,
		},
		APIs: []models.API{
			{
				Name:          "默认OpenAI API",
				Format:        "openai",
				Endpoint:      "https://api.openai.com",
				CustomModelID: "gpt-4",
				TargetModelID: "gpt-4",
				StreamMode:    "",
				Active:        true,
			},
		},
		Server: models.Server{
			Port:       443,
			ManagePort: 8080,
			Debug:      true,
		},
	}
}
