package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/testutil"
)

// ★★ 색인 경로에 사용자가 손으로 쓴 문서가 있으면 **경고도 백업도 없이 사라졌다.**
//
// 색인 자리는 설정([naming] index)이 정한다. 사용자가 그 자리에 이미 뭔가 두고 있는
// 것은 이상한 일이 아니고, 그걸 지우는 것은 이 도구가 절대 해서는 안 되는 일이다.
// 실측으로 재현했다 — 188바이트 손글씨 문서가 색인으로 교체되고 백업이 없었다.
func TestForeignIndexFileIsPreserved(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	p := l.IndexPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	const mine = "# 내가 손으로 쓴 색인\n\n중요한 메모가 잔뜩 있다.\n"
	if err := os.WriteFile(p, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Write(l)
	if err != nil {
		t.Fatal(err)
	}
	if res.Preserved == "" {
		t.Fatal("남의 파일을 말없이 덮어썼다 — 사용자는 자기 문서가 어디 갔는지 모른다")
	}
	got, err := os.ReadFile(res.Preserved)
	if err != nil {
		t.Fatalf("대피본을 못 읽는다: %v", err)
	}
	if string(got) != mine {
		t.Errorf("대피본 내용이 원본과 다르다:\n%q", got)
	}
	// 색인 자체는 정상적으로 만들어져야 한다 — 막으면 capture 가 통째로 실패한다.
	idx, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(idx), indexMarker) {
		t.Errorf("색인이 안 만들어졌다: %v", err)
	}
}

// 우리가 만든 색인은 대피시키지 않는다 — 매 capture 마다 백업이 쌓이면 볼트가 더러워진다.
func TestOwnIndexIsNotPreserved(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)

	first, err := Write(l)
	if err != nil {
		t.Fatal(err)
	}
	if first.Preserved != "" {
		t.Fatalf("첫 생성인데 대피했다: %s", first.Preserved)
	}
	for i := 0; i < 3; i++ {
		res, err := Write(l)
		if err != nil {
			t.Fatal(err)
		}
		if res.Preserved != "" {
			t.Fatalf("%d회차에 우리 색인을 대피시켰다: %s", i+2, res.Preserved)
		}
	}
	m, _ := filepath.Glob(l.IndexPath() + ".casebook-replaced*")
	if len(m) != 0 {
		t.Errorf("백업이 쌓였다: %v", m)
	}
}

// 대피본 이름이 겹치면 덧붙여 고른다. 덮어쓰면 대피의 의미가 없다.
func TestPreservedBackupDoesNotOverwriteEarlierBackup(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	p := l.IndexPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}

	var backups []string
	for i, body := range []string{"첫 번째 손글씨\n", "두 번째 손글씨\n"} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := Write(l)
		if err != nil {
			t.Fatal(err)
		}
		if res.Preserved == "" {
			t.Fatalf("%d회차에 대피하지 않았다", i+1)
		}
		backups = append(backups, res.Preserved)
		got, _ := os.ReadFile(res.Preserved)
		if string(got) != body {
			t.Errorf("%d회차 대피본이 %q, want %q", i+1, got, body)
		}
	}
	if backups[0] == backups[1] {
		t.Errorf("두 번째 대피가 첫 번째를 덮어썼다: %s", backups[0])
	}
	// 첫 번째가 아직 살아 있어야 한다.
	if got, err := os.ReadFile(backups[0]); err != nil || string(got) != "첫 번째 손글씨\n" {
		t.Errorf("첫 대피본이 사라졌다: %q %v", got, err)
	}
}
