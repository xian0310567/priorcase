# `~/project/casebook` → `~/project/priorcase` 개명 절차

> 2026-08-14 작성. **세션을 닫은 뒤** 실행한다 — 살아 있는 세션의 cwd 를 밑에서
> 바꾸면 도구가 상대 경로를 엉뚱한 데로 풀고, 그건 조용하게 틀린다.

## 왜 지금 안전한가 (실측 2026-08-14)

| 걱정한 것 | 실측 |
| --- | --- |
| 도메인 해석이 깨진다 | 설정 `paths` 에 두 경로가 다 있다 — 옮겨도 안 깨진다 |
| 훅이 경로를 박고 있다 | `~/.claude/settings.json` 의 훅 명령에 `casebook` 없음 |
| 체크포인트 418건이 깨진다 | 그건 **트랜스크립트** 경로(`~/.claude/projects/…`)다. 프로젝트 폴더와 무관하다 |
| 새 자리가 이미 차 있다 | `~/project/priorcase` 없음 |

Claude Code 의 트랜스크립트 디렉토리는 cwd 로 이름이 정해진다. 개명 뒤 새
세션은 `-Users-zesty-project-priorcase/` 에 쌓이고, 옛 것은 그대로 읽힌다 —
과거 체크포인트와 미확인 구간 3건은 옛 자리를 계속 가리키므로 안전하다.

## 절차

```sh
# 1. 옮긴다
mv ~/project/casebook ~/project/priorcase

# 2. 설정에서 옛 경로를 뺀다
#    ~/.config/priorcase/config.toml 의 [[domain]] prefix = "priorcase"
#    paths = ["~/project/priorcase", "~/project/casebook"]
#         →  paths = ["~/project/priorcase"]

# 3. 확인
cd ~/project/priorcase
prior doctor              # 도메인·볼트·훅 배선이 전부 ✓ 인지
prior recall "저장 엔진"   # 회수가 priorcase 결정을 주는지
go test ./... -count=1    # 21/21
```

## 확인할 것

- `prior doctor` 의 **도메인** 줄이 9개를 그대로 세는가
- 새 세션에서 훅 주입(`[과거 결정 참조]`)이 뜨는가 — cwd 가 바뀌었으므로
  도메인이 `priorcase` 로 잡혀야 한다
- 미확인 구간 수가 그대로인가 (옛 트랜스크립트를 계속 읽는다)

## 되돌리기

```sh
mv ~/project/priorcase ~/project/casebook
# 설정의 paths 에 옛 경로를 다시 넣는다
```

git 저장소는 통째로 움직이므로 이력은 안 다친다.
