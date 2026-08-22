package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/xian0310567/priorcase/internal/testutil"
)

// fixtureLayout 은 공용 픽스처로 Layout 을 만든다.
func fixtureLayout(t *testing.T) *Layout {
	t.Helper()
	return NewLayout(testutil.VaultConfig(t))
}

func TestList(t *testing.T) {
	l := fixtureLayout(t)
	notes, skipped, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("정상 픽스처인데 건너뛴 노트가 있다: %+v", skipped)
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

// TestDecisionStems 는 한 도메인의 결정 폴더 stem 만 준다는 것과, 폴더가 아직
// 없으면 에러가 아니라 빈 목록이라는 것을 확인한다 (중복 검사가 첫 노트를
// 기록할 때 여기서 죽으면 안 된다).
func TestDecisionStems(t *testing.T) {
	l := fixtureLayout(t)
	stems, err := l.DecisionStems("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(stems) != 2 {
		t.Fatalf("alpha stem %d건, want 2: %v", len(stems), stems)
	}
	for _, s := range stems {
		if !strings.HasPrefix(s, "alpha-") || strings.HasSuffix(s, ".md") {
			t.Errorf("stem 이 아니다: %q", s)
		}
	}
	// common 도메인은 픽스처에 폴더가 있지만, 존재하지 않는 폴더도 빈 목록이어야 한다.
	if err := os.RemoveAll(filepath.Join(l.c.DefaultVaultPath(), "common", "decisions")); err != nil {
		t.Fatal(err)
	}
	empty, err := l.DecisionStems("common")
	if err != nil {
		t.Fatalf("폴더가 없을 때 에러가 났다: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("폴더가 없는데 %d건: %v", len(empty), empty)
	}
	if _, err := l.DecisionStems("없는도메인"); err == nil {
		t.Error("알 수 없는 접두어를 통과시켰다")
	}
}

func TestWriteThenRead(t *testing.T) {
	l := fixtureLayout(t)
	notes, _, err := l.List()
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

// oldSchemaNote 는 다른 도구가 남긴 구 스키마 frontmatter 다
// (title/project/created/superseded-by). ParseFrontmatter 의
// KnownFields(true) 가 잉여 키를 거부하므로 이 노트는 파싱에서 떨어진다.
const oldSchemaNote = `---
title: 구 스키마로 쓰인 결정
project: alpha
created: 2026-01-02
superseded-by: ""
---

## 결정

옛 도구가 남긴 형식이다.
`

// TestListSkipsBrokenFile 은 List 가 읽지 못한 파일을 건너뛰되 **건너뛴 사실을
// 호출자에게 알린다**는 걸 못 박는다.
//
// 건너뛰는 동작 자체는 의도된 것이다 — 노트 한 건이 깨졌다고 List 전체가
// 죽으면 안 된다. 하지만 예전에는 건너뛴 것을 아무에게도 알리지 않아서,
// 실볼트 53건 중 6건이 구 스키마로 빠졌는데도 `prior index` 가 아무 말 없이
// 47행짜리 색인을 만들었다. 그래서 이 테스트는 두 가지를 동시에 고정한다:
// (1) 정상 노트는 전부 나온다 (2) 빠진 노트가 경로·원인과 함께 보고된다.
//
// 두 가지 실패 유형을 다 심는다 — frontmatter 자체가 없는 경우와, 있지만
// 스키마가 옛 것인 경우. 후자가 실볼트에서 실제로 관측된 유형이다.
func TestListSkipsBrokenFile(t *testing.T) {
	l := fixtureLayout(t)

	dir, err := l.decisionsDir("alpha")
	if err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dir, "alpha-결정-깨짐-2026-08-05.md")
	if err := os.WriteFile(broken, []byte("frontmatter 가 없는 그냥 텍스트\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldSchema := filepath.Join(dir, "alpha-결정-구스키마-2026-08-05.md")
	if err := os.WriteFile(oldSchema, []byte(oldSchemaNote), 0o644); err != nil {
		t.Fatal(err)
	}

	notes, skipped, err := l.List()
	if err != nil {
		t.Fatalf("깨진 파일이 있어도 List 자체는 에러를 내면 안 된다: %v", err)
	}
	if len(notes) != 4 {
		t.Fatalf("깨진 파일 2건을 건너뛰고 정상 4건만 나와야 하는데 %d건", len(notes))
	}
	for _, n := range notes {
		if n.Stem == "alpha-결정-깨짐-2026-08-05" || n.Stem == "alpha-결정-구스키마-2026-08-05" {
			t.Fatalf("깨진 노트가 결과에 섞였다: %+v", n)
		}
	}

	// 여기가 이번 수정의 핵심이다: 건너뛴 것이 침묵되지 않는다.
	if len(skipped) != 2 {
		t.Fatalf("건너뛴 노트 %d건, want 2 — 건너뜀이 보고되지 않았다: %+v", len(skipped), skipped)
	}
	byPath := map[string]error{}
	for _, s := range skipped {
		if s.Reason == nil {
			t.Errorf("%s: 원인이 비었다 — 왜 빠졌는지 알 수 없다", s.Path)
		}
		byPath[s.Path] = s.Reason
	}
	if _, ok := byPath[broken]; !ok {
		t.Errorf("frontmatter 없는 파일이 건너뜀 목록에 없다: %+v", skipped)
	}
	reason, ok := byPath[oldSchema]
	if !ok {
		t.Fatalf("구 스키마 파일이 건너뜀 목록에 없다: %+v", skipped)
	}
	// 원인이 사용자에게 그대로 나가므로, 무엇이 문제인지 읽을 수 있어야 한다.
	//
	// 2026-08-09 이후 구 스키마는 **잉여 키가 아니라 표식 부재**로 걸린다. 잉여 키는
	// 이제 Extra 로 보존되므로(사용자가 Obsidian 에서 넣는 aliases 를 지우지 않으려고),
	// 옛 도구 노트를 가르는 기준은 `type: decision` 이 있느냐 하나뿐이다.
	if !strings.Contains(reason.Error(), "결정 노트가 아니다") {
		t.Errorf("구 스키마의 원인이 무엇이 문제인지 안 짚어준다: %v", reason)
	}
	// 경로는 SkippedNote.Path 가 들고 있다. 원인에 또 박혀 있으면 호출부가
	// 볼트 상대 경로로 다듬어 낼 때 절대 경로가 중복으로 찍힌다.
	if strings.Contains(reason.Error(), dir) {
		t.Errorf("원인에 경로가 중복으로 들어 있다: %v", reason)
	}
}

// TestListNormalizesNFDFilenames 는 List() 가 실제 NFD 로 인코딩된 파일명을
// 찾아내고, 반환된 Stem 이 NFC 로 정규화돼 있는지 못 박는다.
//
// 리뷰에서 뮤테이션으로 확인된 문제: vault.go List() 의 `name := NFC(e.Name())`
// 를 `name := e.Name()` 으로 바꿔도 기존 테스트는 전부 통과했다. testdata/vault
// 의 픽스처 4개가 전부 NFC 로 커밋돼 있어서 NFD 경로가 한 번도 실행되지
// 않았기 때문이다. macOS APFS 는 파일명을 준 그대로(NFD 든 NFC 든) 보존해
// 돌려주고 Linux ext4 는 바이트 정확 매칭이므로, 이 정규화가 없으면 결정
// 노트가 조용히 List() 결과에서 사라질 수 있다 — 이게 이 프로젝트가 존재하는
// 이유인 결함 계열이다.
//
// 파일명은 NFC 리터럴로 적고 norm.NFD.String 으로 런타임에 분해한다. 소스에
// NFD 코드포인트를 직접 박으면 에디터/도구가 저장 시 NFC 로 재정규화해
// 커버리지가 조용히 사라질 위험이 있다.
func TestListNormalizesNFDFilenames(t *testing.T) {
	l := fixtureLayout(t)
	dir, err := l.decisionsDir("alpha")
	if err != nil {
		t.Fatal(err)
	}

	nameNFC := "alpha-결정-정규화확인-2026-08-06.md"
	nameNFD := norm.NFD.String(nameNFC)
	if nameNFD == nameNFC {
		t.Fatal("NFD 변환이 원본과 같다 — 분해 가능한 문자가 없어 테스트가 무의미하다")
	}
	if norm.NFC.IsNormalString(nameNFD) {
		t.Fatal("만든 파일명이 이미 NFC 다 — NFD 픽스처가 아니다")
	}

	content := `---
type: decision
date: 2026-08-06
domain: [alpha]
summary: "NFD 파일명 정규화 확인"
status: active
outcome: pending
supersedes: ""
related: []
tags: [decision,alpha,nfd]
source_session: ""
---

## 결정

NFD 로 인코딩된 파일명도 List() 가 찾아내고 Stem 을 NFC 로 돌려줘야 한다.
`
	if err := os.WriteFile(filepath.Join(dir, nameNFD), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// macOS 파일시스템이 이름을 정규화해 저장했을 수 있으니 os.ReadDir 로
	// 되읽어 실제 온디스크 형태를 확인한다. NFC 로 정규화돼 버렸다면 이
	// 테스트가 노리는 NFD 경로 자체가 없는 것이므로 무의미하게 통과시키지
	// 않고 이유를 남기며 건너뛴다.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk string
	found := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if norm.NFC.String(e.Name()) == nameNFC {
			onDisk = e.Name()
			found = true
			break
		}
	}
	if !found {
		t.Fatal("방금 쓴 파일을 os.ReadDir 결과에서 못 찾았다")
	}
	if norm.NFC.IsNormalString(onDisk) {
		t.Skip("파일시스템이 파일명을 NFC 로 정규화해 저장했다 — 이 환경에서는 NFD 경로를 행사할 수 없다")
	}

	notes, _, err := l.List()
	if err != nil {
		t.Fatal(err)
	}

	wantStem := strings.TrimSuffix(nameNFC, ".md")
	var got *Note
	for i := range notes {
		if notes[i].Stem == wantStem {
			got = &notes[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("NFD 파일명 노트를 List() 결과에서 못 찾았다 (want stem=%q, notes=%d건)", wantStem, len(notes))
	}
	if !norm.NFC.IsNormalString(got.Stem) {
		t.Errorf("Stem 이 NFC 로 정규화되지 않았다: %q", got.Stem)
	}
}

// TestWriteCreatesParentDirs 는 Write() 가 아직 없는 중첩 디렉토리를
// os.MkdirAll 로 만들어낸 뒤 정상적으로 쓰는지 확인한다.
// TestWriteThenRead 는 이미 존재하는 디렉토리에만 쓰기 때문에 이 경로가
// 그동안 테스트로 안 덮여 있었다.
func TestWriteCreatesParentDirs(t *testing.T) {
	l := fixtureLayout(t)
	notes, _, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	n := notes[0]

	dir, err := l.decisionsDir("alpha")
	if err != nil {
		t.Fatal(err)
	}
	n.Path = filepath.Join(dir, "new", "nested", "alpha-결정-새폴더-2026-08-06.md")
	n.Stem = "alpha-결정-새폴더-2026-08-06"

	if _, err := os.Stat(filepath.Dir(n.Path)); !os.IsNotExist(err) {
		t.Fatalf("사전조건 실패: 중첩 디렉토리가 이미 존재한다: %v", err)
	}

	if err := l.Write(n); err != nil {
		t.Fatalf("없는 부모 디렉토리 아래로 Write() 가 실패했다: %v", err)
	}

	again, err := l.Read(n.Path)
	if err != nil {
		t.Fatalf("MkdirAll 로 만들어진 경로를 Read() 하지 못했다: %v", err)
	}
	if again.Meta.Summary != n.Meta.Summary {
		t.Errorf("왕복 후 summary 가 변했다: got %q, want %q", again.Meta.Summary, n.Meta.Summary)
	}
}

// ★ **"못 읽었다" 만으로는 사람이 잘못 고친다.**
//
// 2026-08-21 에 실제로 그랬다. 집이 supersedes 를 다중값으로 올리자 회사의 옛 판이
// 그 노트를 파싱조차 못 해 건너뛰었고, 세션 시작은 "결정 노트 N건을 읽지 못해
// 회수에서 빠져 있다" 만 말했다. 파일을 열어 보면 YAML 은 멀쩡해 보인다 — 그래서
// 사람이 **옛 모양으로 되돌려** 다중값을 강등시켰다. 신호는 있었는데 무엇을 하라는
// 말이 없었던 것이 원인이다.
//
// 갈래는 에러가 알려 준다. **아는 키에 모르는 모양**(`cannot unmarshal !!seq into
// string`)은 구조가 멀쩡하다는 뜻이고, 그건 더 새 판이 쓴 것이다. 진짜 깨진 YAML은
// 파서가 스트림 단계에서 다르게 운다. (모르는 **키**는 Extra 가 흡수해 아예 에러가
// 안 난다 — 그래서 남는 것은 이 둘뿐이다.)
func TestSkippedNoteDistinguishesNewerShapeFromBroken(t *testing.T) {
	newer := []string{
		"frontmatter 파싱 실패: yaml: unmarshal errors:\n  line 2: cannot unmarshal !!seq into string",
		"frontmatter 파싱 실패: yaml: unmarshal errors:\n  line 9: cannot unmarshal !!map into string",
		"frontmatter 파싱 실패: yaml: unmarshal errors:\n  line 3: cannot unmarshal !!bool into []string",
	}
	for _, msg := range newer {
		if !(SkippedNote{Reason: errors.New(msg)}).LooksNewer() {
			t.Errorf("더 새 판 모양을 못 알아본다: %s", msg)
		}
	}

	broken := []string{
		"frontmatter 파싱 실패: yaml: line 2: found unexpected end of stream",
		"frontmatter 파싱 실패: yaml: line 2: mapping values are not allowed in this context",
		"frontmatter 가 없다 (--- 로 시작하지 않는다)",
		`결정 노트가 아니다 (type: "log")`,
		"open /x/y.md: permission denied",
	}
	for _, msg := range broken {
		if (SkippedNote{Reason: errors.New(msg)}).LooksNewer() {
			t.Errorf("진짜 깨진 것을 판 갈림으로 오진한다: %s", msg)
		}
	}

	// Reason 이 비면 판정할 근거가 없다. 없는 것을 있다고 하지 않는다.
	if (SkippedNote{}).LooksNewer() {
		t.Error("Reason 이 없는데 판 갈림이라고 한다")
	}
}
