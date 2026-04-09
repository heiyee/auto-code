#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
. "$SCRIPT_DIR/lib/admin-common.sh"

MODULE_NAME="${AUTO_CODE_MODULE_NAME:-auto-code-backend}"
BINARY_NAME="${AUTO_CODE_BINARY_NAME:-auto-code-backend}"
TARGET_OS="${AUTO_CODE_TARGET_OS:-linux}"
TARGET_ARCH="${AUTO_CODE_TARGET_ARCH:-amd64}"
TARGET_CGO_ENABLED="${AUTO_CODE_CGO_ENABLED:-0}"
RELEASE_ROOT="${AUTO_CODE_RELEASE_ROOT:-$PROJECT_ROOT/bin/release}"
FRONTEND_DIR="$PROJECT_ROOT/frontend"
EMBED_FRONTEND_DIR="$PROJECT_ROOT/internal/embedfs/frontend_dist"
BUILD_VERSION="${VERSION:-$(generate_build_version "$PROJECT_ROOT")}"
BUILD_TIME="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
GIT_COMMIT="$(git -C "$PROJECT_ROOT" rev-parse --short HEAD 2>/dev/null || printf 'nogit')"

usage() {
  cat <<EOF
用法:
  ./scripts/build-release.sh

说明:
  - 构建 Linux 发布包，输出到 bin/release/
  - 自动执行 frontend 的 npm ci + npm run build
  - frontend 构建结果会在编译前同步到 Go embed 目录，并直接打进二进制
  - 发布目录包含:
      bin/${BINARY_NAME}
      app.yaml
      BUILD_INFO
      release.env

可覆盖环境变量:
  AUTO_CODE_MODULE_NAME      默认: ${MODULE_NAME}
  AUTO_CODE_BINARY_NAME      默认: ${BINARY_NAME}
  AUTO_CODE_TARGET_OS        默认: ${TARGET_OS}
  AUTO_CODE_TARGET_ARCH      默认: ${TARGET_ARCH}
  AUTO_CODE_CGO_ENABLED      默认: ${TARGET_CGO_ENABLED}
  AUTO_CODE_RELEASE_ROOT     默认: ${RELEASE_ROOT}
  VERSION                    默认: ${BUILD_VERSION}
EOF
}

for arg in "$@"; do
  case "$arg" in
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "未知参数: $arg"
      ;;
  esac
done

case "$TARGET_CGO_ENABLED" in
  0|1) ;;
  *) die "AUTO_CODE_CGO_ENABLED 仅支持 0 或 1" ;;
esac

require_command go npm tar mktemp cp find rm mkdir
ensure_default_proxy_env

RELEASE_ROOT="$(normalize_path "$RELEASE_ROOT")"
ARTIFACT_DIR="$RELEASE_ROOT/$MODULE_NAME"
PACKAGE_PATH="$RELEASE_ROOT/${MODULE_NAME}.tar.gz"
STAGING_DIR=""
FRONTEND_BUILD_DIR=""

sync_frontend_embed_dir() {
  local source_dir="$1"
  local target_dir="$2"

  mkdir -p "$target_dir"
  find "$target_dir" -mindepth 1 -maxdepth 1 ! -name 'README.md' -exec rm -rf {} +
  cp -R "$source_dir"/. "$target_dir"/
}

cleanup_staging_dir() {
  if [ -n "${STAGING_DIR:-}" ] && [ -d "$STAGING_DIR" ]; then
    rm -rf "$STAGING_DIR"
  fi
}

trap cleanup_staging_dir EXIT

mkdir -p "$RELEASE_ROOT"
STAGING_DIR="$(mktemp -d "$RELEASE_ROOT/.tmp-${MODULE_NAME}.${BUILD_VERSION}.XXXXXX")"
FRONTEND_BUILD_DIR="$STAGING_DIR/frontend-dist-build"

log_info "开始构建 frontend 发布资源"
(
  cd "$FRONTEND_DIR"
  npm ci
  AUTO_CODE_FRONTEND_OUT_DIR="$FRONTEND_BUILD_DIR" npm run build
)

log_info "同步 frontend build 到 Go embed 目录"
sync_frontend_embed_dir "$FRONTEND_BUILD_DIR" "$EMBED_FRONTEND_DIR"
rm -rf "$FRONTEND_BUILD_DIR"

log_info "开始编译 ${MODULE_NAME} -> ${TARGET_OS}/${TARGET_ARCH}"
mkdir -p "$STAGING_DIR/bin"
(
  cd "$PROJECT_ROOT"
  GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" CGO_ENABLED="$TARGET_CGO_ENABLED" \
    go build -trimpath -ldflags "-s -w" -o "$STAGING_DIR/bin/$BINARY_NAME" .
)
chmod +x "$STAGING_DIR/bin/$BINARY_NAME"

cp "$PROJECT_ROOT/app.yaml" "$STAGING_DIR/app.yaml"

cat >"$STAGING_DIR/BUILD_INFO" <<EOF
module=${MODULE_NAME}
binary=${BINARY_NAME}
version=${BUILD_VERSION}
build_time=${BUILD_TIME}
git_commit=${GIT_COMMIT}
target_os=${TARGET_OS}
target_arch=${TARGET_ARCH}
frontend_mode=embedded
EOF

cat >"$STAGING_DIR/release.env" <<EOF
MODULE_NAME='${MODULE_NAME}'
BINARY_NAME='${BINARY_NAME}'
EOF

rm -rf "$ARTIFACT_DIR" "$PACKAGE_PATH"
mv "$STAGING_DIR" "$ARTIFACT_DIR"
STAGING_DIR=""
tar -czf "$PACKAGE_PATH" -C "$ARTIFACT_DIR" .

log_info "发布目录: $ARTIFACT_DIR"
log_info "发布压缩包: $PACKAGE_PATH"
log_info "构建版本: $BUILD_VERSION"
