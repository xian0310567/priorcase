package capture

import (
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/i18n"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

const (
	oldStem = "alpha-결정-저장엔진-2026-08-01"
	newStem = "alpha-결정-스키마-2026-08-02"
)

func readStem(t *testing.T, l *store.Layout, stem string) store.Note {
	t.Helper()
	p, err := l.ResolveStem(stem)
	if err != nil {
		t.Fatal(err)
	}
	n, err := l.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// ★★ **번복 이유가 옛 노트에 남아야 한다.**
//
// 이 시그니처에는 원래 이유를 받는 인자가 없었다. supersede() 가 옛 노트에 하던
// 일은 status="superseded" 와 related 링크 한 줄이 전부여서, "무엇이 뒤집었는가" 는
// 남고 "왜" 는 한 글자도 안 남았다. 실측: 실볼트 18노트 중 번복 사유 기록 0건.
//
// 세 자리를 다 본다 — 셋 다 독자가 다르기 때문이다(markOverturned 주석 참고).
func TestCaptureSupersedeRecordsReason(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	const reason = "시스템 gitconfig 의 osxkeychain 이 helper 목록에 누적돼 push 가 403 이 됐다"

	if _, err := Do(l, c, Request{
		Domain: "alpha", Slug: "저장엔진 재선정", Summary: "저장 엔진을 다시 고른다",
		Date: "2026-08-09", Supersedes: oldStem, SupersedeReason: reason,
		Body: []byte("## 결정\n"),
	}); err != nil {
		t.Fatal(err)
	}

	old := readStem(t, l, oldStem)
	if old.Meta.SupersededReason != reason {
		t.Errorf("frontmatter superseded_reason = %q, want %q", old.Meta.SupersededReason, reason)
	}
	if !strings.Contains(old.Meta.Summary, "osxkeychain") {
		t.Errorf("summary 에 번복 이유가 없다: %q\n"+
			"회수는 head(stem+summary+tags)에만 히트를 세므로 여기 없으면 검색이 안 된다", old.Meta.Summary)
	}
	if !strings.Contains(old.Meta.Summary, "저장 엔진을 임베디드 DB 로 고른다") {
		t.Errorf("원래 summary 가 지워졌다: %q", old.Meta.Summary)
	}
	body := string(old.Body)
	if !strings.Contains(body, reason) {
		t.Errorf("회고 절에 온전한 이유가 없다:\n%s", body)
	}
	if !strings.Contains(body, "2026-08-09") {
		t.Errorf("회고 줄에 번복 날짜가 없다:\n%s", body)
	}
	if !strings.Contains(body, "[[alpha-결정-저장엔진-재선정-2026-08-09]]") {
		t.Errorf("회고 줄에 뒤집은 결정 링크가 없다:\n%s", body)
	}
	// 새 노트에는 안 붙는다 — 사유는 옛 결정의 성질이다.
	nw := readStem(t, l, "alpha-결정-저장엔진-재선정-2026-08-09")
	if nw.Meta.SupersededReason != "" {
		t.Errorf("뒤집는 쪽 노트에 사유가 붙었다: %q", nw.Meta.SupersededReason)
	}
}

// ★★★ **이 테스트가 배치를 정당화한다.**
//
// core/search 의 scoreAll 은 head 를 `stem + summary + contentTags` 로만 만들고,
// head 히트가 0이면 `continue` 로 노트를 통째로 버린다(search.go:116). 즉 번복
// 이유를 본문이나 frontmatter 새 키에만 넣으면, 그 낱말로는 그 결정이 영영 안
// 잡힌다 — bodyHits 가 아무리 많아도 소용없다.
//
// 그래서 summary 에 꼬리표를 붙인다. 이 테스트는 "이유에만 나오는 낱말"로 회수가
// 되는지를 확인한다. 배치를 본문 전용으로 되돌리면 여기서 깨진다.
//
// **실측으로 알게 된 것 (2026-08-19):** 뒤집힌 노트는 head 히트가 **둘은 있어야**
// 회수에 뜬다. weightHead=3, penaltySuperseded=5 라 히트 하나면 3-5=-2 이고
// scoreAll 의 `score > 0` 에서 걸러진다. 아래에서 그 산수를 그대로 못 박는다 —
// 즉 이유를 summary 에 넣는 것은 **필요조건이지 충분조건이 아니다**. 가중치 쪽
// 조정은 core/search 의 몫이라 여기서 손대지 않았다.
func TestSupersedeReasonIsRecallable(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	// 볼트 어디에도 없는 낱말을 쓴다 — 이유를 통해서만 걸릴 수 있게.
	const reason = "osxkeychain 이 만료된 서명을 먼저 내줬다"

	if _, err := Do(l, c, Request{
		Domain: "alpha", Slug: "저장엔진 재선정", Summary: "저장 엔진을 다시 고른다",
		Date: "2026-08-09", Supersedes: oldStem, SupersedeReason: reason,
		Body: []byte("## 결정\n"),
	}); err != nil {
		t.Fatal(err)
	}
	recalls := func(q string) bool {
		hits, _, err := search.Recall(l, c, q, search.Options{CrossProject: true, Limit: 5})
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hits {
			if h.Note.Stem == oldStem {
				return true
			}
		}
		return false
	}

	if !recalls("osxkeychain 서명") {
		t.Fatal("번복 이유에만 있는 낱말로 옛 결정을 회수하지 못했다 — " +
			"이유가 head 밖에 있으면 scoreAll 이 노트를 통째로 버린다")
	}
	// 이유가 본문에만 있었다면 이것도 못 찾는다. head 에 넣었기 때문에 찾는다.
	if recalls("osxkeychain") {
		t.Log("히트 하나로도 떴다 — penaltySuperseded 가 낮아졌다면 이 주석을 고쳐라")
	}
}

// TestOverturnMarkIsStrippable 은 **붙이는 쪽과 떼는 쪽이 어긋나지 않는지** 본다.
//
// 꼬리표 리터럴이 두 자리에 있다(overturnMark 의 T() 인자, overturnMarks 슬라이스).
// 나눈 것은 internal/arch 의 T() 상수 검사 때문이고, 어긋남은 주석이 아니라 이
// 테스트가 막는다. 어긋나면 같은 노트를 두 번 손댈 때 꼬리표가 겹쳐 붙어 summary 가
// 번복 이유로 도배된다.
func TestOverturnMarkIsStrippable(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		lang i18n.Lang
	}{
		{"ko", []byte("## 결정\n"), i18n.KO},
		{"en-by-lang", []byte("## 결정\n"), i18n.EN},
		{"en-by-body", []byte("## Decision\n"), i18n.KO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const base = "원래 요약"
			marked := base + overturnMark(tc.body, tc.lang) + "이유"
			if got := stripOverturnMark(marked); got != base {
				t.Fatalf("꼬리표를 못 떼어냈다: %q → %q, want %q", marked, got, base)
			}
			// 두 번 붙여도 하나만 남는다.
			twice := stripOverturnMark(marked) + overturnMark(tc.body, tc.lang) + "다른 이유"
			if strings.Count(twice, "이유") != 1 {
				t.Errorf("꼬리표가 겹쳤다: %q", twice)
			}
		})
	}
}

// TestSupersedeSummaryClipsLongReason 은 긴 이유가 회수 블록을 잡아먹지 않는지 본다.
// 잘린 나머지는 안 잃는다 — frontmatter 정본과 회고 본문에 온전히 남는다.
func TestSupersedeSummaryClipsLongReason(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	reason := strings.Repeat("가", 300)

	if _, err := Do(l, c, Request{
		Domain: "alpha", Slug: "긴 이유", Summary: "다시 정한다",
		Date: "2026-08-09", Supersedes: oldStem, SupersedeReason: reason,
		Body: []byte("## 결정\n"),
	}); err != nil {
		t.Fatal(err)
	}
	old := readStem(t, l, oldStem)
	if n := len([]rune(old.Meta.Summary)); n > 200 {
		t.Errorf("summary 가 %d자다 — 회수 주입 한 줄이 감당 못 한다", n)
	}
	if !strings.HasSuffix(old.Meta.Summary, "…") {
		t.Errorf("잘렸는데 표시가 없다: %q", old.Meta.Summary)
	}
	if old.Meta.SupersededReason != reason {
		t.Errorf("frontmatter 정본이 잘렸다: %d자, want %d자",
			len([]rune(old.Meta.SupersededReason)), len([]rune(reason)))
	}
	if !strings.Contains(string(old.Body), reason) {
		t.Error("회고 본문에 온전한 이유가 없다")
	}
}

// TestSupersedeReasonIsFoldedToOneLine 은 여러 줄 이유가 방출기를 죽이지 않는지 본다.
// store.quote 는 스칼라가 접히면 panic 한다 — frontmatter 로는 한 줄만 가야 한다.
func TestSupersedeReasonIsFoldedToOneLine(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	const reason = "첫 줄\n둘째 줄\n\n셋째 줄"

	if _, err := Do(l, c, Request{
		Domain: "alpha", Slug: "여러 줄 이유", Summary: "다시 정한다",
		Date: "2026-08-09", Supersedes: oldStem, SupersedeReason: reason,
		Body: []byte("## 결정\n"),
	}); err != nil {
		t.Fatal(err)
	}
	old := readStem(t, l, oldStem)
	if strings.ContainsAny(old.Meta.SupersededReason, "\r\n") {
		t.Errorf("frontmatter 에 줄바꿈이 들어갔다: %q", old.Meta.SupersededReason)
	}
	if strings.ContainsAny(old.Meta.Summary, "\r\n") {
		t.Errorf("summary 에 줄바꿈이 들어갔다: %q", old.Meta.Summary)
	}
	// 본문은 원문 그대로 — 줄바꿈이 살아 있어야 측정 결과·명령어를 붙일 수 있다.
	if !strings.Contains(string(old.Body), "첫 줄\n둘째 줄") {
		t.Errorf("본문에서 줄바꿈이 사라졌다:\n%s", old.Body)
	}
}

// TestSupersedeWithoutReasonUnchanged 는 **이유 없는 뒤집기가 예전 그대로**인지 본다.
// 강제하면 아직 이 인자를 안 넘기는 호출부(CLI·MCP)에서 뒤집기가 통째로 막힌다.
func TestSupersedeWithoutReasonUnchanged(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	before := readStem(t, l, oldStem)

	if _, err := Do(l, c, Request{
		Domain: "alpha", Slug: "이유 없는 뒤집기", Summary: "다시 정한다",
		Date: "2026-08-09", Supersedes: oldStem, Body: []byte("## 결정\n"),
	}); err != nil {
		t.Fatal(err)
	}
	old := readStem(t, l, oldStem)
	if old.Meta.SupersededReason != "" {
		t.Errorf("이유를 안 줬는데 키가 생겼다: %q", old.Meta.SupersededReason)
	}
	if old.Meta.Summary != before.Meta.Summary {
		t.Errorf("이유를 안 줬는데 summary 가 변했다: %q → %q", before.Meta.Summary, old.Meta.Summary)
	}
	if string(old.Body) != string(before.Body) {
		t.Errorf("이유를 안 줬는데 본문이 변했다:\n%s", old.Body)
	}
}

// TestReviewSupersedeReasonLandsOnOldNote 는 review 경로에도 같은 자리가 있는지 본다.
func TestReviewSupersedeReasonLandsOnOldNote(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	const reason = "실측에서 쓰기 지연이 4배로 나왔다"

	if _, err := Review(l, ReviewRequest{
		Stem: newStem, Supersedes: oldStem, SupersedeReason: reason,
	}); err != nil {
		t.Fatal(err)
	}
	old := readStem(t, l, oldStem)
	if old.Meta.SupersededReason != reason {
		t.Errorf("옛 노트 superseded_reason = %q, want %q", old.Meta.SupersededReason, reason)
	}
	if !strings.Contains(old.Meta.Summary, "4배") {
		t.Errorf("옛 노트 summary 에 이유가 없다: %q", old.Meta.Summary)
	}
	if got := readStem(t, l, newStem); got.Meta.SupersededReason != "" {
		t.Errorf("뒤집는 쪽에 사유가 붙었다: %q", got.Meta.SupersededReason)
	}
}

// ★ **대체할 새 결정 없는 번복이 더 흔하다.**
//
// 측정으로 가정이 깨졌을 때는 "그냥 그만둔다" 로 끝난다 — 뒤집는 새 결정 노트가
// 없으니 --supersedes 를 쓸 수 없고, 예전에는 그 이유를 담을 자리가 아예 없었다.
func TestReviewSelfOverturn(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	const reason = "임베디드 DB 로는 다중 프로세스 쓰기가 안 된다는 것이 실측으로 드러났다"

	if _, err := Review(l, ReviewRequest{
		Stem: oldStem, Status: "regretted", Outcome: "bad", SupersedeReason: reason,
	}); err != nil {
		t.Fatal(err)
	}
	n := readStem(t, l, oldStem)
	if n.Meta.SupersededReason != reason {
		t.Errorf("superseded_reason = %q", n.Meta.SupersededReason)
	}
	if !strings.Contains(n.Meta.Summary, "다중 프로세스") {
		t.Errorf("summary 에 이유가 없다: %q", n.Meta.Summary)
	}
	if !strings.Contains(string(n.Body), reason) {
		t.Errorf("회고 절에 이유가 없다:\n%s", n.Body)
	}
	// 대체 노트가 없으므로 링크 문구가 들어가면 안 된다.
	if strings.Contains(string(n.Body), "[[]]") {
		t.Errorf("빈 링크가 본문에 들어갔다:\n%s", n.Body)
	}
}

// TestReviewSelfOverturnRequiresStatus 는 "이유는 있는데 여전히 active" 인 노트가
// 만들어지지 않게 막는지 본다. 그 상태가 회수에서 최악이다 — 뒤집힌 결정이
// 감점(search.penaltySuperseded) 없이 만점으로 계속 올라온다.
func TestReviewSelfOverturnRequiresStatus(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	before := readStem(t, l, oldStem)

	_, err := Review(l, ReviewRequest{Stem: oldStem, SupersedeReason: "그냥 접는다"})
	if err == nil {
		t.Fatal("status 를 안 바꾸고 번복 사유만 준 호출을 통과시켰다")
	}
	if !strings.Contains(err.Error(), "--status") {
		t.Errorf("무엇을 더 줘야 하는지 에러가 안 알려 준다: %v", err)
	}
	if after := readStem(t, l, oldStem); after.Meta.Summary != before.Meta.Summary {
		t.Errorf("거부됐는데 파일이 변했다: %q", after.Meta.Summary)
	}
}

// ★★ **갈아치운 summary 를 버리지 않는다.**
//
// 실측 문제: 볼트의 codecommit 노트가 outcome:bad 인데 summary 는 아직 뒤집힌
// 결론을 말하고 있었다 — 회수에 주입되는 유일한 한 줄이 거짓말을 한 것이다.
// 고칠 수 있어야 하고, 동시에 "한때 무엇을 믿었는가" 도 남아야 한다.
//
// 옛 줄은 frontmatter 로 간다. 본문에 두면 summary 만 고치러 온 호출이 본문을
// 건드리게 되고(TestReviewCanCorrectSummary 의 계약), head 에서도 빠져야 폐기한
// 문장이 계속 검색에 걸리지 않는다.
func TestReviewKeepsFormerSummary(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	before := readStem(t, l, oldStem)

	if _, err := Review(l, ReviewRequest{Stem: oldStem, Summary: "저장 엔진은 서버 DB 로 간다"}); err != nil {
		t.Fatal(err)
	}
	n := readStem(t, l, oldStem)
	if len(n.Meta.SummaryHistory) != 1 || n.Meta.SummaryHistory[0] != before.Meta.Summary {
		t.Fatalf("옛 summary 가 보존되지 않았다: %v", n.Meta.SummaryHistory)
	}

	// 두 번째 갱신은 뒤에 쌓인다. 오래된 것이 앞이다.
	if _, err := Review(l, ReviewRequest{Stem: oldStem, Summary: "세 번째 결론"}); err != nil {
		t.Fatal(err)
	}
	got := readStem(t, l, oldStem).Meta.SummaryHistory
	if len(got) != 2 || got[1] != "저장 엔진은 서버 DB 로 간다" {
		t.Fatalf("이력이 누적되지 않았다: %v", got)
	}

	// 같은 값으로 다시 고치면 이력이 늘지 않는다 — 재실행이 이력을 부풀리면 안 된다.
	if _, err := Review(l, ReviewRequest{Stem: oldStem, Summary: "세 번째 결론"}); err != nil {
		t.Fatal(err)
	}
	if again := readStem(t, l, oldStem).Meta.SummaryHistory; len(again) != 2 {
		t.Errorf("같은 값 재갱신이 이력을 늘렸다: %v", again)
	}
}

func TestOneLineAndClipRunes(t *testing.T) {
	if got := oneLine(" 첫\n둘\t\t셋  넷 "); got != "첫 둘 셋 넷" {
		t.Errorf("oneLine = %q", got)
	}
	// 룬 단위로 잘라야 한다 — 바이트로 자르면 한글이 반 토막 난다.
	got := clipRunes(strings.Repeat("가", 10), 4)
	if got != "가가가가…" {
		t.Errorf("clipRunes = %q", got)
	}
	if got := clipRunes("짧다", 10); got != "짧다" {
		t.Errorf("짧은 문자열을 건드렸다: %q", got)
	}
}
