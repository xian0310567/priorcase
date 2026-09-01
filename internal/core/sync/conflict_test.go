package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 충돌은 **공유 볼트에서 예외가 아니라 일상이다.**
//
// # 재현 (2026-09-01)
//
// 결정 노트는 파일명에 날짜와 slug 가 들어가 사람마다 다른 파일이라 안전하다.
// 그런데 작업 로그(`99-{project}-작업-로그.md`)는 프로젝트당 하나이고, 여럿이
// **같은 파일 끝에** 붙인다. 두 사람이 같은 날 같은 프로젝트에서 일하면 충돌한다.
//
// 그때 벌어지던 일:
//
//	① `pull --rebase` 가 실패하고 **볼트가 rebase 중단 상태로 남는다**
//	② 훅은 무슨 일이 있어도 exit 0 이라 **아무도 모른다**
//	③ 다음 세션의 push 가 `add -A` 로 **충돌 마커째 커밋한다**
//	④ 그것이 리모트로 가면 **팀 전원의 볼트에 퍼진다**
//
// ③은 실제로 커밋되는 것까지 확인했다. 그리고 그 파일은 회수 대상이라, 이후
// 회수가 `<<<<<<<` 가 든 텍스트를 결과로 낸다.
//
// 이 파일은 ①과 ③을 잠근다.

// conflict 는 두 사본이 같은 파일 끝에 서로 다른 줄을 붙인 판을 만든다.
// 공유 볼트에서 두 사람이 같은 작업 로그에 적는 상황 그대로다.
func conflict(t *testing.T) (a, b string) {
	t.Helper()
	a, b = pair(t)
	const rel = "priorcase/99-priorcase-작업-로그.md"

	write(t, a, rel, "# 작업 로그\n\n## 2026-09-01\n\n- 첫 줄\n")
	git(t, a, "add", "-A")
	git(t, a, "commit", "-m", "시작")
	git(t, a, "push", "origin", "main")
	git(t, b, "pull", "origin", "main")

	write(t, a, rel, "# 작업 로그\n\n## 2026-09-01\n\n- 첫 줄\n- A 가 붙인 줄\n")
	git(t, a, "add", "-A")
	git(t, a, "commit", "-m", "A")
	git(t, a, "push", "origin", "main")

	write(t, b, rel, "# 작업 로그\n\n## 2026-09-01\n\n- 첫 줄\n- B 가 붙인 줄\n")
	git(t, b, "add", "-A")
	git(t, b, "commit", "-m", "B")
	return a, b
}

func inProgress(dir string) bool {
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(dir, ".git", d)); err == nil {
			return true
		}
	}
	return false
}

func body(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ★★★ 충돌한 pull 은 **볼트를 원래대로 되돌려 놓고** 실패해야 한다.
//
// 반쯤 진행된 rebase 를 남기면 그 볼트는 그 뒤로 pull 도 push 도 안 되고, 훅은
// exit 0 이라 사람은 계속 잘 되는 줄 안다. 기록은 로컬에만 쌓인다.
func TestConflictedPullLeavesTheVaultClean(t *testing.T) {
	_, b := conflict(t)

	r := Pull(b)
	if r.Err == nil {
		t.Fatal("충돌했는데 성공으로 돌아왔다")
	}
	if inProgress(b) {
		t.Error("rebase 가 중단된 채 남았다 — 다음 세션부터 동기화가 통째로 죽는다")
	}
	if got := body(t, b, "priorcase/99-priorcase-작업-로그.md"); strings.Contains(got, "<<<<<<<") {
		t.Error("작업 파일에 충돌 마커가 남았다")
	}
	// 되돌렸으면 내 커밋은 그대로 있어야 한다. 이건 abort 가 아니라 reset 이
	// 됐을 때 잃는 것이다.
	if !strings.Contains(body(t, b, "priorcase/99-priorcase-작업-로그.md"), "B 가 붙인 줄") {
		t.Error("내가 쓴 것이 사라졌다")
	}
}

// ★★★ 충돌 마커가 든 파일은 **절대 커밋되지 않는다.**
//
// `add -A` 를 쓰는 것은 의도된 결정이다(Push 의 §) — 빠뜨린 파일은 다른 머신에서
// 영영 안 보이기 때문이다. 그 대가로 **무엇이든 담기므로** 여기서 막아야 한다.
// 막지 않으면 한 사람의 충돌이 팀 전원의 볼트로 퍼진다.
func TestPushRefusesConflictMarkers(t *testing.T) {
	_, b := conflict(t)
	_ = Pull(b) // 충돌 → 되돌려짐

	// 되돌린 뒤에도 사람이 손으로 마커를 남길 수 있다. 그 판을 직접 만든다.
	const rel = "priorcase/99-priorcase-작업-로그.md"
	write(t, b, rel, "# 작업 로그\n\n<<<<<<< HEAD\n- A\n=======\n- B\n>>>>>>> x\n")

	r := Push(b, "테스트")
	if r.Err == nil {
		t.Fatal("충돌 마커가 든 파일을 밀어 버렸다")
	}
	if !strings.Contains(r.Err.Error(), "충돌") {
		t.Errorf("무엇이 문제인지 안 말한다: %v", r.Err)
	}
	// **커밋도 안 됐어야 한다.** 커밋만 되고 push 가 막히면 다음 세션이 그것을
	// 그대로 밀어낸다 — 한 번 늦출 뿐 결과가 같다.
	out := git(t, b, "log", "--oneline", "-1")
	if strings.Contains(out, "테스트") {
		t.Error("마커가 든 파일이 커밋됐다")
	}
}

// ★ 마커처럼 생긴 글이 **본문에 정당하게 있을 수 있다.** 이 저장소의 결정문이
// 실제로 git 충돌을 설명하며 그 문자열을 인용한다. 줄 첫머리의 마커만 본다.
func TestPushAllowsMarkerLookalikesInProse(t *testing.T) {
	_, b := pair(t)
	write(t, b, "priorcase/decisions/note.md",
		"충돌이 나면 `<<<<<<< HEAD` 같은 줄이 생긴다고 적어 둔다.\n"+
			"들여쓴 코드 예시도 있다:\n\n    <<<<<<< HEAD\n")

	if r := Push(b, "산문"); r.Err != nil {
		t.Fatalf("정당한 인용을 막았다: %v", r.Err)
	}
}
