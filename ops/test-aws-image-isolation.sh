#!/usr/bin/env bash
# 离线回归：仅使用临时配置和 Docker 替身，不连接服务器或镜像仓库。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DIR="$(mktemp -d)"
trap 'rm -rf "$TEST_DIR"' EXIT
AWS_IMAGE='ghcr.io/zuimeidamowang/niu-dali-aws'
OLD_IMAGE='ghcr.io/zuimeidamowang/niu-dali'
SHA='0123456789abcdef0123456789abcdef01234567'
DIGEST='0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'
COUNT=0
mkdir -p "$TEST_DIR/bin" "$TEST_DIR/fixture/lib"
: > "$TEST_DIR/empty.env"
printf 'GHCR_PREFIX=%s\n' "$OLD_IMAGE" > "$TEST_DIR/old.env"
printf 'GHCR_PREFIX=%s\n' "$AWS_IMAGE" > "$TEST_DIR/aws.env"

cat > "$TEST_DIR/bin/docker" <<'DOCKER'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$DOCKER_CALLS"
case "$*" in
  'image ls '*)
    printf '%s\n' \
      'sha256:aws@@ghcr.io/zuimeidamowang/niu-dali-aws:sha-1111111@@2026-10-01@@1GB' \
      'sha256:awskeep@@ghcr.io/zuimeidamowang/niu-dali-aws:sha-2222222@@2026-10-02@@1GB' \
      'sha256:old@@ghcr.io/zuimeidamowang/niu-dali:sha-1111111@@2026-10-01@@1GB' \
      'sha256:similar@@ghcr.io/zuimeidamowang/niu-dali-aws-other:sha-1111111@@2026-10-01@@1GB'
    ;;
  'image inspect '*) printf 'sha256:%s\n' "$EXPECTED_DIGEST" ;;
  'inspect '*) exit 1 ;;
esac
DOCKER
chmod +x "$TEST_DIR/bin/docker"
TEST_PATH="$TEST_DIR/bin:$PATH"

assert_resolves() {
  local input="$1" expected="$2" actual
  actual="$(env -i PATH="$TEST_PATH" OPS_ENV_FILE="$TEST_DIR/empty.env" bash -c \
    '. "$1"; resolve_image_ref "$2"' _ "$SCRIPT_DIR/lib/common.sh" "$input" 2>"$TEST_DIR/stderr")"
  [ "$actual" = "$expected" ] || { printf 'FAIL: resolve %s → %s\n' "$input" "$actual" >&2; exit 1; }
  COUNT=$((COUNT + 1))
}

assert_rejected() {
  local input="$1"
  if env -i PATH="$TEST_PATH" OPS_ENV_FILE="$TEST_DIR/empty.env" bash -c \
    '. "$1"; resolve_image_ref "$2"' _ "$SCRIPT_DIR/lib/common.sh" "$input" >"$TEST_DIR/output" 2>&1; then
    printf 'FAIL: accepted invalid image %s\n' "$input" >&2
    exit 1
  fi
  grep -q 'AWS' "$TEST_DIR/output"
  COUNT=$((COUNT + 1))
}

assert_entry_rejected() {
  local script="$1" env_file="$2" prefix="$3"; shift 3
  : > "$TEST_DIR/docker.calls"
  if env -i PATH="$TEST_PATH" OPS_ENV_FILE="$env_file" GHCR_PREFIX="$prefix" \
    DOCKER_CALLS="$TEST_DIR/docker.calls" bash "$SCRIPT_DIR/$script" "$@" >"$TEST_DIR/output" 2>&1; then
    printf 'FAIL: %s accepted forbidden configuration or argument\n' "$script" >&2
    exit 1
  fi
  grep -q 'AWS' "$TEST_DIR/output"
  [ ! -s "$TEST_DIR/docker.calls" ] || { printf 'FAIL: %s invoked Docker before rejection\n' "$script" >&2; exit 1; }
  COUNT=$((COUNT + 1))
}

assert_resolves "$SHA" "$AWS_IMAGE:sha-$SHA"
assert_resolves "${SHA:0:7}" "$AWS_IMAGE:sha-${SHA:0:7}"
assert_resolves "$AWS_IMAGE:main" "$AWS_IMAGE:main"
assert_resolves "$AWS_IMAGE@sha256:$DIGEST" "$AWS_IMAGE@sha256:$DIGEST"
assert_resolves "$AWS_IMAGE:sha-$SHA@sha256:$DIGEST" "$AWS_IMAGE:sha-$SHA@sha256:$DIGEST"

for ref in \
  '' "$OLD_IMAGE:sha-$SHA" "ghcr.io/another-owner/niu-dali-aws:main" \
  "$AWS_IMAGE-other:main" "$AWS_IMAGE/child:main" "$AWS_IMAGE" \
  "$AWS_IMAGE:main/child" "$AWS_IMAGE:main:extra" "$AWS_IMAGE:@sha256:$DIGEST" \
  "$AWS_IMAGE@sha256:bad" "$AWS_IMAGE@sha512:$DIGEST" "$AWS_IMAGE:main;echo injected" \
  "$AWS_IMAGE:main"$'\n'"injected" "https://$AWS_IMAGE:main" "$AWS_IMAGE:main " \
  'ghcrXio/zuimeidamowang/niu-dali-aws:main'; do
  assert_rejected "$ref"
done

for script in pull-image.sh deploy.sh rollback.sh prune-images.sh; do
  arg="$SHA"
  [ "$script" != prune-images.sh ] || arg='preview'
  assert_entry_rejected "$script" "$TEST_DIR/empty.env" "$OLD_IMAGE" "$arg"
  assert_entry_rejected "$script" "$TEST_DIR/old.env" "$AWS_IMAGE" "$arg"
  assert_entry_rejected "$script" "$TEST_DIR/empty.env" '' "$arg"
done
for script in pull-image.sh deploy.sh rollback.sh; do
  assert_entry_rejected "$script" "$TEST_DIR/aws.env" "$AWS_IMAGE" "$OLD_IMAGE:sha-$SHA"
done
assert_entry_rejected deploy.sh "$TEST_DIR/aws.env" "$AWS_IMAGE" "$SHA" --build-local

# 有效配置实际通过拉取入口，并只把 AWS 镜像传给 Docker 替身。
: > "$TEST_DIR/docker.calls"
env -i PATH="$TEST_PATH" OPS_ENV_FILE="$TEST_DIR/aws.env" \
  DOCKER_CALLS="$TEST_DIR/docker.calls" EXPECTED_DIGEST="$DIGEST" \
  bash "$SCRIPT_DIR/pull-image.sh" "$SHA" >"$TEST_DIR/output" 2>&1
grep -qxF "pull --platform linux/amd64 $AWS_IMAGE:sha-$SHA" "$TEST_DIR/docker.calls"
grep -q "digest: sha256:$DIGEST" "$TEST_DIR/output"
COUNT=$((COUNT + 1))

# 清理候选集不能包含旧仓库或名称相近的仓库。
: > "$TEST_DIR/docker.calls"
env -i PATH="$TEST_PATH" OPS_ENV_FILE="$TEST_DIR/aws.env" RELEASES_DIR="$TEST_DIR/releases" \
  DOCKER_CALLS="$TEST_DIR/docker.calls" EXPECTED_DIGEST="$DIGEST" \
  bash "$SCRIPT_DIR/prune-images.sh" cleanup --apply --keep 1 >"$TEST_DIR/output" 2>&1 \
  || { cat "$TEST_DIR/output" >&2; exit 1; }
[ "$(grep '^image rm ' "$TEST_DIR/docker.calls")" = 'image rm sha256:aws' ]
COUNT=$((COUNT + 1))

# 运行真实发布/回滚函数，以替身隔离备份、Docker、编排和健康检查。
ln -s "$SCRIPT_DIR/lib/common.sh" "$TEST_DIR/fixture/lib/common.sh"
for script in deploy.sh rollback.sh; do
  sed '/^main "\$@"$/d' "$SCRIPT_DIR/$script" > "$TEST_DIR/fixture/$script"
  for previous in "$OLD_IMAGE:sha-$SHA" "$AWS_IMAGE:sha-$SHA" "$AWS_IMAGE@sha256:$DIGEST"; do
    if env -i PATH="$TEST_PATH" OPS_ENV_FILE="$TEST_DIR/aws.env" \
      DOCKER_CALLS="$TEST_DIR/docker.calls" EXPECTED_DIGEST="$DIGEST" bash -c '
        . "$1"
        previous_ref="$2"
        ensure_env_ready() { :; }
        read_current_app_image() { printf "%s\n" "$previous_ref"; }
        rewrite_app_image() { :; }
        compose() { :; }
        wait_healthy() { return 1; }
        NO_BACKUP=1
        if [ "$3" = deploy.sh ]; then
          run_release unknown "$4" ""
        else
          run_rollback unknown "$4"
        fi
      ' _ "$TEST_DIR/fixture/$script" "$previous" "$script" "$AWS_IMAGE:main" >"$TEST_DIR/output" 2>&1; then
      printf 'FAIL: simulated health failure returned success\n' >&2
      exit 1
    fi
    case "$previous" in
      "$OLD_IMAGE:"*)
        grep -q '不能作为' "$TEST_DIR/output"
        if grep -Eq '^  /srv/new-api/ops/(deploy|rollback)\.sh ' "$TEST_DIR/output"; then
          printf 'FAIL: generated rollback command for old repository\n' >&2
          exit 1
        fi
        ;;
      *) grep -qF ".sh $previous" "$TEST_DIR/output" ;;
    esac
    COUNT=$((COUNT + 1))
  done
done

printf 'PASS: %s AWS image isolation checks (temporary configuration and Docker stub only)\n' "$COUNT"
