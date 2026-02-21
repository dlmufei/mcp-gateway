#!/bin/bash
# =====================================================
# MCP Gateway - 本地 macOS 运行脚本
# 
# 使用方法：
# 1. 复制此文件为 run_local.sh
# 2. 修改 MCP_ENDPOINT 为你的实际 token
# 3. chmod +x run_local.sh
# =====================================================

set -e

# 项目目录
PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY_NAME="mcp-gateway"
CONFIG_FILE="${PROJECT_DIR}/configs/mcp_config.json"
LOG_FILE="${PROJECT_DIR}/mcp-gateway.log"
PID_FILE="${PROJECT_DIR}/mcp-gateway.pid"

# ⚠️ 修改为你的 MCP 端点 Token
export MCP_ENDPOINT='wss://api.xiaozhi.me/mcp/?token=YOUR_TOKEN_HERE'

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 编译 macOS 版本
build() {
    log_info "正在编译 macOS (darwin/amd64) 版本..."
    cd "$PROJECT_DIR"
    
    # 编译 macOS amd64
    GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o "${BINARY_NAME}" ./cmd/mcp-gateway
    
    if [ $? -eq 0 ]; then
        log_info "编译成功: ${PROJECT_DIR}/${BINARY_NAME}"
        ls -lh "${PROJECT_DIR}/${BINARY_NAME}"
    else
        log_error "编译失败"
        exit 1
    fi
}

# 启动服务
start() {
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if ps -p "$PID" > /dev/null 2>&1; then
            log_warn "服务已在运行中 (PID: $PID)"
            return 0
        fi
    fi
    
    # 检查二进制文件
    if [ ! -f "${PROJECT_DIR}/${BINARY_NAME}" ]; then
        log_warn "二进制文件不存在，先进行编译..."
        build
    fi
    
    log_info "启动 MCP Gateway..."
    log_info "配置文件: ${CONFIG_FILE}"
    log_info "日志文件: ${LOG_FILE}"
    
    cd "$PROJECT_DIR"
    nohup "./${BINARY_NAME}" -config "${CONFIG_FILE}" > "${LOG_FILE}" 2>&1 &
    
    PID=$!
    echo $PID > "$PID_FILE"
    
    sleep 1
    if ps -p "$PID" > /dev/null 2>&1; then
        log_info "服务启动成功 (PID: $PID)"
    else
        log_error "服务启动失败，查看日志: ${LOG_FILE}"
        cat "${LOG_FILE}" | tail -20
        exit 1
    fi
}

# 停止服务
stop() {
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if ps -p "$PID" > /dev/null 2>&1; then
            log_info "停止服务 (PID: $PID)..."
            kill "$PID"
            sleep 1
            if ps -p "$PID" > /dev/null 2>&1; then
                log_warn "正常停止失败，强制终止..."
                kill -9 "$PID"
            fi
            rm -f "$PID_FILE"
            log_info "服务已停止"
        else
            log_warn "进程不存在，清理 PID 文件"
            rm -f "$PID_FILE"
        fi
    else
        log_warn "PID 文件不存在，服务可能未运行"
    fi
}

# 重启服务
restart() {
    stop
    sleep 1
    start
}

# 查看状态
status() {
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}       MCP Gateway 状态 (macOS)        ${NC}"
    echo -e "${BLUE}========================================${NC}"
    
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if ps -p "$PID" > /dev/null 2>&1; then
            echo -e "状态: ${GREEN}运行中${NC}"
            echo -e "PID:  ${PID}"
            echo -e "内存: $(ps -p $PID -o rss= | awk '{print int($1/1024)"MB"}')"
        else
            echo -e "状态: ${RED}已停止${NC} (PID 文件存在但进程不存在)"
        fi
    else
        echo -e "状态: ${RED}未运行${NC}"
    fi
    
    echo -e "${BLUE}----------------------------------------${NC}"
    echo -e "配置: ${CONFIG_FILE}"
    echo -e "日志: ${LOG_FILE}"
    echo -e "${BLUE}========================================${NC}"
}

# 查看日志
logs() {
    if [ -f "$LOG_FILE" ]; then
        tail -f "$LOG_FILE"
    else
        log_error "日志文件不存在: ${LOG_FILE}"
    fi
}

# 前台运行（调试用）
run() {
    if [ ! -f "${PROJECT_DIR}/${BINARY_NAME}" ]; then
        log_warn "二进制文件不存在，先进行编译..."
        build
    fi
    
    log_info "前台运行 MCP Gateway (Ctrl+C 退出)..."
    cd "$PROJECT_DIR"
    "./${BINARY_NAME}" -config "${CONFIG_FILE}"
}

# 显示帮助
usage() {
    echo "用法: $0 {build|start|stop|restart|status|logs|run}"
    echo ""
    echo "命令:"
    echo "  build    - 编译 macOS 版本"
    echo "  start    - 后台启动服务"
    echo "  stop     - 停止服务"
    echo "  restart  - 重启服务"
    echo "  status   - 查看服务状态"
    echo "  logs     - 查看实时日志"
    echo "  run      - 前台运行（调试用）"
}

# 主入口
case "$1" in
    build)
        build
        ;;
    start)
        start
        ;;
    stop)
        stop
        ;;
    restart)
        restart
        ;;
    status)
        status
        ;;
    logs)
        logs
        ;;
    run)
        run
        ;;
    *)
        usage
        exit 1
        ;;
esac
