package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

func TestBuild(t *testing.T) {
	l := fixtureLayout(t) // fixture_test.go 의 헬퍼
	out, res, err := Build(l)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "| 날짜 | domain | summary | status | outcome | 링크 |") {
		t.Errorf("헤더가 없다:\n%s", s)
	}
	if res.Rows != 4 {
		t.Errorf("행 %d개, want 4:\n%s", res.Rows, s)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("정상 픽스처인데 건너뛴 노트가 있다: %+v", res.Skipped)
	}
	// 최신순 정렬
	iNew := strings.Index(s, "2026-08-04")
	iOld := strings.Index(s, "2026-08-01")
	if iNew < 0 || iOld < 0 || iNew > iOld {
		t.Errorf("최신순 정렬이 아니다:\n%s", s)
	}
}

// TestBuildEscapesPipeAndNewlineInSummary 는 summary 안의 파이프와 개행이
// 색인 표의 열을 밀어내지 않는지 확인한다.
//
// 브리프 원안의 검사(strings.Contains(s, "임베디드 DB | 로"))는 픽스처
// summary 에 파이프가 아예 없어서 항상 통과하는 무의미한 검사였다. 여기서는
// 파이프와 개행을 실제로 포함한 노트를 만들어 그 행을 뽑아낸 뒤, 이스케이프된
// 파이프(`\|`)를 markdown 표 파서처럼 구분자가 아닌 것으로 취급하고 나서
// strings.Split 결과가 정확히 6개 열(경계의 빈 문자열 2개 포함 8파트)인지
// 검사한다. 이스케이프를 고려하지 않고 그냥 "|" 로만 쪼개면, 이스케이프가
// 제대로 됐는지 여부와 무관하게 원본 파이프 개수만큼 항상 더 쪼개져서 이
// 테스트가 지키려는 불변식을 검증할 수 없다.
func TestBuildEscapesPipeAndNewlineInSummary(t *testing.T) {
	l := fixtureLayout(t)

	path, err := l.DecisionPath("alpha", "파이프이스케이프", "2026-08-05")
	if err != nil {
		t.Fatal(err)
	}
	n := store.Note{
		Path: path,
		Meta: store.Meta{
			Type:    "decision",
			Date:    "2026-08-05",
			Domain:  []string{"alpha"},
			Summary: "성능 | 속도 우선\n두 번째 줄은 참고용",
			Status:  "active",
			Outcome: "pending",
		},
		Body: []byte("## 결정\n\n파이프 이스케이프 테스트.\n"),
	}
	if err := l.Write(n); err != nil {
		t.Fatal(err)
	}

	out, res, err := Build(l)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows != 5 {
		t.Fatalf("행 %d개, want 5 (기존 4 + 방금 추가한 1):\n%s", res.Rows, out)
	}
	s := string(out)

	var row string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "| 2026-08-05") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("2026-08-05 행을 못 찾았다:\n%s", s)
	}
	// row 는 strings.Split(s, "\n") 의 원소라 정의상 "\n" 을 포함할 수 없다
	// — strings.Contains(row, "\n") 검사는 절대 발동하지 않는 죽은
	// 단언이었다. 의미 있게 확인해야 할 것은 개행이 "사라진" 게 아니라
	// "이스케이프돼 같은 행에 남아있는" 것이다: escapeCell 이 개행을
	// 스페이스로 바꿔치기만 하므로, summary 원문의 개행 앞부분과 뒷부분이
	// 모두 같은 한 행(row) 안에 들어있어야 한다.
	if !strings.Contains(row, "성능") || !strings.Contains(row, "속도 우선") {
		t.Errorf("개행 앞부분이 행에 없다: %q", row)
	}
	if !strings.Contains(row, "두 번째 줄은 참고용") {
		t.Errorf("개행 뒷부분이 같은 행에 없다(개행이 이스케이프되지 않았을 수 있다): %q", row)
	}

	// 이스케이프된 파이프(백슬래시+파이프)를 지워, markdown 표 파서가 그걸
	// 구분자로 보지 않는 동작을 흉내낸다. 그 다음 순수 구조적 파이프만으로
	// 쪼개 열 개수를 센다.
	unescaped := strings.ReplaceAll(row, `\|`, "")
	cols := strings.Split(unescaped, "|")
	if len(cols) != 8 { // 시작/끝 경계의 빈 문자열 2개 + 실제 6개 열
		t.Errorf("이스케이프에도 불구하고 열이 밀렸다: %d 파트\n원본 행: %s", len(cols), row)
	}
}

// TestBuildReportsSkippedNotes 는 읽지 못한 노트가 색인에서 빠질 때 그 사실이
// Result.Skipped 로 나오는지 본다.
//
// 예전에는 Build 가 행 수만 돌려줬다. 그래서 실볼트 53건 중 6건이 구 스키마라
// 파싱에서 거부됐는데도 "47행 생성" 이라는 참말 하나만 나가고, 그 47이 전부가
// 아니라는 사실은 어디에도 안 남았다 — 스펙 §1.3 이 셸의 죄목으로 든 "조용히
// 데이터를 잃는다" 그대로다.
func TestBuildReportsSkippedNotes(t *testing.T) {
	l, vault := fixtureLayoutVault(t)
	dir := filepath.Join(vault, "alpha", "decisions")

	// 다른 도구가 남긴 구 스키마 — 잉여 키 때문에 파싱에서 떨어진다.
	old := filepath.Join(dir, "alpha-결정-구스키마-2026-01-02.md")
	body := "---\ntitle: 구 스키마로 쓰인 결정\nproject: alpha\ncreated: 2026-01-02\nsuperseded-by: \"\"\n---\n\n## 결정\n\n옛 도구가 남긴 형식이다.\n"
	if err := os.WriteFile(old, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, res, err := Build(l)
	if err != nil {
		t.Fatalf("구 스키마 한 건 때문에 Build 가 죽으면 안 된다: %v", err)
	}
	if res.Rows != 4 {
		t.Errorf("행 %d개, want 4 — 구 스키마 노트는 색인에 들어가면 안 된다", res.Rows)
	}
	if strings.Contains(string(out), "구스키마") {
		t.Errorf("파싱 실패한 노트가 색인에 들어갔다:\n%s", out)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Path != old {
		t.Fatalf("건너뛴 노트가 보고되지 않았다: %+v", res.Skipped)
	}
	if res.Skipped[0].Reason == nil {
		t.Error("건너뛴 원인이 비었다 — 사용자가 무엇을 고쳐야 할지 알 수 없다")
	}
}

// TestWriteReportsSkippedNotes 는 Write 가 Build 의 결과(행 수·건너뜀)를 그대로
// 넘기고, 건너뛴 게 있어도 색인 파일 자체는 쓰는지 본다 — 47행짜리 색인이
// 색인 없음보다는 낫다.
func TestWriteReportsSkippedNotes(t *testing.T) {
	l, vault := fixtureLayoutVault(t)
	dir := filepath.Join(vault, "alpha", "decisions")
	old := filepath.Join(dir, "alpha-결정-구스키마-2026-01-02.md")
	if err := os.WriteFile(old, []byte("---\ntitle: 구 스키마\nproject: alpha\n---\n\n본문\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Write(l)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows != 4 {
		t.Errorf("행 %d개, want 4", res.Rows)
	}
	if len(res.Skipped) != 1 {
		t.Fatalf("건너뛴 노트 %d건, want 1: %+v", len(res.Skipped), res.Skipped)
	}
	if _, err := os.Stat(l.IndexPath()); err != nil {
		t.Errorf("건너뛴 게 있다고 색인을 아예 안 썼다: %v", err)
	}
}

// 색인 머리의 요약 줄. **`아쉬운 결과` 가 이 줄의 존재 이유다** — 뒤집혔거나 나쁘게
// 끝난 결정이 몇 건인지가 표를 끝까지 읽지 않아도 보여야 한다.
//
// 셸 구현에는 있었는데 이관하면서 빠졌고, 실볼트 컷오버 대조에서 발견했다.
func TestIndexHeaderCountsOutcomes(t *testing.T) {
	got, res, err := Build(fixtureLayout(t))
	if err != nil {
		t.Fatal(err)
	}
	// 픽스처 4건: alpha-스키마가 superseded, common-로케일함정이 outcome bad.
	want := "전체 4건 · active 3건 · 아쉬운 결과(regretted/bad) 1건"
	if !strings.Contains(string(got), want) {
		lines := strings.Split(string(got), "\n")
		if len(lines) > 12 {
			lines = lines[:12]
		}
		t.Errorf("요약 줄이 없거나 틀리다. want %q\n--- 머리 ---\n%s", want, strings.Join(lines, "\n"))
	}
	if res.Rows != 4 {
		t.Errorf("Rows = %d, 4여야 한다", res.Rows)
	}
}

// 같은 볼트를 두 번 색인하면 **바이트가 같아야 한다.** 생성 시각을 넣으면 이게 깨진다.
func TestIndexIsDeterministic(t *testing.T) {
	l := fixtureLayout(t)
	a, _, err := Build(l)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := Build(l)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Error("같은 볼트인데 두 번의 색인이 다르다 — 멱등성이 깨졌다")
	}
}

// 볼트 산출물은 설정 언어를 따른다. **결정 노트의 본문 언어와는 별개다** —
// 본문은 대화의 언어를 따르므로 한 볼트에 여러 언어가 섞일 수 있다.
func TestIndexHeaderFollowsLang(t *testing.T) {
	c := testutil.VaultConfig(t)
	c.Lang = "en"
	got, _, err := Build(store.NewLayout(c))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, want := range []string{"# Decision index", "Do not edit", "total ·"} {
		if !strings.Contains(s, want) {
			t.Errorf("영어 색인에 %q 가 없다:\n%s", want, headOf(s, 12))
		}
	}
	if strings.Contains(s, "결정 색인") {
		t.Errorf("lang=en 인데 한국어가 남았다:\n%s", headOf(s, 12))
	}
}

// headOf 는 실패 메시지에 색인 머리말만 싣는다. 표 전체를 찍으면 무엇이 틀렸는지 안 보인다.
func headOf(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
