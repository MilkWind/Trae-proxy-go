package webui

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"trae-proxy-go/internal/autoconfig"
	"trae-proxy-go/internal/cert"
	"trae-proxy-go/internal/config"
	"trae-proxy-go/internal/logger"
	"trae-proxy-go/pkg/models"
)

//go:embed index.html
var htmlContent []byte

type WebUI struct {
	configPath string
	cfg        *models.Config
	logger     *logger.Logger
	server     *http.Server
	mu         sync.Mutex
}

func NewWebUI(configPath string, cfg *models.Config, logger *logger.Logger) *WebUI {
	return &WebUI{
		configPath: configPath,
		cfg:        cfg,
		logger:     logger,
	}
}

func (ui *WebUI) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", ui.handleIndex)
	mux.HandleFunc("/api/config", ui.handleConfig)
	mux.HandleFunc("/api/cert/generate", ui.handleGenerateCert)
	mux.HandleFunc("/api/hosts/write", ui.handleWriteHosts)
	mux.HandleFunc("/api/hosts/restore", ui.handleRestoreHosts)

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
	w.Write(htmlContent)
}

func (ui *WebUI) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		ui.mu.Lock()
		defer ui.mu.Unlock()
		json.NewEncoder(w).Encode(ui.cfg)
		return
	}

	if r.Method == http.MethodPost {
		var newCfg models.Config
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			http.Error(w, fmt.Sprintf(`{"error": "%v"}`, err), http.StatusBadRequest)
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
			http.Error(w, fmt.Sprintf(`{"error": "保存失败: %v"}`, err), http.StatusInternalServerError)
			return
		}

		*ui.cfg = newCfg

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))

		ui.logger.Info("配置已通过Web UI更新。某些更改可能需要重启才能生效。")
		return
	}

	http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
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
		http.Error(w, fmt.Sprintf(`{"error": "%v"}`, err), http.StatusBadRequest)
		return
	}

	if req.Domain == "" {
		http.Error(w, `{"error": "域名不能为空"}`, http.StatusBadRequest)
		return
	}

	if err := cert.GenerateCertificates(req.Domain, "ca"); err != nil {
		ui.logger.Error("Web UI 证书生成失败 [%s]: %v", req.Domain, err)
		http.Error(w, fmt.Sprintf(`{"error": "生成证书失败: %v"}`, err), http.StatusInternalServerError)
		return
	}

	if req.AutoConfig {
		if err := autoconfig.AutoConfigure(req.Domain, "ca", true, true); err != nil {
			ui.logger.Error("Web UI 自动配置失败 (需以管理员身份运行): %v", err)
			msg := fmt.Sprintf("证书已生成，但自动配置失败（可能是因为没有以管理员/root身份运行）：%v", err)
			ui.logger.Info("Web UI 成功生成证书 [%s]，但自动配置失败", req.Domain)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"status":"success","warning":%q}`, msg)))
			return
		}
	}

	ui.logger.Info("Web UI 成功生成证书并配置完毕 [%s]", req.Domain)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func (ui *WebUI) handleWriteHosts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	ui.mu.Lock()
	domains := ui.collectManagedDomainsLocked()
	ui.mu.Unlock()

	if len(domains) == 0 {
		http.Error(w, `{"error":"没有可写入的域名，请先配置主域名或额外域名"}`, http.StatusBadRequest)
		return
	}

	if err := autoconfig.WriteHostsEntries(domains); err != nil {
		if ui.logger != nil {
			ui.logger.Error("Web UI 写入hosts失败: %v", err)
		}
		http.Error(w, fmt.Sprintf(`{"error": "写入hosts失败: %v"}`, err), http.StatusInternalServerError)
		return
	}

	if ui.logger != nil {
		ui.logger.Info("Web UI 已写入hosts，域名数量: %d", len(domains))
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
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
		http.Error(w, fmt.Sprintf(`{"error": "恢复hosts失败: %v"}`, err), http.StatusInternalServerError)
		return
	}

	if ui.logger != nil {
		ui.logger.Info("Web UI 已恢复hosts文件中的 Trae-Proxy 管理块")
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func (ui *WebUI) collectManagedDomainsLocked() []string {
	domains := make([]string, 0, len(ui.cfg.Domains)+1)
	if ui.cfg.Domain != "" {
		domains = append(domains, ui.cfg.Domain)
	}
	domains = append(domains, ui.cfg.Domains...)
	return domains
}
