package capture

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// 지시 1: 브리프의 appendRetrospective 는 "## 회고" 절이 이미 있어도 본문
// 맨 끝에 새 회고를 붙이는 버그가 있었다. 이 테스트는 그 절이 있는 노트에
// 회고를 두 번 붙였을 때, 두 번째 회고가 "## 회고" 절 *안에* 들어가고
// (엉뚱하게 본문 끝으로 밀려나지 않고), 뒤에 다른 절이 있으면 그 앞에
// 들어가는지 확인한다.
func TestAppendRetrospectiveInsertsIntoExistingSection(t *testing.T) {
	body := []byte("## 결정\n\n내용.\n\n## 회고\n\n첫 회고.\n\n## 부록\n\n부록 내용.\n")
	got := string(appendRetrospective(body, "두번째 회고."))

	// "## 부록" 앞, "## 회고" 절 안에 들어가야 한다.
	retroIdx := strings.Index(got, "## 회고")
	appendixIdx := strings.Index(got, "## 부록")
	secondIdx := strings.Index(got, "두번째 회고.")
	if retroIdx < 0 || appendixIdx < 0 || secondIdx < 0 {
		t.Fatalf("절 구조가 깨졌다:\n%s", got)
	}
	if !(retroIdx < secondIdx && secondIdx < appendixIdx) {
		t.Errorf("두번째 회고가 '## 회고' 절 안(부록 앞)에 있어야 하는데 순서가 다르다:\n%s", got)
	}
	// 첫 회고도 그대로 남아 있어야 한다.
	if !strings.Contains(got, "첫 회고.") {
		t.Errorf("첫 회고가 사라졌다:\n%s", got)
	}
	// 부록 절 내용이 온전해야 한다.
	if !strings.Contains(got, "## 부록\n\n부록 내용.") {
		t.Errorf("부록 절이 손상됐다:\n%s", got)
	}
}

// TestAppendRetrospectiveTwiceNoFollowingSection 은 뒤에 다른 절이 없는
// 경우(가장 흔한 경우 — capture 의 기본 본문 템플릿처럼 "## 회고" 가 마지막
// 절인 노트)에도 두 번째 회고가 같은 절 안에, 첫 회고 뒤에 붙는지 본다.
func TestAppendRetrospectiveTwiceNoFollowingSection(t *testing.T) {
	body := []byte("## 결정\n\n내용.\n\n## 회고\n\n첫 회고.\n")
	got := string(appendRetrospective(body, "두번째 회고."))

	want := "## 결정\n\n내용.\n\n## 회고\n\n첫 회고.\n\n두번째 회고.\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// 지시 2: Review() 는 supersedes 처리에서 옛 노트를 먼저 쓴 뒤 새 노트를
// 검증하면, 새 노트 검증이 실패했을 때 옛 노트만 superseded 로 남는다.
// 이 테스트는 새 노트 쪽에 스키마 위반(허용값 밖 outcome)을 함께 주고,
// Review() 가 실패한 뒤에도 옛 노트 파일이 전혀 바뀌지 않았는지 — 즉 두
// 노트를 모두 검증한 뒤에야 쓰기 시작하는지 — 확인한다.
func TestReviewValidatesBothNotesBeforeWritingEither(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	newStem := "alpha-결정-스키마-2026-08-02"
	oldStem := "alpha-결정-저장엔진-2026-08-01"

	oldPath, err := l.ResolveStem(oldStem)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}

	err = Review(l, ReviewRequest{
		Stem: newStem, Supersedes: oldStem, Outcome: "허용값-밖",
	})
	if err == nil {
		t.Fatal("허용값 밖 outcome 인데 Review() 가 성공했다")
	}

	after, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("새 노트 검증이 실패했는데 옛 노트가 이미 쓰였다(부분 실패):\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if strings.Contains(string(after), "status: superseded") {
		t.Errorf("옛 노트가 superseded 로 바뀌었다 — 새 노트 검증 실패 전에 이미 쓰였다는 뜻이다:\n%s", after)
	}
}

// 지시 3: 아무 필드도 채우지 않은 요청(Stem 만 있음)은 파일을 정본 형식으로
// 재작성할 뿐, 의미 있는 변경은 없어야 한다 — Meta 값도 본문도 그대로여야
// 하고, frontmatter 만 정본 형식(예: tags 콤마 뒤 공백)으로 정규화된다.
func TestReviewNoOpNormalizesOnly(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	stem := "alpha-결정-저장엔진-2026-08-01"
	p, err := l.ResolveStem(stem)
	if err != nil {
		t.Fatal(err)
	}
	before, err := l.Read(p)
	if err != nil {
		t.Fatal(err)
	}

	if err := Review(l, ReviewRequest{Stem: stem}); err != nil {
		t.Fatalf("빈 요청인데 실패: %v", err)
	}

	after, err := l.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Meta, after.Meta) {
		t.Errorf("의미 변화가 없어야 하는데 메타가 바뀌었다:\nbefore=%+v\nafter=%+v", before.Meta, after.Meta)
	}
	if string(before.Body) != string(after.Body) {
		t.Errorf("의미 변화가 없어야 하는데 본문이 바뀌었다:\nbefore=%q\nafter=%q", before.Body, after.Body)
	}

	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// 원본 픽스처는 "tags: [decision,alpha,저장엔진]" (콤마 뒤 공백 없음).
	// 정본 방출기(EmitFrontmatter)는 ", " 로 join 한다.
	if !strings.Contains(string(raw), "tags: [decision, alpha, 저장엔진]") {
		t.Errorf("정본 형식으로 정규화되지 않았다:\n%s", raw)
	}
}

// 지시 5(실은 4): appendUnique 는 정확 일치만 본다. 같은 stem 을 두 번
// supersedes 해도 옛 노트의 related 에 같은 위키링크가 중복으로 쌓이지
// 않는지 확인한다.
func TestReviewSupersedesTwiceKeepsRelatedUnique(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	newStem := "alpha-결정-스키마-2026-08-02"
	oldStem := "alpha-결정-저장엔진-2026-08-01"

	if err := Review(l, ReviewRequest{Stem: newStem, Supersedes: oldStem}); err != nil {
		t.Fatal(err)
	}
	if err := Review(l, ReviewRequest{Stem: newStem, Supersedes: oldStem}); err != nil {
		t.Fatal(err)
	}

	p, err := l.ResolveStem(oldStem)
	if err != nil {
		t.Fatal(err)
	}
	n, err := l.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range n.Meta.Related {
		if r == "[["+newStem+"]]" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("related 에 같은 링크가 %d번 들어갔다(중복 제거 실패): %v", count, n.Meta.Related)
	}
}
