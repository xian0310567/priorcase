# casebook

에이전트가 내린 결정을 사람이 시키지 않아도 남기고, 다음 판단 시점에 알아서 꺼내 주는 층이다.
마크다운 + frontmatter 로 볼트에 쌓이는 결정 노트를 만들고(`capture`), 색인을 갱신하고
(`index`), 관련 있는 과거 결정을 찾아 오고(`recall`), 결과가 나오면 회고를 붙인다(`review`).
런타임 의존이 없는 Go 단일 정적 바이너리 `cb` 하나로 전부 한다.

## 설치

```sh
npm install -g casebook
```

`cb` 가 PATH 에 들어가고, 훅·CLI·MCP 가 전부 그 하나로 돈다.

**MCP 만 쓸 거면 설치도 필요 없다.**

```json
{ "mcpServers": { "casebook": { "command": "npx", "args": ["-y", "casebook", "mcp"] } } }
```

**팀이면 레포에 묶어라.** 그래야 팀원 전원이 같은 판을 쓴다 — 공유 볼트에 서로 다른
판이 쓰면 갱신이 막힌다(아래 [스키마 판](#스키마-판) 참고).

```jsonc
// package.json
"devDependencies": { "casebook": "1.2.3" }
```

darwin/linux × arm64/x64. npm 이 자기 플랫폼 바이너리만 받는다 —
실행되는 것은 정적 Go 바이너리이고 **실행 시점 런타임 의존은 0 이다.**
(Node 는 설치 경로일 뿐 실행에 관여하지 않는다.)

> **⚠️ 아직 게시 전이다.** 첫 릴리스 태그를 밀어야 위 명령이 동작한다.

### 배선

```sh
cb doctor        # 지금 상태를 진단한다
cb init          # 훅 배선 계획을 보여 준다 (파일을 안 고친다)
cb init --apply  # 실제로 배선한다
```

**`cb` 가 PATH 에 있는지 확인하라.** 훅은 절대 경로로 배선되므로, PATH 에 없으면
**시스템은 멀쩡히 도는데 사람만 명령을 못 친다.** `cb doctor` 가 이걸 검사한다.

> **`npx` 로는 훅이 안 된다.** 훅은 `settings.json` 에 절대 경로가 박히고
> `user-prompt-submit` 은 매 프롬프트마다 도는데, npx 해석 지연이 거기 얹히면
> 대화가 느려진다. 훅을 쓰려면 `npm install -g` 로 깔아라.

### macOS 서명

**서명·공증하지 않는다. 그래도 막히지 않는다.**

macOS 의 `com.apple.quarantine` 딱지는 **다운로드한 프로그램이** 붙인다.
`npm`·`curl` 은 안 붙이고 브라우저는 붙인다. npm 으로 깔면 딱지가 없으므로
Gatekeeper 를 만나지 않는다.

브라우저로 tar.gz 를 직접 받았다면 그때만 한 줄이 필요하다:

```sh
xattr -dr com.apple.quarantine ./cb
```

Apple Developer 계정은 이 셋 중 하나가 될 때 든다 — 브라우저 다운로드를 주 경로로
삼을 때, `.app`·`.pkg` 를 낼 때, B2B 고객이 보안 심사에서 서명을 요구할 때.

### 라이선스

독점 소프트웨어다 ([LICENSE](LICENSE)). **다만 casebook 이 쓰는 파일은 당신 것이다** —
평문 마크다운이고, 라이선스가 끝나도 그대로 남는다. 포함된 오픈소스 구성요소는
[THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md) 를 보라.

### 스키마 판

결정 노트에는 `schema` 판이 붙는다 (판 1 은 생략한다).

**팀이 볼트를 공유하면 한 명이 먼저 올린 상태가 정상이다.** 그때 옛 `cb` 는
그 사람의 노트를 **읽고 회수하지만 고치지는 않는다** — 모르는 규칙으로 쓰인 것을
우리 규칙으로 되쓰면 조용히 망가뜨리기 때문이다. `cb doctor` 가 그걸 경고로 알린다.

## 현재 상태

**v1 구현 완료.** 서브커맨드 열 개가 동작한다 — `capture` `recall` `review` `index`
`rollup` `doctor` `mcp` `watch` `hook` `init`.

다른 호스트 파서와 임베딩 검색은 v2 다.

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

### `default_domain` 을 비우지 마라

작업 디렉토리가 어느 `[[domain]]` 의 `paths` 에도 안 걸리면 여기 적힌 도메인으로
기록된다. **비우면 그런 자리에서는 아무것도 기록되지 않는데, 겉으로는 조용하다** —
훅은 돌고 안전망은 표시까지 하다가 마지막에 막힌다. `cb init` 이 만드는 설정에는
`common` 이 들어 있다.

### `lang` 은 볼트에 남는 문자열만 정한다

색인 머리말, 회수 주입 라벨 같은 것이다. **결정 노트의 본문 언어는 여기가 정하지
않는다** — 판별기가 대화의 언어를 따라가므로, 한 볼트에 영어 대화와 한국어 대화가
섞여도 각 노트가 제 언어를 갖는다. CLI 진단 출력은 이 설정의 범위 밖이다.

```toml
vault = "~/Documents/Obsidian Vault"
lang  = "ko"                     # 볼트에 남는 문자열의 언어 (ko | en)

# exclude·default_domain 은 top-level 키이므로 [[domain]] 보다 앞에 둔다.
# TOML 의 테이블 스코프 규칙상 테이블 헤더([[domain]]) 뒤에 오는 bare key 는
# 그 테이블(마지막 domain)의 필드로 읽힌다 — 여기 두면 top-level 이 아니라
# 도메인 항목의 필드가 되어 버린다.
exclude = ["/home/t/project/scratch"]

# 어느 [[domain]] 의 paths 에도 안 걸릴 때 쓸 도메인.
# 비우면 그런 자리에서는 아무것도 기록되지 않는다.
default_domain = "common"

# [naming] 은 필수다. rollup 만 선택이다 (없으면 cb rollup 이 무엇을 적으라고 알려 준다).
[naming]
decision_file = "{domain}-결정-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog       = "99-{project}-작업-로그.md"
index         = "_meta/00-결정-색인.md"
rollup        = "98-{project}-작업-로그-요약.md"

# 데몬(cb watch)과 훅이 쓴다.
[capture]
signals = ["결정", "선택"]        # 판별기가 있으면 쓰이지 않는다
min_turns = 6
quiesce_seconds = 3
judge_path  = ""                 # 비면 자동 탐색. 못 찾으면 자동 기록이 꺼진다
judge_model = "claude-haiku-4-5"

# 무소속 결정이 쌓이는 곳. default_domain 이 이걸 가리킨다.
[[domain]]
prefix = "common"
folder = "common"

[[domain]]
prefix = "work"
folder = "work"
paths = ["/home/t/project/work"]

[[domain]]
prefix = "shop"
folder = "Shop"
paths = ["/home/t/Documents/shop-automation"]
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

exclude = ["/home/t/project/scratch"]
```

그 자리에서 바로 에러가 난다 (실제 실행 결과):

```
$ cb --config bad-config.toml index
cb: 설정에 알 수 없는 키가 있다 (bad-config.toml):
10| prefix = "casebook-demo"
11| folder = "casebook-demo"
12|
13| exclude = ["/home/t/project/scratch"]
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

#### 읽지 못한 노트는 조용히 사라지지 않는다

frontmatter 가 없거나 스키마가 옛 것이라 파싱에 실패한 노트는 색인에서 빠진다.
한 건 때문에 색인 전체가 죽지 않게 하려는 것이지만, **빠졌다는 사실은 반드시
알린다** — 요약 줄에 건수가 박히고, 어느 파일이 왜인지는 stderr 로 나온다.

위 데모 볼트에 깨진 노트 두 건을 넣어 보자 — 하나는 다른 도구가 남긴 구 스키마,
하나는 프론트매터가 아예 없는 것이다.

```
$ cb --config demo-config.toml index
색인 2행 생성 (2건 건너뜀 — 색인이 불완전하다)
경고: 결정 노트 2건을 읽지 못해 건너뛰었다 — 색인·회수에서 빠진다:
  - casebook-demo/decisions/casebook-demo-결정-구스키마-2026-08-05.md
      frontmatter 파싱 실패: yaml: unmarshal errors:
        line 1: field title not found in type store.Meta
        line 2: field project not found in type store.Meta
        line 3: field created not found in type store.Meta
  - casebook-demo/decisions/casebook-demo-결정-머리말없음-2026-08-06.md
      frontmatter 가 없다 (--- 로 시작하지 않는다)
```

종료 코드는 그래도 0 이다. 원인은 `cb` 가 고칠 수 있는 것이 아니라 볼트 데이터를
사람이 정본 10키로 옮겨야 하는 것이고, 훅·크론에서 도는 `cb index` 가 그때까지
매번 실패하면 무시하는 법만 학습시키기 때문이다. `cb capture` · `cb review` 도
내부적으로 색인을 다시 쓰므로 같은 경고를 낸다.

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

회수 대상에서 읽기 실패로 빠진 노트가 있으면 `cb index` 와 같은 경고를 낸다.
포맷과 무관하게 **항상 stderr** 다 — `--format inject` 의 stdout 은 훅이 그대로
컨텍스트에 넣는 순수 데이터라 한 줄도 섞이면 안 된다.

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

## MCP 서버로 쓰기

`cb mcp` 는 stdio MCP 서버를 띄운다. 사람이 직접 실행할 일은 없다 — 호스트가 이
프로세스를 띄우고 stdin/stdout 으로 JSON-RPC 를 주고받는다. **그래서 이 명령이 도는
동안 stdout 은 프로토콜 전용이다.** 진단 출력은 전부 stderr 로 나간다.

호스트 설정에 이렇게 등록한다 (Claude Desktop·Claude Code·그 밖의 MCP 호스트 공통 형태):

```json
{
  "mcpServers": {
    "casebook": {
      "command": "cb",
      "args": ["--config", "/home/t/.config/casebook/config.toml", "mcp"]
    }
  }
}
```

`--config` 를 생략하면 `CASEBOOK_CONFIG` → `$XDG_CONFIG_HOME/casebook/config.toml`
순으로 찾는다. 호스트가 환경변수를 물려주지 않는 경우가 많으므로 **경로를 명시하는
쪽을 권한다.**

### 도구 4종

| 도구 | 필수 인자 | 하는 일 |
|---|---|---|
| `casebook_recall` | `query` | 관련 과거 결정을 찾는다 |
| `casebook_capture` | `domain` `slug` `summary` | 결정을 기록한다 |
| `casebook_review` | `stem` | outcome·상태·회고를 갱신하거나 결정을 뒤집는다 |
| `casebook_pending` | — | 데몬이 표시한 미확인 구간을 보고 해소한다 |

### 편승 — 응답에 과거 결정이 딸려 온다

도구 결과는 그 자체로 컨텍스트 주입이다. 그래서 무엇을 부르든 관련 과거 결정을 얹는다.
특히 `capture` 시점은 곧 결정 시점이라, 기록할 때 과거 결정이 따라 나오는 것이 가장
정확한 타이밍이다.

```
기록됨: alpha/decisions/alpha-결정-캐시계층-2026-08-07.md

[과거 결정 참조]
- 2026-08-01 저장 엔진을 임베디드 DB 로 고른다 (active/pending) → alpha/decisions/alpha-결정-저장엔진-2026-08-01.md
```

### 세션 진입 — 요약 덤프가 아니라 행동 계약

MCP 에는 서버가 대화 중간에 텍스트를 밀어넣는 채널이 없다. 유일한 자리가 `initialize`
응답의 `instructions` 인데 **세션당 한 번**이다. 거기에 최근 결정을 쏟아부어도 주제가
바뀌는 순간 낡는다. 그래서 요약이 아니라 "언제 무엇을 부르라"를 심는다.

```
casebook — 이 워크스페이스의 과거 결정을 기록하고 회수한다.

**새 작업이나 주제로 넘어갈 때마다 먼저 `casebook_recall(주제)` 를 부른다.**
지금 볼트에 결정 4건이 쌓여 있다. 부르지 않으면 이미 뒤집힌 결정을 다시 제안하게 된다.

**되돌리기 어려운 선택을 했으면 그 자리에서 `casebook_capture` 를 부른다.**
아키텍처·스키마·외부 서비스·가격처럼 나중에 "왜 이렇게 했지"를 묻게 될 선택이 대상이다.
...
```

호스트가 `instructions` 를 어떻게 쓰는지는 구현 재량이다. Claude Code 는 시스템
프롬프트에 넣는 것이 확인됐으나 전수 확인은 하지 않았다 — **무시하는 호스트가 있을 수
있다.** 그때는 편승만 남는다.

### 읽지 못한 노트는 응답 본문으로 알린다

CLI 는 같은 정보를 stderr 로 낸다. MCP 에서 그렇게 하면 호스트 로그로 흘러가고 에이전트
컨텍스트에는 안 들어간다 — 회수에서 노트가 빠졌다는 사실을 정작 회수하는 쪽이 모르게 된다.
그래서 **응답 본문에** 싣는다.

```
⚠️ 결정 노트 1건을 읽지 못해 색인·회수에서 빠졌다:
  - alpha/decisions/alpha-결정-깨짐-2026-01-01.md
      frontmatter 파싱 실패: yaml: unmarshal errors:
        line 1: field title not found in type store.Meta
정본 10키로 옮겨야 회수 대상으로 돌아온다.
```

## `cb watch` — 놓친 기록을 줍는 데몬

에이전트가 `casebook_capture` 를 부르지 않고 지나간 구간을 표시한다. **LLM 을 부르지
않는다** — "이 구간에 결정이 있었을 수 있다" 는 표시만 남기고, 판별은 다음 세션의
에이전트가 한다. 그 모델이 이미 전체 맥락을 갖고 있고, API 키 등록은 오픈소스의
진입 장벽이다.

```
$ cb watch
cb watch: transcript 1173개 — 현재 지점부터 감시 1173개 · 밀린 구간 확인 0개
cb watch: 감시 시작 (/Users/t/.claude/projects)
```

**transcript 는 읽기만 한다.** 쓰는 것은 상태 파일뿐이고, 그것도 볼트 밖에 둔다.

| 파일 | 자리 |
|---|---|
| 체크포인트 · pending | `$XDG_STATE_HOME/casebook/state.json` |
| 단일 인스턴스 락 | `$XDG_STATE_HOME/casebook/watch.lock` |

### 기동 시 무엇을 하나

- **처음 보는 파일은 현재 끝으로 시딩한다.** 데몬이 켜지기 전의 대화는 안전망 대상이
  아니다. 실측 1173개를 전부 훑으면 표시가 쏟아지고, 안전망이 소음이 되면 에이전트가
  무시하는 법을 배운다. 켜기 전 기록까지 훑으려면 `--backfill`.
- **이미 아는 파일은 훑는다.** 데몬이 꺼져 있는 동안 자란 구간이 있을 수 있고, 그 파일은
  다시 바뀌지 않으므로 이때 안 보면 **영원히** 검토되지 않는다.

### 언제 표시하나

```
체크포인트 이후 구간
  → 쓰기가 quiesce_seconds 동안 멎을 때까지 대기
  → 턴 수 임계 (도구 호출·도구 결과는 세지 않는다)
  → 키워드 시그널 ([capture] signals · 판별기가 있으면 건너뛴다)
  → **면제 크레딧이 남아 있지 않을 것**
  → pending 기록
```

마지막 조건이 중요하다. 실측으로 발화 6개를 넘는 세션 585개 중 **578개(99%)** 가
기본 시그널에 걸린다 — `변경`·`선택`·`대신` 은 흔한 낱말이다. 이 조건이 없으면
에이전트가 제 할 일을 다 한 세션까지 전부 표시된다.

**면제는 소모성이다. 마지막 확인 이후 새 노트가 생겼을 때만 면제한다.**

```
세션 축:  그 세션 id 를 단 노트 수        ← 날짜에 무관, 단조
날짜 축:  날짜별로, 그날 그 도메인 노트 수  ← 날짜마다 따로 센다

면제한다 ⟺ 어느 한 축이라도 지난번에 소모한 수보다 늘었다
```

노트가 새로 생겼다는 것은 그 사이 에이전트가 기록했다는 직접 증거다. 안 늘었으면
이 구간은 아직 아무도 안 봤다. 걸린 노트들은 거기서 소모된다.

**두 축을 통짜 개수 하나로 합치면 안 된다.** 날짜 축은 *그 구간이 걸친 날짜*로
걸러지므로 구간마다 창이 움직인다. 고점을 넓은 창으로 찍고 비교를 좁은 창으로 하면,
바쁜 날을 한 번 지나간 세션은 **그 뒤로 영영 면제되지 않는다** — 자정 넘김,
`claude --continue`, 데몬 정지 뒤 backfill, 볼트 아카이브가 전부 그 상태를 만든다.
실제로 그렇게 만들었다가 리뷰에서 잡혔다.

**안전망은 자기 출력으로 자기를 억제하지 않는다.** 판별기가 자동으로 만든 노트는
만든 그 자리에서 크레딧을 소모시킨다. 안 그러면 다음 스캔이 그것을 "새로 생겼다" 로
세어 아직 아무도 안 본 구간을 면제한다.

> **처음에는 있다/없다로 판정했고, 그것이 안전망을 죽였다.** 세션 축은 첫 노트가
> 생긴 순간부터 그 세션을 영구 면제했고, 날짜 축은 그날 그 도메인 전체를 면제했다 —
> **기록을 잘 하는 프로젝트일수록 안전망이 죽는** 구조였다. 컷오버 1일차에 한
> 세션의 노트 11건이 하루 종일 안전망을 껐고, `pending: null` 은 "깨끗하다" 가
> 아니라 "애초에 안 봤다" 였는데 `cb doctor` 는 그것을 `이상 없다` 로 보고했다.
>
> **남은 거칢**: 날짜 축은 같은 날 *다른 세션*이 남긴 노트도 센다. 나란히 띄운 두
> 창 중 부지런한 쪽이 조용한 쪽의 구간을 한 번 가려 줄 수 있다. 손해가 구간 하나로
> 묶이고, 앞서 표시한 구간이 그 때문에 사라지지는 않는다.

### 안전망이 한 일은 어디에 남나

`cb doctor` 가 완전 실패와 정상을 구분하려면 흔적이 있어야 한다. 없으면 둘 다 초록불이다.

| 어디 | 무엇 |
|---|---|
| `state.json` 의 `checkpoints[].at` | 마지막으로 훑은 시각. **성공한 스캔만** 남긴다 (실패가 "방금 훑음" 으로 보이면 증거가 거짓말을 한다) |
| `state.json` 의 `checkpoints[].session_credited` · `day_credited` · `suppressed` | 축별로 소모한 크레딧 · 누적 면제 횟수 |
| `promotions.jsonl` | 승격 **세 갈래 전부** — 기록함 · 기록 안 함(이유) · 실패(에러) |

상태 디렉토리(`$XDG_STATE_HOME/casebook`)에는 이 둘 말고 잠금 파일 두 개가 더 있다 —
`watch.lock`(누가 훑기의 주인인가)과 `state.lock`(상태 파일을 고치는 동안). 잠금 파일은
백업할 필요가 없다.

`promotions.jsonl` 은 덧붙이기 전용이고 아무것도 이걸 정본으로 읽지 않는다. 언제든
지워도 된다. `state.json` 에 안 넣은 이유는 둘이다 — 상태 파일은 매 스캔마다 통째로
다시 쓰이고, 깨져서 지우면 이력까지 사라지는데 그때가 이력이 필요한 순간이다.

`cb doctor` 의 안전망 줄이 이렇게 나온다:

```
✓ 안전망  훅이 턴 경계마다 훑는다 (데몬 없음) · 최근 7일 기록 3건
          · 마지막 훑기 30분 전 · 최근 7일 자동 기록 3건/판정 12건
```

`자동 기록 0건/판정 12건` 은 "판별기가 안 돈다" 가 아니라 **"12번 봤는데 기록할 게
없었다"** 이고, 그 둘은 전혀 다른 진단이다.

### 체크포인트는 언제 전진하나

전진 규칙이 이 데몬의 핵심이다. 옛 셸 구현에서 데이터가 사라지던 자리다.

| 상황 | 전진 | 왜 |
|---|---|---|
| 깨진 줄이 있다 | ❌ | 그 구간이 영원히 검토되지 않는다 |
| 턴 수가 임계 미만이다 | ❌ | 전진하면 임계가 영원히 안 찬다 |
| 임계를 넘겼다 | ✅ | 다 봤다 |

**쓰이는 중인 마지막 줄은 아예 읽지 않는다.** 개행으로 끝난 줄까지만 소비하므로,
반쯤 쓰인 줄은 다음 스캔이 처음부터 다시 읽는다.

### 표시된 구간을 확인하는 법

MCP 로 붙으면 세션 진입 `instructions` 에 실리고, `casebook_pending` 으로 목록과
해소를 다룬다.

```
⚠️ **데몬이 표시한 미확인 구간이 2건 있다.** 이전 세션에서 결정을 내리고도
기록하지 않고 지나간 자리다. 확인해서 실제 결정이면 `casebook_capture` 로 남기고,
아니면 `casebook_pending` 으로 지워라 — 쌓아 두면 다음 세션에도 그대로 뜬다.
  - 2026-08-06 alpha · 발화 12 · 시그널 결정
  - 2026-08-07 beta · 발화 7 · 시그널 채택
```

### 설정

```toml
[capture]
signals = ["결정", "선택", "하기로", "채택", "대신", "전략", "포기", "변경"]
min_turns = 6
quiesce_seconds = 3
```

`signals` 가 비면 **어떤 구간도 표시되지 않는다.** 그 상태로도 데몬은 정상 기동한
것처럼 보이므로, 그때는 기동 시 경고를 낸다.

## Claude Code 훅

`cb init` 이 배선한다. **기본은 계획만 보여 준다** — 이 설정 파일은 casebook 만의 것이
아니라 다른 도구들과 공유하는 자리라, 실수로 한 번 돌려서 남의 훅이 사라지면 안 된다.

```
$ cb init
설정 파일: /home/t/.claude/settings.json
백업:      /home/t/.claude/settings.json.casebook-backup-20260807-235038

걷어낼 훅:
  - SessionStart: /home/t/.claude/hooks/second-brain/session-start.sh
  ...

심을 훅:
  + SessionStart: CASEBOOK_HOOK=1 "/usr/local/bin/cb" hook session-start
  ...

손대지 않는 훅: 11개

계획만 보여 줬다. 실제로 바꾸려면 --apply 를 붙인다.
```

`--apply` 가 수정 전에 백업을 남기고, `cb init --revert` 가 **바이트 그대로** 되돌린다.

| 지키는 것 | 어떻게 |
|---|---|
| 남의 훅을 안 지운다 | `CASEBOOK_HOOK=1` 마커와 `--remove-matching` 에 걸리는 것만 지운다 |
| 모르는 설정 키를 안 잃는다 | 설정 전체를 map 으로 읽고 `hooks` 만 손댄다 |
| 깨진 설정을 안 덮어쓴다 | JSON 이 아니면 손대기 전에 멈춘다 |
| 두 번 돌려도 안전 | 마커로 자기 것을 먼저 걷어내고 다시 심는다 |
| 바로 쓸 수 있는 설정 | 볼트 디렉토리를 만들고, `common` 도메인과 `default_domain` 을 넣는다 |
| 로케일을 본다 | `LANG` 이 `ko` 로 시작하면 한국어 설정, 아니면 영어 설정 |
| 기존 설정 파일을 안 건드린다 | `config.toml` 은 **없을 때만** 만든다 |

### 각 훅이 하는 일

| 이벤트 | 하는 일 |
|---|---|
| `user-prompt-submit` | **관련 과거 결정을 강제 주입한다.** 이 어댑터의 존재 이유다 |
| `session-start` | 도메인 · 최근 결정 · 미확인 구간 · 기록 계약 |
| `stop` · `pre-compact` · `session-end` | 데몬이 안 돌면 대신 훑는다 |
| `pre-compact` · `session-end` | **판별기에 넘겨 자동 기록한다 — 데몬이 돌든 말든** |

### 데몬 없이도 안전망이 돈다

`cb watch` 는 상태 디렉토리에 락을 잡고 산다. 훅이 그 락을 시도해서 **얻으면 데몬이 없는
것**이므로 자기가 훑고 놓는다. 못 얻으면 데몬이 주인이라 건너뛴다.

소유자가 언제나 하나뿐이라 중복 처리가 구조적으로 불가능하고, **데몬 등록에 실패한
사용자도 턴 경계마다 안전망을 얻는다.** 그래서 `cb init` 은 launchd·systemd 에 서비스를
등록하지 않는다 — 되돌리기 어려운 일을 필수도 아닌 것에 하지 않는다.

> **승격은 이 소유권과 무관하다.** 이 락은 *훑기*의 주인만 정한다. 데몬은 판별기를
> 부르지 않고 세션이 끝난 것도 모르므로, 훅은 락을 못 얻어도 `session-end`·`pre-compact`
> 에서 자동 기록을 한다. 그러지 않으면 **`cb watch` 를 켜는 것이 자동 기록을 끄는
> 행위**가 된다 — 실제로 그렇게 만들었다가 리뷰에서 잡혔다.
>
> 그래서 데몬이 도는 동안에도 훅이 상태 파일을 고친다. 상태 파일은 **디스크가 정본**이고
> (쓰기는 `state.lock` 안에서 다시 읽고 고쳐 쓴다), 같은 구간을 둘이 집지 않도록
> pending 마다 **선점 표시**(`claimed_at`, 5분 뒤 자동 해제)를 찍는다.

### 세 가지 규율

1. **무슨 일이 있어도 종료 코드 0.** 훅이 실패해서 대화가 막히면, 사용자는 casebook 을
   고치는 게 아니라 지운다.
2. **stdout 은 에이전트 컨텍스트다.** `user-prompt-submit`·`session-start` 의 stdout 은
   그대로 주입되므로 경고·에러가 한 줄도 섞이지 않는다. 설정 파일이 없을 때조차 그렇다.
3. **실패를 조용히 넘기지 않는다.** stdout 이 비어도 stderr 에는 반드시 남는다.

## `cb doctor` — 조용한 무동작을 보는 자리

이 시스템의 부품은 **전부 실패해도 대화를 막지 않도록** 만들어졌다. 훅은 무슨 일이
있어도 종료 코드 0이고, 회수는 못 찾으면 아무것도 안 내고, 데몬은 백그라운드다.
그 설계의 대가로 **고장이 정상과 구별되지 않는다.** `cb doctor` 가 그걸 구별한다.

```
$ cb doctor
✓ 설정           /home/t/.config/casebook/config.toml
✓ 볼트           /home/t/vault
✓ 도메인 폴더    8개 중 4개는 아직 없다 [...] — 첫 결정을 쓸 때 만들어진다
✓ 미선언 도메인  없다
✓ 결정 노트      58건 전부 읽힌다
✓ 색인           58건과 일치한다
✓ 훅 배선        5개 전부 (남의 훅 11개는 손대지 않음)
✓ 훅 바이너리    /home/t/go/bin/cb
✓ 안전망         훅이 턴 경계마다 훑는다 (데몬 없음) · 미확인 구간 없음

이상 없다.
```

종료 코드로 옮긴다 — **경고 1 · 오류 2.** 자동화가 기계적으로 읽을 수 있다.

특히 보는 것 셋:

- **미선언 도메인** — 볼트에 결정 폴더가 있는데 설정에 없으면 그 프로젝트의 결정이
  색인·회수에서 **통째로** 빠진다. 그런데 색인은 정상 생성되고 회수도 에러를 안 낸다.
- **훅 바이너리** — 훅에는 `cb` 의 절대 경로가 박힌다. 그 파일이 사라지면 훅은
  종료 코드 0으로 아무 일도 안 하면서 정상으로 보인다.
- **미확인 구간 누적** — 7일 넘게 방치된 것이 있으면 따로 센다. 쌓인다는 것은
  `cb capture` 가 안 불리고 있다는 뜻이다.
- **PATH** — `cb` 를 그냥 칠 수 있는가. 이게 안 되면 진단이 내는 모든 `→` 를
  실행할 수 없어서 진단 자체가 무용지물이 된다.

경고마다 `→` 로 고치는 법을 준다. 진단만 하고 무엇을 하라는 말이 없으면
사용자는 그 경고를 무시하는 법을 배운다.

## `cb rollup` — 작업 로그 주간 요약

작업 로그(`99-*`)를 주 단위로 묶어 요약 파일(`98-*`)에 붙인다. **원본은 손대지 않는다.**

**요약문은 casebook 이 만들지 않는다.** 어느 주가 남았는지 찾고, 그 주의 로그를 뽑고,
중복 없이 붙이는 일만 한다. 무엇을 요약이라 부를지는 전체 맥락을 가진 에이전트가 정한다 —
`cb capture` 와 같은 구조다. (셸 시절에는 여기서 LLM 을 불렀는데, 데몬에서 걷어낸 것과
같은 의존이라 같은 선택을 했다.)

```
$ cb rollup
mesh
  2026-W29  → 요약 필요 (30632B)
synth
  2026-W31  → 요약 필요 (46212B)
  2026-W32  진행 중인 주 — 끝나면 요약한다

요약이 필요한 주 2개. 한 주씩:
  1. cb rollup <프로젝트> <주>            로그를 읽는다
  2. 읽고 요약문을 쓴다                   ← 여기는 에이전트가 한다
  3. cb rollup <프로젝트> <주> --body -   붙인다
```

건너뛴 주도 **이유와 함께** 보여 준다 — 목록에서 조용히 빠지면 왜 요약이 안 되는지
알 수 없다. 같은 주를 두 번 붙이지 않는다(덮어쓰면 앞의 요약이 사라진다).

`[naming]` 에 `rollup` 키가 필요하다. 없으면 무엇을 적을지 알려 주고 멈춘다.

```toml
[naming]
rollup = "98-{project}-작업-로그-요약.md"
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
그리고 **색인 행 + 건너뛴 노트 == 디스크의 결정 노트** 가 성립하는지 본다.
이 등식이 지켜지면 노트는 색인에 들어갔거나 빠졌다고 보고됐거나 둘 중 하나이고,
조용히 사라진 것은 하나도 없다. 전후 스냅샷을 대조해 쓰지 않았음도 확인한다.

## 보장 수준

| | Claude Code | MCP 전용 호스트 |
|---|---|---|
| 결정 순간 기록 | 에이전트 `cb capture` | 동일 |
| 놓친 기록 줍기 | 데몬 | 동일 |
| 세션 진입 컨텍스트 | 훅 (보장) | `initialize.instructions` (사실상 동등) |
| 주제 전환 시 회수 | 훅 (강제) | 계약 + 편승 (유도) |

MCP 에는 서버가 대화 중간에 텍스트를 밀어넣는 채널이 없다. 마지막 줄이 유일한 차이고,
**그 차이는 v1 에서 닫히지 않는다** — 프로토콜의 한계이지 구현의 게으름이 아니다.
Claude Code 에서는 `cb hook user-prompt-submit` 이 매 프롬프트마다 관련 결정을 밀어넣고,
그 밖의 호스트에서는 `initialize.instructions` 의 행동 계약과 도구 응답 편승으로 유도한다.

네 칸 모두 이제 실제로 동작한다.

### 기록은 3층이다

**협조 없이도 기록된다.** 에이전트가 `cb capture` 를 부르지 않아도 세션 끝에
판별기가 대신 남긴다.

| 층 | 무엇 | 협조 | 비용 |
|---|---|---|---|
| ① | 에이전트가 결정 시점에 `cb capture` | 필요 | 0 |
| ② | 매 프롬프트에 **발췌를 들이민다** | 유도가 강해짐 | 0 |
| ③ | 세션 끝에 판별기가 대신 기록 | **불필요** | 토큰 |

②가 발췌를 같이 싣는 것이 핵심이다. "미확인 1건" 만 알리면 확인하려고 대화를 다시
읽어야 하는데, 그 비용이 크면 그냥 넘어간다. 눈앞에 있으면 부르는 것이 읽는 것보다 싸다.

③의 판별기는 **호스트 CLI 만** 쓴다 (`~/.local/bin/claude` → PATH 의 `claude`).
API 키를 직접 읽지 않는다 — 그건 진짜 장벽이고, 사용자가 모르는 사이에 과금되는
경로를 만들지 않기 위해서다. **CLI 가 없으면 자동 승격이 꺼지고 ①②만 남는다.**

```toml
[capture]
judge_path  = ""                  # 비면 자동 탐색. 못 찾으면 자동 승격 꺼짐
judge_model = "claude-haiku-4-5"
```

**판별기가 있으면 `[capture] signals` 는 쓰이지 않는다.** 시그널은 "이 구간에 결정이
있을까" 를 낱말로 어림하는 것인데, 실측으로 발화 6개를 넘는 세션의 **98.8%** 를
통과시켜 거의 거르지 못한다. 그러면서 설정에 적힌 낱말이라 **대화 언어와 어긋나면
시스템이 조용히 죽는다** — 한국어 시그널로 영어 대화를 훑으면 아무것도 안 걸리는데
로그에는 정상으로 보인다. 판별기가 있으면 그 앞을 막지 않는다.

판별기가 **없는** 설치에서는 `signals` 가 유일한 필터이므로 대화 언어와 맞아야 한다.
`cb doctor` 가 어느 쪽인지 알려 준다.

`cb doctor` 가 어느 상태인지 알려 준다.

> **`summary` 와 `tags` 는 검색어다.** 회수는 파일명·`summary`·`tags` 만 본다 —
> 본문에만 있는 낱말로는 찾을 수 없다. 실측: 같은 노트가 태그를 주제 분류로 썼을 때
> 관련 질문 3개 중 0개, 회수 어휘로 바꾸니 3개 다 걸렸다. 판별기 지시문이 이걸 알려 준다.

> **판별기는 보수적이다.** 자동 노트는 손으로 쓴 것과 구분되지 않으므로, 애매한 것을
> 기록하면 볼트가 조용히 오염된다. 지시문의 절반이 "애매하면 기록하지 마라" 다.
> 실측: 진행 보고 8턴 → 0건, 진짜 결정 8턴 → 1건.

`cb pending` 으로 표시된 구간을 직접 보고 지울 수 있다.

### 안전망이 실제로 볼 수 있는 것

데몬은 transcript 를 읽어 결정 시그널을 찾는다. **그 기반은 생각보다 얇다.**
이 저장소를 만들면서 실 transcript 1173개(476MB)를 재어 본 결과다.

| | 실측 |
|---|---|
| 전체 transcript | 476 MB |
| 그중 **눈에 보이는 발화** | 15 MB — **3.3%** |
| 나머지 | 도구 호출·도구 결과·빈 thinking 서명 |

**에이전트의 사고(thinking)는 아예 볼 수 없다.** Claude Code 는 thinking 블록에
암호화된 서명만 저장하고 본문은 빈 문자열로 둔다 — 블록 13451개가 **전부** 그랬다.

그래서 한계가 분명하다. **사고 안에서만 내려지고 밖으로 한 줄도 안 나온 결정은
데몬이 볼 수 없다.** 데몬은 주 경로가 아니라 안전망이고, 주 경로는 결정을 내린
에이전트가 그 자리에서 `casebook_capture` 를 부르는 것이다.

## 라이선스

MIT. `LICENSE` 참고.
