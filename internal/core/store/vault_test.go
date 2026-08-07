package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xian0310567/casebook/internal/testutil"
)

// fixtureLayout 은 공용 픽스처로 Layout 을 만든다.
func fixtureLayout(t *testing.T) *Layout {
	t.Helper()
	return NewLayout(testutil.VaultConfig(t))
}

func TestList(t *testing.T) {
	l := fixtureLayout(t)
	notes, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 4 {
		t.Fatalf("노트 %d건, want 4", len(notes))
	}
	// 두 배열 형식을 모두 읽는지
	for _, n := range notes {
		if len(n.Meta.Tags) == 0 {
			t.Errorf("%s: tags 를 못 읽었다", n.Stem)
		}
	}
}

func TestWriteThenRead(t *testing.T) {
	l := fixtureLayout(t)
	notes, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	n := notes[0]
	if err := l.Write(n); err != nil {
		t.Fatal(err)
	}
	again, err := l.Read(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Meta.Summary != n.Meta.Summary {
		t.Errorf("왕복 후 summary 가 변했다")
	}
	// 정본형으로 재기록된 뒤에는 바이트 동일이어야 한다
	before, _ := os.ReadFile(n.Path)
	if err := l.Write(again); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(n.Path)
	if string(before) != string(after) {
		t.Errorf("정본형 재기록이 멱등하지 않다")
	}
}

// TestListSkipsBrokenFile 은 List 가 frontmatter 없는(깨진) 파일을 조용히
// 건너뛰고 나머지 정상 노트는 전부 돌려준다는 걸 못 박는다. vault.go 의 Read
// 에러 시 continue 하는 동작이 의도된 것임을 테스트로 고정한다 — 노트 한
// 건이 깨졌다고 List 전체가 죽으면 안 된다는 요구사항이다.
func TestListSkipsBrokenFile(t *testing.T) {
	l := fixtureLayout(t)

	// alpha 결정 폴더에 frontmatter 가 없는 깨진 파일을 하나 심는다.
	dir, err := l.decisionsDir("alpha")
	if err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dir, "alpha-결정-깨짐-2026-08-05.md")
	if err := os.WriteFile(broken, []byte("frontmatter 가 없는 그냥 텍스트\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	notes, err := l.List()
	if err != nil {
		t.Fatalf("깨진 파일이 있어도 List 자체는 에러를 내면 안 된다: %v", err)
	}
	if len(notes) != 4 {
		t.Fatalf("깨진 파일 1건을 건너뛰고 정상 4건만 나와야 하는데 %d건", len(notes))
	}
	for _, n := range notes {
		if n.Stem == "alpha-결정-깨짐-2026-08-05" {
			t.Fatalf("깨진 노트가 결과에 섞였다: %+v", n)
		}
	}
}
