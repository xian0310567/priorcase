package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// 이 파일은 **같은 세션에 이미 밀어 넣은 것을 또 밀어 넣지 않게** 한다.
//
// # 왜
//
// 회수는 매 프롬프트마다 상위 3건 + 참고 2건을 주입한다. 개수는 고정이라 볼트가
// 커져도 안 늘지만, **같은 노트가 한 세션에서 몇 번이고 다시 주입된다.** 모델은
// 그것을 이미 컨텍스트에 갖고 있으므로 두 번째부터는 아무것도 더해 주지 않는다.
//
// 실측(2026-08-23, 이 저장소의 실제 세션 28프롬프트): 주입 38,316자 중 **16,126자
// (42%)가 재주입**이었다. 하필 제일 긴 요약들이 제일 자주 반복됐다 — 한 노트가
// 1,540자짜리 요약으로 네 번 실렸다.
//
// # 자리는 남긴다
//
// 통째로 빼지 않는다. "지금 이게 관련 있다" 는 신호는 매번 필요하고, 경로가 있어야
// 에이전트가 열어 본다. 요약 본문만 뺀다 — 1,540자가 100자가 된다.
//
// # 압축하면 되돌린다
//
// 압축으로 앞부분이 날아가면 요약은 컨텍스트에 없는데 포인터만 남는다. 그러면
// 이 최적화가 **조용한 품질 저하**가 된다. 그래서 pre-compact 와 세션 clear 에서
// 표시를 지운다 (run.go).

// seenTTL 은 세션 기록을 남겨 두는 기간이다. 지나면 청소한다 —
// 이 파일이 머신 수명만큼 자라면 안 된다.
const seenTTL = 24 * time.Hour

const seenName = "recalled.json"

type seenFile struct {
	Sessions map[string][]string `json:"sessions"`
	At       map[string]int64    `json:"at"` // 세션 → 마지막 갱신(Unix). 청소용
}

// sessionKey 는 이 세션을 가리키는 키다.
//
// transcript 경로가 정본이다 — **두 호스트가 다 준다.** session_id 는 Claude Code
// 만 주고 Codex 는 turn_id·thread_id 를 준다(host.go 참고). 둘 다 없으면 빈
// 문자열이고, 그러면 이 최적화는 **꺼진다** — 세션을 못 가르는데 억누르면 다른
// 대화의 주입까지 삼킨다. 조용히 덜 주는 것보다 그냥 다 주는 편이 낫다.
func (o Options) sessionKey() string {
	if o.Input.TranscriptPath != "" {
		return o.Input.TranscriptPath
	}
	return o.Input.SessionID
}

func seenPath(stateDir string) string { return filepath.Join(stateDir, seenName) }

func readSeen(stateDir string) seenFile {
	f := seenFile{Sessions: map[string][]string{}, At: map[string]int64{}}
	if stateDir == "" {
		return f
	}
	b, err := os.ReadFile(seenPath(stateDir))
	if err != nil {
		return f
	}
	var got seenFile
	if err := json.Unmarshal(b, &got); err != nil {
		return f // 깨졌으면 처음부터 — 최적화일 뿐이라 실패로 만들지 않는다
	}
	if got.Sessions == nil {
		got.Sessions = map[string][]string{}
	}
	if got.At == nil {
		got.At = map[string]int64{}
	}
	return got
}

// writeSeen 은 원자적으로 쓰고 오래된 세션을 청소한다.
//
// **락을 잡지 않는다.** 두 세션이 동시에 쓰면 뒤엣것이 이긴다 — 그때 잃는 것은
// "이미 봤다" 표시 하나이고, 결과는 노트가 한 번 더 주입되는 것뿐이다. 정확성이
// 아니라 절약의 문제라 락으로 매 프롬프트를 느리게 할 이유가 없다.
func writeSeen(stateDir string, f seenFile, now time.Time) {
	if stateDir == "" {
		return
	}
	for k, ts := range f.At {
		if now.Sub(time.Unix(ts, 0)) > seenTTL {
			delete(f.Sessions, k)
			delete(f.At, k)
		}
	}
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	_ = store.WriteFileAtomic(seenPath(stateDir), b, 0o600)
}

// markSeen 은 이미 주입한 노트에 표시를 달고, 이번 것을 기록한다.
//
// **표시만 단다. 빼지 않는다.** 무엇을 어떻게 그릴지는 방출기 한 곳(RenderInject)이
// 정한다 — 여기서 걸러 내면 그리는 규칙이 두 군데로 갈라진다.
func (o Options) markSeen(hits []search.Hit) []search.Hit {
	key := o.sessionKey()
	if key == "" || o.StateDir == "" {
		return hits
	}
	f := readSeen(o.StateDir)
	was := map[string]bool{}
	for _, s := range f.Sessions[key] {
		was[s] = true
	}
	for i := range hits {
		stem := hits[i].Note.Stem
		if was[stem] {
			hits[i].Seen = true
			continue
		}
		was[stem] = true
		f.Sessions[key] = append(f.Sessions[key], stem)
	}
	f.At[key] = time.Now().Unix()
	writeSeen(o.StateDir, f, time.Now())
	return hits
}

// resetSeen 은 이 세션의 표시를 지운다. 컨텍스트가 비워졌을 때 부른다.
func (o Options) resetSeen() {
	key := o.sessionKey()
	if key == "" || o.StateDir == "" {
		return
	}
	f := readSeen(o.StateDir)
	delete(f.Sessions, key)
	delete(f.At, key)
	writeSeen(o.StateDir, f, time.Now())
}
