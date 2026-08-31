#!/usr/bin/env bash
# Tauri 사이드카로 넣을 prior 바이너리를 만든다.
#
# **Tauri 는 `binaries/prior-<타깃트리플>` 이라는 이름을 요구한다.** 파일이 없으면
# `tauri build` 가 통째로 실패하므로, 로컬에서 앱을 띄울 때도 이걸 먼저 돌려야 한다.
#
# 왜 번들하나: 사내 배포 대상이 개발자만이 아니다. 윈도우 기획자에게 "Node 를 깔고
# npm i -g priorcase 를 치세요" 라고 하면 거기서 멈춘다 — 앱 하나로 되어야 한다.
# 다만 앱은 **PATH 의 prior 를 먼저 쓴다**(commands.rs 의 prior_bin). 번들은 폴백이다.
#
# 사용: app/scripts/bundle-prior.sh [타깃트리플]
#       트리플을 안 주면 이 머신의 것을 쓴다.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/app/src-tauri/binaries"

triple="${1:-$(rustc -vV | awk '/^host:/ {print $2}')}"
[ -n "$triple" ] || { echo "타깃 트리플을 알 수 없다 — 인자로 줘라" >&2; exit 1; }

# 트리플에서 GOOS/GOARCH 를 유도한다. Tauri 가 쓰는 이름이 Go 와 달라서 표가 필요하다.
case "$triple" in
  *apple-darwin)        goos=darwin ;;
  *pc-windows-msvc)     goos=windows ;;
  *unknown-linux-gnu)   goos=linux ;;
  *) echo "모르는 트리플: $triple" >&2; exit 1 ;;
esac
case "$triple" in
  aarch64-*) goarch=arm64 ;;
  x86_64-*)  goarch=amd64 ;;
  *) echo "모르는 아키텍처: $triple" >&2; exit 1 ;;
esac

ext=""
[ "$goos" = "windows" ] && ext=".exe"

mkdir -p "$OUT"
# 판을 박는다. 안 박으면 번들된 바이너리가 자기를 0.0.0 이라 부르고, 앱이 그걸
# 그대로 보여 준다 — npm-pack.sh 가 같은 검사를 거는 그 사고다.
ver="${PRIORCASE_VERSION:-$(git -C "$ROOT" describe --tags --abbrev=0 2>/dev/null || echo 0.0.0)}"
ver="${ver#v}"

( cd "$ROOT" && GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/xian0310567/priorcase/internal/adapter/cli.Version=${ver}" \
    -o "$OUT/prior-${triple}${ext}" ./cmd/prior )

chmod +x "$OUT/prior-${triple}${ext}"
echo "사이드카: $OUT/prior-${triple}${ext} (판 ${ver})"
