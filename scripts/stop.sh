#!/bin/bash
set -e

# auto-code 停止脚本

APP_NAME="auto-code"
APP_BIN="./auto-code-linux-amd64"
PID_FILE="./auto-code.pid"

cd "$(dirname "$0")/.."

echo "Stopping $APP_NAME..."

if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if kill -0 "$PID" 2>/dev/null; then
        echo "Stopping process (PID: $PID)..."
        kill "$PID" 2>/dev/null || true
        sleep 2
        if kill -0 "$PID" 2>/dev/null; then
            echo "Force killing..."
            kill -9 "$PID" 2>/dev/null || true
        fi
    fi
    rm -f "$PID_FILE"
fi

# 也通过进程名检查
pkill -f "$APP_BIN" 2>/dev/null || true

echo "$APP_NAME stopped."
