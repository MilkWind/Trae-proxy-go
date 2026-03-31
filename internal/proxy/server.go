package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"trae-proxy-go/internal/logger"
	"trae-proxy-go/pkg/models"
)

// Server 代理服务器
type Server struct {
	config     *models.Config
	logger     *logger.Logger
	handler    *Handler
	tlsConfig  *tls.Config
	httpServer *http.Server
}

// NewServer 创建新的代理服务器
func NewServer(config *models.Config, logger *logger.Logger, certFile, keyFile string) (*Server, error) {
	handler := NewHandler(config, logger)

	var tlsConfig *tls.Config
	var certs []tls.Certificate

	// 加载默认证书(从命令行参数来的)
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err == nil {
			certs = append(certs, cert)
		} else {
			logger.Error("加载默认证书失败 [%s]: %v", certFile, err)
		}
	}

	// 加载 config.Certificates 里自定义的证书
	for _, c := range config.Certificates {
		if c.CertFile != "" && c.KeyFile != "" {
			cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
			if err == nil {
				certs = append(certs, cert)
			} else {
				logger.Error("加载多证书失败 [%s]: %v", c.Domain, err)
			}
		}
	}

	// 尝试加载 config.Domains 生成的证书
	for _, d := range config.Domains {
		// 为了避免处理太多依赖，简单拼接相对路径 (类似于 cmd/proxy/main.go)
		dCert := "ca/" + d + ".crt"
		dKey := "ca/" + d + ".key"
		// 这里简单跳过命令行已经传了的情况防止重复加载(实际上重复了也没大碍)
		if dCert == certFile || dCert == "" {
			continue
		}
		cert, err := tls.LoadX509KeyPair(dCert, dKey)
		if err == nil {
			certs = append(certs, cert)
		}
	}

	if len(certs) > 0 {
		tlsConfig = &tls.Config{
			Certificates: certs,
		}
	}

	return &Server{
		config:    config,
		logger:    logger,
		handler:   handler,
		tlsConfig: tlsConfig,
	}, nil
}

// Start 启动服务器
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// 注册路由
	mux.HandleFunc("/", s.handler.HandleRoot)
	mux.HandleFunc("/v1", s.handler.HandleV1Root)
	mux.HandleFunc("/v1/models", s.handler.HandleModels)
	mux.HandleFunc("/v1/models/", s.handler.HandleModelByID)
	mux.HandleFunc("/v1/chat/completions", s.handler.HandleChatCompletions)
	mux.HandleFunc("/v1/messages", s.handler.HandleMessages)
	mux.HandleFunc("/anthropic/v1/messages", s.handler.HandleMessages)

	//wrappedMux := LoggingMiddleware(s.logger)(mux)

	s.httpServer = &http.Server{
		Addr:      fmt.Sprintf(":%d", s.config.Server.Port),
		Handler:   mux,
		TLSConfig: s.tlsConfig,
	}

	if s.logger != nil {
		s.logger.Info("启动代理服务器，监听端口: %d", s.config.Server.Port)
		if len(s.config.APIs) > 0 {
			s.logger.Info("多后端配置已启用，共 %d 个API配置", len(s.config.APIs))
			for _, api := range s.config.APIs {
				status := "激活"
				if !api.Active {
					status = "未激活"
				}
				s.logger.Info("  - %s [%s]: %s -> %s", api.Name, status, api.Endpoint, api.CustomModelID)
			}
		}
	}

	var err error
	if s.tlsConfig != nil {
		// 当使用TLSConfig时，certFile和keyFile可以为空，证书从TLSConfig中获取
		err = s.httpServer.ListenAndServeTLS("", "")
	} else {
		err = s.httpServer.ListenAndServe()
	}
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}
