#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$SCRIPT_DIR/lib/admin-common.sh"

DEPLOY_SCRIPT_VERSION="2026-04-07.1"

usage() {
  cat <<EOF
用法:
  ./scripts/deploy.sh --package ./bin/release/auto-code-backend.tar.gz --deploy-root /home/kevin/deploy/auto-code-backend
  ./scripts/deploy.sh --package ./bin/release/auto-code-backend --deploy-root /home/kevin/deploy/auto-code-backend

说明:
  - 仅接收已经构建好的 auto-code-backend 发布目录或压缩包
  - 发布流程: 停服 -> 备份 current -> 投放新版本 -> 同步 app.yaml -> 启动 -> 失败时回滚
  - SQLite 数据固定保留在 <deploy-root>/auto-code.db，不随发布目录替换
  - 运行配置固定保留在 <deploy-root>/app.yaml
  - 环境变量固定保留在 <deploy-root>/service.env
  - 当前运行目录固定为 <deploy-root>/current

选项:
  --package <path>      发布目录或 .tar.gz 包
  --deploy-root <path>  目标部署目录
  --                    其后的参数原样传给服务进程
  -h, --help            显示帮助
EOF
}

startup_policy() {
  local policy="${AUTO_CODE_DEPLOY_STARTUP_POLICY:-}"

  if [ -z "$policy" ]; then
    policy="required"
  fi

  case "${policy,,}" in
    required|strict) printf '%s\n' "required" ;;
    best-effort|best_effort|ignore-failure|ignore_failure) printf '%s\n' "best-effort" ;;
    skip|disabled|none) printf '%s\n' "skip" ;;
    *) die "AUTO_CODE_DEPLOY_STARTUP_POLICY 仅支持 required、best-effort 或 skip" ;;
  esac
}

has_release_markers() {
  local candidate_dir="$1"
  [ -d "$candidate_dir/bin" ] || return 1
  [ -f "$candidate_dir/BUILD_INFO" ] || [ -f "$candidate_dir/release.env" ]
}

stage_release_to_dir() {
  local package_path="$1"
  local staged_release_dir="$2"

  mkdir -p "$staged_release_dir"
  if [ -d "$package_path" ]; then
    cp -R "$package_path"/. "$staged_release_dir"/
  else
    tar -xf "$package_path" -C "$staged_release_dir"
  fi
}

locate_release_dir() {
  local staged_release_dir="$1"
  local marker_path=""
  local child_dir=""
  local child_count=0
  local selected_child=""

  if has_release_markers "$staged_release_dir"; then
    printf '%s\n' "$staged_release_dir"
    return 0
  fi

  while IFS= read -r marker_path; do
    [ -n "$marker_path" ] || continue
    printf '%s\n' "${marker_path%/*}"
    return 0
  done <<EOF
$(find "$staged_release_dir" -mindepth 2 -maxdepth 4 -type f \( -name 'release.env' -o -name 'BUILD_INFO' \) | LC_ALL=C sort)
EOF

  while IFS= read -r child_dir; do
    [ -n "$child_dir" ] || continue
    child_count=$((child_count + 1))
    selected_child="$child_dir"
  done <<EOF
$(find "$staged_release_dir" -mindepth 1 -maxdepth 1 -type d | LC_ALL=C sort)
EOF

  if [ "$child_count" -eq 1 ] && [ -n "$selected_child" ]; then
    printf '%s\n' "$selected_child"
    return 0
  fi

  printf '%s\n' "$staged_release_dir"
}

resolve_release_metadata() {
  local release_dir="$1"
  local module_name=""
  local binary_name=""
  local info_file="$release_dir/BUILD_INFO"

  module_name="$(build_info_value "$info_file" module 2>/dev/null || true)"
  binary_name="$(build_info_value "$info_file" binary 2>/dev/null || true)"

  if [ -z "$module_name" ]; then
    module_name="$(release_env_value "$release_dir/release.env" MODULE_NAME 2>/dev/null || true)"
  fi
  if [ -z "$binary_name" ]; then
    binary_name="$(release_env_value "$release_dir/release.env" BINARY_NAME 2>/dev/null || true)"
  fi

  if [ -z "$binary_name" ] && [ -d "$release_dir/bin" ]; then
    binary_name="$(find "$release_dir/bin" -mindepth 1 -maxdepth 1 -type f ! -name '.*' -printf '%f\n' | LC_ALL=C sort | sed -n '1p')"
  fi
  if [ -z "$module_name" ] && [ -n "$binary_name" ]; then
    module_name="$binary_name"
  fi

  [ -n "$module_name" ] || die "无法识别发布模块名，请检查 BUILD_INFO/release.env"
  [ -n "$binary_name" ] || die "无法识别发布二进制，请检查 bin/ 或 BUILD_INFO/release.env"

  printf '%s\n%s\n' "$module_name" "$binary_name"
}

backup_current_release() {
  local current_dir="$1"
  local backup_root="$2"
  local backup_path=""

  [ -d "$current_dir" ] || return 0

  mkdir -p "$backup_root"
  backup_path="$(mktemp -d "$backup_root/$(date '+%Y%m%d%H%M%S').XXXXXX")"
  cp -R "$current_dir"/. "$backup_path"/
  printf '%s\n' "$backup_path"
}

restore_release_backup() {
  local backup_path="$1"
  local current_dir="$2"

  [ -d "$backup_path" ] || return 1
  rm -rf "$current_dir"
  mkdir -p "$current_dir"
  cp -R "$backup_path"/. "$current_dir"/
}

prune_release_backups() {
  local backup_root="$1"
  local keep_count="$2"
  local backup_count=""
  local prune_count=0
  local entry=""

  [ -d "$backup_root" ] || return 0

  backup_count="$(find "$backup_root" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d '[:space:]')"
  case "$backup_count" in
    ''|*[!0-9]*) backup_count="0" ;;
  esac

  [ "$backup_count" -gt "$keep_count" ] || return 0

  prune_count=$((backup_count - keep_count))
  while IFS= read -r entry; do
    [ -n "$entry" ] || continue
    rm -rf "$entry"
    log_info "已删除旧备份: $entry"
  done <<EOF
$(find "$backup_root" -mindepth 1 -maxdepth 1 -type d | LC_ALL=C sort | head -n "$prune_count")
EOF
}

seed_runtime_app_config() {
  local release_dir="$1"
  local deploy_root="$2"
  local runtime_app="$deploy_root/app.yaml"
  local runtime_app_example="$deploy_root/app.yaml.example"

  if [ -f "$release_dir/app.yaml" ]; then
    cp "$release_dir/app.yaml" "$runtime_app_example"
  fi

  if [ -f "$runtime_app" ]; then
    return 0
  fi

  [ -f "$release_dir/app.yaml" ] || die "首次部署缺少 app.yaml，无法初始化运行配置"
  cp "$release_dir/app.yaml" "$runtime_app"
  log_warn "deploy_root/app.yaml 不存在，已用发布包初始化: $runtime_app"
  log_warn "请在首次发布后按实际环境检查并调整该配置文件"
}

sync_runtime_app_config() {
  local deploy_root="$1"
  local current_dir="$2"
  local runtime_app="$deploy_root/app.yaml"

  [ -f "$runtime_app" ] || die "运行配置不存在: $runtime_app"
  cp "$runtime_app" "$current_dir/app.yaml"
}

ensure_service_env_file() {
  local service_env_file="$1"
  local deploy_root="$2"

  if [ -f "$service_env_file" ]; then
    return 0
  fi

  cat >"$service_env_file" <<EOF
# auto-code-backend 运行环境变量
# 当前文件只会在首次发布时生成，后续发布不会覆盖。

# DATA_DIR 由 deploy.sh 在启动时固定注入为部署根目录，避免发布 current/ 时被清空。
# 不要在此文件中覆盖 DATA_DIR；如需调整策略，应修改 deploy.sh。

# 可选：如需覆盖端口，可取消注释。
# export PORT='9080'

# 可选：生产环境建议通过环境变量覆盖认证信息，而不是直接改仓库里的 app.yaml。
# export AUTH_USERNAME='replace-me'
# export AUTH_PASSWORD='replace-me'
# export AUTH_SESSION_SECRET='replace-with-32-byte-random-secret'

# 可选：运行所需的 CLI token 也可在这里注入。
# export ZHI_PU_AUTH_TOKEN='replace-me'

# 可选：如果 codex/claude/cursor 安装目录不在 TeamCity 或守护进程 PATH 中，
# 可显式追加 CLI 可执行文件目录。支持使用 : 分隔多个目录。
# export AUTO_CODE_CLI_EXTRA_PATH='$HOME/.nvm/versions/node/v20.12.2/bin:$HOME/.local/bin'
EOF
}

deploy_release() {
  local package_path="$1"
  local deploy_root="$2"
  local staging_root=""
  local staged_release_dir=""
  local effective_release_dir=""
  local metadata=""
  local module_name=""
  local binary_name=""
  local current_dir=""
  local current_binary=""
  local pid_file=""
  local logs_dir=""
  local startup_log=""
  local backup_root=""
  local backup_path=""
  local service_env_file=""
  local current_startup_policy=""

  [ -n "$package_path" ] || die "请通过 --package 指定发布目录或压缩包"
  [ -n "$deploy_root" ] || die "请通过 --deploy-root 指定部署目录"

  package_path="$(normalize_path "$package_path")"
  deploy_root="$(normalize_path "$deploy_root")"
  if [ ! -f "$package_path" ] && [ ! -d "$package_path" ]; then
    die "构建产物不存在: $package_path"
  fi

  log_info "deploy.sh version=${DEPLOY_SCRIPT_VERSION}"
  if [ -f "$package_path" ]; then
    log_info "发布输入: package_file=$package_path"
  else
    log_info "发布输入: package_dir=$package_path"
  fi

  staging_root="$(mktemp -d "${TMPDIR:-/tmp}/auto-code-deploy.XXXXXX")"
  staged_release_dir="$staging_root/release"
  stage_release_to_dir "$package_path" "$staged_release_dir"
  effective_release_dir="$(locate_release_dir "$staged_release_dir")"
  metadata="$(resolve_release_metadata "$effective_release_dir")"
  module_name="$(printf '%s\n' "$metadata" | sed -n '1p')"
  binary_name="$(printf '%s\n' "$metadata" | sed -n '2p')"

  current_dir="$deploy_root/current"
  current_binary="$current_dir/bin/$binary_name"
  pid_file="$deploy_root/${module_name}.pid"
  logs_dir="$deploy_root/logs"
  startup_log="$logs_dir/startup.log"
  backup_root="$deploy_root/backup/releases"
  service_env_file="$deploy_root/service.env"

  mkdir -p "$deploy_root" "$logs_dir" "$backup_root"
  seed_runtime_app_config "$effective_release_dir" "$deploy_root"
  ensure_service_env_file "$service_env_file" "$deploy_root"

  [ -f "$effective_release_dir/bin/$binary_name" ] || die "构建产物缺少二进制: bin/$binary_name"
  chmod +x "$effective_release_dir/bin/$binary_name"

  current_startup_policy="$(startup_policy)"
  log_info "发布诊断: module_name=$module_name binary_name=$binary_name startup_policy=$current_startup_policy"

  stop_managed_service "$pid_file" "$current_binary" "$module_name"

  backup_path="$(backup_current_release "$current_dir" "$backup_root")"
  if [ -n "$backup_path" ]; then
    log_info "${module_name} 当前版本已备份到: ${backup_path}"
  fi

  rm -rf "$current_dir"
  mkdir -p "$current_dir"
  cp -R "$effective_release_dir"/. "$current_dir"/
  sync_runtime_app_config "$deploy_root" "$current_dir"
  chmod +x "$current_binary"
  rm -rf "$staging_root"

  if [ "$current_startup_policy" = "skip" ]; then
    prune_release_backups "$backup_root" 3
    log_warn "${module_name} 已按策略跳过启动校验，当前版本文件已投放到: $deploy_root"
    return 0
  fi

  if start_managed_service "$current_dir" "$current_binary" "$pid_file" "$startup_log" "$service_env_file" "$deploy_root" "${SERVICE_ARGS[@]-}"; then
    prune_release_backups "$backup_root" 3
    log_info "${module_name} 发布成功: $deploy_root"
    log_info "运行目录: $current_dir"
    log_info "运行配置: $deploy_root/app.yaml"
    log_info "数据库文件: $deploy_root/auto-code.db"
    log_info "环境文件: $service_env_file"
    return 0
  fi

  if [ "$current_startup_policy" = "best-effort" ]; then
    prune_release_backups "$backup_root" 3
    log_warn "${module_name} 启动校验失败，但当前策略为 best-effort，已保留新版本文件"
    log_warn "请结合启动日志排查: $startup_log"
    return 0
  fi

  log_error "${module_name} 新版本启动失败，准备回滚"
  stop_managed_service "$pid_file" "$current_binary" "$module_name" || true

  if [ -n "$backup_path" ] && [ -d "$backup_path" ]; then
    log_warn "${module_name} 正在回滚到: $backup_path"
    restore_release_backup "$backup_path" "$current_dir" || die "${module_name} 回滚失败，无法恢复备份"
    sync_runtime_app_config "$deploy_root" "$current_dir"
    chmod +x "$current_binary" 2>/dev/null || true
    if start_managed_service "$current_dir" "$current_binary" "$pid_file" "$startup_log" "$service_env_file" "$deploy_root" "${SERVICE_ARGS[@]-}"; then
      log_warn "${module_name} 回滚版本已恢复运行"
      return 1
    fi
    die "${module_name} 新版本启动失败，回滚版本也未能启动，请人工检查"
  fi

  die "${module_name} 新版本启动失败，且没有可回滚备份"
}

require_command tar nohup ps awk sed tr cat cp find rm mkdir mktemp head chmod

PACKAGE_PATH=""
CUSTOM_DEPLOY_ROOT=""
SERVICE_ARGS=()

while [ "$#" -gt 0 ]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    --package)
      shift
      [ "$#" -gt 0 ] || die "--package 缺少参数"
      PACKAGE_PATH="$1"
      shift
      ;;
    --deploy-root)
      shift
      [ "$#" -gt 0 ] || die "--deploy-root 缺少参数"
      CUSTOM_DEPLOY_ROOT="$1"
      shift
      ;;
    --)
      shift
      while [ "$#" -gt 0 ]; do
        SERVICE_ARGS+=("$1")
        shift
      done
      ;;
    *)
      die "未知参数: $1"
      ;;
  esac
done

deploy_release "$PACKAGE_PATH" "$CUSTOM_DEPLOY_ROOT"
