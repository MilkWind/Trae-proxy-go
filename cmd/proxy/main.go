package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"trae-proxy-go/internal/config"
	"trae-proxy-go/internal/logger"
	"trae-proxy-go/internal/proxy"
	"trae-proxy-go/internal/traffic"
	"trae-proxy-go/internal/tray"
	"trae-proxy-go/internal/webui"
)

func main() {
	var (
		configPath = flag.String("config", "config.yaml", "配置文件路径")
		certFile   = flag.String("cert", "", "证书文件路径")
		keyFile    = flag.String("key", "", "私钥文件路径")
		debug      = flag.Bool("debug", false, "启用调试模式")
	)
	flag.Parse()

	log := logger.NewLogger(*debug)
	trafficStore := traffic.NewStore(2000)

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Error("加载配置失败: %v", err)
		os.Exit(1)
	}

	if *debug {
		cfg.Server.Debug = true
	}

	defaultDomain := ""
	if len(cfg.Domains) > 0 {
		defaultDomain = cfg.Domains[0]
	} else if cfg.Domain != "" {
		defaultDomain = cfg.Domain
	}

	if *certFile == "" && defaultDomain != "" {
		*certFile = filepath.Join("ca", fmt.Sprintf("%s.crt", defaultDomain))
	}
	if *keyFile == "" && defaultDomain != "" {
		*keyFile = filepath.Join("ca", fmt.Sprintf("%s.key", defaultDomain))
	}

	iconFile := filepath.Join("internal", "tray", "icon.ico")

	webUI := webui.NewWebUI(*configPath, cfg, log, trafficStore)

	go func() {
		var lastModTime time.Time
		if info, err := os.Stat(*configPath); err == nil {
			lastModTime = info.ModTime()
		}
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			info, err := os.Stat(*configPath)
			if err != nil {
				continue
			}
			if info.ModTime().After(lastModTime) {
				lastModTime = info.ModTime()

				newCfg, err := config.LoadConfig(*configPath)
				if err != nil {
					log.Error("配置文件变更，但加载失败: %v", err)
					continue
				}

				if *debug {
					newCfg.Server.Debug = true
				}

				*cfg = *newCfg
				log.Info("配置文件已从磁盘热重载")
			}
		}
	}()

	srv, err := proxy.NewServer(cfg, log, trafficStore, *certFile, *keyFile)
	if err != nil {
		log.Error("创建代理服务器失败: %v", err)
		os.Exit(1)
	}

	var startOnce sync.Once
	var app *tray.App
	app = tray.New(
		cfg.Server.ManagePort,
		iconFile,
		func() {
			startOnce.Do(func() {
				go func() {
					if err := webUI.Start(); err != nil {
						log.Error("Web UI 启动失败: %v", err)
					}
				}()

				go func() {
					if err := srv.Start(); err != nil {
						log.Error("代理服务器启动失败: %v", err)
						app.Quit()
					}
				}()
			})
		},
		func() {
			log.Info("Trae Proxy 已退出")
		},
		webUI,
		srv,
	)

	app.Run()
}
