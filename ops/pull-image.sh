#!/usr/bin/env bash
# ops/pull-image.sh —— 从 AWS 专用 GHCR 仓库拉取镜像（默认 linux/amd64），可选记录发布台账。
#
# 用法：
#   ops/pull-image.sh <sha|image-ref> [--record]
#     <完整40位sha> | <7-39位短sha> | ghcr.io/zuimeidamowang/niu-dali-aws:<tag>
#     --record   把 "<UTC 时间> <ref> <digest>" 追加到 $RELEASES_DIR/pulled.log
#
# AWS 包设为 public 并验证匿名拉取后，本脚本无需 GitHub 凭据或 docker login。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
. "$SCRIPT_DIR/lib/common.sh"

usage() {
  cat <<'USAGE'
用法：pull-image.sh <sha|image-ref> [--record]

参数：
  <sha|image-ref>   完整40位sha | 7-39位短sha | ghcr.io/zuimeidamowang/niu-dali-aws:<tag>
                   也接受该 AWS 仓库的 @sha256:<digest> 引用；其它仓库一律拒绝
  --record          记录到 $RELEASES_DIR/pulled.log
  -h, --help        显示本帮助
USAGE
}

main() {
  local record=0
  local arg=""

  while [ "$#" -gt 0 ]; do
    case "$1" in
      -h|--help) usage; exit 0 ;;
      --record) record=1; shift ;;
      -*) die "未知参数：${1}（用 --help 查看用法）" ;;
      *) if [ -n "$arg" ]; then die "只能指定一个 <sha|image-ref>（收到多个）"; fi; arg="$1"; shift ;;
    esac
  done

  [ -n "$arg" ] || { usage; die "缺少 <sha|image-ref> 参数"; }

  local ref
  ref="$(resolve_image_ref "$arg")"
  require_cmd docker
  log "解析镜像引用：$arg → $ref"

  docker pull --platform linux/amd64 "$ref"

  local digest
  digest="$(image_digest "$ref")"
  if [ -n "$digest" ]; then
    ok "已拉取：$ref"
    printf 'digest: %s\n' "$digest"
  else
    warn "已拉取 ${ref}，但未能读取 RepoDigest（可能未进行 registry 校验）。"
  fi

  if [ "$record" -eq 1 ]; then
    mkdir -p "$RELEASES_DIR"
    printf '%s %s %s\n' "$(now_utc)" "$ref" "${digest:-unknown}" >> "$RELEASES_DIR/pulled.log"
    ok "已记录到 $RELEASES_DIR/pulled.log"
  fi
}

main "$@"
