# Trae-Proxy Go

一个高性能的 API 代理工具，用 Go 实现，专门用于拦截并重定向 OpenAI / Anthropic API 请求到自定义后端服务。
测试情况
- Trae 可以
- vscode copliot 可以
- 其他 待测

## 特性

- **多协议支持**：兼容 OpenAI Chat (`/v1/chat/completions`)、OpenAI Responses (`/v1/responses`) 和 Anthropic (`/v1/messages`)
- **多后端路由**：按模型 ID 精确匹配后端，自动回退到第一个激活的 API
- **模型 ID 映射**：对客户端暴露自定义模型 ID，转发时自动替换为目标模型 ID
- **流式响应**：支持 SSE 流式和非流式响应，可按后端强制覆盖
- **多域名 + 多证书**：同时代理多个域名，每个域名独立管理 TLS 证书
- **自动系统配置**：一键安装 CA 证书到系统信任存储，自动写入 hosts 文件
- **TUI 管理界面**：基于 Bubble Tea 的终端交互界面，支持增删改查配置
- **Web 管理界面**：内嵌 HTML 管理页面，默认监听 `8080` 端口
- **系统托盘**：支持后台系统托盘模式运行（Windows）
- **环境诊断**：`doctor` 命令检测端口冲突及代理配置状态

## 目录结构

```
cmd/
  cli/        # CLI / TUI 入口
  proxy/      # 代理服务器入口
internal/
  autoconfig/ # 跨平台系统自动配置（hosts + CA 证书）
  cert/       # TLS 证书生成
  config/     # 配置文件读写
  doctor/     # 环境诊断
  logger/     # 日志组件
  proxy/      # 核心代理逻辑（handler / router / stream / server）
  tray/       # 系统托盘
  tui/        # Bubble Tea TUI 界面
  webui/      # 内嵌 Web 管理界面
pkg/
  models/     # 公共数据模型定义
```

## 快速开始

### 依赖

- Go 1.21+

### 构建

```bash
git clone <repository-url>
cd trae-proxy-go
go mod download

go build -o trae-proxy     ./cmd/proxy
go build -o trae-proxy-cli ./cmd/cli
go build -o main.exe ./cmd/proxy
```

### 配置文件

编辑 `config.yaml`：

```yaml
domains:
  - api.openai.com
  - api.anthropic.com

certificates: []

apis:
  - name: deepseek-r1
    format: openai          # openai | responses | anthropic
    endpoint: https://api.deepseek.com
    custom_model_id: deepseek-reasoner   # 客户端使用的模型 ID
    target_model_id: deepseek-reasoner   # 实际发送给后端的模型 ID
    stream_mode: ""                       # "true" 强制开启 | "false" 强制关闭 | "" 跟随请求
    active: true

server:
  port: 443
  manage_port: 8080
  debug: false
```

| 字段 | 说明 |
|---|---|
| `format` | 后端 API 格式，`openai`（默认）/ `responses` / `anthropic` |
| `custom_model_id` | 对客户端暴露的模型名称 |
| `target_model_id` | 转发给后端时实际使用的模型名称 |
| `stream_mode` | 强制覆盖流式设置，为空则跟随客户端原始请求 |
| `active` | 是否参与路由选择 |

## 使用

### TUI 界面（推荐）

无参数运行 CLI 工具即可启动 TUI：

```bash
./trae-proxy-cli
```

| 快捷键 | 功能 |
|---|---|
| `a` | 添加 API 配置 |
| `e` | 编辑选中配置 |
| `d` | 删除选中配置 |
| `Space` | 激活 / 停用选中配置 |
| `D` | 设置代理域名 |
| `C` | 生成 SSL 证书 |
| `↑ ↓` | 上下选择 |
| `q` | 退出 |

### CLI 命令

```bash
# 列出所有 API 配置
./trae-proxy-cli list

# 添加 / 删除 / 更新 / 激活配置
./trae-proxy-cli add
./trae-proxy-cli remove
./trae-proxy-cli update
./trae-proxy-cli activate

# 设置代理域名
./trae-proxy-cli domain

# 生成证书
./trae-proxy-cli cert
./trae-proxy-cli cert --domain api.openai.com
./trae-proxy-cli cert --auto-config   # 生成并自动配置系统（需管理员权限）
./trae-proxy-cli cert --install-ca    # 仅安装 CA 证书到系统信任存储
./trae-proxy-cli cert --update-hosts  # 仅更新 hosts 文件

# 启动代理（CLI 方式）
./trae-proxy-cli start

# 环境诊断
./trae-proxy-cli doctor
```

### 直接启动代理服务器

```bash
./trae-proxy
./trae-proxy --config config.yaml --cert ca/api.openai.com.crt --key ca/api.openai.com.key
./trae-proxy --debug
```

## API 路由

| 路径 | 方法 | 说明 |
|---|---|---|
| `/v1/chat/completions` | POST | OpenAI 格式聊天请求 |
| `/v1/responses` | POST | Cursor/OpenAI Responses 格式请求 |
| `/v1/messages` | POST | Anthropic 格式请求 |
| `/anthropic/v1/messages` | POST | Anthropic 格式请求（备用路径） |
| `/v1/models` | GET | 模型列表（自动识别 OpenAI / Responses / Anthropic 格式） |
| `/v1/models/:id` | GET | 单个模型详情（OpenAI/Responses/Anthropic） |

### 后端选择逻辑

1. 按请求中的 `model` 字段精确匹配 `custom_model_id`
2. 无精确匹配时回退到第一个 `active: true` 的配置
3. 全部未激活时使用配置列表中的第一项

## Web 管理界面

代理运行后访问：

```
http://localhost:8080
```

支持在线修改 API 配置、生成证书、写入 / 还原 hosts 文件。

## 证书与系统配置

证书默认生成到 `ca/` 目录，文件命名为 `<domain>.crt` / `<domain>.key`。

自动配置（`--auto-config`）会完成：

1. 生成自签名 CA 及域名证书
2. 将 CA 证书安装到系统信任存储
3. 将域名解析到 `127.0.0.1` 写入 hosts 文件

> 需要管理员 / root 权限。
