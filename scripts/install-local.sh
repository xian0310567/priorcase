#!/usr/bin/env bash
# 레포 HEAD 를 빌드해 **훅이 실제로 부르는 자리**에 꽂는다.
#
# 왜 필요한가: 훅 배선은 npm 이 깐 플랫폼 바이너리를 절대 경로로 가리킨다
# (~/.claude/settings.json). 그래서 레포를 아무리 고쳐도 `go build` 만으로는
# 도는 것이 안 바뀐다 — 실측으로 설치본이 v0.1.2 인 채 레포가 39커밋 앞서 있었고,
# 그동안 sweep·MaxJudgeFails 가 전부 안 돌았다.
#
# **ad-hoc 서명이 필수다.** 서명 없이 덮어쓰면 macOS 가 SIGKILL 로 죽인다
# (실측 rc=137). npm 이 깐 바이너리에는 서명이 붙어 있어서, 그 자리를 새 파일로
# 갈아치우면 서명만 어긋난 상태가 된다.
#
# 데몬도 다시 띄운다. 도는 프로세스는 옛 inode 를 붙들고 있어서 파일만 바꿔서는
# 새 코드가 안 돈다.
set -euo pipefail

BIN="${PRIORCASE_INSTALL_PATH:-$HOME/.nvm/versions/node/v24.19.0/lib/node_modules/priorcase/node_modules/priorcase-darwin-arm64/bin/prior}"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

REV="$(git rev-parse --short HEAD)"
DIRTY=""
git diff --quiet || DIRTY=".dirty"
VER="$(git describe --tags --abbrev=0 2>/dev/null || echo 0.0.0)+local.${REV}${DIRTY}"

echo "빌드 → $VER"
go build -trimpath \
  -ldflags="-s -w -X github.com/xian0310567/priorcase/internal/adapter/cli.Version=${VER}" \
  -o /tmp/prior-local ./cmd/prior

if [ ! -e "${BIN}.bak-npm" ] && [ -e "$BIN" ]; then
  cp -p "$BIN" "${BIN}.bak-npm"
  echo "원본 백업 → ${BIN}.bak-npm"
fi

# 데몬을 먼저 멈춘다. 옛 inode 를 붙든 채로 두면 새 코드가 안 돈다.
#
# ⚠️ `pkill -f` 는 **명령줄 전체**를 본다. 그래서 이 패턴을 인자로 달고 있는
# 프로세스 — 이 스크립트를 부른 셸 자체 — 까지 잡을 수 있다. 실제로 한 번 물렸다.
# 그래서 pgrep 으로 후보를 뽑아 자신과 부모를 빼고 죽인다.
watchpids() { pgrep -f -- "$BIN watch" 2>/dev/null | grep -vx -e "$$" -e "$PPID" || true; }
for pid in $(watchpids); do kill "$pid" 2>/dev/null || true; done
sleep 1

cp /tmp/prior-local "$BIN"
xattr -c "$BIN" 2>/dev/null || true
codesign --force --sign - "$BIN"
"$BIN" --version

# 데몬을 다시 띄운다. 홈에서 띄우는 이유: cwd 가 도메인 판정에 안 쓰이는 경로다.
( cd "$HOME" && nohup "$BIN" watch >/dev/null 2>&1 & )
sleep 2
if [ -n "$(watchpids)" ]; then
  echo "데몬 재시작됨"
else
  echo "⚠️ 데몬이 안 떴다 — prior watch 를 직접 확인하라"
fi
