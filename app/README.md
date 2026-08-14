# priorcase 데스크탑 앱

**설정 콘솔이다.** 어느 도구의 대화를 훑을지, 프로젝트를 어느 볼트에 엮을지,
볼트를 어디에 만들지를 여기서 정한다. macOS 메뉴바에 상주한다.

설계: `docs/superpowers/specs/2026-08-12-데스크탑-감독앱-design.md`
계획: `docs/superpowers/plans/2026-08-13-데스크탑-감독앱.md`

## 왜 감독 큐가 아닌가

처음 판(2026-08-13)은 감독 표면이었다 — 판별기가 표시한 구간을 사람이 보고
[기록한다]/[아니다] 를 누르는 화면 셋. 그것을 실제로 띄우자마자 뒤집혔다.

> 이 서비스의 근간은 자동으로 기록해주는 거야. 이렇게 직접 피드백하고 기록할지
> 말지 선택하면 의미가 없는 거 아니야?

맞다. 그리고 승격 원장이 그것을 뒷받침했다 — 136건 중 **기록 3건**, 사흘째 0건.
확인 큐 30건은 기능이 아니라 **자동 층이 못 따라잡은 잔해**였고, 그것을 사람 앞에
놓는 순간 자동 기록이라는 전제를 사람이 대신 갚는다.

그래서 큐 셋을 들어내고, 지금 아무 데서도 못 하던 일을 앱이 맡는다.
밀린 구간은 데몬이 소화하고(`internal/daemon/backlog.go`), 결과를 묻는 일은
대화 도중에 훅이 한다(`internal/core/retro/ask.go`).

상세: 볼트 `priorcase-결정-앱을-감독큐에서-설정콘솔로-2026-08-14`

## 화면 셋

| 탭 | 무엇 | 무엇을 누르면 |
| --- | --- | --- |
| 호스트 | Claude Code · Codex CLI 를 훑을지 | `prior hosts enable/disable` |
| 볼트 | 볼트 목록·만들기·열기, 프로젝트 ↔ 볼트 | `prior vault add` · `prior domain bind` |
| 상태 | `prior doctor` 와 같은 검사 + 밀린 일감 한 줄 | (보기만) |

**밀린 일감은 진단이지 할 일 목록이 아니다.** 누를 것이 없다 — 데몬이 5분마다
소화한다. 그래도 적는 이유는, 처리량이 쌓이는 속도를 못 따라가면 사람이 그 사실을
알 자리가 아무 데도 없기 때문이다.

**메뉴바는 고장났을 때만 말한다.** 상태 검사에 fail 이 있으면 `⚠`, 아니면 아무
글자도 없다. 늘 무언가 떠 있으면 사람이 그것을 무시하는 법을 배운다.

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

픽스처는 **명령마다 다르게 답한다** (`$1` 이 `settings` 인지 본다). 앱이 `queue` 와
`settings` 를 함께 읽으므로 한 가지만 내면 설정 화면이 빈 객체를 받아 죽는다.

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

`tests/realdata.test.ts` 는 **진짜 기계의 설정과 큐**를 화면 함수에 통과시킨다.
`~/.local/bin/prior` 가 없으면 건너뛰지만, **있는데 낡았으면 실패한다** — 낡은 것을
없는 것으로 뭉개면 그 시험이 영영 건너뛰기만 하면서 통과한 척한다.

`tests/wiring.test.ts` 는 진짜 `#app` 에 앱을 띄우고 진짜 위젯을 조작해 **어떤
IPC 가 나가는지**를 본다. 화면 함수가 옳아도 조립이 틀리면 여기서만 잡힌다.
설정 화면에서 그 사고는 특히 조용하다 — 껐는데 계속 훑히거나, 볼트를 엮었는데
기록이 옛 볼트로 계속 간다.

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

**앱은 `prior` 명령만 부른다.** 볼트 파일도 설정 파일도 직접 쓰지 않는다 —
주석 보존·스칼라 볼트 변환·고친 뒤 검증이 두 벌이 되면 한쪽만 고쳐진 채로 남고,
그때 망가지는 것은 사람이 손으로 쓴 설정이다. 볼트를 열 때도 경로를 조립하지
않고 이름으로 `prior settings` 에 묻는다.

**먼저 말을 걸지 않는다.** 푸시 알림이 없고, 메뉴바 글자만 조용히 바뀐다.
먼저 말을 거는 도구는 지워지고, 앱이 지워지면 자동 기록까지 같이 잃는다.

**사람에게 승인을 구하지 않는다.** 기계가 판정할 수 있는 것을 사람에게 물으면
그것은 기능이 아니라 자동화 실패를 사람이 갚는 일이다.
