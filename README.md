# MCP Gateway

A high-performance MCP (Model Context Protocol) gateway written in Go that bridges multiple downstream MCP servers to an upstream WebSocket endpoint.

## Features

- 🚀 **High Performance**: Native Go concurrency with goroutines
- 🔌 **Multiple Transport Types**: Supports stdio, HTTP, SSE, and StreamableHTTP
- 🔄 **Auto Reconnection**: Exponential backoff with jitter
- 📊 **Tool Aggregation**: Aggregates tools from multiple MCP servers
- 🛡️ **Robust Error Handling**: Graceful shutdown and process management
- 📝 **Structured Logging**: Using Go 1.21+ slog

## Architecture

```
┌─────────────────┐      WebSocket       ┌──────────────────┐
│  Upstream AI    │ ◄─────────────────► │   mcp-gateway    │
│  (e.g. xiaozhi) │                      │                  │
└─────────────────┘                      └────────┬─────────┘
                                                  │
                                    ┌─────────────┼─────────────┐
                                    ▼             ▼             ▼
                              ┌──────────┐ ┌──────────┐ ┌──────────┐
                              │ stdio    │ │  HTTP    │ │  SSE     │
                              │ adapter  │ │ adapter  │ │ adapter  │
                              └────┬─────┘ └────┬─────┘ └────┬─────┘
                                   │            │            │
                              ┌────▼────┐ ┌────▼────┐ ┌────▼────┐
                              │calculator│ │web-search│ │ other  │
                              └─────────┘ └─────────┘ └─────────┘
```

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

# 远程 Linux 部署
./deploy_remote.sh deploy   # 编译 + 上传 + 启动
./deploy_remote.sh status   # 远程状态
./deploy_remote.sh logs     # 远程日志
```

## Configuration

### Environment Variables

```bash
# Required: WebSocket endpoint with token
export MCP_ENDPOINT='wss://api.xiaozhi.me/mcp/?token=YOUR_TOKEN'

# Optional: Log level
export MCP_LOG_LEVEL=debug
```

### Config File Format

See `configs/mcp_config.example.json` for full example:

```json
{
  "upstream": {
    "endpoint": "${MCP_ENDPOINT}",
    "reconnect": {
      "enabled": true,
      "initialBackoff": "1s",
      "maxBackoff": "10m",
      "multiplier": 2
    }
  },
  "mcpServers": {
    "server-name": {
      "type": "stdio|http|sse",
      "command": "python",           // for stdio
      "args": ["script.py"],         // for stdio
      "url": "http://...",           // for http/sse
      "headers": {
        "Authorization": "your-token"
      },
      "timeout": "30s",
      "disabled": false
    }
  }
}
```

### Server Types

| Type | Description |
|------|-------------|
| `stdio` | Local process with stdin/stdout communication |
| `http` | HTTP/StreamableHTTP endpoint |
| `sse` | Server-Sent Events endpoint |

## Files Structure

```
mcp-gateway/
├── cmd/mcp-gateway/              # Main entry point
├── internal/
│   ├── adapter/                  # MCP server adapters
│   ├── config/                   # Configuration management
│   ├── protocol/                 # MCP protocol definitions
│   ├── router/                   # Message routing
│   └── upstream/                 # WebSocket upstream client
├── pkg/retry/                    # Retry utilities
├── configs/
│   ├── mcp_config.example.json   # ⭐ 配置示例（提交到 git）
│   └── mcp_config.json           # 🔒 实际配置（不提交）
├── run_local.example.sh          # ⭐ 本地脚本示例（提交到 git）
├── run_local.sh                  # 🔒 本地脚本（不提交，含 token）
├── deploy_remote.example.sh      # ⭐ 部署脚本示例（提交到 git）
├── deploy_remote.sh              # 🔒 部署脚本（不提交，含 token）
└── .gitignore
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
