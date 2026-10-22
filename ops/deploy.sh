#!/usr/bin/env bash
# ops/deploy.sh —— AWS 专属发布入口（服务器只拉取 + 重建 app 容器）。
#
# ⚠️  本脚本的硬边界（不可协商）：
#   - 永不触碰 postgres / redis / caddy 容器；
#   - 永不执行 `docker compose down`（尤其禁止 `down -v`）；
#   - 永不修改停止超时参数（SHUTDOWN_TIMEOUT_SECONDS=120 / 容器 stop timeout 60 属历史未决项 OPS-02）；
#   - 只允许重建 app 一个服务：compose up -d --no-deps app（project 固定 newapi-prod）。
#   - 失败时**不做自动回滚**（生产规则：维护失败只告警），仅打印带回滚命令的明确提示。
#
# 用法：
#   deploy.sh <sha|image-ref> [--image-digest <sha256:...>] [--no-backup] [--skip-health]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
. "$SCRIPT_DIR/lib/common.sh"

# --------------------------- 参数 ---------------------------
ARG_REF=""
ARG_DIGEST=""
BUILD_LOCAL=0
NO_BACKUP=0
SKIP_HEALTH=0

usage() {
  cat <<'USAGE'
用法：deploy.sh <sha|image-ref> [options]

参数：
  <sha|image-ref>            完整40位sha | 7-39位短sha | ghcr.io/zuimeidamowang/niu-dali-aws:<tag>
                           也接受该 AWS 仓库的 @sha256:<digest> 引用；其它仓库一律拒绝
  --image-digest <sha256:..> 期望的镜像 digest；与实际拉取结果不一致则中止（防拉错）
  --build-local              已禁用；请使用 AWS 流水线构建的专用 GHCR 镜像
  --no-backup                跳过备份（谨慎使用；生产默认必须备份）
  --skip-health              跳过健康检查等待
  -h, --help                 显示本帮助

流程：解析 → 环境校验 → 发布锁 → 备份 → 拉取 → 原子改写 .env → 重建 app → 健康检查 → 记录
USAGE
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -h|--help) usage; exit 0 ;;
      --build-local) BUILD_LOCAL=1; shift ;;
      --no-backup) NO_BACKUP=1; shift ;;
      --skip-health) SKIP_HEALTH=1; shift ;;
      --image-digest) [ "$#" -ge 2 ] || die "--image-digest 需要一个值"; ARG_DIGEST="$2"; shift 2 ;;
      -*) die "未知参数：${1}（用 --help 查看用法）" ;;
      *) if [ -n "$ARG_REF" ]; then die "只能指定一个 <sha|image-ref>（收到多个）"; fi; ARG_REF="$1"; shift ;;
    esac
  done
  [ -n "$ARG_REF" ] || { usage; die "缺少 <sha|image-ref> 参数"; }
}

# --------------------------- .env 原子改写 ---------------------------
# 只改写 APP_IMAGE= 这一行，其余行一字不动。
rewrite_app_image() {
  local new_ref="$1"
  local env_file="$COMPOSE_DIR/.env"
  local backup
  local tmp

  # 先声明再赋值：避免 local 的返回码掩蔽命令替换的返回码（SC2155），使 set -e 能在失败时生效。
  backup="${env_file}.bak.$(date -u +%Y%m%dT%H%M%SZ)" \
    || die "生成 .env 备份文件名失败（date 命令异常）。修复：确认系统 date 可用后重试。"

  cp -a "$env_file" "$backup"
  log "已备份 .env → $backup"

  tmp="$(mktemp "${env_file}.tmp.XXXXXX")"
  awk -v ref="$new_ref" '
    BEGIN { done = 0 }
    /^APP_IMAGE=/ { print "APP_IMAGE=" ref; done = 1; next }
    { print }
    END { if (!done) print "APP_IMAGE=" ref }
  ' "$env_file" > "$tmp"

  # 尽力保留原文件权限；失败不致命。
  chmod --reference="$env_file" "$tmp" 2>/dev/null || true
  mv -f "$tmp" "$env_file"
  ok "已原子更新 $env_file 的 APP_IMAGE → $new_ref"
}

read_current_app_image() {
  local env_file="$COMPOSE_DIR/.env"
  awk -F= '/^APP_IMAGE=/{ sub(/^APP_IMAGE=/, ""); print; exit }' "$env_file"
}

# --------------------------- 健康等待 ---------------------------
wait_healthy() {
  require_cmd curl
  local deadline status=""
  deadline=$(( $(date +%s) + HEALTH_TIMEOUT_SECONDS ))
  log "等待容器 $APP_CONTAINER 变为 healthy（上限 ${HEALTH_TIMEOUT_SECONDS}s）..."

  while [ "$(date +%s)" -lt "$deadline" ]; do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$APP_CONTAINER" 2>/dev/null || printf 'missing')"
    case "$status" in
      healthy) break ;;
      *) sleep 3 ;;
    esac
  done
  [ "$status" = "healthy" ] || { warn "容器健康状态：${status}（未达 healthy）"; return 1; }
  ok "容器 $APP_CONTAINER 状态：healthy"

  local body
  body="$(curl -fsS "$HEALTH_URL_LOCAL" 2>/dev/null || true)"
  if printf '%s' "$body" | grep -Eq '"success"[[:space:]]*:[[:space:]]*true'; then
    ok "健康接口 $HEALTH_URL_LOCAL 返回 \"success\": true"
  else
    warn "健康接口 $HEALTH_URL_LOCAL 未断言到 \"success\": true。响应：$(printf '%s' "$body" | head -c 200)"
    return 1
  fi
}

# --------------------------- 发布主体（在发布锁内执行）---------------------------
run_release() {
  local sha="$1"
  local ref="$2"
  local expect_digest="$3"

  # 取锁之后、备份之前完成全部环境与目标校验（含 compose project/服务/容器，
  # 以便在「选错 project 会另起并行容器」时大声失败，绝不允许改完 .env 才发现）。
  ensure_env_ready

  local backup_id="none"
  if [ "$NO_BACKUP" -eq 1 ]; then
    warn "已跳过备份（--no-backup）。生产发布强烈建议保留备份。"
  else
    [ -x "$BACKUP_SCRIPT" ] || [ -f "$BACKUP_SCRIPT" ] || die "备份脚本不存在：$BACKUP_SCRIPT
修复：生产发布必须能备份。请确认 ops/backup.sh 存在并可执行，或用 --no-backup（不推荐）。"
    log "执行备份：$BACKUP_SCRIPT"
    local backup_out
    backup_out="$("$BACKUP_SCRIPT")"
    printf '%s\n' "$backup_out"
    backup_id="$(printf '%s\n' "$backup_out" | awk 'NF{last=$0} END{print last}')"
    [ -n "$backup_id" ] || backup_id="unknown"
    ok "备份完成，标识：$backup_id"
  fi

  local prev_ref
  prev_ref="$(read_current_app_image)"
  log "当前 APP_IMAGE（回滚锚点）：${prev_ref:-<空>}"

  log "拉取镜像（linux/amd64）：$ref"
  docker pull --platform linux/amd64 "$ref"

  local digest
  digest="$(image_digest "$ref")"
  if [ -n "$expect_digest" ]; then
    if [ "$digest" != "$expect_digest" ]; then
      die "镜像 digest 不匹配！
  期望：$expect_digest
  实际：$digest
修复：确认 GHCR 上的 tag 与期望 digest 一致后重试；不要带着不一致的 digest 继续发布。"
    fi
    ok "digest 校验通过：$digest"
  else
    log "digest：${digest:-<未获取到>}"
  fi

  rewrite_app_image "$ref"

  log "重建 app 服务：compose up -d --no-deps $APP_SERVICE"
  compose up -d --no-deps "$APP_SERVICE"

  if [ "$SKIP_HEALTH" -eq 1 ]; then
    warn "已跳过健康检查（--skip-health）。请人工确认服务正常。"
  elif ! wait_healthy; then
    warn "发布后健康检查未通过。按生产规则**不做自动回滚**。"
    cat >&2 <<EOF
------------------------------------------------------------------------
发布失败，请人工处置。
当前 APP_IMAGE 已写入：$ref
旧 APP_IMAGE（上一版）：${prev_ref:-<空>}
备份标识：$backup_id
------------------------------------------------------------------------
EOF
    if [ -n "$prev_ref" ] && (resolve_image_ref "$prev_ref" >/dev/null 2>&1); then
      printf '核对数据库 schema 兼容性后，可回滚至 AWS 上一版：\n  /srv/new-api/ops/rollback.sh %s\n' "$prev_ref" >&2
    else
      warn "旧 APP_IMAGE 不属于有效的 AWS 专用镜像，不能作为回滚目标。请人工选择已验证的 AWS 镜像及数据库兼容方案。"
    fi
    return 1
  fi

  mkdir -p "$RELEASES_DIR"
  printf '%s sha=%s ref=%s digest=%s backup=%s prev=%s\n' \
    "$(now_utc)" "$sha" "$ref" "${digest:-unknown}" "$backup_id" "${prev_ref:-}" \
    >> "$RELEASES_DIR/releases.log"

  ok "发布成功。"
  printf '  旧 ref：%s\n' "${prev_ref:-<空>}"
  printf '  新 ref：%s\n' "$ref"
  printf '  digest：%s\n' "${digest:-unknown}"
  printf '  备份  ：%s\n' "$backup_id"
  printf '下一步：运行 ops/prune-images.sh preview 查看可回收镜像。\n'
}

main() {
  parse_args "$@"

  if [ "$BUILD_LOCAL" -eq 1 ]; then
    die "AWS 发布已禁用遗留 --build-local 入口，避免旧服务器脚本写入其它镜像仓库。请使用 AWS 流水线构建的 ghcr.io/zuimeidamowang/niu-dali-aws 镜像。"
  fi

  local ref sha
  ref="$(resolve_image_ref "$ARG_REF")"
  require_cmd docker
  require_cmd git
  # 从 ref 中提取镜像 tag 里的 sha（若有），用于台账；image-ref 直传时可能取不到。
  sha="$(printf '%s' "$ref" | awk -F':sha-' 'NF>1{print $2; exit}')"
  [ -n "$sha" ] || sha="unknown"

  with_publish_lock run_release "$sha" "$ref" "$ARG_DIGEST"
}

main "$@"
