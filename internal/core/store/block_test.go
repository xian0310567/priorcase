package store

import (
	"strings"
	"testing"
)

// ★★★ **사람이 쓴 글은 한 글자도 안 건드린다.**
//
// 이 함수가 없던 동안 유일한 길이 `prior brief --out` 이었고, 그건 파일을 통째로
// 덮어쓴다 — 사람이 자기 AGENTS.md 로 겨누는 순간 그 문서가 사라진다.
func TestSpliceKeepsTheirText(t *testing.T) {
	their := "# 우리 팀 지침\n\n## 규칙\n손대면 안 되는 글이다.\n"
	got, changed, err := SpliceBlock(their, "우리 내용")
	if err != nil || !changed {
		t.Fatalf("err=%v changed=%v", err, changed)
	}
	if !strings.Contains(got, "손대면 안 되는 글이다.") {
		t.Errorf("사람의 글이 사라졌다:\n%s", got)
	}
	if !strings.Contains(got, "우리 내용") {
		t.Errorf("우리 내용이 안 들어갔다:\n%s", got)
	}
	if strings.Index(got, "손대면") > strings.Index(got, "우리 내용") {
		t.Errorf("우리 구간이 사람의 글보다 앞에 붙었다:\n%s", got)
	}
}

// ★★ **두 번 돌려도 같아야 한다.** 배선 명령은 사람이 여러 번 돌린다 — 설정을
// 바꾸고 다시, 새 머신에서 또. 블록이 하나씩 늘면 그 문서가 우리 안내로 도배된다.
func TestSpliceIsIdempotent(t *testing.T) {
	first, _, err := SpliceBlock("# 지침\n", "내용 A")
	if err != nil {
		t.Fatal(err)
	}
	second, changed, err := SpliceBlock(first, "내용 A")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("같은 내용인데 바뀌었다고 한다 — 매번 파일 mtime 이 움직인다")
	}
	if second != first {
		t.Errorf("두 번째가 달라졌다:\n%s", second)
	}
	if n := strings.Count(second, blockStart); n != 1 {
		t.Errorf("블록이 %d개다 — 하나여야 한다", n)
	}
}

// 내용이 바뀌면 갈아 끼운다. 옛 내용이 남으면 안 된다.
func TestSpliceReplacesOldBody(t *testing.T) {
	first, _, _ := SpliceBlock("# 지침\n", "옛 내용")
	second, changed, err := SpliceBlock(first, "새 내용")
	if err != nil || !changed {
		t.Fatalf("err=%v changed=%v", err, changed)
	}
	if strings.Contains(second, "옛 내용") {
		t.Errorf("옛 내용이 남았다:\n%s", second)
	}
	if !strings.Contains(second, "새 내용") {
		t.Errorf("새 내용이 없다:\n%s", second)
	}
}

// ★★ **표시가 반쪽이면 아무것도 안 한다.** 남은 한쪽을 기준으로 자르면 사람의
// 글을 먹는다. 조용히 추측하는 것보다 에러로 알리는 편이 낫다.
func TestSpliceRefusesHalfMarker(t *testing.T) {
	for _, bad := range []string{
		"# 지침\n" + blockStart + "\n내용\n",            // 끝 표시 없음
		"# 지침\n내용\n" + blockEnd + "\n",              // 시작 표시 없음
		"# 지침\n" + blockEnd + "\n내용\n" + blockStart, // 뒤집힘
	} {
		if _, _, err := SpliceBlock(bad, "새 내용"); err == nil {
			t.Errorf("반쪽 표시인데 통과했다:\n%s", bad)
		}
	}
}

// 빈 파일에서도 된다 — 새 프로젝트의 첫 배선이 그 경우다.
func TestSpliceOnEmptyFile(t *testing.T) {
	got, changed, err := SpliceBlock("", "내용")
	if err != nil || !changed {
		t.Fatalf("err=%v changed=%v", err, changed)
	}
	if strings.HasPrefix(got, "\n") {
		t.Errorf("빈 줄로 시작한다: %q", got)
	}
	if !HasBlock(got) {
		t.Errorf("블록이 없다: %q", got)
	}
}
