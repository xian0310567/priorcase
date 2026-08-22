package store

import (
	"bytes"
	"strings"
	"testing"
)

const noteA = `---
type: decision
date: 2026-08-01
domain: [omni]
summary: "저장 엔진은 OPFS + SQLite 로 간다"
status: active
outcome: pending
supersedes: ""
related: ["[[omni-결정-데이터모델-2026-08-01]]"]
tags: [decision,omni,저장엔진]
source_session: ""
---

## 결정

OPFS 위의 SQLite 를 쓴다.
`

func TestParseFrontmatter(t *testing.T) {
	m, body, err := ParseFrontmatter([]byte(noteA))
	if err != nil {
		t.Fatalf("ParseFrontmatter() error = %v", err)
	}
	if m.Type != "decision" || m.Date != "2026-08-01" {
		t.Errorf("type/date = %q/%q", m.Type, m.Date)
	}
	if len(m.Domain) != 1 || m.Domain[0] != "omni" {
		t.Errorf("domain = %v", m.Domain)
	}
	if m.Summary != "저장 엔진은 OPFS + SQLite 로 간다" {
		t.Errorf("summary = %q", m.Summary)
	}
	if len(m.Tags) != 3 || m.Tags[2] != "저장엔진" {
		t.Errorf("tags = %v — 공백 없는 콤마 형식을 못 읽었다", m.Tags)
	}
	if !bytes.Contains(body, []byte("OPFS 위의 SQLite")) {
		t.Errorf("body 가 잘못 잘렸다: %q", body)
	}
}

func TestParseRejectsMissingFrontmatter(t *testing.T) {
	if _, _, err := ParseFrontmatter([]byte("# 그냥 마크다운\n")); err == nil {
		t.Fatal("frontmatter 없는 문서를 통과시켰다")
	}
}

// 정본형 멱등성: 기존 볼트와의 바이트 동일은 불가능하지만(볼트에 두 형식이 섞여 있다),
// 정본형으로 한 번 방출한 뒤로는 반드시 안정적이어야 한다.
func TestEmitIsIdempotent(t *testing.T) {
	m1, body1, err := ParseFrontmatter([]byte(noteA))
	if err != nil {
		t.Fatal(err)
	}
	once := EmitNote(m1, body1)

	m2, body2, err := ParseFrontmatter(once)
	if err != nil {
		t.Fatalf("자기 출력을 다시 못 읽는다: %v", err)
	}
	twice := EmitNote(m2, body2)

	if !bytes.Equal(once, twice) {
		t.Errorf("멱등하지 않다\n--- 1회 ---\n%s\n--- 2회 ---\n%s", once, twice)
	}
}

func TestEmitKeyOrderAndFormat(t *testing.T) {
	m := Meta{
		Type: "decision", Date: "2026-08-07", Domain: []string{"priorcase"},
		Summary: `따옴표 " 와 백슬래시 \ 가 든 요약`, Status: "active", Outcome: "pending",
		Related: []string{"[[a]]", "[[b]]"}, Tags: []string{"decision", "go"},
	}
	got := string(EmitFrontmatter(m))

	wantOrder := []string{"type:", "date:", "domain:", "summary:", "status:",
		"outcome:", "supersedes:", "related:", "tags:", "source_session:"}
	pos := -1
	for _, k := range wantOrder {
		i := strings.Index(got, "\n"+k)
		if i < 0 {
			t.Fatalf("키 %q 가 없다:\n%s", k, got)
		}
		if i <= pos {
			t.Fatalf("키 순서가 틀렸다 (%q):\n%s", k, got)
		}
		pos = i
	}
	if !strings.Contains(got, "tags: [decision, go]") {
		t.Errorf("정본 배열 형식(콤마+공백)이 아니다:\n%s", got)
	}
	if !strings.Contains(got, `supersedes: ""`) {
		t.Errorf("빈 supersedes 가 \"\" 로 안 나왔다:\n%s", got)
	}
	// 이스케이프는 yaml 에 맡긴다 — 손으로 짜면 반드시 틀린다
	rt, _, err := ParseFrontmatter(append(EmitFrontmatter(m), []byte("\n본문\n")...))
	if err != nil {
		t.Fatal(err)
	}
	if rt.Summary != m.Summary {
		t.Errorf("이스케이프 왕복 실패: %q != %q", rt.Summary, m.Summary)
	}
}

func TestEmitEmptyRelated(t *testing.T) {
	got := string(EmitFrontmatter(Meta{Type: "decision"}))
	if !strings.Contains(got, "related: []") {
		t.Errorf("빈 배열이 [] 로 안 나왔다:\n%s", got)
	}
}

// --- 지시사항 3: KnownFields(true) — 10키 밖의 잉여 키는 에러가 나야 한다 ---

// ★ 계약이 뒤집혔다 (2026-08-09). 예전에는 잉여 키를 **거부**했다 — "조용히 버리지
// 않는다" 는 뜻이었는데, 버리지 않는 대신 **읽기를 포기해서** 사용자가 Obsidian 에서
// `aliases:` 한 줄만 넣어도 그 결정이 색인·회수·review 에서 통째로 사라졌다.
//
// 이제 받아서 되쓴다. 버리지도, 잃지도 않는다.
//
// "우리 노트인가" 판정은 이 함수가 하지 않는다 — `Layout.readNote` 가 `type: decision`
// 표식으로 가른다. 파서는 파싱만 한다.
func TestParseFrontmatterKeepsUnknownField(t *testing.T) {
	const withExtra = `---
type: decision
date: 2026-08-01
domain: [omni]
summary: "요약"
status: active
outcome: pending
supersedes: ""
related: []
tags: [decision]
source_session: ""
extra_field: "사용자가 넣은 키"
---

본문
`
	m, _, err := ParseFrontmatter([]byte(withExtra))
	if err != nil {
		t.Fatalf("잉여 키 하나에 노트를 통째로 못 읽는다: %v", err)
	}
	if len(m.Extra) != 1 {
		t.Fatalf("잉여 키가 Extra 로 안 왔다: %v", m.Extra)
	}
	if !strings.Contains(string(EmitFrontmatter(m)), `extra_field: "사용자가 넣은 키"`) {
		t.Errorf("잉여 키가 되쓰이지 않았다:\n%s", EmitFrontmatter(m))
	}
}

// --- 지시사항 2: quote() 의 접힘 트립와이어 — 아주 긴 스칼라도 한 줄이어야 한다 ---

func TestQuoteDoesNotFoldLongScalar(t *testing.T) {
	// 5,000자 한글 summary. 접히면 quote() 가 panic 한다 — 이 테스트는
	// panic 없이 완주하고, 방출 결과가 한 줄이며, 왕복이 원문과 같은지 확인한다.
	var b strings.Builder
	for i := 0; i < 5000; i++ {
		b.WriteRune('가')
	}
	longSummary := b.String()

	m := Meta{
		Type: "decision", Date: "2026-08-07", Domain: []string{"priorcase"},
		Summary: longSummary, Status: "active", Outcome: "pending",
	}

	var got []byte
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("긴 summary 방출이 panic 했다: %v", r)
			}
		}()
		got = EmitFrontmatter(m)
	}()

	rt, _, err := ParseFrontmatter(append(got, []byte("\n본문\n")...))
	if err != nil {
		t.Fatalf("긴 summary 방출 결과를 다시 못 읽는다: %v", err)
	}
	if rt.Summary != longSummary {
		t.Errorf("긴 summary 왕복 실패: len(got)=%d len(want)=%d", len(rt.Summary), len(longSummary))
	}

	// frontmatter 전체 줄 수가 "---" + 10키 + "---" = 12줄과 정확히 일치해야 한다.
	// summary 가 여러 줄로 접혔다면 이보다 늘어난다.
	lines := bytes.Split(got, []byte("\n"))
	nonEmptyTrailing := len(lines)
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		nonEmptyTrailing-- // trailing "\n" 뒤에 Split 이 만드는 빈 마지막 요소
	}
	const wantLines = 12 // "---" + 10키 + "---"
	if nonEmptyTrailing != wantLines {
		t.Errorf("frontmatter 줄 수 = %d, want %d — summary 가 접혔을 가능성", nonEmptyTrailing, wantLines)
	}
}

// --- 지시사항 4: EmitNote 의 본문 처리 경계 ---

func TestEmitNoteBodyBoundaries(t *testing.T) {
	m := Meta{Type: "decision", Date: "2026-08-07"}
	fm := EmitFrontmatter(m)

	t.Run("본문이 비었을 때", func(t *testing.T) {
		got := EmitNote(m, []byte{})
		want := append(append([]byte{}, fm...), '\n')
		if !bytes.Equal(got, want) {
			t.Errorf("got = %q, want %q", got, want)
		}
		if !bytes.HasSuffix(got, []byte("---\n\n")) {
			t.Errorf("빈 본문일 때 frontmatter 뒤 빈 줄 하나로 끝나야 한다: %q", got)
		}
	})

	t.Run("본문이 개행으로 시작할 때", func(t *testing.T) {
		body := []byte("\n## 제목\n")
		got := EmitNote(m, body)
		want := append(append([]byte{}, fm...), append([]byte{'\n'}, body...)...)
		if !bytes.Equal(got, want) {
			t.Errorf("got = %q, want %q", got, want)
		}
		// EmitNote 는 body 를 손대지 않는다 — frontmatter 뒤 빈 줄 하나에 이어
		// body 의 선행 개행이 그대로 더해져 빈 줄이 두 개가 된다.
		if !bytes.Contains(got, []byte("---\n\n\n## 제목\n")) {
			t.Errorf("선행 개행이 그대로 보존되지 않았다: %q", got)
		}
	})

	t.Run("본문 끝에 개행이 없을 때", func(t *testing.T) {
		body := []byte("마지막 줄에 개행 없음")
		got := EmitNote(m, body)
		if bytes.HasSuffix(got, []byte("\n")) {
			t.Errorf("EmitNote 가 없는 트레일링 개행을 임의로 추가했다: %q", got)
		}
		if !bytes.HasSuffix(got, body) {
			t.Errorf("본문이 그대로 보존되지 않았다: %q", got)
		}
	})
}

// ★★ **author 가 비면 줄 자체가 없어야 한다.**
//
// 이 키는 순수 증분이다. 비었는데도 `author: ""` 를 쓰면 **기존 노트 전부가**
// 다음 저장 때 한 줄씩 늘어난다 — 볼트가 git 이면 그날 diff 가 전 노트로 번지고,
// 정작 무엇이 바뀌었는지 사람이 못 본다. 팀에서는 그게 리뷰를 통째로 막는다.
func TestAuthorIsOmittedWhenEmpty(t *testing.T) {
	m := Meta{
		Type: "decision", Date: "2026-08-11", Domain: []string{"alpha"},
		Summary: "요약", Status: "active", Outcome: "pending",
	}
	got := string(EmitFrontmatter(m))
	if strings.Contains(got, "author") {
		t.Errorf("author 가 비었는데 줄이 나왔다 — 기존 노트가 전부 한 줄씩 늘어난다:\n%s", got)
	}

	m.Author = "김철수 <kim@example.com>"
	got = string(EmitFrontmatter(m))
	if !strings.Contains(got, `author: "김철수 <kim@example.com>"`) {
		t.Errorf("author 가 안 나왔다:\n%s", got)
	}
	// date 바로 뒤여야 한다 — 누가·언제가 붙어 있어야 사람이 읽는다.
	if i, j := strings.Index(got, "date:"), strings.Index(got, "author:"); !(i < j && j < strings.Index(got, "domain:")) {
		t.Errorf("author 위치가 date 와 domain 사이가 아니다:\n%s", got)
	}
}

// ★ **왕복해도 author 가 살아 있어야 한다.** 여기가 깨지면 review·supersede 처럼
// 노트를 다시 쓰는 명령이 남의 author 를 조용히 지운다.
func TestAuthorSurvivesRoundTrip(t *testing.T) {
	src := "---\ntype: decision\ndate: 2026-08-11\nauthor: \"김철수\"\ndomain: [alpha]\n" +
		"summary: \"요약\"\nstatus: active\noutcome: pending\nsupersedes: \"\"\n" +
		"related: []\ntags: [decision]\nsource_session: \"\"\n---\n\n본문\n"
	m, body, err := ParseFrontmatter([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if m.Author != "김철수" {
		t.Fatalf("Author = %q", m.Author)
	}
	// Extra 로 새면 10키 뒤에 다시 붙어 순서가 흔들린다.
	if _, ok := m.Extra["author"]; ok {
		t.Error("author 가 Extra 로 샜다 — 정식 키인데 잉여 키로 취급됐다")
	}
	again, _, err := ParseFrontmatter(append(EmitFrontmatter(m), append([]byte("\n"), body...)...))
	if err != nil {
		t.Fatal(err)
	}
	if again.Author != "김철수" {
		t.Errorf("왕복 후 Author = %q", again.Author)
	}
}

// ★ supersedes 를 다중값으로 올릴 때 **디스크의 기존 노트가 전부 죽을 수 있다.**
//
// EmitFrontmatter 는 supersedes 를 omitempty 없이 항상 쓰므로 실볼트 191건 전부가
// `supersedes: ""` 를 갖고 있다. yaml.v3 는 `!!str` 를 `[]string` 에 못 넣는다 —
// 타입만 바꾸면 readNote 가 전건 실패하고 색인 0행·회수 0건이 된다.
// 스키마 판을 올려도 못 막는다: 파싱이 판 검사보다 먼저 일어난다.
func TestSupersedesReadsBothScalarAndSequence(t *testing.T) {
	cases := []struct {
		name, line string
		want       []string
	}{
		{"빈 스칼라 (디스크의 191건)", `supersedes: ""`, nil},
		{"단일 스칼라", `supersedes: "[[a-결정-x-2026-08-01]]"`, []string{"[[a-결정-x-2026-08-01]]"}},
		{"빈 시퀀스", `supersedes: []`, nil},
		{"시퀀스", `supersedes: ["[[a-결정-x-2026-08-01]]", "[[b-결정-y-2026-08-02]]"]`,
			[]string{"[[a-결정-x-2026-08-01]]", "[[b-결정-y-2026-08-02]]"}},
	}
	for _, c := range cases {
		src := "---\ntype: decision\ndate: 2026-08-09\ndomain: [a]\nsummary: \"s\"\n" +
			"status: active\noutcome: pending\n" + c.line + "\nrelated: []\n" +
			"tags: [decision]\nsource_session: \"\"\n---\n\n## 결정\n"
		m, _, err := ParseFrontmatter([]byte(src))
		if err != nil {
			t.Errorf("%s: 파싱 실패 — %v", c.name, err)
			continue
		}
		if len(m.Supersedes) != len(c.want) {
			t.Errorf("%s: %d개, want %d개 (%v)", c.name, len(m.Supersedes), len(c.want), m.Supersedes)
			continue
		}
		for i := range c.want {
			if m.Supersedes[i] != c.want[i] {
				t.Errorf("%s: [%d] = %q, want %q", c.name, i, m.Supersedes[i], c.want[i])
			}
		}
	}
}

// **기존 노트를 다시 저장해도 바이트가 안 바뀌어야 한다.** Author·Schema 가 이미
// 세운 규칙이다. 언제나 시퀀스로 쓰면 191건 전부의 diff 가 뜨고, 옛 바이너리는
// 그 노트를 못 읽는다.
func TestSupersedesEmitsScalarUnlessMultiple(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, `supersedes: ""`},
		{[]string{"[[a-결정-x-2026-08-01]]"}, `supersedes: "[[a-결정-x-2026-08-01]]"`},
		{[]string{"[[a-결정-x-2026-08-01]]", "[[b-결정-y-2026-08-02]]"},
			`supersedes: ["[[a-결정-x-2026-08-01]]", "[[b-결정-y-2026-08-02]]"]`},
	}
	for _, c := range cases {
		got := string(EmitFrontmatter(Meta{Type: "decision", Supersedes: c.in}))
		if !strings.Contains(got, c.want+"\n") {
			t.Errorf("Supersedes=%v 방출이 다르다.\nwant 줄: %s\n실제:\n%s", c.in, c.want, got)
		}
	}
}

// ★★ **기존 노트가 안 깨져야 한다.**
//
// 아래 frontmatter 는 실볼트의 editup-decision-codecommit-…-2026-08-18.md 원문이다
// (실볼트 18노트 전부가 이 모양 — summary_history·superseded_reason 이 없다).
// 새 키를 더하면서 이 노트를 다시 저장할 때 **바이트가 하나도 안 바뀌어야** 한다.
// 안 그러면 새 판을 깔았다는 이유만으로 볼트 전체가 diff 로 뜬다.
func TestExistingNoteBytesUnchangedByNewKeys(t *testing.T) {
	const real = `---
type: decision
date: 2026-08-18
domain: [editup]
summary: "CodeCommit 클론 인증은 git 전역 설정 대신 레포 로컬 credential.helper 로 한다"
status: active
outcome: bad
supersedes: ""
related: []
tags: [decision, codecommit, git, 인증, aws, 프로필, 클론]
source_session: "58d4ab65-90ff-4519-9f9a-afc79cb7b345"
---

## 결정
CodeCommit 레포는 레포 로컬 credential.helper 로 받는다.
`
	m, body, err := ParseFrontmatter([]byte(real))
	if err != nil {
		t.Fatalf("실볼트 노트를 못 읽는다: %v", err)
	}
	if m.SupersededReason != "" || len(m.SummaryHistory) != 0 {
		t.Fatalf("없는 키가 채워졌다: reason=%q history=%v", m.SupersededReason, m.SummaryHistory)
	}
	if got := string(EmitNote(m, body)); got != real {
		t.Errorf("기존 노트를 되쓰니 바이트가 변했다\n--- got ---\n%s\n--- want ---\n%s", got, real)
	}
}

// TestOverturnKeysRoundTrip 은 새 키 둘이 왕복하는지, 그리고 방출 위치가
// 의도대로인지 본다 — superseded_reason 은 supersedes 바로 뒤(같은 사건의 양쪽),
// summary_history 는 summary 바로 뒤.
func TestOverturnKeysRoundTrip(t *testing.T) {
	m := Meta{
		Type: "decision", Date: "2026-08-07", Domain: []string{"alpha"},
		Summary:          `고쳐진 한 줄 " 따옴표 포함`,
		SummaryHistory:   []string{"옛 한 줄", "그 전 한 줄"},
		Status:           "superseded",
		Outcome:          "bad",
		SupersededReason: `실측에서 뒤집혔다 " 따옴표 포함`,
	}
	got := string(EmitFrontmatter(m))

	wantOrder := []string{"summary:", "summary_history:", "status:", "outcome:",
		"supersedes:", "superseded_reason:", "related:"}
	pos := -1
	for _, k := range wantOrder {
		i := strings.Index(got, "\n"+k)
		if i < 0 {
			t.Fatalf("키 %q 가 없다:\n%s", k, got)
		}
		if i <= pos {
			t.Fatalf("키 순서가 틀렸다 (%q):\n%s", k, got)
		}
		pos = i
	}

	rt, _, err := ParseFrontmatter(append(EmitFrontmatter(m), []byte("\n본문\n")...))
	if err != nil {
		t.Fatal(err)
	}
	if rt.SupersededReason != m.SupersededReason {
		t.Errorf("superseded_reason 왕복 실패: %q", rt.SupersededReason)
	}
	if len(rt.SummaryHistory) != 2 || rt.SummaryHistory[0] != "옛 한 줄" {
		t.Errorf("summary_history 왕복 실패: %v", rt.SummaryHistory)
	}

	// 멱등: 두 번 방출해도 같아야 한다.
	if once, twice := EmitFrontmatter(m), EmitFrontmatter(rt); !bytes.Equal(once, twice) {
		t.Errorf("멱등하지 않다\n--- 1회 ---\n%s\n--- 2회 ---\n%s", once, twice)
	}
}
