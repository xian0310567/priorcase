package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
)

// 공유 볼트 — **작업 로그는 동기화하지 않는다.**
//
// # 왜
//
// 결정 노트는 파일명에 날짜와 slug 가 들어가 사람마다 다른 파일이라 안전하다.
// 작업 로그는 프로젝트당 하나이고 여럿이 **같은 파일 끝에** 붙이므로, 두 사람이
// 같은 날 같은 프로젝트에서 일하면 충돌이 예외가 아니라 일상이 된다
// (conflict_test.go 의 재현).
//
// 자동 병합은 아무리 잘 짜도 위험하다. 그래서 충돌할 파일을 **아예 안 만든다.**
// 작업 로그는 "내 작업 기록" 이지 "팀의 결정" 이 아니므로, 공유해야 할 이유도
// 약하다 — 팀이 나눠야 하는 것은 결정이다.
//
// # 파일은 지우지 않는다
//
// git 에서만 뺀다. 파일은 제자리에 그대로 남아 회수·rollup·doctor 가 전부
// 하던 대로 돈다. 볼트에 둔 것을 지우지 않는다는 규칙은 여기도 같다.

func sharedConfig(t *testing.T, vault string) *config.Config {
	t.Helper()
	return &config.Config{
		Vaults: []config.Vault{{Name: "회사", Path: vault, Shared: true}},
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-작업-로그.md",
		},
		Domain: []config.Domain{{Prefix: "priorcase", Folder: "priorcase"}},
	}
}

// tracked 는 git 이 추적 중인 파일이다.
//
// **-z 를 쓴다.** 기본 출력은 한글 파일명을 `\354\236\221` 처럼 이스케이프해
// 따옴표로 감싸는데, 이 볼트의 파일명은 거의 전부 한글이라 그대로 비교하면
// 무엇과도 안 맞는다 — 시험이 조용히 통과해 버린다.
func tracked(t *testing.T, dir string) []string {
	t.Helper()
	out := git(t, dir, "ls-files", "-z")
	var got []string
	for _, l := range strings.Split(out, "\x00") {
		if strings.TrimSpace(l) != "" {
			got = append(got, l)
		}
	}
	return got
}

// ★★★ 공유 볼트에서는 작업 로그가 리모트로 안 간다.
func TestSharedVaultDoesNotSyncWorklogs(t *testing.T) {
	_, b := pair(t)
	write(t, b, "priorcase/99-priorcase-작업-로그.md", "# 작업 로그\n\n- 내 기록\n")
	write(t, b, "priorcase/decisions/priorcase-결정-무언가-2026-09-01.md", "---\ntype: decision\n---\n본문\n")

	rs := All(sharedConfig(t, b), Options{}, false, true, "테스트")
	for _, v := range rs {
		for _, r := range v.Results {
			if r.Err != nil {
				t.Fatalf("동기화 실패: %v", r.Err)
			}
		}
	}

	got := strings.Join(tracked(t, b), "\n")
	if strings.Contains(got, "작업-로그") {
		t.Errorf("작업 로그가 커밋됐다:\n%s", got)
	}
	// **결정은 반드시 가야 한다.** 그것이 공유 볼트의 존재 이유다.
	if !strings.Contains(got, "priorcase-결정-무언가") {
		t.Errorf("결정이 안 갔다:\n%s", got)
	}
}

// ★ 이미 추적 중이던 작업 로그도 떼어 낸다.
//
// 개인 볼트로 쓰다가 공유로 돌리는 것이 흔한 경로다(사업주가 지금 그 길이다).
// 새로 만든 것만 막으면 옛 파일은 계속 동기화되어 충돌이 그대로 남는다.
func TestSharedVaultUntracksExistingWorklogs(t *testing.T) {
	_, b := pair(t)
	const rel = "priorcase/99-priorcase-작업-로그.md"
	write(t, b, rel, "# 작업 로그\n\n- 옛 기록\n")
	git(t, b, "add", "-A")
	git(t, b, "commit", "-m", "공유 전")

	if got := strings.Join(tracked(t, b), "\n"); !strings.Contains(got, "작업-로그") {
		t.Fatal("픽스처가 틀렸다 — 작업 로그가 추적 중이어야 한다")
	}

	All(sharedConfig(t, b), Options{}, false, true, "테스트")

	if got := strings.Join(tracked(t, b), "\n"); strings.Contains(got, "작업-로그") {
		t.Errorf("옛 작업 로그가 계속 추적된다:\n%s", got)
	}
	// **파일은 지우지 않는다.** git 에서만 뺀다 — 회수와 rollup 이 계속 읽는다.
	if _, err := os.Stat(filepath.Join(b, rel)); err != nil {
		t.Errorf("파일을 지웠다: %v", err)
	}
}

// ★ .gitignore 에 남긴다. 다음에 다시 담기지 않게 하고, **git 도구로 봐도**
// 왜 그 파일이 안 올라가는지 사람이 알 수 있어야 한다.
func TestSharedVaultRecordsTheReasonInGitignore(t *testing.T) {
	_, b := pair(t)
	write(t, b, "priorcase/99-priorcase-작업-로그.md", "# 작업 로그\n")
	All(sharedConfig(t, b), Options{}, false, true, "테스트")

	raw, err := os.ReadFile(filepath.Join(b, ".gitignore"))
	if err != nil {
		t.Fatalf(".gitignore 가 없다: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "작업-로그") {
		t.Errorf("무엇을 뺐는지 안 적혀 있다:\n%s", got)
	}
	if !strings.Contains(got, "priorcase") {
		t.Errorf("누가 왜 넣었는지 안 적혀 있다 — 사람이 지우기 무섭다:\n%s", got)
	}
}

// ★ 두 번 돌려도 .gitignore 가 두 번 늘지 않는다. 동기화는 세션마다 도는데
// 매번 줄이 붙으면 그 파일이 곧 쓰레기가 된다.
func TestSharedVaultIsIdempotent(t *testing.T) {
	_, b := pair(t)
	write(t, b, "priorcase/99-priorcase-작업-로그.md", "# 작업 로그\n")
	c := sharedConfig(t, b)
	All(c, Options{}, false, true, "한 번")
	first, _ := os.ReadFile(filepath.Join(b, ".gitignore"))
	write(t, b, "priorcase/99-priorcase-작업-로그.md", "# 작업 로그\n- 더\n")
	All(c, Options{}, false, true, "두 번")
	second, _ := os.ReadFile(filepath.Join(b, ".gitignore"))

	if string(first) != string(second) {
		t.Errorf(".gitignore 가 자란다:\n첫 번째:\n%s\n두 번째:\n%s", first, second)
	}
}

// ★★ 개인 볼트는 **아무것도 달라지지 않는다.** 혼자 쓰는 볼트에서 작업 로그가
// 머신 사이를 오가는 것은 기능이지 고장이 아니다.
func TestPrivateVaultStillSyncsWorklogs(t *testing.T) {
	_, b := pair(t)
	write(t, b, "priorcase/99-priorcase-작업-로그.md", "# 작업 로그\n")
	c := sharedConfig(t, b)
	c.Vaults[0].Shared = false

	All(c, Options{}, false, true, "테스트")

	if got := strings.Join(tracked(t, b), "\n"); !strings.Contains(got, "작업-로그") {
		t.Errorf("개인 볼트인데 작업 로그가 안 갔다:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(b, ".gitignore")); err == nil {
		t.Error("개인 볼트의 .gitignore 를 건드렸다")
	}
}
