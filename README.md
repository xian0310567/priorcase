# casebook

에이전트가 내린 결정을 사람이 시키지 않아도 남기고, 다음 판단 시점에 알아서 꺼내 주는 층이다.
마크다운 + frontmatter 로 볼트에 쌓이는 결정 노트를 만들고(`capture`), 색인을 갱신하고
(`index`), 관련 있는 과거 결정을 찾아 오고(`recall`), 결과가 나오면 회고를 붙인다(`review`).
런타임 의존이 없는 Go 단일 정적 바이너리 `cb` 하나로 전부 한다.

## 설치

    go install github.com/xian0310567/casebook/cmd/cb@latest

또는 [GitHub Releases](https://github.com/xian0310567/casebook/releases) 에서
`casebook_<os>_<arch>.tar.gz` 를 받아 `cb` 를 PATH 에 둔다. darwin/linux × amd64/arm64.

**Homebrew 는 아직 쓸 수 없다.** `.goreleaser.yaml` 에 tap 배포 설정(`homebrew_casks`)은
들어 있지만 tap 저장소 `xian0310567/homebrew-tap` 을 아직 만들지 않았다. 그래서 지금
`brew install xian0310567/tap/casebook` 은 동작하지 않는다. tap 저장소를 만들고 릴리스
워크플로에 `HOMEBREW_TAP_GITHUB_TOKEN` 시크릿을 넣으면 그때부터 켜진다 — 그 전까지는
릴리스가 cask 업로드를 건너뛰고 정상 종료한다.

## 현재 상태

**Plan 1 (코어 + CLI) 구현 완료.** `cb index` · `cb recall` · `cb capture` · `cb review`
네 서브커맨드가 코어(`internal/core/*`)를 통해 실제로 동작한다.

MCP 서버 · 데몬(놓친 기록 안전망) · Claude Code 훅 어댑터는 아직 없다 (각각 Plan 2~4).
지금은 CLI 를 직접 호출해서 쓰는 단계다.

## 설정

설정 파일 경로는 이 순서로 정해진다.

1. `--config <경로>` 플래그
2. `CASEBOOK_CONFIG` 환경변수
3. `$XDG_CONFIG_HOME/casebook/config.toml` (보통 `~/.config/casebook/config.toml`)

플래그를 붙일 수 없는 자리(훅·데몬 어댑터)가 있어서 환경변수 통로가 따로 있다.
셋 다 없으면 3번 경로를 열려다 실패한다 (실제 실행 결과 — 기본 경로를 명확히 보이려고
`XDG_CONFIG_HOME` 을 빈 디렉토리로 지정했다):

```
$ XDG_CONFIG_HOME=/tmp/nonexistent-xdg cb index
cb: 설정 파일을 열 수 없다 (/tmp/nonexistent-xdg/casebook/config.toml): open /tmp/nonexistent-xdg/casebook/config.toml: no such file or directory
```

`CASEBOOK_VAULT` 는 별개다 — 설정 파일의 `vault` 값만 덮어쓴다 (테스트 볼트 격리용).

```toml
vault = "~/Documents/Obsidian Vault"

# exclude 는 top-level 키이므로 [[domain]] 보다 앞에 둔다.
# TOML 의 테이블 스코프 규칙상 테이블 헤더([[domain]]) 뒤에 오는 bare key 는
# 그 테이블(마지막 domain)의 필드로 읽힌다 — 여기 두면 top-level exclude 가
# 아니라 도메인 항목의 필드가 되어 버린다.
exclude = ["/home/t/project/NOI"]

# [naming] 은 필수다. 네 키가 다 있어야 한다.
[naming]
decision_file = "{domain}-결정-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog = "99-{project}-작업-로그.md"
index = "decisions/INDEX.md"

# [capture] 는 데몬(Plan 4)이 쓸 값이다. 지금은 읽기만 하고 아무도 쓰지 않는다.
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

`paths` 는 "지금 이 디렉토리가 어느 도메인인가"를 판정하는 데 쓴다 — `cb recall` 이
cwd 도메인 결정에 가산점을 준다. `exclude` 는 그 판정에서 빼는 경로다.
`worklog` 는 아직 어느 명령도 쓰지 않는다 (작업 로그 기록은 미구현).

### `[naming]` 은 필수다

`[naming]` 절이 통째로 빠지거나 네 키 중 하나라도 비면 그 자리에서 죽는다.
예전에는 통과시켰는데, 그러면 `decisions_dir` 이 빈 문자열이라 결정 폴더가 볼트 루트가
되고 색인이 볼트 디렉터리를 덮어쓰려 든다 — 설정 오류가 엉뚱한 층에서 엉뚱한 메시지로
터진다. 설정의 함정은 설정을 읽는 자리에서 잡는다. `[naming]` 을 뺀 `no-naming.toml`:

```toml
vault = "./vault"

[[domain]]
prefix = "casebook-demo"
folder = "casebook-demo"
```

```
$ cb --config no-naming.toml index
cb: [naming] 의 decision_file 항목이 비어 있다 — 설정에 [naming] 절이 통째로 빠졌는지 확인하라
```

`decision_file` 에는 추가 제약이 있다. `{domain}` · `{slug}` · `{date}` 자리표시자가 다
있어야 하고, `{domain}` 이 `{slug}` 보다 앞이어야 하며 둘 사이에 **결정 표식**이 있어야
하고, `-{date}.md` 로 끝나야 한다. `decisions_dir` 에는 `{project}` 가 있어야 한다.

### `decision_file` 이 국제화 지점이다

`{domain}` 과 `{slug}` 사이의 문자열이 **결정 표식**이다. 기본 한국어 템플릿에서는
`-결정-` 이고, 이 값이 파일명 필터·접두어 추출·스키마 검증의 유일한 정본이다. 코드
어디에도 `-결정-` 리터럴이 없으므로 템플릿을 바꾸면 전부 따라 바뀐다.

```toml
vault = "./vault-en"

[naming]
decision_file = "{domain}-decision-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog = "99-{project}-worklog.md"
index = "decisions/INDEX.md"

[[domain]]
prefix = "demo"
folder = "demo"
```

이 설정으로 실제로 돌린 결과다.

```
$ cb --config en-config.toml capture \
    --domain demo --slug storage-format-markdown \
    --summary "Store decision notes as markdown with YAML frontmatter" \
    --tag storage --date 2026-08-07
기록됨: demo/decisions/demo-decision-storage-format-markdown-2026-08-07.md

$ cb --config en-config.toml index
색인 1행 생성

$ cb --config en-config.toml recall --format inject storage format markdown
[과거 결정 참조]
- 2026-08-07 Store decision notes as markdown with YAML frontmatter (active/pending) → demo/decisions/demo-decision-storage-format-markdown-2026-08-07.md

$ cb --config en-config.toml review demo-decision-storage-format-markdown-2026-08-07 --outcome good
갱신됨: demo-decision-storage-format-markdown-2026-08-07
```

### 오타 키는 조용히 넘어가지 않는다

`cb` 는 go-toml/v2 의 `DisallowUnknownFields()` 로 strict 하게 읽는다. `exclude` 를
`[[domain]]` 뒤에 둔 `bad-config.toml`:

```toml
vault = "./vault"

[naming]
decision_file = "{domain}-결정-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog = "99-{project}-작업-로그.md"
index = "decisions/INDEX.md"

[[domain]]
prefix = "casebook-demo"
folder = "casebook-demo"

exclude = ["/home/t/project/NOI"]
```

그 자리에서 바로 에러가 난다 (실제 실행 결과):

```
$ cb --config bad-config.toml index
cb: 설정에 알 수 없는 키가 있다 (bad-config.toml):
10| prefix = "casebook-demo"
11| folder = "casebook-demo"
12|
13| exclude = ["/home/t/project/NOI"]
  | ~~~~~~~ unknown field
```

## 사용 예

아래는 전부 실제로 `cb` 를 빌드해 빈 임시 디렉토리에서 돌려서 나온 출력이다.
순서대로 따라 하면 그대로 재현된다 — `--date` 를 명시했으므로 오늘 날짜와 무관하다.

먼저 작업 디렉토리에 `demo-config.toml` 을 만든다.

```toml
vault = "./vault"

[naming]
decision_file = "{domain}-결정-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog = "99-{project}-작업-로그.md"
index = "decisions/INDEX.md"

[[domain]]
prefix = "casebook-demo"
folder = "casebook-demo"
```

### `cb capture` — 결정을 기록한다

```
$ cb --config demo-config.toml capture \
    --domain casebook-demo \
    --slug 저장포맷-마크다운 \
    --summary "결정 노트는 프론트매터 있는 마크다운으로 저장한다" \
    --tag 저장 --tag 포맷 \
    --date 2026-08-07 \
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
    --date 2026-08-07 \
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

`decisions/INDEX.md` (설정의 `naming.index`) 에 날짜 · domain · summary · status ·
outcome · 링크 표가 생긴다.

### `cb recall` — 관련 과거 결정을 찾는다

```
$ cb --config demo-config.toml recall --format inject 회수 키워드
[과거 결정 참조]
- 2026-08-07 회수는 임베딩 대신 파일명 접두어 + 키워드 매칭으로 시작한다 (active/pending) → casebook-demo/decisions/casebook-demo-결정-회수-키워드매칭-2026-08-07.md
```

`--format inject` 는 훅·MCP 어댑터가 그대로 컨텍스트에 주입할 수 있는 형태다.
`--format human`(기본)은 점수와 stem 을 보여준다.

```
$ cb --config demo-config.toml recall 회수 키워드
  8  casebook-demo-결정-회수-키워드매칭-2026-08-07
     회수는 임베딩 대신 파일명 접두어 + 키워드 매칭으로 시작한다
```

`status: regretted` 이거나 `outcome: bad` 인 결정이 결과에 끼면 `--format inject` 출력
끝에 회고를 먼저 읽으라는 경고 줄이 붙는다.

### 유사 slug 는 거부된다

같은 결정이 두 노트로 갈라지면 회수가 둘 다 물어오고 어느 쪽이 정본인지 알 수 없게 된다.
하이픈 · 공백 · 밑줄 · 대소문자만 다른 slug 는 같은 결정으로 보고 막는다 (실제 실행 결과):

```
$ cb --config demo-config.toml capture \
    --domain casebook-demo --slug 회수_키워드매칭 \
    --summary "같은 결정을 다시 쓰려 한다" --date 2026-08-07
cb: 유사한 결정이 이미 있다: "casebook-demo-결정-회수-키워드매칭-2026-08-07" (하이픈·공백·밑줄·대소문자만 다르다). 뒤집는 결정이면 --supersedes 를 쓰고, 정말 다른 결정이면 slug 를 구별되게 바꿔라
```

### `--supersedes` 는 양방향으로 엮는다

`cb capture --supersedes` 와 `cb review --supersedes` 가 같은 로직을 탄다. 새 노트의
`supersedes` 에 위키링크가 들어갈 뿐 아니라, **뒤집힌 옛 노트도 함께 갱신된다** —
`status` 가 `superseded` 가 되고 `related` 에 새 노트가 추가된다. 옛 노트가 `active` 로
남아 있으면 회수 감점이 안 걸려 이미 뒤집힌 결정이 계속 만점으로 올라온다.

```
$ cb --config demo-config.toml capture \
    --domain casebook-demo \
    --slug 회수-임베딩전환 \
    --summary "회수를 임베딩 유사도로 바꾼다" \
    --supersedes casebook-demo-결정-회수-키워드매칭-2026-08-07 \
    --date 2026-08-08
기록됨: casebook-demo/decisions/casebook-demo-결정-회수-임베딩전환-2026-08-08.md

관련 과거 결정:
  - 2026-08-07 회수는 임베딩 대신 파일명 접두어 + 키워드 매칭으로 시작한다
```

뒤집힌 옛 노트가 실제로 이렇게 바뀐다.

```
$ head -9 vault/casebook-demo/decisions/casebook-demo-결정-회수-키워드매칭-2026-08-07.md
---
type: decision
date: 2026-08-07
domain: [casebook-demo]
summary: "회수는 임베딩 대신 파일명 접두어 + 키워드 매칭으로 시작한다"
status: superseded
outcome: pending
supersedes: ""
related: ["[[casebook-demo-결정-회수-임베딩전환-2026-08-08]]"]
```

두 노트를 모두 검증한 뒤에야 쓰기가 시작된다. 새 노트가 스키마 검증에서 걸리면 옛 노트도
건드리지 않는다 — 반쪽짜리 연결이 디스크에 남지 않는다.

### `cb review` — 결과가 나온 결정에 outcome·회고를 붙인다

```
$ cb --config demo-config.toml review casebook-demo-결정-저장포맷-마크다운-2026-08-07 \
    --outcome good \
    --retro "그대로 잘 갔다. grep 으로 바로 찾아진다."
갱신됨: casebook-demo-결정-저장포맷-마크다운-2026-08-07
```

### `CASEBOOK_CONFIG` — 플래그를 못 쓰는 자리용

```
$ CASEBOOK_CONFIG=$PWD/demo-config.toml cb index
색인 3행 생성
```

플래그가 환경변수를 이긴다.

```
$ CASEBOOK_CONFIG=/없는/경로.toml cb --config demo-config.toml index
색인 3행 생성
```

## 개발

    make build   # go build -trimpath -ldflags="-s -w" -o cb ./cmd/cb
    make test    # go test ./...
    make lint    # go vet ./...

`Makefile` 은 `GOTOOLCHAIN=auto` 를 저장소에 고정해 둔다. 개발 머신이 Homebrew Go
1.23.3 + `GOTOOLCHAIN=local` 이면 최신 `x/text` 가 요구하는 Go 버전 때문에 맨몸
`go build`/`go mod tidy` 가 실패한다. `GOTOOLCHAIN=auto` 는 go.mod 가 요구하는
툴체인을 필요할 때 자동으로 받아 오게 해서 이 문제를 없앤다.

CI 는 `gofmt -l` · `go vet` · `go test -race` 를 돌린다.

### 실볼트 대조 테스트

실볼트 사본은 저장소에 넣지 않는다 (결정 노트에 개인 내용이 들어 있다). 대신
`CASEBOOK_TEST_VAULT` 가 설정됐을 때만 도는 로컬 전용 테스트가 있다. CI 에서는 건너뛴다.

    CASEBOOK_TEST_VAULT="$HOME/Documents/Obsidian Vault" go test ./... -run RealVault -v

실볼트를 **읽기만** 한다 — 모든 결정 노트가 파싱되는지, 스키마를 통과하는지,
`cb index` 가 노트 수만큼 행을 내는지 본다. 전후 스냅샷을 대조해 쓰지 않았음도 확인한다.

## 보장 수준

| | Claude Code | MCP 전용 호스트 |
|---|---|---|
| 결정 순간 기록 | 에이전트 `cb capture` | 동일 |
| 놓친 기록 줍기 | 데몬 | 동일 |
| 세션 진입 컨텍스트 | 훅 (보장) | `initialize.instructions` (사실상 동등) |
| 주제 전환 시 회수 | 훅 (강제) | 계약 + 편승 (유도) |

MCP 에는 서버가 대화 중간에 텍스트를 밀어넣는 채널이 없다. 마지막 줄이 유일한 차이다.

이 표의 "데몬" · "훅" 줄은 아직 구현되지 않았다 (Plan 3~4). 지금 동작하는 것은
`cb capture` 를 직접 부르는 경로뿐이다.

## 라이선스

MIT. `LICENSE` 참고.
