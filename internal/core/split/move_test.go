package split

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// 도메인을 **다른 볼트로 통째로** 옮긴다.
//
// # 왜 split 과 다른가
//
// `Build`/`Apply` 는 폴백에 쌓인 노트에서 **새 도메인을 뽑아내는** 일이라 파일명과
// frontmatter 를 다시 쓴다. 볼트 간 이동은 그럴 필요가 없다 — 도메인 이름이 그대로라
// 파일명도, `domain:` 도, 위키링크(옵시디언은 파일명으로 푼다)도 안 바뀐다.
// **폴더만 옮기면 된다.**
//
// # 고치려는 고장
//
// 2026-09-01: 앱에서 `editup` 을 회사 볼트로 옮겼더니 **설정만 바뀌고 파일 63건은
// 옛 볼트에 남았다.** 회수는 새 볼트의 빈 폴더를 보므로 그 프로젝트의 결정이
// 통째로 사라졌고, 화면에는 "결정 0건" 이라고만 떴다. 사람이 알아챌 방법이 없었다.
//
// 설정을 바꾸는 길은 있는데 파일을 옮기는 길이 없었던 것이 원인이다.

func vaultAt(t *testing.T, root, name string) (*config.Config, *store.Layout, *store.Layout) {
	t.Helper()
	src := filepath.Join(root, "개인")
	dst := filepath.Join(root, name)
	for _, d := range []string{src, dst} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	c := &config.Config{
		Vaults: []config.Vault{
			{Name: config.DefaultVaultName, Path: src},
			{Name: name, Path: dst},
		},
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-작업-로그.md",
		},
		// **설정은 이미 새 볼트를 가리킨다.** 실제 순서가 그렇다 — 앱에서 볼트를
		// 바꾸면 설정만 바뀌고 파일은 그대로 남는다. 이 명령은 그 뒤를 잇는다.
		Domain: []config.Domain{{Prefix: "editup", Folder: "editup", Vault: name}},
	}
	return c, store.NewLayoutFor(c, c.Vaults[0]), store.NewLayoutFor(c, c.Vaults[1])
}

func seedDomain(t *testing.T, l *store.Layout, n int) {
	t.Helper()
	dir := filepath.Join(l.Vault(), "editup", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		body := "---\ntype: decision\ndate: 2026-08-01\ndomain: [editup]\n" +
			"summary: \"결정 " + string(rune('가'+i)) + "\"\nstatus: active\ntags: [decision]\n---\n\n본문\n"
		p := filepath.Join(dir, "editup-결정-무언가"+string(rune('가'+i))+"-2026-08-01.md")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 작업 로그와 참고 문서도 같은 폴더에 산다.
	if err := os.WriteFile(filepath.Join(l.Vault(), "editup", "99-editup-작업-로그.md"),
		[]byte("# 작업 로그\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ★★★ 결정이 실제로 새 볼트로 간다. 이것이 고장의 핵심이다.
func TestMoveDomainCarriesTheNotes(t *testing.T) {
	c, src, dst := vaultAt(t, t.TempDir(), "회사")
	seedDomain(t, src, 3)

	p, err := PlanMove(c, src, dst, "editup")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Moves) == 0 {
		t.Fatal("옮길 것이 없다고 한다")
	}
	if err := ApplyMove(p); err != nil {
		t.Fatal(err)
	}

	got, _, err := dst.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("새 볼트에 결정이 %d건 — 3건이어야 한다", len(got))
	}
	// **옛 볼트에는 안 남아야 한다.** 남으면 같은 결정이 두 볼트에 있고,
	// 회수가 중복으로 내면 어느 쪽이 정본인지 알 수 없다.
	//
	// `List()` 로 못 본다 — 설정상 editup 은 이미 회사 볼트 소속이라 개인 볼트의
	// List 는 그 폴더를 훑지도 않는다. 파일이 실제로 사라졌는지 봐야 한다.
	if _, err := os.Stat(filepath.Join(src.Vault(), "editup")); !os.IsNotExist(err) {
		t.Errorf("옛 볼트에 editup 폴더가 남았다 (%v)", err)
	}
}

// ★ 작업 로그·참고 문서도 같이 간다. 폴더 전체가 그 프로젝트의 것이다.
func TestMoveDomainCarriesTheWholeFolder(t *testing.T) {
	root := t.TempDir()
	c, src, dst := vaultAt(t, root, "회사")
	seedDomain(t, src, 1)

	p, _ := PlanMove(c, src, dst, "editup")
	if err := ApplyMove(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst.Vault(), "editup", "99-editup-작업-로그.md")); err != nil {
		t.Errorf("작업 로그가 안 따라왔다: %v", err)
	}
}

// ★★ **목적지에 이미 있으면 거부한다.** 덮어쓰면 남의 결정이 조용히 사라진다.
func TestMoveRefusesWhenDestinationHasTheFolder(t *testing.T) {
	root := t.TempDir()
	c, src, dst := vaultAt(t, root, "회사")
	seedDomain(t, src, 1)
	seedDomain(t, dst, 1)

	if _, err := PlanMove(c, src, dst, "editup"); err == nil {
		t.Fatal("목적지에 이미 있는데 통과했다")
	}
}

// ★ 옮길 것이 없으면 그렇게 말한다 — 조용히 성공하면 사람은 옮겨진 줄 안다.
func TestMoveSaysWhenNothingToMove(t *testing.T) {
	c, src, dst := vaultAt(t, t.TempDir(), "회사")
	if _, err := PlanMove(c, src, dst, "editup"); err == nil {
		t.Fatal("빈 폴더인데 통과했다")
	}
}

// ★ 같은 볼트로 옮기라는 것은 실수다. 조용히 성공하면 사람은 뭔가 됐다고 믿는다.
func TestMoveRefusesSameVault(t *testing.T) {
	c, src, _ := vaultAt(t, t.TempDir(), "회사")
	seedDomain(t, src, 1)
	if _, err := PlanMove(c, src, src, "editup"); err == nil {
		t.Fatal("같은 볼트인데 통과했다")
	}
}

// ★★★ **설정이 이미 목적지를 가리켜도 파일이 있는 볼트에서 옮겨야 한다.**
//
// 이것이 실제 순서다. 앱에서 볼트를 바꾸면 설정이 먼저 바뀌고 파일은 옛 볼트에
// 남는다 — 이 명령은 **그 상태를 고치려고** 있다. 그런데 원본을 "설정상 그 도메인의
// 볼트" 로 찾으면 목적지와 같아져서 "옮길 곳이 아니다" 로 거부한다.
// 즉 고치려던 바로 그 상태에서만 안 돈다.
func TestFindSourceLooksAtWhereFilesActuallyAre(t *testing.T) {
	root := t.TempDir()
	c, src, dst := vaultAt(t, root, "회사")
	seedDomain(t, src, 2) // 설정은 회사를 가리키는데 파일은 개인 볼트에 있다

	got, err := FindSource(c, "editup", dst.Vault())
	if err != nil {
		t.Fatal(err)
	}
	if got != src.Vault() {
		t.Fatalf("원본을 %q 로 봤다 — 파일이 있는 %q 여야 한다", got, src.Vault())
	}
}

// ★ 어디에도 없으면 그렇게 말한다.
func TestFindSourceSaysWhenNowhere(t *testing.T) {
	c, _, dst := vaultAt(t, t.TempDir(), "회사")
	if _, err := FindSource(c, "editup", dst.Vault()); err == nil {
		t.Fatal("파일이 없는데 원본을 찾았다고 한다")
	}
}
