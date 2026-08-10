package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/testutil"
)

// fixtureNoteCount 는 픽스처 볼트(testdata/vault)의 결정 노트 수다.
// `prior index` 가 보고하는 행 수의 기대값이므로, 픽스처에 노트를 더하면 여기도 는다.
const fixtureNoteCount = 4

// TestIndexCmd 는 `prior index` 가 --config 로 지정된 설정 파일을 읽어 색인을 실제로
// 만들고, "색인 N행 생성" 형식으로 행 수를 보고하는지 확인한다.
func TestIndexCmd(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	buf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(errBuf)
	root.SetArgs([]string{"index", "--config", cfgPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("prior index 실행 실패: %v", err)
	}

	want := fmt.Sprintf("색인 %d행 생성\n", fixtureNoteCount)
	if got := buf.String(); got != want {
		t.Errorf("출력 = %q, want %q", got, want)
	}
	// 건너뛴 게 없으면 경고도 없어야 한다 — 늘 뜨는 경고는 아무도 안 읽는다.
	if got := errBuf.String(); got != "" {
		t.Errorf("건너뛴 게 없는데 stderr 에 출력이 있다: %q", got)
	}

	idxPath := filepath.Join(c.Vault, c.Naming.Index)
	data, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("색인 파일이 생기지 않았다 (%s): %v", idxPath, err)
	}
	if !strings.Contains(string(data), "저장 엔진을 임베디드 DB 로 고른다") {
		t.Errorf("색인 내용에 픽스처 노트의 summary 가 없다:\n%s", data)
	}
}

// legacyNoteBody 는 다른 도구가 남긴 구 스키마 frontmatter 다
// (title/project/created/superseded-by). `type: decision` 표식이 없어서 우리 노트가
// 아닌 것으로 걸린다 — 이런 노트가 섞여도 색인이 조용히 줄어들지 않는 것이 이 테스트의
// 요지다. (2026-08-09 이전에는 잉여 키 자체가 파싱을 막았는데, 같은 규칙이 사용자가
// 손으로 넣은 aliases 도 막아서 멀쩡한 결정을 지웠다.)
const legacyNoteBody = `---
title: 구 스키마로 쓰인 저장 엔진 결정
project: alpha
created: 2026-01-02
superseded-by: ""
---

## 결정

옛 도구가 남긴 형식이다.
`

// plantLegacyNote 는 픽스처 볼트의 alpha 결정 폴더에 구 스키마 노트를 하나 심고
// 그 볼트 상대 경로를 준다.
func plantLegacyNote(t *testing.T, vault string) string {
	t.Helper()
	rel := filepath.Join("alpha", "decisions", "alpha-결정-저장엔진구형-2026-01-02.md")
	if err := os.WriteFile(filepath.Join(vault, rel), []byte(legacyNoteBody), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}

// TestIndexCmdRevealsSkippedNotes 는 `prior index` 가 건너뛴 노트를 사용자에게
// 드러내는지 확인한다.
//
// 예전에는 읽기 실패한 노트를 조용히 건너뛰고 "색인 N행 생성" 만 냈다. 실볼트
// 53건 중 6건이 구 스키마라 거부됐는데도 아무 말 없이 47행짜리 색인이 만들어졌고,
// 사용자는 6건이 사라진 걸 알 방법이 없었다 — 스펙 §1.3 이 셸의 죄목으로 든
// "조용히 데이터를 잃는다" 그대로다.
//
// 요구사항 두 가지를 다 본다: (1) stdout 요약 줄만 봐도 불완전함을 알 수 있다
// (2) stderr 에 어느 파일이 왜 빠졌는지가 나온다.
func TestIndexCmdRevealsSkippedNotes(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)
	rel := plantLegacyNote(t, c.Vault)

	root := NewRootCmd()
	buf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(errBuf)
	root.SetArgs([]string{"index", "--config", cfgPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("깨진 노트 한 건 때문에 prior index 가 죽으면 안 된다: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, fmt.Sprintf("색인 %d행 생성", fixtureNoteCount)) {
		t.Errorf("정상 노트 %d건이 색인에 안 들어갔다: %q", fixtureNoteCount, out)
	}
	if !strings.Contains(out, "1건 건너뜀") {
		t.Errorf("stdout 요약이 건너뜀을 숨긴다: %q", out)
	}

	warn := errBuf.String()
	if !strings.Contains(warn, "읽지 못해 건너뛰었다") {
		t.Errorf("stderr 에 경고가 없다: %q", warn)
	}
	if !strings.Contains(warn, rel) {
		t.Errorf("어느 파일인지 안 알려준다 (want %q):\n%s", rel, warn)
	}
	if !strings.Contains(warn, "결정 노트가 아니다") {
		t.Errorf("왜 빠졌는지 안 알려준다:\n%s", warn)
	}
	// 경로는 볼트 상대로 한 번만 나와야 한다 — 절대 경로가 함께 찍히면
	// 긴 경로 두 개가 한 줄에 겹쳐 읽기 어려워진다.
	if strings.Contains(warn, c.Vault) {
		t.Errorf("경고에 절대 경로가 중복으로 들어 있다:\n%s", warn)
	}
}
