#!/bin/bash
set -e

# auto-code 启动脚本
# 自动杀掉历史进程，使用nohup后台运行

APP_NAME="auto-code"
PORT=8082
APP_BIN="./auto-code-linux-amd64"
LOG_FILE="./auto-code.log"
PID_FILE="./auto-code.pid"
CONFIG_FILE="./app.yaml"
FRONTEND_DIST_DIR="./frontend/dist"

cd "$(dirname "$0")"

echo "Starting $APP_NAME on port $PORT..."

# 检查配置文件
if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: Config file not found: $CONFIG_FILE"
    echo "Please create app.yaml in current directory."
    exit 1
fi

if [ ! -f "${FRONTEND_DIST_DIR}/index.html" ]; then
    echo "Error: Frontend build not found: ${FRONTEND_DIST_DIR}/index.html"
    echo "Please package or build the frontend before starting the service."
    exit 1
fi

# 杀掉历史进程
if [ -f "$PID_FILE" ]; then
    OLD_PID=$(cat "$PID_FILE")
    if kill -0 "$OLD_PID" 2>/dev/null; then
        echo "Killing existing process (PID: $OLD_PID)..."
        kill "$OLD_PID" 2>/dev/null || true
        sleep 1
        # 强制杀掉如果还在运行
        if kill -0 "$OLD_PID" 2>/dev/null; then
            kill -9 "$OLD_PID" 2>/dev/null || true
        fi
    fi
    rm -f "$PID_FILE"
fi

# 也通过进程名检查并杀掉
pkill -f "$APP_BIN" 2>/dev/null || true
sleep 1

# 检查端口是否被占用
if lsof -i:"$PORT" >/dev/null 2>&1; then
    echo "Port $PORT is still in use, killing process..."
    lsof -ti:"$PORT" | xargs kill -9 2>/dev/null || true
    sleep 1
fi

# 启动服务
echo "Starting $APP_NAME..."
export PORT=$PORT
nohup "$APP_BIN" > "$LOG_FILE" 2>&1 &
echo $! > "$PID_FILE"

sleep 2

# 验证启动
if kill -0 $(cat "$PID_FILE") 2>/dev/null; then
    echo "$APP_NAME started successfully!"
    echo "  PID: $(cat $PID_FILE)"
    echo "  Port: $PORT"
    echo "  Config: $CONFIG_FILE"
    echo "  Log: $LOG_FILE"
    echo "  URL: http://127.0.0.1:$PORT"
else
    echo "Failed to start $APP_NAME. Check $LOG_FILE for details."
    exit 1
fi
