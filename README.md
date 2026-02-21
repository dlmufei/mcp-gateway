# MCP Gateway

A high-performance MCP (Model Context Protocol) gateway written in Go that bridges multiple downstream MCP servers to multiple upstream WebSocket endpoints.

## Features

- 🚀 **High Performance**: Native Go concurrency with goroutines
- 🔌 **Multiple Transport Types**: Supports stdio, HTTP, SSE, and StreamableHTTP
- 🔄 **Auto Reconnection**: Exponential backoff with jitter
- 📊 **Tool Aggregation**: Aggregates tools from multiple MCP servers
- 🛡️ **Robust Error Handling**: Graceful shutdown and process management
- 📝 **Structured Logging**: Using Go 1.21+ slog with verbose mode
- 🌐 **Multi-Upstream Support**: Connect multiple upstreams with independent MCP server configurations
- 🔧 **Environment Variables**: Support `${VAR}` syntax in config files

## Architecture

```
┌─────────────────┐      WebSocket       ┌──────────────────┐
│  Upstream AI 1  │ ◄─────────────────► │                  │
│  (xiaozhi-1)    │                      │                  │
└─────────────────┘                      │   mcp-gateway    │
                                         │                  │
┌─────────────────┐      WebSocket       │                  │
│  Upstream AI 2  │ ◄─────────────────► │                  │
│  (xiaozhi-2)    │                      └────────┬─────────┘
└─────────────────┘                               │
                                    ┌─────────────┼─────────────┐
                                    ▼             ▼             ▼
                              ┌──────────┐ ┌──────────┐ ┌──────────┐
                              │ stdio    │ │  HTTP    │ │  SSE     │
                              │ adapter  │ │ adapter  │ │ adapter  │
                              └────┬─────┘ └────┬─────┘ └────┬─────┘
                                   │            │            │
                              ┌────▼────┐ ┌────▼────┐ ┌────▼────┐
                              │calculator│ │web-search│ │smartrun │
                              └─────────┘ └─────────┘ └─────────┘
```

**Note**: Each upstream has its own independent MCP servers configuration, allowing different tokens/credentials per upstream.

## Quick Start

### 1. Clone and Build

```bash
cd mcp-gateway
go mod tidy
go build -o mcp-gateway ./cmd/mcp-gateway
```

### 2. Configure

```bash
# 复制示例配置文件
cp configs/mcp_config.example.json configs/mcp_config.json
cp run_local.example.sh run_local.sh
cp deploy_remote.example.sh deploy_remote.sh

# 编辑配置文件，填入你的 token
vim configs/mcp_config.json
vim run_local.sh
vim deploy_remote.sh

# 添加执行权限
chmod +x run_local.sh deploy_remote.sh
```

### 3. Run

```bash
# 本地 macOS 运行
./run_local.sh build    # 编译
./run_local.sh start    # 启动
./run_local.sh status   # 查看状态
./run_local.sh logs     # 查看日志
./run_local.sh check    # 检查 MCP 服务器状态和工具列表

# 远程 Linux 部署
./deploy_remote.sh deploy   # 编译 + 上传 + 启动
./deploy_remote.sh status   # 远程状态
./deploy_remote.sh logs     # 远程日志
```

## Check Command

使用 `check` 命令可以验证配置并检查所有 MCP 服务器的连接状态和可用工具：

```bash
# 通过脚本运行
./run_local.sh check

# 或直接运行二进制
./mcp-gateway -check -config configs/mcp_config.json

# JSON 格式输出（方便脚本解析）
./mcp-gateway -check -config configs/mcp_config.json -output json

# 自定义超时时间
./mcp-gateway -check -config configs/mcp_config.json -timeout 60s
```

输出示例：

```
==================================================
         MCP Gateway Configuration Check
==================================================

📡 Upstream: xiaozhi-agent
   Endpoint: wss://api.xiaozhi.me/mcp/?token=***

   ✅ [tencentdocs] (http)
      URL: https://docs.qq.com/openapi/mcp
      Status: Connected
      Server: mcp-McpserverService v1.0.0
      Tools (12):
        • create_word_by_markdown      通过Markdown格式创建在线Word文档...
        • get_content                  获取文档完整内容...
        ...

   ❌ [local-mcp] (stdio)
      Command: python mcp_server.py
      Status: Failed
      Error: start failed: exec: "python": not found

--------------------------------------------------
📊 Summary
--------------------------------------------------
   Total Upstreams:    1
   Total MCP Servers:  2
     • Healthy:        1
     • Failed:         1
   Total Tools:        12
==================================================
```

## Configuration

### Environment Variables

```bash
# Optional: WebSocket endpoint (can be used with ${MCP_ENDPOINT} in config)
export MCP_ENDPOINT='wss://api.xiaozhi.me/mcp/?token=YOUR_TOKEN'

# Optional: Log level
export MCP_LOG_LEVEL=debug
```

### Config File Format

See `configs/mcp_config.example.json` for full example:

```json
{
  "upstreams": [
    {
      "name": "xiaozhi-agent-1",
      "endpoint": "${MCP_ENDPOINT}",
      "reconnect": {
        "enabled": true,
        "initialBackoff": "1s",
        "maxBackoff": "10m",
        "multiplier": 2
      },
      "keepalive": {
        "interval": "30s",
        "timeout": "10s"
      },
      "mcpServers": {
        "tencentdocs": {
          "type": "http",
          "url": "https://docs.qq.com/openapi/mcp",
          "headers": {
            "Authorization": "your-token"
          },
          "timeout": "60s",
          "disabled": false
        },
        "web-search": {
          "type": "http",
          "url": "http://127.0.0.1:3000/mcp",
          "timeout": "120s"
        }
      }
    },
    {
      "name": "xiaozhi-agent-2",
      "endpoint": "wss://api.xiaozhi.me/mcp/?token=ANOTHER_TOKEN",
      "reconnect": { "enabled": true },
      "keepalive": { "interval": "30s", "timeout": "10s" },
      "mcpServers": {
        "smartrun": {
          "type": "http",
          "url": "https://smartrun.woa.com/mcp",
          "headers": { "Authorization": "Bearer your-smartrun-token" },
          "timeout": "180s"
        }
      }
    }
  ],
  "logging": {
    "level": "info",
    "format": "text",
    "verbose": false
  },
  "metrics": {
    "enabled": false,
    "port": 9090
  }
}
```

### Configuration Structure

| Field | Description |
|-------|-------------|
| `upstreams` | Array of upstream configurations, each with independent MCP servers |
| `upstreams[].name` | Unique name for the upstream instance |
| `upstreams[].endpoint` | WebSocket URL (supports `${VAR}` environment variable expansion) |
| `upstreams[].reconnect` | Reconnection settings (enabled, initialBackoff, maxBackoff, multiplier) |
| `upstreams[].keepalive` | Keepalive settings (interval, timeout) |
| `upstreams[].mcpServers` | Map of MCP server configurations for this upstream |
| `logging.verbose` | When `true`, logs full request arguments and response content |

### Server Types

| Type | Description |
|------|-------------|
| `stdio` | Local process with stdin/stdout communication |
| `http` | HTTP/StreamableHTTP endpoint |
| `sse` | Server-Sent Events endpoint |

### Multi-Upstream Use Case

Each upstream can have its own set of MCP servers with different configurations:

- **Different AI agents**: Connect multiple AI assistants simultaneously
- **Different credentials**: Each upstream can use different tokens for the same MCP service
- **Independent routing**: Tools from each upstream's MCP servers are routed independently

## Files Structure

```
mcp-gateway/
├── cmd/mcp-gateway/              # Main entry point
│   └── main.go                   # Application bootstrap
├── internal/
│   ├── adapter/                  # MCP server adapters (stdio, http, sse)
│   ├── checker/                  # Configuration check & validation
│   ├── config/                   # Configuration management
│   ├── protocol/                 # MCP protocol definitions
│   ├── router/                   # Message routing & tool aggregation
│   └── upstream/                 # WebSocket upstream client
├── pkg/retry/                    # Retry utilities with backoff
├── configs/
│   ├── mcp_config.example.json   # ⭐ 配置示例（提交到 git）
│   └── mcp_config.json           # 🔒 实际配置（不提交）
├── run_local.example.sh          # ⭐ 本地运行脚本示例
├── run_local.sh                  # 🔒 本地运行脚本（含环境变量）
├── deploy_remote.example.sh      # ⭐ 远程部署脚本示例
├── deploy_remote.sh              # 🔒 远程部署脚本（含服务器信息）
├── go.mod                        # Go module definition
└── .gitignore                    # Git ignore rules
```

## Security Notes

⚠️ **敏感信息处理**：

以下文件包含 token 等敏感信息，**不要提交到 git**：
- `configs/mcp_config.json`
- `run_local.sh`
- `deploy_remote.sh`
- `.env`

请使用 `.example` 后缀的示例文件作为模板，复制后填入你的实际配置。

## Building

```bash
# Development build
go build -o mcp-gateway ./cmd/mcp-gateway

# Production build with optimizations
CGO_ENABLED=0 go build -ldflags="-s -w" -o mcp-gateway ./cmd/mcp-gateway

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 go build -o mcp-gateway-linux ./cmd/mcp-gateway
```

## Comparison with Python Version

| Aspect | Python (mcp_pipe.py) | Go (mcp-gateway) |
|--------|---------------------|------------------|
| Performance | Single-threaded asyncio | Native goroutines |
| Memory | ~50-100MB | ~10-20MB |
| Startup | 2-3s | <100ms |
| Deployment | Python + venv + deps | Single binary |
| Process Mgmt | Subprocess + mcp_proxy | In-process |
| Stability | Basic error handling | Robust recovery |

## License

MIT
