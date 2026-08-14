# npm 옛 패키지 deprecate 절차

> 2026-08-14 실측. **건마다 2FA 라 사람이 직접 돌려야 한다.**

## 지금 올라가 있는 것

| 패키지 | 판 |
| --- | --- |
| `priorcase` | 0.1.0 · 0.1.1 · **0.1.2(현재)** |
| `priorcase-{darwin,linux}-{arm64,x64}` | 0.1.0 · 0.1.1 · **0.1.2(현재)** |
| `casebook` | 0.0.1 (개명 전 이름) |
| `casebook-{darwin,linux}-{arm64,x64}` | 0.0.1 |

## 무엇을 왜

**`casebook` 계열 5개는 통째로 deprecate 한다.** 제품명이 `priorcase` 로
바뀌었고([[priorcase-결정-제품명-priorcase-개명-2026-08-10]]), 그 이름으로는
더 안 올린다. 지금 설치하면 옛 바이너리를 받는다.

**플랫폼 패키지의 옛 판(0.1.0)** 도 같이 정리한다. 아무도 직접 설치하지 않지만
(주 패키지의 optionalDependencies 로만 딸려온다), 검색에 뜨는 것이 혼란을 준다.

`priorcase@0.1.0`·`0.1.1` 자체는 **건드리지 않는다** — 그 판을 고정해 쓰는
사람이 있을 수 있고, deprecate 는 설치할 때마다 경고를 띄운다.

## 명령

```sh
# casebook 계열 — 개명 안내
for p in casebook casebook-darwin-arm64 casebook-darwin-x64 \
         casebook-linux-arm64 casebook-linux-x64; do
  npm deprecate "$p@0.0.1" "priorcase 로 개명했다. npm i -g priorcase 를 쓰라"
done

# 플랫폼 패키지의 옛 판
for p in priorcase-darwin-arm64 priorcase-darwin-x64 \
         priorcase-linux-arm64 priorcase-linux-x64; do
  npm deprecate "$p@0.1.0" "옛 판이다. priorcase 최신판이 알아서 받아 간다"
done
```

각 명령마다 2FA 코드를 묻는다 (총 9회).

## 확인

```sh
npm view casebook deprecated
npm view priorcase-darwin-arm64@0.1.0 deprecated
```

## 되돌리기

```sh
npm deprecate "casebook@0.0.1" ""   # 빈 문자열이면 해제된다
```
