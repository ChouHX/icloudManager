#!/usr/bin/env bash
#
# 一键启动本地开发环境:Go 后端 + Vite 前端 dev server。
#
#   ./dev.sh                          # 后端 127.0.0.1:8081,前端 127.0.0.1:5173
#   ./dev.sh -b 9000 -f 4000          # 自定义端口
#   ./dev.sh -p 'your-password'       # 指定管理员密码
#   ./dev.sh --smoke                  # 启动后再跑一遍浏览器冒烟测试
#   ./dev.sh --backend-only           # 只启动后端(用内嵌构建产物调试时)
#
# 说明:
#   - 未提供密码时使用本地开发默认值,服务只监听 127.0.0.1,不要用于公网
#   - 首次启动会自动安装前端依赖(npm ci)并编译后端(go build)
#   - 前端 /api 请求经 vite 代理到后端;Ctrl-C 同时停止两端
#   - 日志分别写入 build/dev-logs/{api,web}.log,可单独查看
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

BACKEND_HOST="127.0.0.1"
BACKEND_PORT="8081"
FRONTEND_HOST="127.0.0.1"
FRONTEND_PORT="5173"
DATA_DIR="./data"
ADMIN_PASSWORD="${ICLOUD_HME_ADMIN_PASSWORD:-dev-admin-pass-2026}"
START_FRONTEND=1
RUN_SMOKE=0
STOP_ONLY=0

LOG_DIR="build/dev-logs"
API_LOG="$LOG_DIR/api.log"
WEB_LOG="$LOG_DIR/web.log"
API_PID_FILE="$LOG_DIR/api.pid"
WEB_PID_FILE="$LOG_DIR/web.pid"

usage() {
  cat <<'USAGE'
用法: ./dev.sh [选项]

  -b, --backend-port <port>   后端端口(默认 8081)
  -f, --frontend-port <port>  前端 dev server 端口(默认 5173)
  -d, --data <dir>            数据目录(默认 ./data)
  -p, --password <pwd>        管理员密码(默认取 ICLOUD_HME_ADMIN_PASSWORD)
  -s, --stop                  停止本项目残留的后端与前端进程后退出
      --smoke                 启动就绪后额外跑一遍浏览器冒烟测试
      --backend-only          只启动后端
  -h, --help                  显示本帮助
USAGE
  exit 0
}

# kill_tree 终止进程及其全部子进程。
# npm 会再 spawn vite,只杀父进程会留下监听端口的孤儿。
kill_tree() {
  local pid="$1" sig="${2:-TERM}" child
  [[ -z $pid ]] && return 0
  for child in $(pgrep -P "$pid" 2>/dev/null || true); do
    kill_tree "$child" "$sig"
  done
  kill -"$sig" "$pid" 2>/dev/null || true
}

# is_dev_server 判断 pid 是否真的是本项目的后端进程。
# 通过命令行内容判定,既能匹配相对路径(./build/dev-server)也能匹配绝对路径,
# 同时排除 pgrep 与 dev.sh 自身,避免误杀。
is_dev_server() {
  local pid="$1" cmd
  cmd="$(ps -o args= -p "$pid" 2>/dev/null || true)"
  [[ $cmd == *"build/dev-server"* && $cmd != *pgrep* && $cmd != *"dev.sh"* ]]
}

# stop_services 清理本脚本启动过的进程,以及上次异常退出留下的孤儿。
stop_services() {
  local stopped=0 pid file
  for file in "$API_PID_FILE" "$WEB_PID_FILE"; do
    [[ -f $file ]] || continue
    pid="$(cat "$file" 2>/dev/null || true)"
    if [[ -n $pid ]] && kill -0 "$pid" 2>/dev/null; then
      kill_tree "$pid"
      stopped=1
    fi
    rm -f "$file"
  done
  # 兜底:PID 文件丢失时按命令行内容匹配
  for pid in $(pgrep -f "build/dev-server" 2>/dev/null || true); do
    is_dev_server "$pid" || continue
    kill_tree "$pid"
    stopped=1
  done
  [[ $stopped -eq 1 ]] && sleep 1
  return 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -b|--backend-port) BACKEND_PORT="$2"; shift 2 ;;
    -f|--frontend-port) FRONTEND_PORT="$2"; shift 2 ;;
    -d|--data) DATA_DIR="$2"; shift 2 ;;
    -p|--password) ADMIN_PASSWORD="$2"; shift 2 ;;
    -s|--stop) STOP_ONLY=1; shift ;;
    --smoke) RUN_SMOKE=1; shift ;;
    --backend-only) START_FRONTEND=0; shift ;;
    -h|--help) usage ;;
    *) echo "未知参数: $1 (用 -h 查看用法)"; exit 1 ;;
  esac
done

if [[ $STOP_ONLY -eq 1 ]]; then
  stop_services
  echo "==> 已停止本项目残留的后端与前端进程"
  exit 0
fi

if [[ ${#ADMIN_PASSWORD} -lt 8 ]]; then
  echo "管理员密码至少 8 个字符" >&2
  exit 1
fi

# ---------- 前置检查 ----------

for cmd in go npm curl; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "缺少命令: $cmd" >&2; exit 1; }
done

port_busy() {
  (exec 3<>"/dev/tcp/$1/$2") 2>/dev/null && { exec 3<&- 3>&-; return 0; }
  return 1
}

if port_busy "$BACKEND_HOST" "$BACKEND_PORT"; then
  echo "后端端口 $BACKEND_PORT 已被占用:可能是上次未退出的实例,可先执行 ./dev.sh --stop,或用 -b 指定其它端口" >&2
  exit 1
fi
if [[ $START_FRONTEND -eq 1 ]] && port_busy "$FRONTEND_HOST" "$FRONTEND_PORT"; then
  echo "前端端口 $FRONTEND_PORT 已被占用:可能是上次未退出的实例,可先执行 ./dev.sh --stop,或用 -f 指定其它端口" >&2
  exit 1
fi

if [[ ! -d web/node_modules ]]; then
  echo "==> 安装前端依赖 (首次运行,约需 20s)"
  npm --prefix web ci
fi

echo "==> 编译后端"
mkdir -p "$LOG_DIR" "$DATA_DIR"
go build -o build/dev-server .

# ---------- 进程管理 ----------

API_PGID=""
WEB_PGID=""
TAIL_PIDS=()

cleanup() {
  trap - EXIT INT TERM
  echo ""
  echo "==> 正在停止服务"
  if [[ ${#TAIL_PIDS[@]} -gt 0 ]]; then
    for tail_pid in "${TAIL_PIDS[@]}"; do
      kill "$tail_pid" 2>/dev/null || true
    done
  fi
  stop_services
  wait 2>/dev/null || true
  echo "==> 已停止(日志保留在 $LOG_DIR/)"
}
trap cleanup EXIT INT TERM

wait_for_http() {
  local url="$1" timeout="${2:-60}" waited=0
  while (( waited < timeout * 2 )); do
    local code
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$url" 2>/dev/null || true)"
    if [[ -n $code && $code != "000" ]]; then
      return 0
    fi
    sleep 0.5
    waited=$((waited + 1))
  done
  return 1
}

# ---------- 启动后端 ----------

: > "$API_LOG"
set -m
ICLOUD_HME_ADMIN_PASSWORD="$ADMIN_PASSWORD" ./build/dev-server \
  -addr "$BACKEND_HOST:$BACKEND_PORT" -data "$DATA_DIR" -debug >> "$API_LOG" 2>&1 &
API_PGID=$!
set +m
echo "$API_PGID" > "$API_PID_FILE"

if ! wait_for_http "http://$BACKEND_HOST:$BACKEND_PORT/api/auth/session" 60; then
  echo "后端启动失败,最后 20 行日志:" >&2
  tail -n 20 "$API_LOG" >&2
  exit 1
fi
echo "==> 后端就绪  http://$BACKEND_HOST:$BACKEND_PORT"

# ---------- 启动前端 ----------

if [[ $START_FRONTEND -eq 1 ]]; then
  : > "$WEB_LOG"
  set -m
  (
    cd web
    HME_API_TARGET="http://$BACKEND_HOST:$BACKEND_PORT" \
      npm run dev -- --host "$FRONTEND_HOST" --port "$FRONTEND_PORT" --strictPort
  ) >> "$WEB_LOG" 2>&1 &
  WEB_PGID=$!
  set +m
  echo "$WEB_PGID" > "$WEB_PID_FILE"

  PROBE_HOST="$FRONTEND_HOST"
  [[ $FRONTEND_HOST == "0.0.0.0" ]] && PROBE_HOST="127.0.0.1"
  if ! wait_for_http "http://$PROBE_HOST:$FRONTEND_PORT/" 60; then
    echo "前端启动失败,最后 20 行日志:" >&2
    tail -n 20 "$WEB_LOG" >&2
    exit 1
  fi
  ENTRY="http://$PROBE_HOST:$FRONTEND_PORT"
  echo "==> 前端就绪  $ENTRY"
else
  ENTRY="http://$BACKEND_HOST:$BACKEND_PORT"
  echo "==> 仅后端模式,界面由内嵌构建产物提供"
fi

# ---------- 输出与日志跟随 ----------

cat <<EOF

------------------------------------------------------------
  管理界面   $ENTRY
  管理员密码 $ADMIN_PASSWORD
  数据目录   $DATA_DIR
  接口代理   /api → http://$BACKEND_HOST:$BACKEND_PORT
------------------------------------------------------------
  下一步:浏览器打开上面的地址登录,然后在「账号设置」里添加 iCloud 账号。
  - 创建别名需要 Cookie(或用「iCloud 密码登录」直接换取)
  - 取件建议再配置 App 专用密码,走 IMAP 才能读正文
  Ctrl-C 停止全部服务
------------------------------------------------------------
EOF

tail -n +1 -F "$API_LOG" 2>/dev/null | sed -u 's/^/\033[36m[后端]\033[0m /' &
TAIL_PIDS+=($!)
if [[ $START_FRONTEND -eq 1 ]]; then
  tail -n +1 -F "$WEB_LOG" 2>/dev/null | sed -u 's/^/\033[35m[前端]\033[0m /' &
  TAIL_PIDS+=($!)
fi

if [[ $RUN_SMOKE -eq 1 ]]; then
  echo ""
  echo "==> 运行浏览器冒烟测试(针对 $ENTRY)"
  if BASE_URL="$ENTRY" ICLOUD_HME_ADMIN_PASSWORD="$ADMIN_PASSWORD" npm --prefix web run e2e; then
    echo "==> 冒烟测试通过,服务继续运行"
  else
    echo "==> 冒烟测试失败,服务继续运行以便排查" >&2
  fi
fi

wait
