#!/usr/bin/env bash
# 릴리스 산출물로 npm 패키지 7개를 만든다 (런처 1 + 플랫폼 6).
#
# goreleaser 는 npm 게시를 기본 지원하지 않는다. 그래서 dist/ 의 tar.gz 를 풀어
# 플랫폼 패키지에 넣고, 버전을 태그로 맞춘다. 게시는 릴리스 워크플로가 한다.
#
# 사용: scripts/npm-pack.sh <version> [outdir]
set -euo pipefail

VERSION="${1:?사용: npm-pack.sh <version> [outdir]}"
VERSION="${VERSION#v}"
OUT="${2:-dist/npm}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# goreleaser 의 os/arch 이름과 npm 이 보는 이름이 다르다.
#   arch: amd64 → x64
#   os:   windows → win32   (node 의 process.platform 은 64비트에서도 win32 다)
# 표의 넷째 칸이 npm 패키지 이름에 들어가는 os 다. 여기가 어긋나면 런처가
# 못 찾는데, 그 실패는 그 플랫폼 사용자에게만 보인다.
declare -a TARGETS=(
  "darwin  amd64 x64   darwin"
  "darwin  arm64 arm64 darwin"
  "linux   amd64 x64   linux"
  "linux   arm64 arm64 linux"
  "windows amd64 x64   win32"
  "windows arm64 arm64 win32"
)

rm -rf "$OUT"
mkdir -p "$OUT"

for t in "${TARGETS[@]}"; do
  read -r goos goarch npmarch npmos <<<"$t"

  # 윈도우 산출물은 zip 이고 실행파일에 확장자가 붙는다 (.goreleaser.yaml 의 §).
  if [ "$goos" = "windows" ]; then
    archive="$ROOT/dist/priorcase_${goos}_${goarch}.zip"; exe="prior.exe"
  else
    archive="$ROOT/dist/priorcase_${goos}_${goarch}.tar.gz"; exe="prior"
  fi
  [ -f "$archive" ] || { echo "없다: $archive — goreleaser 를 먼저 돌려라" >&2; exit 1; }

  pkgdir="$OUT/${npmos}-${npmarch}"
  mkdir -p "$pkgdir/bin"
  if [ "$goos" = "windows" ]; then
    unzip -q -o -j "$archive" "$exe" -d "$pkgdir/bin"
  else
    tar xzf "$archive" -C "$pkgdir/bin" "$exe"
  fi
  chmod +x "$pkgdir/bin/$exe"
  cp "$ROOT/LICENSE" "$ROOT/THIRD-PARTY-NOTICES.md" "$pkgdir/"

  # os·cpu 를 적어 두면 npm 이 **다른 플랫폼에는 아예 안 받는다.** 이게 없으면
  # 사용자가 4개를 전부 받아 디스크를 4배 쓴다.
  cat > "$pkgdir/package.json" <<JSON
{
  "name": "priorcase-${npmos}-${npmarch}",
  "version": "${VERSION}",
  "description": "priorcase binary for ${npmos}-${npmarch}",
  "license": "SEE LICENSE IN LICENSE",
  "os": ["${npmos}"],
  "cpu": ["${npmarch}"],
  "files": ["bin/", "LICENSE", "THIRD-PARTY-NOTICES.md"]
}
JSON
done

# 런처. optionalDependencies 의 버전을 이번 판으로 고정한다 — ^ 나 ~ 를 쓰면
# 런처와 바이너리의 판이 갈릴 수 있고, 그건 진단이 가장 어려운 종류다.
launcher="$OUT/priorcase"
mkdir -p "$launcher"
cp -R "$ROOT/npm/priorcase/bin" "$launcher/"
cp "$ROOT/LICENSE" "$ROOT/THIRD-PARTY-NOTICES.md" "$ROOT/README.md" "$launcher/"
python3 - "$ROOT/npm/priorcase/package.json" "$launcher/package.json" "$VERSION" <<'PY'
import json, sys
src, dst, ver = sys.argv[1], sys.argv[2], sys.argv[3]
p = json.load(open(src))
p["version"] = ver
p["optionalDependencies"] = {k: ver for k in p["optionalDependencies"]}
json.dump(p, open(dst, "w"), indent=2, ensure_ascii=False)
open(dst, "a").write("\n")
PY

# **바이너리가 자기를 뭐라고 부르는지 확인한다.**
#
# 판이 두 곳에서 따로 정해진다. goreleaser 가 ldflags 로 바이너리에 박고, 이
# 스크립트는 인자로 받아 package.json 에 쓴다. **둘이 같은지 아무도 안 봤다.**
#
# 2026-08-10 에 그래서 틀렸다. `goreleaser --snapshot` 으로 빌드한 산출물을
# `npm-pack.sh 0.1.0` 으로 포장해 게시했더니, 설치는 0.1.0 인데 `prior --version`
# 은 `0.0.1-SNAPSHOT-<sha>` 라고 답했다. MCP 의 serverInfo.version 도 같이 틀렸다.
# 설치도 실행도 멀쩡해서 **아무 데서도 안 걸린다** — 사용자가 판을 물어볼 때만 드러난다.
#
# 호스트와 같은 플랫폼 것 하나만 실제로 실행해 본다. 넷은 같은 빌드에서 나오므로
# 하나가 맞으면 넷 다 맞고, 크로스 플랫폼 바이너리는 여기서 돌릴 수 없다.
host_goos=$(go env GOOS 2>/dev/null || uname -s | tr 'A-Z' 'a-z')
host_goarch=$(go env GOARCH 2>/dev/null || true)
case "$host_goarch" in amd64) host_npmarch=x64 ;; arm64) host_npmarch=arm64 ;; *) host_npmarch="" ;; esac

case "$host_goos" in windows) host_npmos=win32 ;; *) host_npmos="$host_goos" ;; esac
if [ -n "$host_npmarch" ] && [ -x "$OUT/${host_npmos}-${host_npmarch}/bin/prior" ]; then
  reported=$("$OUT/${host_npmos}-${host_npmarch}/bin/prior" --version 2>&1 | awk '{print $NF}')
  if [ "$reported" != "$VERSION" ]; then
    echo "판이 어긋난다 — package.json 은 ${VERSION} 인데 바이너리는 ${reported} 라고 답한다." >&2
    echo "  goreleaser 를 --snapshot 으로 돌렸을 때 이렇게 된다. 태그를 붙여 다시 빌드해라." >&2
    exit 1
  fi
  echo "판 확인: 바이너리와 package.json 둘 다 ${VERSION}"
else
  echo "판 확인 건너뜀 — 호스트(${host_goos}/${host_goarch})와 같은 플랫폼 산출물이 없다" >&2
fi

echo "npm 패키지 7개: $OUT"
