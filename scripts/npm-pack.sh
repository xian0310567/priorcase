#!/usr/bin/env bash
# 릴리스 산출물로 npm 패키지를 만든다 (런처 1 + 플랫폼 N).
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

# PRIORCASE_SKIP_PLATFORMS 는 **이번 포장에서만** 뺄 npm os 이름들이다 (공백 구분).
#
# # 왜 있나 (2026-09-02)
#
# npm 트러스티드 퍼블리싱은 **이미 존재하는 패키지에만** 설정할 수 있다. 그래서 새
# 플랫폼을 추가하면 그 패키지 이름의 첫 게시를 사람이 브라우저 인증으로 해야 하고,
# 그 전까지 릴리스 워크플로가 404 로 죽는다 — v0.5.0 이 정확히 그렇게 절반만 나갔다
# (플랫폼 4개는 게시됐는데 win32 둘이 404 라 런처까지 못 나갔다).
#
# 이 스위치는 그때 **나머지를 먼저 내보내기 위한 것**이다. 소스 package.json 은
# 여섯 플랫폼을 그대로 선언하고 arch 불변식 테스트도 그대로 여섯을 지킨다 — 의도는
# 안 바뀌었고 게시만 미룬다.
#
# **되돌리는 법: 릴리스 워크플로에서 이 환경변수를 지운다.** 한 줄이다.
declare -a SKIPPED=()
if [ -n "${PRIORCASE_SKIP_PLATFORMS:-}" ]; then
  declare -a KEPT=()
  for t in "${TARGETS[@]}"; do
    read -r _ _ _ npmos <<<"$t"
    skip=""
    for s in ${PRIORCASE_SKIP_PLATFORMS}; do [ "$npmos" = "$s" ] && skip=1; done
    if [ -n "$skip" ]; then SKIPPED+=("$t"); else KEPT+=("$t"); fi
  done
  TARGETS=("${KEPT[@]}")
  # **조용히 빼지 않는다.** 빠진 플랫폼은 그 사용자에게만 안 보이는 고장이 된다.
  echo "건너뛴 플랫폼: ${PRIORCASE_SKIP_PLATFORMS} (${#SKIPPED[@]}개 타깃)" >&2
  [ "${#TARGETS[@]}" -gt 0 ] || { echo "전부 건너뛰면 배포할 것이 없다" >&2; exit 1; }
fi

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
# **실제로 포장한 것만 선언한다.** 소스의 키를 그대로 베끼면, 건너뛴 플랫폼이
# optionalDependencies 에 남아 npm 이 없는 패키지를 받으려다 실패한다 — optional 이라
# 설치는 성공하고 실행만 안 되는, 사용자가 원인을 짐작 못 하는 종류다.
packed=$(printf '%s\n' "${TARGETS[@]}" | awk '{print "priorcase-" $4 "-" $3}' | sort -u | paste -sd, -)
python3 - "$ROOT/npm/priorcase/package.json" "$launcher/package.json" "$VERSION" "$packed" <<'PY'
import json, sys
src, dst, ver, packed = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
p = json.load(open(src))
p["version"] = ver
names = [n for n in packed.split(",") if n]
unknown = [n for n in names if n not in p["optionalDependencies"]]
if unknown:
    # 포장한 이름이 소스에 없다 = 표와 선언이 어긋났다. 게시 전에 죽는 편이 낫다.
    sys.exit(f"포장한 패키지가 소스 optionalDependencies 에 없다: {unknown}")
p["optionalDependencies"] = {n: ver for n in names}
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

echo "npm 패키지 $(( ${#TARGETS[@]} + 1 ))개 (런처 1 + 플랫폼 ${#TARGETS[@]}): $OUT"
