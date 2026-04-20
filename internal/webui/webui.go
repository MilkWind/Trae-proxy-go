package webui

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"trae-proxy-go/internal/autoconfig"
	"trae-proxy-go/internal/cert"
	"trae-proxy-go/internal/config"
	"trae-proxy-go/internal/logger"
	"trae-proxy-go/internal/traffic"
	"trae-proxy-go/pkg/models"
)

//go:embed index.html
var htmlContent []byte

type WebUI struct {
	configPath string
	cfg        *models.Config
	logger     *logger.Logger
	traffic    *traffic.Store
	server     *http.Server
	mu         sync.Mutex
}

func NewWebUI(configPath string, cfg *models.Config, logger *logger.Logger, trafficStore *traffic.Store) *WebUI {
	return &WebUI{
		configPath: configPath,
		cfg:        cfg,
		logger:     logger,
		traffic:    trafficStore,
	}
}

func (ui *WebUI) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", ui.handleIndex)
	mux.HandleFunc("/api/config", ui.handleConfig)
	mux.HandleFunc("/api/cert/generate", ui.handleGenerateCert)
	mux.HandleFunc("/api/cert/generate-bulk", ui.handleGenerateBulkCert)
	mux.HandleFunc("/api/hosts/write", ui.handleWriteHosts)
	mux.HandleFunc("/api/hosts/restore", ui.handleRestoreHosts)
	mux.HandleFunc("/api/traffic/logs", ui.handleTrafficLogs)
	mux.HandleFunc("/api/traffic/clear", ui.handleTrafficClear)

	addr := fmt.Sprintf(":%d", ui.cfg.Server.ManagePort)
	ui.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	if ui.logger != nil {
		ui.logger.Info("启动HTML管理页面: http://localhost%s", addr)
	}

	err := ui.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (ui *WebUI) Shutdown(ctx context.Context) error {
	if ui.server == nil {
		return nil
	}
	return ui.server.Shutdown(ctx)
}

func (ui *WebUI) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(htmlContent)
}

func (ui *WebUI) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		ui.mu.Lock()
		defer ui.mu.Unlock()
		_ = json.NewEncoder(w).Encode(ui.cfg)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var newCfg models.Config
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		http.Error(w, fmt.Sprintf(`{"error": %q}`, err.Error()), http.StatusBadRequest)
		return
	}

	ui.mu.Lock()
	defer ui.mu.Unlock()

	if newCfg.Server.ManagePort == 0 {
		newCfg.Server.ManagePort = ui.cfg.Server.ManagePort
	}
	if newCfg.Server.Port == 0 {
		newCfg.Server.Port = ui.cfg.Server.Port
	}

	if err := config.SaveConfig(&newCfg, ui.configPath); err != nil {
		http.Error(w, fmt.Sprintf(`{"error": %q}`, "保存失败: "+err.Error()), http.StatusInternalServerError)
		return
	}

	*ui.cfg = newCfg

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"success"}`))

	if ui.logger != nil {
		ui.logger.Info("配置已通过Web UI更新，某些变更可能需要重启后生效")
	}
}

func (ui *WebUI) handleGenerateCert(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Domain     string `json:"domain"`
		AutoConfig bool   `json:"auto_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error": %q}`, err.Error()), http.StatusBadRequest)
		return
	}

	domain := strings.TrimSpace(req.Domain)
	if domain == "" {
		http.Error(w, `{"error":"域名不能为空"}`, http.StatusBadRequest)
		return
	}

	if err := cert.GenerateCertificates(domain, "ca"); err != nil {
		if ui.logger != nil {
			ui.logger.Error("Web UI 证书生成失败 [%s]: %v", domain, err)
		}
		http.Error(w, fmt.Sprintf(`{"error": %q}`, "生成证书失败: "+err.Error()), http.StatusInternalServerError)
		return
	}

	if req.AutoConfig {
		if err := autoconfig.AutoConfigure(domain, "ca", true, true); err != nil {
			if ui.logger != nil {
				ui.logger.Error("Web UI 自动配置失败: %v", err)
				ui.logger.Info("Web UI 成功生成证书 [%s]，但自动配置失败", domain)
			}
			msg := fmt.Sprintf("证书已生成，但自动配置失败（可能缺少管理员权限）：%v", err)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"status":"success","warning":%q}`, msg)))
			return
		}
	}

	if ui.logger != nil {
		ui.logger.Info("Web UI 成功生成证书并配置完毕 [%s]", domain)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"success"}`))
}

func (ui *WebUI) handleGenerateBulkCert(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	domains := make([]string, 0)
	var req struct {
		Domains []string `json:"domains"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if len(req.Domains) > 0 {
		domains = normalizeDomains(req.Domains)
	} else {
		ui.mu.Lock()
		domains = ui.collectAllDomainsLocked()
		ui.mu.Unlock()
	}

	if len(domains) == 0 {
		http.Error(w, `{"error":"没有可签发证书的域名"}`, http.StatusBadRequest)
		return
	}

	type result struct {
		Domain string `json:"domain"`
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}

	results := make([]result, 0, len(domains))
	failCount := 0
	for _, domain := range domains {
		if err := cert.GenerateCertificates(domain, "ca"); err != nil {
			failCount++
			results = append(results, result{Domain: domain, Status: "failed", Error: err.Error()})
			if ui.logger != nil {
				ui.logger.Error("Web UI 批量证书生成失败 [%s]: %v", domain, err)
			}
			continue
		}
		results = append(results, result{Domain: domain, Status: "success"})
	}

	resp := map[string]interface{}{
		"status":  "success",
		"results": results,
		"total":   len(domains),
		"failed":  failCount,
	}

	if failCount > 0 {
		resp["warning"] = fmt.Sprintf("批量签发完成：共 %d 个，失败 %d 个", len(domains), failCount)
	}

	_ = json.NewEncoder(w).Encode(resp)
}

func (ui *WebUI) handleWriteHosts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	domains := make([]string, 0)
	var req struct {
		Domains []string `json:"domains"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if len(req.Domains) > 0 {
		domains = normalizeDomains(req.Domains)
	} else {
		ui.mu.Lock()
		domains = ui.collectEnabledDomainsLocked()
		ui.mu.Unlock()
	}

	if len(domains) == 0 {
		http.Error(w, `{"error":"没有可写入的已启用域名"}`, http.StatusBadRequest)
		return
	}

	if err := autoconfig.WriteHostsEntries(domains); err != nil {
		if ui.logger != nil {
			ui.logger.Error("Web UI 写入hosts失败: %v", err)
		}
		http.Error(w, fmt.Sprintf(`{"error": %q}`, "写入hosts失败: "+err.Error()), http.StatusInternalServerError)
		return
	}

	if ui.logger != nil {
		ui.logger.Info("Web UI 已写入hosts，域名数量: %d", len(domains))
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"domains": domains,
	})
}

func (ui *WebUI) handleRestoreHosts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if err := autoconfig.RestoreHostsFile(); err != nil {
		if ui.logger != nil {
			ui.logger.Error("Web UI 恢复hosts失败: %v", err)
		}
		http.Error(w, fmt.Sprintf(`{"error": %q}`, "恢复hosts失败: "+err.Error()), http.StatusInternalServerError)
		return
	}

	if ui.logger != nil {
		ui.logger.Info("Web UI 已恢复hosts文件中的 Trae-Proxy 管理块")
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"success"}`))
}

func (ui *WebUI) handleTrafficLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if ui.traffic == nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success",
			"data":   []traffic.Entry{},
		})
		return
	}

	var sinceID uint64
	var limit int
	if _, err := fmt.Sscanf(strings.TrimSpace(r.URL.Query().Get("since_id")), "%d", &sinceID); err != nil {
		sinceID = 0
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(r.URL.Query().Get("limit")), "%d", &limit); err != nil {
		limit = 200
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"data":   ui.traffic.List(sinceID, limit),
	})
}

func (ui *WebUI) handleTrafficClear(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if ui.traffic != nil {
		ui.traffic.Clear()
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (ui *WebUI) collectAllDomainsLocked() []string {
	seen := make(map[string]struct{})
	domains := make([]string, 0, len(ui.cfg.Domains))
	for _, domain := range ui.cfg.Domains {
		d := strings.TrimSpace(domain)
		if d == "" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		domains = append(domains, d)
	}
	return domains
}

func (ui *WebUI) collectEnabledDomainsLocked() []string {
	all := ui.collectAllDomainsLocked()
	if len(all) == 0 {
		return all
	}

	if ui.cfg.DomainEnabled == nil {
		return all
	}

	enabled := make([]string, 0, len(all))
	for _, domain := range all {
		if flag, ok := ui.cfg.DomainEnabled[domain]; !ok || flag {
			enabled = append(enabled, domain)
		}
	}
	return enabled
}

func normalizeDomains(domains []string) []string {
	seen := make(map[string]struct{}, len(domains))
	normalized := make([]string, 0, len(domains))
	for _, domain := range domains {
		d := strings.TrimSpace(domain)
		if d == "" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		normalized = append(normalized, d)
	}
	return normalized
}
