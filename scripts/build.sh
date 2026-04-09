#!/bin/bash
set -e

# 编译auto-code为Linux可执行文件
# 前端文件通过embed打包进二进制

VERSION=${VERSION:-"dev"}
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS="-s -w -X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME"

echo "Building auto-code for Linux..."
echo "Version: $VERSION"
echo "Build Time: $BUILD_TIME"

cd "$(dirname "$0")/.."

# 编译 Linux amd64
echo "Building for linux/amd64..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o bin/auto-code-linux-amd64 .

# 编译 Linux arm64
echo "Building for linux/arm64..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$LDFLAGS" -o bin/auto-code-linux-arm64 .

echo "Build completed!"
ls -lh bin/
