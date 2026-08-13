# priorcase 데스크탑 감독 앱

기계가 한 일(자동 기록)을 사람이 클릭 한 번으로 승인·정정·판정하는 macOS
메뉴바 상주 앱.

설계: `docs/superpowers/specs/2026-08-12-데스크탑-감독앱-design.md`
계획: `docs/superpowers/plans/2026-08-13-데스크탑-감독앱.md`

## 화면 넷

| 탭 | 무엇 | 무엇을 누르면 |
| --- | --- | --- |
| 확인 | 판별기가 표시한 미기록 구간 | `prior promote` / `prior pending --resolve` |
| 검토 | 판별기가 **스스로 만든** 노트 | `prior reviewed` (검증 표시) / `prior path` 로 열기 |
| 회고 | 결과를 물어볼 때가 된 결정 | `prior review --outcome` / 열기 / 아직 |
| 상태 | `prior doctor` 와 같은 검사 | (보기만) |

**검토의 [맞다] 는 `outcome` 을 안 건드린다.** outcome 은 "그 결정이 결과적으로
좋았나" 이고 회고 큐가 그 값이 정해진 노트를 영영 제외한다. 검토는 "판별기가
사실대로 썼나" 라는 다른 질문이라 승격 원장에 따로 남긴다.

**발췌가 없는 검토 줄은 [맞다] 가 막혀 있다.** 대조할 것이 없는데 "사실대로
썼다" 고 표시하는 것은 검증이 아니라 서명이다.

**회고의 [아직] 은 앱이 떠 있는 동안만 기억한다.** 파일에 남기면 "왜 안 뜨지"
를 설명할 규칙이 둘이 되고, 되살릴 방법이 없어진다.

## 개발

```bash
pnpm install
pnpm tauri dev
```

가짜 `prior` 로 오류 화면을 보려면:

```bash
PRIORCASE_APP_BIN="$PWD/fixtures/fake-prior-exit1.sh"      pnpm tauri dev  # stderr 표시
PRIORCASE_APP_BIN="$PWD/fixtures/fake-prior-broken-json.sh" pnpm tauri dev  # 깨진 출력
PRIORCASE_APP_BIN="$PWD/fixtures/fake-prior-warnings.sh"    pnpm tauri dev  # 불완전한 큐
PRIORCASE_APP_BIN="$PWD/fixtures/fake-prior-onebroken.sh"   pnpm tauri dev  # 볼트 하나만 고장
PRIORCASE_APP_BIN=/없는/자리/prior                            pnpm tauri dev  # 설치 안내
```

개발 빌드에는 창을 토글하는 파일 방아쇠가 있다 (트레이 클릭은 진짜 마우스
이벤트라 스크립트로 만들 수 없다):

```bash
touch /tmp/priorcase-toggle
```

## 테스트

```bash
pnpm test                                          # 프런트 (jsdom)
cargo test --manifest-path src-tauri/Cargo.toml    # Rust
```

`tests/realdata.test.ts` 는 **진짜 볼트의 큐**를 화면 함수에 통과시킨다.
`~/.local/bin/prior` 가 없으면 건너뛴다. 합성 픽스처는 내가 상상한 모양만
담는데, 실제 데이터는 다르다 — 발췌 29~113줄, 발췌 없는 검토 줄, 회고 50건.

`tests/wiring.test.ts` 는 진짜 `#app` 에 앱을 띄우고 진짜 버튼을 눌러 **어떤
IPC 가 나가는지**를 본다. 화면 함수가 옳아도 조립이 틀리면 여기서만 잡힌다.

## 빌드

```bash
pnpm tauri build --bundles app
xattr -dr com.apple.quarantine src-tauri/target/release/bundle/macos/priorcase.app
open src-tauri/target/release/bundle/macos/priorcase.app
```

**공증하지 않는다.** v1 은 내가 쓰는 것이고, 한두 명에게 줄 때 다시 본다.
DMG 는 만들지 않는다 — 번들링이 비대화형 세션에서 멈춘다.

메뉴바 아이콘 오른쪽 클릭 → **종료**.

## 규칙

**앱은 `prior` 명령만 부른다.** 볼트 파일을 직접 읽거나 쓰지 않는다 —
frontmatter 방출·스키마 검증·색인 갱신·볼트 선택이 두 벌이 되면 한쪽만
고쳐진 채로 남는다. 노트를 열 때도 경로를 조립하지 않고 `prior path <stem>`
에게 묻는다.

**먼저 말을 걸지 않는다.** 푸시 알림이 없고, 메뉴바 숫자만 조용히 바뀐다.
먼저 말을 거는 도구는 지워지고, 앱이 지워지면 자동 기록까지 같이 잃는다.

**할 일이 없으면 메뉴바에 글자가 없다.** 늘 무언가 떠 있으면 사람이 그것을
무시하는 법을 배우고, 그러면 진짜 할 일이 있을 때도 안 보인다.
