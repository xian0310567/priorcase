# Plan 3 — transcript 파서 + 데몬 구현 계획

> **에이전트 작업자에게:** 태스크 단위로 구현한다. 각 단계는 체크박스(`- [ ]`)다.

**목표:** 에이전트가 `prior capture` 를 부르지 않고 지나간 구간을 데몬이 **놓치지 않고 표시**해,
다음 세션의 에이전트가 읽고 판단하게 한다.

**아키텍처:** `internal/transcript`(호스트별 파서, 읽기 전용) + `internal/daemon`
(체크포인트·필터 체인·pending). **데몬은 LLM 을 부르지 않는다** — 판별은 다음 세션의
에이전트가 한다.

**스택:** `github.com/fsnotify/fsnotify` v1.10.1 · `github.com/gofrs/flock` v0.12.1

---

## Global Constraints

- **데몬은 LLM 을 부르지 않는다.** 플래그만 남긴다 (스펙 §7.2, 기록회수모델 결정).
- **체크포인트 규칙은 하나뿐이다: 구간을 끝까지 성공 처리했을 때만 전진한다.**
  파싱 실패한 줄은 건너뛰되 전진하지 않는다 (스펙 §7.2).
- **transcript 는 읽기만 한다.** 절대 쓰지 않는다.
- 상태 파일(체크포인트·pending·락)은 **볼트 밖** `$XDG_STATE_HOME/priorcase/` (스펙 §5).
  볼트에는 문서만 둔다.
- 상태 파일 쓰기는 `store.WriteFileAtomic`. 데몬은 SIGKILL 로 죽을 수 있고,
  잘린 체크포인트는 구간 전체를 잃는 것과 같다.
- **어댑터 경계**(§4.1)를 지킨다. `internal/arch` 가 강제한다 — 새 패키지도 그 검사에 넣는다.
- 침묵 금지: 건너뛴 줄·전진 못 한 체크포인트를 **반환값으로** 알린다.

## 닫아야 할 감사 결함

| # | 결함 | 이 계획에서 닫는 방법 | 테스트 |
|---|---|---|---|
| 1 | 부분 기록된 한 줄이 구간 전체를 삼킨다 (빈 발췌 → "내용 없음" 오판 → 체크포인트 전진) | 줄 단위 파싱 + **완결된 줄(개행으로 끝난)만** 대상 + 실패 시 미전진 | T1·T2 |
| 2 | `대상 없음` 실패만 체크포인트가 전진한다 | 단일 규칙으로 통합 — 성공 처리 시에만 전진 | T2 |
| 3 | 서기 동시 실행 락이 없다 | `prior watch` 단일 인스턴스 (flock) | T4 |
| 6 | 턴 수 임계가 무력 (tool_result 까지 셈) | Turn 정의에서 `tool_use`·`tool_result`·`thinking`·`isMeta` 제외 | T1 |

**실측 근거** (이 저장소 개발 중 실제 transcript 8607줄): 레코드 5920개 중
`tool_use` 1981 · `tool_result` 1981 · `thinking` 932. 실제 발화는 user 77 + assistant 914 =
**991개**. 결함 6 은 이론이 아니라 6배 부풀림이다.

## 파일 구조

| 파일 | 책임 |
| --- | --- |
| `internal/transcript/transcript.go` | `Turn` 타입과 `Parser` 인터페이스 — 호스트 중립 |
| `internal/transcript/claudecode/parse.go` | JSONL → `[]Turn`. 완결 줄만, 실패 줄 보고 |
| `internal/transcript/claudecode/discover.go` | `~/.claude/projects/**/*.jsonl` 탐색 |
| `internal/daemon/state.go` | 체크포인트·pending 저장소 (`$XDG_STATE_HOME/priorcase/`) |
| `internal/daemon/filter.go` | 필터 체인 — 턴 수 임계 · 키워드 시그널 |
| `internal/daemon/daemon.go` | 락 · fsnotify · quiesce · 스캔 루프 |
| `internal/adapter/cli/watch.go` | `prior watch` |

## 이 계획에서 하지 않는 것

- **다른 호스트 파서** — v2 (스펙 §10). 인터페이스만 열어 둔다.
- **데몬 수명주기 등록**(launchd/systemd) — `prior init` 의 몫이고 그건 Plan 4 다.
  지금은 사람이 `prior watch` 를 띄운다.
- **pending 을 결정으로 승격** — 에이전트가 `priorcase_capture` 로 한다. 데몬은 표시만.

---

### Task 1: transcript 파서

**Files:** `internal/transcript/transcript.go` · `internal/transcript/claudecode/parse.go`
· 테스트 각각

**Interfaces:**
- Produces: `transcript.Turn{Role, Text, Timestamp, SessionID, Cwd, IsSidechain}`
- Produces: `claudecode.Parse(r io.Reader) (turns []transcript.Turn, consumed int64, bad int, err error)`
  — `consumed` 는 **완결된 줄까지의 바이트 수**. 호출자는 이 값으로만 체크포인트를 옮긴다.

- [ ] **Step 1: 결함 6 실패 테스트** — `tool_use`·`tool_result`·`thinking`·`isMeta`
      레코드가 섞인 JSONL 에서 Turn 수가 실제 발화 수와 같은지.
- [ ] **Step 2: 결함 1 실패 테스트** — 마지막 줄이 개행 없이 잘린 JSONL 에서
      (a) 앞 구간이 파싱되고 (b) `consumed` 가 **잘린 줄 앞까지만** 가리키는지.
- [ ] **Step 3: 중간 줄 깨짐 테스트** — 완결됐지만 JSON 이 깨진 줄은 `bad` 로 세고
      건너뛴다. `consumed` 는 전진하지 않는다.
- [ ] **Step 4: 구현**
- [ ] **Step 5: 실 transcript 대조 테스트** (`PRIORCASE_TEST_TRANSCRIPT` 게이트)

> **T1 실측 결과 — 안전망의 한계 하나가 확정됐다.** Claude Code 의 thinking 블록은
> 암호화된 `signature` 만 담고 본문은 비어 있다 (파일 1173개 · 블록 13451개 **전부**).
> 그래서 시그널 검색이 실제로 보는 것은 **밖으로 나온 말뿐**이다 — 사고 안에서만 내려지고
> 한 줄도 밖에 안 나온 결정은 데몬이 볼 수 없다. 문서에 적는다 (T6).
- [ ] **Step 6: 커밋**

### Task 2: 체크포인트 저장소

**Files:** `internal/daemon/state.go` · `state_test.go`

**Interfaces:**
- Produces: `daemon.NewStore(dir string) *Store`
- Produces: `(*Store) Checkpoint(path string) int64` · `(*Store) Advance(path string, off int64) error`
- Produces: `(*Store) Load() error` · 원자적 저장

- [ ] **Step 1: 결함 1·2 실패 테스트** — 파싱 실패가 있으면 `Advance` 를 부르지 않는
      호출 규약을 테스트로 고정한다 (스캔 함수 단위, T3 에서 통합).
- [ ] **Step 2: 파일 교체 감지 테스트** — 체크포인트보다 파일이 작아지면 0 으로 재설정.
- [ ] **Step 3: 구현.** `WriteFileAtomic` 사용.
- [ ] **Step 4: 커밋**

### Task 3: 필터 체인 + pending

**Files:** `internal/daemon/filter.go` · `internal/daemon/scan.go` · 테스트

**Interfaces:**
- Produces: `daemon.Scan(...) (Result, error)` — 한 파일 한 구간 처리
- Produces: `daemon.Pending{SessionID, Path, Cwd, Domain, Turns, Signals, From, To, At}`

- [ ] **Step 1: 시그널 필터 테스트** — 설정의 `signals` 중 하나라도 있으면 통과.
- [ ] **Step 2: 턴 수 임계 테스트** — `min_turns` 미만이면 pending 없음. **단 체크포인트는
      전진한다** (내용을 다 봤고 결정이 없다고 판단한 것이므로).
- [ ] **Step 3: 미전진 시 중복 pending 금지 테스트** — 깨진 줄 때문에 전진 못 한 구간을
      두 번 스캔해도 pending 은 하나여야 한다. (전진 못 함 + 매번 새 레코드 = 무한 증식)
- [ ] **Step 4: 구현**
- [ ] **Step 5: 커밋**

### Task 4: `prior watch` — 락 · fsnotify · quiesce

**Files:** `internal/daemon/daemon.go` · `internal/adapter/cli/watch.go` · 테스트

- [ ] **Step 1: 결함 3 실패 테스트** — 같은 상태 디렉토리로 `Run` 을 두 번 부르면
      두 번째가 즉시 종료한다.
- [ ] **Step 2: quiesce 테스트** — 쓰기가 멎고 `quiesce_seconds` 가 지나야 스캔한다.
- [ ] **Step 3: 구현.** flock · fsnotify · 디바운스.
- [ ] **Step 4: `prior watch` 서브커맨드** (조립은 `cmd/prior`)
- [ ] **Step 5: 커밋**

### Task 5: pending 을 MCP instructions 에 노출

**Files:** `internal/adapter/mcp/instructions.go` · 테스트

Plan 2 에서 "소스가 없는 값을 0 으로 박으면 거짓말"이라 비워 둔 자리를 채운다.

- [ ] **Step 1: 실패 테스트** — pending 이 N건이면 instructions 에 그 사실과
      "확인하고 `priorcase_capture` 하라"가 들어간다. 0건이면 그 문단이 없다.
- [ ] **Step 2: 구현.** mcp 는 daemon 의 읽기 API 만 쓴다.
- [ ] **Step 3: 커밋**

### Task 6: 문서

- [ ] README — `prior watch` 절, 보장 표의 "놓친 기록 줍기" 갱신, 상태 파일 위치
- [ ] 스펙 — §7.2 구현 결과와 어긋난 곳, §11 테스트 표의 결함 대응 표시
- [ ] 커밋

---

## 구현 후 — 계획과 달라진 것

| # | 계획 | 실제 | 왜 |
|---|---|---|---|
| 1 | 전진 규칙 2갈래 | **3갈래** | 임계 미달일 때 전진하면 임계가 영원히 안 찬다 |
| 2 | 시그널 검색이 thinking 도 본다 | **못 본다** | Claude Code 가 thinking 본문을 저장하지 않는다 (13451블록 전부 빈 문자열) |
| 3 | (없음) | **중복 방지 추가** | readme 4-B 조항을 빠뜨렸었다. 없으면 실질 세션의 99%가 표시된다 |
| 4 | (없음) | **기동 시 밀린 구간 확인** | 데몬이 꺼진 사이 끝난 세션은 fsnotify 이벤트가 다시 오지 않아 영원히 안 보인다 |
| 5 | (없음) | **무시그널 경고** | [capture] 없는 설정이면 조용히 무동작한다 |
| 6 | T5 는 instructions 노출만 | **priorcase_pending 도구까지** | 건수만 보여 주면 확인·해소할 수단이 없어 pending 이 영원히 쌓인다 |

**실측 근거** (실 transcript 1173개 · 476MB):
- 눈에 보이는 발화는 15MB — **3.3%**. 나머지는 도구 호출·결과·빈 thinking 서명
- 발화 6개 초과 세션 585개 (50%)
- 그중 시그널에 걸리는 것 578개 — **99%**
- 깨진 줄이 있는 파일 **0개** (파서가 실물을 정확히 읽는다)
