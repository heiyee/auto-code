#!/usr/bin/env bash

set -euo pipefail

log_timestamp() {
  date '+%Y-%m-%d %H:%M:%S'
}

log_info() {
  printf '[INFO] %s %s\n' "$(log_timestamp)" "$*"
}

log_warn() {
  printf '[WARN] %s %s\n' "$(log_timestamp)" "$*"
}

log_error() {
  printf '[ERROR] %s %s\n' "$(log_timestamp)" "$*" >&2
}

die() {
  log_error "$*"
  exit 1
}

require_command() {
  local command_name=""
  for command_name in "$@"; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
      die "缺少必要命令: $command_name"
    fi
  done
}

normalize_path() {
  case "$1" in
    /*) printf '%s\n' "$1" ;;
    *) printf '%s/%s\n' "$PWD" "$1" ;;
  esac
}

build_info_value() {
  local info_file="$1"
  local key_name="$2"

  [ -f "$info_file" ] || return 1

  awk -F= -v key_name="$key_name" '
    $1 == key_name {
      sub(/^[^=]*=/, "", $0)
      print
      exit
    }
  ' "$info_file"
}

release_env_value() {
  local env_file="$1"
  local key_name="$2"

  [ -f "$env_file" ] || return 1

  (
    set -a
    . "$env_file"
    eval "printf '%s\n' \"\${$key_name:-}\""
  )
}

generate_build_version() {
  local project_root="${1:-$PWD}"
  local timestamp=""
  local git_commit=""

  timestamp="$(date '+%Y%m%d%H%M%S')"
  git_commit="$(git -C "$project_root" rev-parse --short HEAD 2>/dev/null || printf 'nogit')"
  printf '%s_%s\n' "$timestamp" "$git_commit"
}

detect_local_clash_http_proxy() {
  local candidate=""
  local candidates=(
    "127.0.0.1:7890"
    "127.0.0.1:7897"
    "127.0.0.1:9090"
  )

  command -v ss >/dev/null 2>&1 || return 1

  for candidate in "${candidates[@]}"; do
    if ss -ltn 2>/dev/null | awk '{print $4}' | grep -qx "$candidate"; then
      printf 'http://%s\n' "$candidate"
      return 0
    fi
  done

  return 1
}

ensure_default_proxy_env() {
  local detected_proxy=""
  local host_proxy=""
  local no_proxy_default="127.0.0.1,localhost,::1,host.docker.internal"

  host_proxy="${AUTO_CODE_HTTP_PROXY:-${HTTP_PROXY:-${http_proxy:-}}}"
  if [ -z "$host_proxy" ]; then
    detected_proxy="$(detect_local_clash_http_proxy 2>/dev/null || true)"
    host_proxy="$detected_proxy"
  fi

  [ -n "$host_proxy" ] || return 0

  export HTTP_PROXY="${HTTP_PROXY:-$host_proxy}"
  export HTTPS_PROXY="${HTTPS_PROXY:-$HTTP_PROXY}"
  export http_proxy="${http_proxy:-$HTTP_PROXY}"
  export https_proxy="${https_proxy:-$HTTPS_PROXY}"
  export NO_PROXY="${NO_PROXY:-$no_proxy_default}"
  export no_proxy="${no_proxy:-$NO_PROXY}"
}

command_matches_binary() {
  local command_line="$1"
  local binary_path="$2"
  local binary_name="$3"
  local executable=""

  executable="${command_line%% *}"
  case "$executable" in
    "$binary_path"|"$binary_path "*) return 0 ;;
  esac

  [ -n "$binary_name" ] || return 1
  case "$(basename "$executable")" in
    "$binary_name") return 0 ;;
  esac

  return 1
}

find_pid_by_binary_path() {
  local binary_path="$1"
  local binary_name=""
  local ps_output=""
  local pid=""
  local command_line=""
  local line=""
  local trimmed_line=""

  binary_name="$(basename "$binary_path")"
  ps_output="$(ps -eo pid=,command= 2>/dev/null || true)"

  while IFS= read -r line; do
    [ -n "$line" ] || continue
    trimmed_line="${line#"${line%%[![:space:]]*}"}"
    pid="$(printf '%s\n' "$trimmed_line" | awk '{print $1}')"
    command_line="${trimmed_line#"$pid"}"
    command_line="${command_line#"${command_line%%[![:space:]]*}"}"
    if command_matches_binary "$command_line" "$binary_path" "$binary_name"; then
      printf '%s\n' "$pid"
      return 0
    fi
  done <<EOF
$ps_output
EOF

  return 1
}

managed_pid() {
  local pid_file="$1"
  local binary_path="$2"
  local binary_name=""
  local pid=""
  local command_line=""

  binary_name="$(basename "$binary_path")"

  if [ -f "$pid_file" ]; then
    pid="$(tr -d '[:space:]' <"$pid_file" 2>/dev/null || true)"
    if [ -n "$pid" ]; then
      command_line="$(ps -p "$pid" -o command= 2>/dev/null || true)"
      if [ -n "$command_line" ] && command_matches_binary "$command_line" "$binary_path" "$binary_name"; then
        printf '%s\n' "$pid"
        return 0
      fi
    fi
    rm -f "$pid_file"
  fi

  if pid="$(find_pid_by_binary_path "$binary_path" 2>/dev/null || true)"; then
    if [ -n "$pid" ]; then
      printf '%s\n' "$pid" >"$pid_file"
      printf '%s\n' "$pid"
      return 0
    fi
  fi

  return 1
}

stop_managed_service() {
  local pid_file="$1"
  local binary_path="$2"
  local service_name="$3"
  local pid=""
  local wait_count=0

  if ! pid="$(managed_pid "$pid_file" "$binary_path" 2>/dev/null || true)"; then
    log_info "$service_name 没有运行中的旧进程"
    rm -f "$pid_file"
    return 0
  fi

  if [ -z "$pid" ]; then
    log_info "$service_name 没有运行中的旧进程"
    rm -f "$pid_file"
    return 0
  fi

  log_info "停止 $service_name 旧进程 PID=$pid"
  kill -TERM "$pid" 2>/dev/null || true

  while [ "$wait_count" -lt 20 ]; do
    if ! ps -p "$pid" >/dev/null 2>&1; then
      rm -f "$pid_file"
      log_info "$service_name 已停止"
      return 0
    fi
    sleep 1
    wait_count=$((wait_count + 1))
  done

  log_warn "$service_name 在 20 秒内未退出，升级为 SIGKILL"
  kill -KILL "$pid" 2>/dev/null || true
  sleep 1

  if ps -p "$pid" >/dev/null 2>&1; then
    die "$service_name 停止失败，请人工检查 PID=$pid"
  fi

  rm -f "$pid_file"
  log_info "$service_name 已强制停止"
}

start_managed_service() {
  local working_dir="$1"
  local binary_path="$2"
  local pid_file="$3"
  local startup_log="$4"
  local service_env_file="$5"
  local fixed_data_dir="$6"
  local pid=""
  local command_line=""
  local line=""

  shift 6

  mkdir -p "$(dirname "$startup_log")"
  : >"$startup_log"
  rm -f "$pid_file"

  (
    cd "$working_dir"
    if [ -n "$service_env_file" ] && [ -f "$service_env_file" ]; then
      set -a
      . "$service_env_file"
      set +a
    fi
    if [ -n "$fixed_data_dir" ]; then
      export DATA_DIR="$fixed_data_dir"
    fi
    nohup "$binary_path" "$@" >>"$startup_log" 2>&1 < /dev/null &
    printf '%s\n' "$!" >"$pid_file"
  )

  sleep 1
  pid="$(tr -d '[:space:]' <"$pid_file" 2>/dev/null || true)"
  if [ -z "$pid" ]; then
    log_error "启动失败: 未获取到进程 PID"
  else
    command_line="$(ps -p "$pid" -o command= 2>/dev/null || true)"
    if [ -n "$command_line" ] && command_matches_binary "$command_line" "$binary_path" "$(basename "$binary_path")"; then
      log_info "服务已启动 PID=$pid"
      return 0
    fi
    rm -f "$pid_file"
    log_error "启动失败: 进程未保持运行"
  fi

  if [ -s "$startup_log" ]; then
    log_warn "启动输出:"
    while IFS= read -r line; do
      log_warn "  $line"
    done <"$startup_log"
  fi

  return 1
}
