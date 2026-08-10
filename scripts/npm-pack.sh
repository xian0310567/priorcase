#!/usr/bin/env bash
# 릴리스 산출물로 npm 패키지 5개를 만든다 (런처 1 + 플랫폼 4).
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

# goreleaser 의 os/arch 이름과 npm 의 process.arch 이름이 다르다. amd64 → x64.
declare -a TARGETS=("darwin amd64 x64" "darwin arm64 arm64" "linux amd64 x64" "linux arm64 arm64")

rm -rf "$OUT"
mkdir -p "$OUT"

for t in "${TARGETS[@]}"; do
  read -r goos goarch npmarch <<<"$t"
  tarball="$ROOT/dist/casebook_${goos}_${goarch}.tar.gz"
  [ -f "$tarball" ] || { echo "없다: $tarball — goreleaser 를 먼저 돌려라" >&2; exit 1; }

  pkgdir="$OUT/${goos}-${npmarch}"
  mkdir -p "$pkgdir/bin"
  tar xzf "$tarball" -C "$pkgdir/bin" cb
  chmod +x "$pkgdir/bin/cb"
  cp "$ROOT/LICENSE" "$ROOT/THIRD-PARTY-NOTICES.md" "$pkgdir/"

  # os·cpu 를 적어 두면 npm 이 **다른 플랫폼에는 아예 안 받는다.** 이게 없으면
  # 사용자가 4개를 전부 받아 디스크를 4배 쓴다.
  cat > "$pkgdir/package.json" <<JSON
{
  "name": "casebook-${goos}-${npmarch}",
  "version": "${VERSION}",
  "description": "casebook binary for ${goos}-${npmarch}",
  "license": "SEE LICENSE IN LICENSE",
  "os": ["${goos}"],
  "cpu": ["${npmarch}"],
  "files": ["bin/", "LICENSE", "THIRD-PARTY-NOTICES.md"]
}
JSON
done

# 런처. optionalDependencies 의 버전을 이번 판으로 고정한다 — ^ 나 ~ 를 쓰면
# 런처와 바이너리의 판이 갈릴 수 있고, 그건 진단이 가장 어려운 종류다.
launcher="$OUT/casebook"
mkdir -p "$launcher"
cp -R "$ROOT/npm/casebook/bin" "$launcher/"
cp "$ROOT/LICENSE" "$ROOT/THIRD-PARTY-NOTICES.md" "$ROOT/README.md" "$launcher/"
python3 - "$ROOT/npm/casebook/package.json" "$launcher/package.json" "$VERSION" <<'PY'
import json, sys
src, dst, ver = sys.argv[1], sys.argv[2], sys.argv[3]
p = json.load(open(src))
p["version"] = ver
p["optionalDependencies"] = {k: ver for k in p["optionalDependencies"]}
json.dump(p, open(dst, "w"), indent=2, ensure_ascii=False)
open(dst, "a").write("\n")
PY

echo "npm 패키지 5개: $OUT"
