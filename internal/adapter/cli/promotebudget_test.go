package cli

import (
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/judge"
)

// ★★ **예산이 판별기 상한보다 작으면 `prior promote` 가 통째로 멎는다.**
//
// 승격은 "판별기가 다 돌 시간이 남았을 때만 시작" 한다. 예산이 상한보다 작으면 그
// 조건이 영원히 거짓이라 **판별기를 한 번도 안 부른다** — 명령은 조용히 실패하고,
// 사람은 [결정이다] 를 눌렀는데 아무 일도 안 났다고 본다.
//
// 그리고 이 관계는 **컴파일로 안 지켜진다.** 실제로 판별기 상한을 45초에서 75초로
// 올릴 때 여기 60초가 그대로 남아 하마터면 그 상태가 될 뻔했다.
func TestPromoteBudgetFitsJudge(t *testing.T) {
	if promoteBudget <= judge.DefaultTimeout {
		t.Fatalf("예산(%v) ≤ 판별기 상한(%v) — 판별기를 한 번도 못 부른다",
			promoteBudget, judge.DefaultTimeout)
	}
	// 판별기가 끝난 뒤 원장을 쓰고 표시를 해소할 여유.
	if promoteBudget-judge.DefaultTimeout < 10*time.Second {
		t.Errorf("예산(%v)에서 판별기(%v)를 빼면 마무리할 여유가 없다",
			promoteBudget, judge.DefaultTimeout)
	}
}
