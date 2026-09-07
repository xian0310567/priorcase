package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/transcript"
)

// ★★★ 이 파일이 잠그는 것은 **주제가 프롬프트가 아니라 세션에 있을 때**다.
//
// 대화체는 대명사·생략·직전 화제 참조로 굴러가서, 사람이 방금 친 한 줄에는
// 내용어가 없다. 실볼트 실측(2026-09-04, 실물 라벨 165건 · 서로 다른 질의 60개)으로
// 상위3에 정답이 든 질의가 **15건에서 33건**이 됐다 (search.Options.Context 의 표).

// writeConversation 은 Claude Code 모양의 JSONL 을 쓴다. 값은 합성이지만 형태는
// 실물 그대로다 — 파서를 그대로 태워야 재는 것이 실제 경로다.
//
// scan_test.go 의 writeTranscript 와 다른 것이다. 그쪽은 "결정 시그널이 든 구간"
// 을 만드는 것이고 여기는 **대화 내용**이 필요하다.
func writeConversation(t *testing.T, turns ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "session.jsonl")
	var b strings.Builder
	for i, text := range turns {
		kind := "user"
		if i%2 == 1 {
			kind = "assistant"
		}
		rec := map[string]any{
			"type": kind, "isSidechain": false, "cwd": "/tmp/proj/alpha",
			"sessionId": "S1",
			"timestamp": fmt.Sprintf("2026-09-04T01:00:%02d.000Z", i),
			"message":   map[string]any{"role": kind, "content": text},
		}
		j, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(j)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// ★ 주제어가 직전 대화에만 있어도 회수가 그 노트를 꺼낸다.
func TestRecallUsesRecentConversation(t *testing.T) {
	c := cfg(t)
	// 프롬프트에는 내용어가 없다. `저장 엔진` 은 앞선 대화에만 있다.
	tr := writeConversation(t,
		"저장 엔진을 무엇으로 할지 정해야 한다",
		"임베디드 DB 로 하는 것이 좋겠다",
	)
	const vague = "그거 결국 어떻게 하기로 했었지"

	got := runHook(t, c, pendingStore(t), EventUserPromptSubmit, Input{
		Cwd: "/tmp/proj/alpha", Prompt: vague, TranscriptPath: tr,
	}).out
	if !strings.Contains(got, "저장엔진") {
		t.Errorf("맥락에 주제어가 있는데 그 결정이 안 실렸다:\n%s", got)
	}
}

// 대조 — 같은 프롬프트에 맥락이 없으면 못 찾는다. 이게 고쳐진 고장이다.
func TestSamePromptFindsNothingWithoutTranscript(t *testing.T) {
	c := cfg(t)
	got := runHook(t, c, pendingStore(t), EventUserPromptSubmit, Input{
		Cwd: "/tmp/proj/alpha", Prompt: "그거 결국 어떻게 하기로 했었지",
	}).out
	if strings.Contains(got, "저장엔진") {
		t.Errorf("맥락이 없는데 찾았다 — 이 대조가 성립해야 위 시험이 뜻을 가진다:\n%s", got)
	}
}

// ★★ **기록 파일이 깨져 있거나 없어도 회수는 산다.**
//
// 맥락은 회수를 좋게 하려는 것이지 회수의 전제가 아니다. 훅은 무슨 일이 있어도
// exit 0 이고(hook.go 의 규율 1), 여기서 죽으면 지금보다 나빠진다.
func TestBrokenTranscriptDoesNotKillRecall(t *testing.T) {
	c := cfg(t)
	broken := filepath.Join(t.TempDir(), "broken.jsonl")
	if err := os.WriteFile(broken, []byte("{이건 JSON 이 아니다\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, tr := range map[string]string{
		"깨진 파일": broken,
		"없는 파일": filepath.Join(t.TempDir(), "없다.jsonl"),
		"빈 경로":  "",
		"디렉토리":  t.TempDir(),
	} {
		got := runHook(t, c, pendingStore(t), EventUserPromptSubmit, Input{
			Cwd: "/tmp/proj/alpha", Prompt: probe, TranscriptPath: tr,
		})
		if got.e != nil {
			t.Errorf("%s: 훅이 실패했다: %v", name, got.e)
		}
		if !strings.Contains(got.out, "[과거 결정 참조]") {
			t.Errorf("%s: 맥락을 못 읽었다고 회수까지 죽었다:\n%s", name, got.out)
		}
	}
}

// 맥락은 **뒤에서부터** 채운다 — 직전 화제가 주제다. 그리고 담은 뒤에는 대화
// 순서로 되돌린다: 점수는 낱말 집합만 보므로 순서가 안 중요하지만,
// `prior recall --context` 로 같은 맥락을 손에 놓고 볼 때 거꾸로면 사람이 못 읽는다.
func TestJoinTailKeepsConversationOrder(t *testing.T) {
	turns := []transcript.Turn{
		{Kind: transcript.KindUser, Text: "첫째"},
		{Kind: transcript.KindAssistant, Text: "둘째"},
		{Kind: transcript.KindUser, Text: "셋째"},
	}
	if got := joinTail(turns, 1000); got != "첫째\n둘째\n셋째" {
		t.Errorf("대화 순서가 아니다: %q", got)
	}
	// 상한에 걸리면 **뒤쪽**이 남는다.
	if got := joinTail(turns, 4); got != "둘째\n셋째" {
		t.Errorf("상한에서 앞쪽이 남았다: %q — 직전 화제가 주제다", got)
	}
}
