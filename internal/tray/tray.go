package tray

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/getlantern/systray"
)

type Services interface {
	Shutdown(context.Context) error
}

type App struct {
	managePort int
	iconPath   string
	onReady    func()
	onExit     func()
	services   []Services
	quitOnce   sync.Once
}

func New(managePort int, iconPath string, onReady func(), onExit func(), services ...Services) *App {
	return &App{
		managePort: managePort,
		iconPath:   iconPath,
		onReady:    onReady,
		onExit:     onExit,
		services:   services,
	}
}

func (a *App) Run() {
	systray.Run(a.handleReady, a.handleExit)
}

func (a *App) handleReady() {
	systray.SetTitle("Trae Proxy")
	systray.SetTooltip("Trae Proxy")

	if a.iconPath != "" {
		iconData, err := loadIconData(a.iconPath)
		if err == nil && iconData != nil {
			systray.SetIcon(iconData)
		}
	}

	if a.onReady != nil {
		a.onReady()
	}

	mOpen := systray.AddMenuItem("打开管理页面", "在浏览器中打开管理页面")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "退出 Trae Proxy")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				_ = openBrowser(a.managementURL())
			case <-mQuit.ClickedCh:
				a.Quit()
				return
			}
		}
	}()
}

func (a *App) handleExit() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	for _, svc := range a.services {
		if svc == nil {
			continue
		}
		_ = svc.Shutdown(ctx)
	}

	if a.onExit != nil {
		a.onExit()
	}
}

func (a *App) Quit() {
	a.quitOnce.Do(func() {
		systray.Quit()
	})
}

func (a *App) managementURL() string {
	return fmt.Sprintf("http://localhost:%d", a.managePort)
}

func openBrowser(target string) error {
	parsedURL, err := url.Parse(target)
	if err != nil {
		return err
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", parsedURL.String())
	case "darwin":
		cmd = exec.Command("open", parsedURL.String())
	default:
		cmd = exec.Command("xdg-open", parsedURL.String())
	}

	return cmd.Start()
}

func loadIconData(iconPath string) ([]byte, error) {
	absPath, err := filepath.Abs(iconPath)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	return data, nil
}
