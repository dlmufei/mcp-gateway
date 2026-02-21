#!/bin/bash
# =====================================================
# MCP Gateway - 远程 Linux 服务器部署脚本
#
# 使用方法：
# 1. 复制此文件为 deploy_remote.sh
# 2. 修改 REMOTE_HOST 为你的服务器地址
# 3. 修改 MCP_ENDPOINT_TOKEN 为你的实际 token
# 4. chmod +x deploy_remote.sh
#
# 目标服务器: ssh root@your-server-ip
# 部署目录: /opt/mcp-gateway
# =====================================================

set -e

# 项目目录
PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY_NAME="mcp-gateway-linux"

# ⚠️ 修改为你的服务器地址
REMOTE_HOST="root@your-server-ip"
REMOTE_DIR="/opt/mcp-gateway"

# ⚠️ 修改为你的 MCP 端点 Token
MCP_ENDPOINT_TOKEN='wss://api.xiaozhi.me/mcp/?token=YOUR_TOKEN_HERE'

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

# 编译 Linux 版本
build_linux() {
    log_info "正在编译 Linux (linux/amd64) 版本..."
    cd "$PROJECT_DIR"
    
    # 交叉编译 Linux amd64
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "${BINARY_NAME}" ./cmd/mcp-gateway
    
    if [ $? -eq 0 ]; then
        log_info "编译成功: ${PROJECT_DIR}/${BINARY_NAME}"
        ls -lh "${PROJECT_DIR}/${BINARY_NAME}"
    else
        log_error "编译失败"
        exit 1
    fi
}

# 部署到远程服务器
deploy() {
    log_info "开始部署到 ${REMOTE_HOST}:${REMOTE_DIR}"
    
    # 1. 编译 Linux 版本
    build_linux
    
    # 2. 创建远程目录
    log_info "创建远程目录..."
    ssh ${REMOTE_HOST} "mkdir -p ${REMOTE_DIR}/configs"
    
    # 3. 停止现有服务
    log_info "停止现有服务（如果存在）..."
    ssh ${REMOTE_HOST} "if [ -f ${REMOTE_DIR}/run.sh ]; then cd ${REMOTE_DIR} && ./run.sh stop || true; fi"
    
    # 4. 上传文件
    log_info "上传二进制文件..."
    scp "${PROJECT_DIR}/${BINARY_NAME}" "${REMOTE_HOST}:${REMOTE_DIR}/mcp-gateway"
    
    log_info "上传配置文件..."
    scp "${PROJECT_DIR}/configs/mcp_config.json" "${REMOTE_HOST}:${REMOTE_DIR}/configs/"
    
    # 5. 创建远程运行脚本
    log_info "创建远程运行脚本..."
    ssh ${REMOTE_HOST} "cat > ${REMOTE_DIR}/run.sh << 'REMOTE_SCRIPT'
#!/bin/bash
# =====================================================
# MCP Gateway - Linux 服务器运行脚本
# =====================================================

set -e

# 项目目录
PROJECT_DIR=\"/opt/mcp-gateway\"
BINARY_NAME=\"mcp-gateway\"
CONFIG_FILE=\"\${PROJECT_DIR}/configs/mcp_config.json\"
LOG_FILE=\"\${PROJECT_DIR}/mcp-gateway.log\"
PID_FILE=\"\${PROJECT_DIR}/mcp-gateway.pid\"

# MCP 端点配置
export MCP_ENDPOINT='${MCP_ENDPOINT_TOKEN}'

# 颜色输出
RED='\\033[0;31m'
GREEN='\\033[0;32m'
YELLOW='\\033[1;33m'
BLUE='\\033[0;34m'
NC='\\033[0m'

log_info() {
    echo -e \"\${GREEN}[INFO]\${NC} \$1\"
}

log_warn() {
    echo -e \"\${YELLOW}[WARN]\${NC} \$1\"
}

log_error() {
    echo -e \"\${RED}[ERROR]\${NC} \$1\"
}

# 启动服务
start() {
    if [ -f \"\$PID_FILE\" ]; then
        PID=\$(cat \"\$PID_FILE\")
        if ps -p \"\$PID\" > /dev/null 2>&1; then
            log_warn \"服务已在运行中 (PID: \$PID)\"
            return 0
        fi
    fi
    
    log_info \"启动 MCP Gateway...\"
    log_info \"配置文件: \${CONFIG_FILE}\"
    log_info \"日志文件: \${LOG_FILE}\"
    
    cd \"\$PROJECT_DIR\"
    nohup \"./\${BINARY_NAME}\" -config \"\${CONFIG_FILE}\" > \"\${LOG_FILE}\" 2>&1 &
    
    PID=\$!
    echo \$PID > \"\$PID_FILE\"
    
    sleep 1
    if ps -p \"\$PID\" > /dev/null 2>&1; then
        log_info \"服务启动成功 (PID: \$PID)\"
    else
        log_error \"服务启动失败，查看日志: \${LOG_FILE}\"
        tail -20 \"\${LOG_FILE}\"
        exit 1
    fi
}

# 停止服务
stop() {
    if [ -f \"\$PID_FILE\" ]; then
        PID=\$(cat \"\$PID_FILE\")
        if ps -p \"\$PID\" > /dev/null 2>&1; then
            log_info \"停止服务 (PID: \$PID)...\"
            kill \"\$PID\"
            sleep 1
            if ps -p \"\$PID\" > /dev/null 2>&1; then
                log_warn \"正常停止失败，强制终止...\"
                kill -9 \"\$PID\"
            fi
            rm -f \"\$PID_FILE\"
            log_info \"服务已停止\"
        else
            log_warn \"进程不存在，清理 PID 文件\"
            rm -f \"\$PID_FILE\"
        fi
    else
        log_warn \"PID 文件不存在，服务可能未运行\"
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
    echo -e \"\${BLUE}========================================\${NC}\"
    echo -e \"\${BLUE}       MCP Gateway 状态 (Linux)        \${NC}\"
    echo -e \"\${BLUE}========================================\${NC}\"
    
    if [ -f \"\$PID_FILE\" ]; then
        PID=\$(cat \"\$PID_FILE\")
        if ps -p \"\$PID\" > /dev/null 2>&1; then
            echo -e \"状态: \${GREEN}运行中\${NC}\"
            echo -e \"PID:  \${PID}\"
            MEM=\$(ps -p \$PID -o rss= 2>/dev/null | awk '{print int(\$1/1024)\"MB\"}')
            echo -e \"内存: \${MEM}\"
        else
            echo -e \"状态: \${RED}已停止\${NC} (PID 文件存在但进程不存在)\"
        fi
    else
        echo -e \"状态: \${RED}未运行\${NC}\"
    fi
    
    echo -e \"\${BLUE}----------------------------------------\${NC}\"
    echo -e \"配置: \${CONFIG_FILE}\"
    echo -e \"日志: \${LOG_FILE}\"
    echo -e \"\${BLUE}========================================\${NC}\"
}

# 查看日志
logs() {
    if [ -f \"\$LOG_FILE\" ]; then
        tail -f \"\$LOG_FILE\"
    else
        log_error \"日志文件不存在: \${LOG_FILE}\"
    fi
}

# 前台运行
run() {
    log_info \"前台运行 MCP Gateway (Ctrl+C 退出)...\"
    cd \"\$PROJECT_DIR\"
    \"./\${BINARY_NAME}\" -config \"\${CONFIG_FILE}\"
}

# 显示帮助
usage() {
    echo \"用法: \$0 {start|stop|restart|status|logs|run}\"
    echo \"\"
    echo \"命令:\"
    echo \"  start    - 后台启动服务\"
    echo \"  stop     - 停止服务\"
    echo \"  restart  - 重启服务\"
    echo \"  status   - 查看服务状态\"
    echo \"  logs     - 查看实时日志\"
    echo \"  run      - 前台运行（调试用）\"
}

# 主入口
case \"\$1\" in
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
REMOTE_SCRIPT"
    
    # 6. 设置权限
    log_info "设置文件权限..."
    ssh ${REMOTE_HOST} "chmod +x ${REMOTE_DIR}/mcp-gateway ${REMOTE_DIR}/run.sh"
    
    # 7. 启动服务
    log_info "启动服务..."
    ssh ${REMOTE_HOST} "cd ${REMOTE_DIR} && ./run.sh start"
    
    # 8. 显示状态
    log_info "部署完成！查看状态..."
    ssh ${REMOTE_HOST} "cd ${REMOTE_DIR} && ./run.sh status"
}

# 仅编译
build_only() {
    build_linux
}

# 仅上传
upload() {
    log_info "上传文件到 ${REMOTE_HOST}:${REMOTE_DIR}"
    
    ssh ${REMOTE_HOST} "mkdir -p ${REMOTE_DIR}/configs"
    
    if [ -f "${PROJECT_DIR}/${BINARY_NAME}" ]; then
        scp "${PROJECT_DIR}/${BINARY_NAME}" "${REMOTE_HOST}:${REMOTE_DIR}/mcp-gateway"
        ssh ${REMOTE_HOST} "chmod +x ${REMOTE_DIR}/mcp-gateway"
        log_info "二进制文件上传完成"
    else
        log_error "二进制文件不存在，请先执行 build"
        exit 1
    fi
    
    scp "${PROJECT_DIR}/configs/mcp_config.json" "${REMOTE_HOST}:${REMOTE_DIR}/configs/"
    log_info "配置文件上传完成"
}

# 远程启动
remote_start() {
    log_info "远程启动服务..."
    ssh ${REMOTE_HOST} "cd ${REMOTE_DIR} && ./run.sh start"
}

# 远程停止
remote_stop() {
    log_info "远程停止服务..."
    ssh ${REMOTE_HOST} "cd ${REMOTE_DIR} && ./run.sh stop"
}

# 远程重启
remote_restart() {
    log_info "远程重启服务..."
    ssh ${REMOTE_HOST} "cd ${REMOTE_DIR} && ./run.sh restart"
}

# 远程状态
remote_status() {
    ssh ${REMOTE_HOST} "cd ${REMOTE_DIR} && ./run.sh status"
}

# 远程日志
remote_logs() {
    ssh ${REMOTE_HOST} "cd ${REMOTE_DIR} && tail -100 mcp-gateway.log"
}

# 远程实时日志
remote_logs_follow() {
    ssh ${REMOTE_HOST} "cd ${REMOTE_DIR} && tail -f mcp-gateway.log"
}

# 显示帮助
usage() {
    echo "用法: $0 {deploy|build|upload|start|stop|restart|status|logs|logs-f}"
    echo ""
    echo "命令:"
    echo "  deploy   - 完整部署（编译 + 上传 + 启动）"
    echo "  build    - 仅编译 Linux 版本"
    echo "  upload   - 仅上传文件到远程服务器"
    echo "  start    - 远程启动服务"
    echo "  stop     - 远程停止服务"
    echo "  restart  - 远程重启服务"
    echo "  status   - 查看远程服务状态"
    echo "  logs     - 查看远程日志（最近100行）"
    echo "  logs-f   - 实时查看远程日志"
    echo ""
    echo "远程服务器: ${REMOTE_HOST}"
    echo "部署目录:   ${REMOTE_DIR}"
}

# 主入口
case "$1" in
    deploy)
        deploy
        ;;
    build)
        build_only
        ;;
    upload)
        upload
        ;;
    start)
        remote_start
        ;;
    stop)
        remote_stop
        ;;
    restart)
        remote_restart
        ;;
    status)
        remote_status
        ;;
    logs)
        remote_logs
        ;;
    logs-f)
        remote_logs_follow
        ;;
    *)
        usage
        exit 1
        ;;
esac
