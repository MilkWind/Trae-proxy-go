package autoconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	hostsManagedBegin = "# >>> Trae-Proxy managed hosts >>>"
	hostsManagedEnd   = "# <<< Trae-Proxy managed hosts <<<"
)

func AutoConfigure(domain, caDir string, installCA, updateHosts bool) error {
	var errorMsgs []string

	if installCA {
		if err := installCACertificate(caDir); err != nil {
			errorMsgs = append(errorMsgs, fmt.Sprintf("安装CA证书失败: %v", err))
		}
	}

	if updateHosts {
		if err := updateHostsFile(domain); err != nil {
			errorMsgs = append(errorMsgs, fmt.Sprintf("更新hosts文件失败: %v", err))
		}
	}

	if len(errorMsgs) > 0 {
		return fmt.Errorf("自动配置遇到以下错误:\n%s", strings.Join(errorMsgs, "\n"))
	}

	return nil
}

func installCACertificate(caDir string) error {
	caCertPath := filepath.Join(caDir, "ca.crt")

	if _, err := os.Stat(caCertPath); os.IsNotExist(err) {
		return fmt.Errorf("CA证书文件不存在: %s", caCertPath)
	}

	return installCACert(caCertPath)
}

func GetHostsPath() (string, error) {
	switch runtime.GOOS {
	case "windows":
		systemRoot := os.Getenv("SystemRoot")
		if systemRoot == "" {
			return "", fmt.Errorf("未找到 SystemRoot 环境变量")
		}
		return filepath.Join(systemRoot, "System32", "drivers", "etc", "hosts"), nil
	case "darwin", "linux":
		return "/etc/hosts", nil
	default:
		return "", fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}
}

func WriteHostsEntries(domains []string) error {
	hostsPath, err := GetHostsPath()
	if err != nil {
		return err
	}

	content, err := os.ReadFile(hostsPath)
	if err != nil {
		return fmt.Errorf("读取hosts文件失败: %w", err)
	}

	normalizedDomains := normalizeDomains(domains)
	if len(normalizedDomains) == 0 {
		return fmt.Errorf("没有可写入的域名")
	}

	baseContent := stripManagedHostsBlock(string(content))
	newContent := strings.TrimRight(baseContent, "\r\n")
	if newContent != "" {
		newContent += "\n\n"
	}
	newContent += buildManagedHostsBlock(normalizedDomains)
	newContent += "\n"

	if err := os.WriteFile(hostsPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("写入hosts文件失败（需要管理员/root权限）: %w", err)
	}

	return nil
}

func RestoreHostsFile() error {
	hostsPath, err := GetHostsPath()
	if err != nil {
		return err
	}

	content, err := os.ReadFile(hostsPath)
	if err != nil {
		return fmt.Errorf("读取hosts文件失败: %w", err)
	}

	restoredContent := stripManagedHostsBlock(string(content))
	restoredContent = strings.TrimRight(restoredContent, "\r\n")
	if restoredContent != "" {
		restoredContent += "\n"
	}

	if err := os.WriteFile(hostsPath, []byte(restoredContent), 0644); err != nil {
		return fmt.Errorf("恢复hosts文件失败（需要管理员/root权限）: %w", err)
	}

	return nil
}

func updateHostsFile(domain string) error {
	return WriteHostsEntries([]string{domain})
}

func buildManagedHostsBlock(domains []string) string {
	lines := []string{hostsManagedBegin}
	for _, domain := range domains {
		lines = append(lines, fmt.Sprintf("127.0.0.1 %s", domain))
	}
	lines = append(lines, hostsManagedEnd)
	return strings.Join(lines, "\n")
}

func stripManagedHostsBlock(content string) string {
	for {
		start := strings.Index(content, hostsManagedBegin)
		if start == -1 {
			return content
		}

		end := strings.Index(content[start:], hostsManagedEnd)
		if end == -1 {
			return strings.TrimRight(content[:start], "\r\n")
		}

		end += start + len(hostsManagedEnd)
		content = content[:start] + content[end:]
	}
}

func normalizeDomains(domains []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(domains))

	for _, domain := range domains {
		normalized := strings.ToLower(strings.TrimSpace(domain))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}

	return result
}

func containsHostsEntry(content, domain string) bool {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}

		fields := strings.Fields(trimmed)
		for i := 1; i < len(fields); i++ {
			if fields[i] == domain {
				return true
			}
		}
	}
	return false
}

func NeedsElevatedPrivileges() bool {
	return true
}

func GetInstructions(domain, caDir string) string {
	caCertPath := filepath.Join(caDir, "ca.crt")

	var instructions string
	switch runtime.GOOS {
	case "windows":
		instructions = fmt.Sprintf(`Windows 手动配置说明：

1. 安装CA证书：
   - 右键点击 %s
   - 选择"安装证书"
   - 选择"本地计算机"
   - 选择"将所有证书放入下列存储" → "浏览" → "受信任的根证书颁发机构"
   - 完成安装

2. 修改hosts文件：
   - 以管理员身份打开记事本
   - 打开文件: C:\Windows\System32\drivers\etc\hosts
   - 添加以下行:
     127.0.0.1 %s

3. 重启浏览器或应用程序使更改生效
`, caCertPath, domain)
	case "darwin":
		instructions = fmt.Sprintf(`macOS 手动配置说明：

1. 安装CA证书：
   sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain %s

2. 修改hosts文件：
   sudo sh -c 'echo "127.0.0.1 %s" >> /etc/hosts'

3. 刷新DNS缓存：
   sudo dscacheutil -flushcache
   sudo killall -HUP mDNSResponder
`, caCertPath, domain)
	case "linux":
		instructions = fmt.Sprintf(`Linux 手动配置说明：

1. 安装CA证书：
   sudo cp %s /usr/local/share/ca-certificates/trae-proxy-ca.crt
   sudo update-ca-certificates

2. 修改hosts文件：
   sudo sh -c 'echo "127.0.0.1 %s" >> /etc/hosts'

注意：某些Linux发行版可能需要不同的命令
`, caCertPath, domain)
	default:
		instructions = fmt.Sprintf("不支持的操作系统: %s\n请手动配置CA证书和hosts文件", runtime.GOOS)
	}

	return instructions
}
