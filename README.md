# casebook

에이전트가 내린 결정을 사람이 시키지 않아도 남기고, 다음 판단 시점에 알아서 꺼내 주는 층이다.
마크다운 + frontmatter 로 볼트에 쌓이는 결정 노트를 만들고(`capture`), 색인을 갱신하고
(`index`), 관련 있는 과거 결정을 찾아 오고(`recall`), 결과가 나오면 회고를 붙인다(`review`).
런타임 의존이 없는 Go 단일 정적 바이너리 `cb` 하나로 전부 한다.

## 설치

    brew install xian0310567/tap/casebook
    # 또는
    go install github.com/xian0310567/casebook/cmd/cb@latest

런타임 의존이 없는 단일 정적 바이너리다.

## 현재 상태

**Plan 1 (코어 + CLI) 구현 완료.** `cb index` · `cb recall` · `cb capture` · `cb review`
네 서브커맨드가 코어(`internal/core/*`)를 통해 실제로 동작한다.

MCP 서버 · 데몬(놓친 기록 안전망) · Claude Code 훅 어댑터는 아직 없다 (각각 Plan 2~4).
지금은 CLI 를 직접 호출해서 쓰는 단계다.

## 설정

기본 경로는 `$XDG_CONFIG_HOME/casebook/config.toml` (보통 `~/.config/casebook/config.toml`)이다.
`--config` 플래그나 `CASEBOOK_CONFIG` 로 다른 경로를 줄 수 있다.

```toml
vault = "~/Documents/Obsidian Vault"

# exclude 는 top-level 키이므로 [[domain]] 보다 앞에 둔다.
# TOML 의 테이블 스코프 규칙상 테이블 헤더([[domain]]) 뒤에 오는 bare key 는
# 그 테이블(마지막 domain)의 필드로 읽힌다 — 여기 두면 top-level exclude 가
# 아니라 도메인 항목의 필드가 되어 버린다.
exclude = ["/home/t/project/NOI"]

[naming]
decision_file = "{domain}-결정-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog = "99-{project}-작업-로그.md"
index = "decisions/INDEX.md"

[capture]
signals = ["결정", "선택"]
min_turns = 6
quiesce_seconds = 3

[[domain]]
prefix = "omni"
folder = "omni"
paths = ["/home/t/project/omni"]

[[domain]]
prefix = "occ"
folder = "OCC"
paths = ["/home/t/Documents/automation-dropshipping"]
```

실수로 `exclude` 를 `[[domain]]` 뒤에 두어도 조용히 넘어가지 않는다. `cb` 는
go-toml/v2 의 `DisallowUnknownFields()` 로 strict 하게 읽으므로, 위 예제에서
`exclude` 를 맨 아래로 옮기고 실행하면 그 자리에서 바로 에러가 난다 (실제 실행
결과):

```
$ cb --config bad-config.toml index
cb: 설정에 알 수 없는 키가 있다 (bad-config.toml):
21| folder = "OCC"
22| paths = ["/home/t/Documents/automation-dropshipping"]
23|
24| exclude = ["/home/t/project/NOI"]
  | ~~~~~~~ unknown field
```

## 사용 예

아래는 실제로 `cb` 를 빌드해 임시 볼트에 돌려서 나온 출력이다 (지어낸 예시가 아니다).

### `cb capture` — 결정을 기록한다

```
$ cb --config demo-config.toml capture \
    --domain casebook-demo \
    --slug 저장포맷-마크다운 \
    --summary "결정 노트는 프론트매터 있는 마크다운으로 저장한다" \
    --tag 저장 --tag 포맷 \
    --body - <<'EOF'
## 결정
결정 노트는 YAML 프론트매터 + 마크다운 본문으로 저장한다.

## 근거
사람이 grep/에디터로 바로 읽고 고칠 수 있어야 한다. DB는 그 자체로 회수 채널이 하나 더 필요해진다.
EOF
기록됨: casebook-demo/decisions/casebook-demo-결정-저장포맷-마크다운-2026-08-07.md
```

한 번 더 기록해 둔다 — 아래 `index`·`recall` 예시가 이 두 번째 결정을 근거로 한다.

```
$ cb --config demo-config.toml capture \
    --domain casebook-demo \
    --slug 회수-키워드매칭 \
    --summary "회수는 임베딩 대신 파일명 접두어 + 키워드 매칭으로 시작한다" \
    --tag 회수 --tag 검색 \
    --body - <<'EOF'
## 결정
회수는 임베딩 유사도 대신 파일명 접두어(domain) + 키워드 매칭으로 시작한다.

## 근거
임베딩은 인덱싱 파이프라인과 벡터 스토어가 필요해 CLI 단일 바이너리 원칙과 맞지 않는다.
키워드 매칭은 의존 없이 바로 동작하고, 결정 노트는 파일명과 태그가 이미 신호가 풍부하다.
EOF
기록됨: casebook-demo/decisions/casebook-demo-결정-회수-키워드매칭-2026-08-07.md
```

### `cb index` — 색인을 재생성한다

```
$ cb --config demo-config.toml index
색인 2행 생성
```

`decisions/INDEX.md` 에 날짜 · domain · summary · status · outcome · 링크 표가 생긴다.

### `cb recall` — 관련 과거 결정을 찾는다

```
$ cb --config demo-config.toml recall --format inject 회수 키워드
[과거 결정 참조]
- 2026-08-07 회수는 임베딩 대신 파일명 접두어 + 키워드 매칭으로 시작한다 (active/pending) → casebook-demo/decisions/casebook-demo-결정-회수-키워드매칭-2026-08-07.md
```

`--format inject` 는 훅·MCP 어댑터가 그대로 컨텍스트에 주입할 수 있는 형태다.
`--format human`(기본)은 점수와 stem 을 보여준다.

### `cb review` — 결과가 나온 결정에 outcome·회고를 붙인다

```
$ cb --config demo-config.toml review casebook-demo-결정-저장포맷-마크다운-2026-08-07 \
    --outcome good \
    --retro "그대로 잘 갔다. grep 으로 바로 찾아진다."
갱신됨: casebook-demo-결정-저장포맷-마크다운-2026-08-07
```

## 개발

    make build   # go build -trimpath -ldflags="-s -w" -o cb ./cmd/cb
    make test    # go test ./...
    make lint    # go vet ./...

`Makefile` 은 `GOTOOLCHAIN=auto` 를 저장소에 고정해 둔다. 개발 머신이 Homebrew Go
1.23.3 + `GOTOOLCHAIN=local` 이면 최신 `x/text` 가 요구하는 Go 버전 때문에 맨몸
`go build`/`go mod tidy` 가 실패한다. `GOTOOLCHAIN=auto` 는 go.mod 가 요구하는
툴체인을 필요할 때 자동으로 받아 오게 해서 이 문제를 없앤다.

## 보장 수준

| | Claude Code | MCP 전용 호스트 |
|---|---|---|
| 결정 순간 기록 | 에이전트 `cb capture` | 동일 |
| 놓친 기록 줍기 | 데몬 | 동일 |
| 세션 진입 컨텍스트 | 훅 (보장) | `initialize.instructions` (사실상 동등) |
| 주제 전환 시 회수 | 훅 (강제) | 계약 + 편승 (유도) |

MCP 에는 서버가 대화 중간에 텍스트를 밀어넣는 채널이 없다. 마지막 줄이 유일한 차이다.

## 라이선스

MIT. `LICENSE` 참고.
