#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_ROOT/docker-compose.acceptance.yml"
OFFICIAL_COMPOSE_FILE="$PROJECT_ROOT/docker-compose.yml"
STATE_DIR="$PROJECT_ROOT/tmp/local-acceptance"
FRONTEND_PID_FILE="$STATE_DIR/frontend.pid"
FRONTEND_LOG="$PROJECT_ROOT/logs/local-acceptance-frontend.log"
FRONTEND_PORT="${LOCAL_ACCEPTANCE_FRONTEND_PORT:-18091}"
BACKEND_PORT="${LOCAL_ACCEPTANCE_BACKEND_PORT:-8090}"
FRONTEND_HOST="${LOCAL_ACCEPTANCE_FRONTEND_HOST:-127.0.0.1}"
ACCEPTANCE_URL="${LOCAL_ACCEPTANCE_URL:-http://127.0.0.1/platform/videos}"
BACKEND_CONTAINER="${LOCAL_ACCEPTANCE_BACKEND_CONTAINER:-vidsage-custom-backend}"
FRONTEND_CONTAINER="${LOCAL_ACCEPTANCE_FRONTEND_CONTAINER:-WeKnora-frontend}"
FRONTEND_IMAGE="${LOCAL_ACCEPTANCE_FRONTEND_IMAGE:-weknora/vidsage-ui:local-acceptance}"
APP_IMAGE="${LOCAL_ACCEPTANCE_APP_IMAGE:-weknora/weknora-app:local-acceptance}"
APP_CONTAINER="${LOCAL_ACCEPTANCE_APP_CONTAINER:-WeKnora-app}"
AGENT_SYNC_FILE="${LOCAL_ACCEPTANCE_AGENT_SYNC_FILE:-$PROJECT_ROOT/config/vidsage_agent_sync.json}"
AGENT_SYNC_ID="${LOCAL_ACCEPTANCE_AGENT_ID:-6f3691c2-8d15-48f8-b1f4-dfadd222ca53}"
BACKEND_TARGET="${VITE_CUSTOM_BACKEND_TARGET:-http://127.0.0.1:${BACKEND_PORT}}"
OFFICIAL_BACKEND_TARGET="${VITE_DEV_PROXY_TARGET:-${FRONTEND_BACKEND_URL:-http://127.0.0.1:8080}}"

cd "$PROJECT_ROOT"

compose() {
    docker compose -f "$COMPOSE_FILE" "$@"
}

load_tencent_mps_credentials() {
    local provider credentials_file credentials secret_id secret_key
    provider="$(awk -F= '/^[[:space:]]*CUSTOM_TRANSCRIPTION_PROVIDER[[:space:]]*=/ { value=$2; gsub(/[[:space:]\"'\''\r]/, "", value); print value; exit }' "$PROJECT_ROOT/.env")"
    [ "$provider" = "tencent_mps" ] || return 0
    [ -n "${TENCENTCLOUD_SECRET_ID:-}" ] && [ -n "${TENCENTCLOUD_SECRET_KEY:-}" ] && return 0

    credentials_file="${LOCAL_ACCEPTANCE_TENCENT_CREDENTIALS_FILE:-$PROJECT_ROOT/../reference/SecretKey-腾讯云.csv}"
    [ -r "$credentials_file" ] || die "腾讯云 MPS 已启用，但本地凭据文件不可读取: $credentials_file"

    credentials="$(awk -F, 'NR == 2 { gsub(/[\r\"[:space:]]/, "", $1); gsub(/[\r\"[:space:]]/, "", $2); print $1 "\t" $2; exit }' "$credentials_file")"
    secret_id="${credentials%%$'\t'*}"
    secret_key="${credentials#*$'\t'}"
    [[ "$secret_id" == AKID* ]] && [ "${#secret_key}" -ge 20 ] || die "腾讯云 MPS 本地凭据文件格式无效"
    export TENCENTCLOUD_SECRET_ID="$secret_id"
    export TENCENTCLOUD_SECRET_KEY="$secret_key"
}

die() {
    printf '[ERROR] %s\n' "$1" >&2
    exit 1
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || die "未找到命令: $1"
}

require_jq() {
    require_command jq
    [ -r "$AGENT_SYNC_FILE" ] || die "Agent 同步配置不可读取: $AGENT_SYNC_FILE"
    jq -e 'type == "object" and (.agent_mode | type == "string") and (.max_iterations | type == "number") and (.llm_call_timeout | type == "number") and (.selected_skills | type == "array") and (.allowed_tools | type == "array")' "$AGENT_SYNC_FILE" >/dev/null \
        || die "Agent 同步配置格式无效: $AGENT_SYNC_FILE"
}

require_docker() {
    require_command docker
    require_command curl
    docker info >/dev/null 2>&1 || die 'Docker Desktop 未运行，先启动 Docker Desktop 后重试'
    compose version >/dev/null 2>&1 || die '当前 Docker 不支持 Compose'
}

container_exists() {
    docker container inspect "$1" >/dev/null 2>&1
}

container_is_running() {
    [ "$(docker inspect -f '{{.State.Status}}' "$1" 2>/dev/null || true)" = running ]
}

container_is_healthy() {
    [ "$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$1" 2>/dev/null || true)" = healthy ]
}

container_env_value() {
    local container="$1"
    local key="$2"
    docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$container" 2>/dev/null \
        | awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print; exit }'
}

mps_runtime_config_ready() {
    local provider key
    provider="$(container_env_value "$BACKEND_CONTAINER" CUSTOM_TRANSCRIPTION_PROVIDER)"
    [ "$provider" = "tencent_mps" ] || return 0
    for key in \
        TENCENTCLOUD_SECRET_ID \
        TENCENTCLOUD_SECRET_KEY \
        TENCENTCLOUD_MPS_INPUT_BUCKET \
        TENCENTCLOUD_MPS_OUTPUT_BUCKET; do
        [ -n "$(container_env_value "$BACKEND_CONTAINER" "$key")" ] || return 1
    done
}

assert_mps_runtime_config() {
    mps_runtime_config_ready || die '腾讯云 MPS 运行配置未进入 custom-backend，已停止验收启动'
}

ensure_official_container() {
    local container="$1"
    local service="$2"
    if container_exists "$container"; then
        if ! container_is_running "$container"; then
            docker start "$container" >/dev/null
        fi
        return 0
    fi
    docker compose -p weknora -f "$OFFICIAL_COMPOSE_FILE" up -d "$service"
}

wait_for_container_health() {
    local container="$1"
    local timeout_seconds="$2"
    local elapsed=0
    while [ "$elapsed" -lt "$timeout_seconds" ]; do
        container_is_healthy "$container" && return 0
        sleep 1
        elapsed=$((elapsed + 1))
    done
    return 1
}

wait_for_container_http() {
    local container="$1"
    local url="$2"
    local timeout_seconds="$3"
    local elapsed=0
    while [ "$elapsed" -lt "$timeout_seconds" ]; do
        if docker exec "$container" wget -qO- --timeout=2 "$url" >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
        elapsed=$((elapsed + 1))
    done
    return 1
}

sync_agent_config() {
    require_jq
    [[ "$AGENT_SYNC_ID" =~ ^[A-Za-z0-9_-]+$ ]] || die "Agent ID 格式无效: $AGENT_SYNC_ID"
    local config_json
    config_json="$(jq -c . "$AGENT_SYNC_FILE")"
    docker exec -i WeKnora-postgres psql -v ON_ERROR_STOP=1 -U "${DB_USER:-postgres}" -d "${DB_NAME:-WeKnora}" \
        -v agent_id="$AGENT_SYNC_ID" -v agent_config="$config_json" <<'SQL'
UPDATE custom_agents
SET config = config || :'agent_config'::jsonb,
    updated_at = CURRENT_TIMESTAMP
WHERE id = :'agent_id';
SQL
    local summary
    summary="$(docker exec -i WeKnora-postgres psql -At -U "${DB_USER:-postgres}" -d "${DB_NAME:-WeKnora}" \
        -c "SELECT id || '|' || (config->>'agent_mode') || '|' || (config->>'max_iterations') || '|' || (config->>'llm_call_timeout') || '|' || jsonb_array_length(COALESCE(config->'selected_skills','[]'::jsonb)) || '|' || jsonb_array_length(COALESCE(config->'allowed_tools','[]'::jsonb)) FROM custom_agents WHERE id = '${AGENT_SYNC_ID}';")"
    [ -n "$summary" ] || die "数据库中不存在 Agent: $AGENT_SYNC_ID"
    printf '[INFO] 已同步 Agent 配置: %s\n' "$summary"
}

assert_agent_config() {
    require_jq
    local expected actual
    expected="$(jq -r '[.agent_mode, .agent_type, .model_id, (.max_iterations|tostring), (.llm_call_timeout|tostring), (.selected_skills|length|tostring), (.allowed_tools|length|tostring)] | join("|")' "$AGENT_SYNC_FILE")"
    actual="$(docker exec -i WeKnora-postgres psql -At -U "${DB_USER:-postgres}" -d "${DB_NAME:-WeKnora}" \
        -c "SELECT (config->>'agent_mode') || '|' || (config->>'agent_type') || '|' || (config->>'model_id') || '|' || (config->>'max_iterations') || '|' || (config->>'llm_call_timeout') || '|' || jsonb_array_length(COALESCE(config->'selected_skills','[]'::jsonb)) || '|' || jsonb_array_length(COALESCE(config->'allowed_tools','[]'::jsonb)) FROM custom_agents WHERE id = '${AGENT_SYNC_ID}';")"
    [ "$actual" = "$expected" ] || die "数据库 Agent 配置与声明文件不一致: expected=$expected actual=$actual"
}

build_and_start_app() {
    local previous_image rollback_tag
    previous_image="$(docker inspect -f '{{.Config.Image}}' "$APP_CONTAINER" 2>/dev/null || true)"
    rollback_tag="weknora/weknora-app:rollback-$(date +%Y%m%d%H%M%S)"
    if [ -n "$previous_image" ]; then
        docker tag "$previous_image" "$rollback_tag"
        printf '[INFO] App 回滚镜像: %s\n' "$rollback_tag"
    fi
    if container_exists "$APP_CONTAINER"; then
        docker rm -f "$APP_CONTAINER" >/dev/null
    fi
    LOCAL_ACCEPTANCE_APP_IMAGE="$APP_IMAGE" compose build app
    LOCAL_ACCEPTANCE_APP_IMAGE="$APP_IMAGE" compose up -d app
    wait_for_container_health "$APP_CONTAINER" 120 || die '当前源码 WeKnora-app 未就绪'
    wait_for_container_http "$APP_CONTAINER" http://127.0.0.1:8080/health 10 || die '当前源码 WeKnora-app 健康检查失败'
    printf '[INFO] 已启动当前源码 App: image=%s started=%s\n' "$APP_IMAGE" "$(docker inspect -f '{{.State.StartedAt}}' "$APP_CONTAINER")"
}

start_backend() {
    load_tencent_mps_credentials
    if container_exists "$BACKEND_CONTAINER"; then
        printf '[INFO] 移除可能来自旧验收项目的 custom-backend 容器: %s\n' "$BACKEND_CONTAINER"
        docker rm -f "$BACKEND_CONTAINER" >/dev/null
    fi
    # 固定验收必须使用官方 WeKnora 数据栈中的真实库、对象存储和 app。
    # 仅停止本地开发 MinIO 容器释放宿主端口，数据卷不删除。
    docker stop WeKnora-minio-dev >/dev/null 2>&1 || true
    ensure_official_container WeKnora-postgres postgres
    ensure_official_container WeKnora-redis redis
    ensure_official_container WeKnora-docreader docreader
    ensure_official_container WeKnora-minio minio
    wait_for_container_health WeKnora-postgres 90 || die '官方 PostgreSQL 未就绪'
    wait_for_container_health WeKnora-minio 90 || die '官方 MinIO 未就绪'
    wait_for_container_health WeKnora-docreader 90 || die '官方 docreader 未就绪'
    sync_agent_config
    build_and_start_app
    assert_agent_config
    compose up -d --build custom-backend
    assert_mps_runtime_config
}

pid_is_running() {
    [ -f "$1" ] && kill -0 "$(cat "$1")" >/dev/null 2>&1
}

wait_for_http() {
    local url="$1"
    local timeout_seconds="$2"
    local elapsed=0
    while [ "$elapsed" -lt "$timeout_seconds" ]; do
        if curl -fsS --max-time 2 "$url" >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
        elapsed=$((elapsed + 1))
    done
    return 1
}

start_frontend() {
    mkdir -p "$STATE_DIR" "$PROJECT_ROOT/logs"
    if pid_is_running "$FRONTEND_PID_FILE"; then
        printf '[INFO] 前端已运行，PID=%s\n' "$(cat "$FRONTEND_PID_FILE")"
        return 0
    fi
    rm -f "$FRONTEND_PID_FILE"
    require_command npm
    [ -d "$PROJECT_ROOT/frontend/node_modules" ] || die '前端依赖不存在，请先在 frontend 目录执行 npm install'
    (
        cd "$PROJECT_ROOT/frontend"
        exec env \
            VITE_CUSTOM_BACKEND_TARGET="$BACKEND_TARGET" \
            VITE_DEV_PROXY_TARGET="$OFFICIAL_BACKEND_TARGET" \
            npm run dev -- --host "$FRONTEND_HOST" --port "$FRONTEND_PORT"
    ) >"$FRONTEND_LOG" 2>&1 &
    echo "$!" > "$FRONTEND_PID_FILE"
    printf '[INFO] 前端启动中，PID=%s\n' "$(cat "$FRONTEND_PID_FILE")"
}

start_fixed_frontend() {
    require_command npm
    [ -d "$PROJECT_ROOT/frontend/node_modules" ] || die '前端依赖不存在，请先在 frontend 目录执行 npm install'
    (
        cd "$PROJECT_ROOT/frontend"
        npm run build
    )
    docker build -t "$FRONTEND_IMAGE" "$PROJECT_ROOT/frontend"
    if container_exists "$FRONTEND_CONTAINER"; then
        docker rm -f "$FRONTEND_CONTAINER" >/dev/null
    fi
    local network
    network="$(docker inspect -f '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}' "$BACKEND_CONTAINER" 2>/dev/null || true)"
    [ -n "$network" ] || die 'custom-backend 未加入验收网络，无法启动当前前端'
    docker run -d \
        --name "$FRONTEND_CONTAINER" \
        --network "$network" \
        -p "${LOCAL_ACCEPTANCE_HTTP_PORT:-80}:80" \
        "$FRONTEND_IMAGE" >/dev/null
    printf '[INFO] 已用当前源码构建并启动固定验收前端: %s\n' "$FRONTEND_CONTAINER"
    return 0
}

stop_frontend() {
    if ! [ -f "$FRONTEND_PID_FILE" ]; then
        return 0
    fi
    local pid
    pid="$(cat "$FRONTEND_PID_FILE")"
    if kill -0 "$pid" >/dev/null 2>&1; then
        kill "$pid" >/dev/null 2>&1 || true
        for _ in 1 2 3 4 5; do
            kill -0 "$pid" >/dev/null 2>&1 || break
            sleep 1
        done
    fi
    rm -f "$FRONTEND_PID_FILE"
    printf '[INFO] 前端已停止\n'
}

up() {
    require_docker
    start_backend
    if ! wait_for_http "http://127.0.0.1:${BACKEND_PORT}/healthz" 60; then
        container_is_healthy "$BACKEND_CONTAINER" || die "custom-backend 未在 ${BACKEND_PORT} 就绪"
    fi
    start_fixed_frontend
    wait_for_http "$ACCEPTANCE_URL" 30 || die "固定验收前端未在 ${ACCEPTANCE_URL} 就绪"
    printf '\n[SUCCESS] 本地验收服务已就绪\n'
    if wait_for_http "$ACCEPTANCE_URL" 3; then
        printf '前端验收地址: %s\n' "$ACCEPTANCE_URL"
    else
        printf '前端验收地址: http://127.0.0.1:%s/platform/videos\n' "$FRONTEND_PORT"
    fi
    printf '后端健康检查: http://127.0.0.1:%s/healthz\n' "$BACKEND_PORT"
}

down() {
    require_docker
    stop_frontend
    if container_exists "$BACKEND_CONTAINER"; then
        docker stop "$BACKEND_CONTAINER" >/dev/null 2>&1 || true
    fi
    if container_exists "$FRONTEND_CONTAINER"; then
        docker rm -f "$FRONTEND_CONTAINER" >/dev/null 2>&1 || true
    fi
    printf '[SUCCESS] 本地验收服务已停止，数据卷保留\n'
}

restart() {
    down
    up
}

status() {
    require_docker
    docker ps -a --filter "name=^/${BACKEND_CONTAINER}$" --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
    docker ps -a --filter "name=^/${APP_CONTAINER}$" --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
    if pid_is_running "$FRONTEND_PID_FILE"; then
        printf 'frontend: running (PID=%s)\n' "$(cat "$FRONTEND_PID_FILE")"
    elif wait_for_http "$ACCEPTANCE_URL" 3; then
        printf 'frontend: fixed acceptance URL available (%s)\n' "$ACCEPTANCE_URL"
    else
        printf 'frontend: stopped\n'
    fi
    if curl -fsS --max-time 3 "http://127.0.0.1:${BACKEND_PORT}/healthz" >/dev/null 2>&1; then
        printf 'custom-backend: healthy\n'
    else
        printf 'custom-backend: unavailable\n'
    fi
    if container_exists "$BACKEND_CONTAINER" && [ "$(container_env_value "$BACKEND_CONTAINER" CUSTOM_TRANSCRIPTION_PROVIDER)" = "tencent_mps" ]; then
        if mps_runtime_config_ready; then
            printf 'tencent-mps: ready\n'
        else
            printf 'tencent-mps: missing runtime configuration\n'
        fi
    fi
    if container_exists "$APP_CONTAINER"; then
        printf 'app-image: %s\n' "$(docker inspect -f '{{.Config.Image}}' "$APP_CONTAINER")"
        printf 'app-started: %s\n' "$(docker inspect -f '{{.State.StartedAt}}' "$APP_CONTAINER")"
        printf 'app-config-hashes:\n'
        docker exec "$APP_CONTAINER" sha256sum /app/config/builtin_agents.yaml /app/config/prompt_templates/agent_system_prompt.yaml
        assert_agent_config
    fi
}

logs() {
    require_docker
    compose logs -f custom-backend
}

usage() {
    cat <<'EOF'
本地视频验收服务

用法:
  ./scripts/local-acceptance.sh up       启动依赖、custom-backend 和前端
  ./scripts/local-acceptance.sh restart  重启本地验收服务
  ./scripts/local-acceptance.sh down     停止服务，保留数据卷
  ./scripts/local-acceptance.sh status   查看服务状态
  ./scripts/local-acceptance.sh logs     查看 custom-backend 日志
EOF
}

case "${1:-up}" in
    up) up ;;
    down) down ;;
    restart) restart ;;
    status) status ;;
    logs) logs ;;
    help|--help|-h) usage ;;
    *) usage; exit 2 ;;
esac
