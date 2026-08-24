package daemon

import (
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/judge"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
)

// **대화를 만든 호스트의 CLI 가 앞에 서야 한다.** Codex 에서 일한 대화를 판정하려고
// Claude 쿼터를 쓰는 것이 사용자가 물은 그 고장이다.
//
// 실제 CLI 가 깔려 있는지에 의존하면 머신마다 결과가 달라지므로, 여기서는
// **선호 종류를 고르는 판단**만 본다 (judgeFor 가 그 판단으로 FindFor 를 부른다).
func TestPreferredFlavorFollowsTranscriptHost(t *testing.T) {
	rs := []hosts.Resolved{
		{Host: hosts.Host{ID: "claude-code"}, Root: "/home/u/.claude/projects"},
		{Host: hosts.Host{ID: "codex"}, Root: "/home/u/.codex/sessions"},
	}
	for path, want := range map[string]judge.Flavor{
		"/home/u/.codex/sessions/2026/08/24/rollout-x.jsonl": judge.FlavorCodex,
		"/home/u/.claude/projects/p/abc.jsonl":               judge.FlavorClaude,
		"/tmp/어디에도-안-속한다.jsonl":                              judge.FlavorClaude, // 모르면 원래 동작
	} {
		if got := preferredFlavor(path, rs); got != want {
			t.Errorf("preferredFlavor(%q) = %q, want %q", path, got, want)
		}
	}
}

// 호스트 목록이 비면(설정이 그 호스트를 껐거나 루트를 못 찾았을 때) claude 를
// 앞에 둔다 — 원래 동작이고, 사슬이라 codex 도 뒤에 붙으므로 잃는 것이 없다.
func TestPreferredFlavorWithoutHostsIsClaude(t *testing.T) {
	if got := preferredFlavor("/home/u/.codex/sessions/x.jsonl", nil); got != judge.FlavorClaude {
		t.Errorf("호스트 목록이 없을 때 = %q, want claude", got)
	}
}

// judgeFor 는 설정을 그대로 통과시킨다 — 판별기가 없는 머신에서 nil 이 나오는 것이
// 정상이고(고장이 아니라 설정), 그때 호출부가 표시만 남긴다.
func TestJudgeForTolerantOfMissingConfig(t *testing.T) {
	_ = judgeFor(&config.Config{}, "/tmp/x.jsonl", nil) // 죽지 않으면 통과
}
