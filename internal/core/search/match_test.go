package search

import "testing"

// ★★ ASCII 는 낱말 경계를, CJK 는 부분 매칭을 쓴다.
//
// 한 규칙으로는 안 된다. 한국어는 띄어쓰기 없이 복합어를 만들어서("저장엔진")
// 경계만 요구하면 회수가 죽고, ASCII 는 부분 매칭이면 `ok` 가 `hooks` 안에 걸려
// 잡음이 된다. 실볼트 실측: `ok` 5건, `use` 4건, 영어 문장 5건이 전부 오탐이었다.
func TestMatchRuleDependsOnScript(t *testing.T) {
	for _, tc := range []struct {
		name, text, keyword string
		want                bool
	}{
		// ASCII — 낱말 경계를 요구한다
		{"ASCII 낱말 일치", "use postgres for jsonb", "postgres", true},
		{"ASCII 낱말 안쪽은 아니다", "casebook hooks and tokens", "ok", false},
		{"ASCII 접미가 붙으면 아니다", "we used it because", "use", false},
		{"하이픈은 경계다", "alpha-use-postgres-2026", "postgres", true},
		{"언더스코어도 경계다", "source_session", "session", true},
		{"점도 경계다", "draft.ai", "ai", true},
		{"숫자는 낱말 안쪽", "sqlite3", "sqlite", false},

		// CJK — 부분 매칭을 허용한다 (띄어쓰기가 낱말을 안 가른다)
		{"한글 복합어 부분", "저장엔진을 골랐다", "저장", true},
		{"한글 중간 부분", "결정-안전망한계-실측공개", "안전망", true},
		{"한글 없는 것", "저장엔진", "네트워크", false},
		// ★ 여기가 두 규칙이 갈리는 자리다. 경계 매칭이면 ASCII 바로 뒤의 한글을
		// 낱말 안쪽으로 보고 놓친다 — 그래서 CJK 는 부분 매칭이어야 한다.
		{"ASCII 뒤에 붙은 한글", "sqlite저장소", "저장", true},
		{"ASCII 뒤에 붙은 한글 2", "api연동-설계", "연동", true},
		{"숫자 뒤에 붙은 한글", "v2스키마", "스키마", true},

		// 섞인 것 — CJK 가 하나라도 있으면 부분 매칭
		{"한영 혼합", "casebook-결정-국제화범위", "국제화", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := matches(tc.text, tc.keyword); got != tc.want {
				t.Errorf("matches(%q, %q) = %v, want %v", tc.text, tc.keyword, got, tc.want)
			}
		})
	}
}

// 빈 키워드는 아무것도 안 건드린다 — 안 그러면 전 노트가 걸린다.
func TestEmptyKeywordMatchesNothing(t *testing.T) {
	if matches("아무 텍스트", "") {
		t.Error("빈 키워드가 매칭됐다 — 전 노트가 걸린다")
	}
}

// 경계 검색이 무한 루프나 인덱스 초과에 빠지지 않아야 한다.
func TestContainsWordHandlesEdges(t *testing.T) {
	for _, tc := range []struct {
		text, kw string
		want     bool
	}{
		{"", "a", false},
		{"a", "a", true},
		{"aa", "a", false},
		{"a b a", "a", true},
		{"xa", "a", false},
		{"ax", "a", false},
	} {
		if got := containsWord(tc.text, tc.kw); got != tc.want {
			t.Errorf("containsWord(%q, %q) = %v, want %v", tc.text, tc.kw, got, tc.want)
		}
	}
}

// ★ 실측으로 오탐이던 질의들. 픽스처 볼트에서 0건이어야 한다.
//
// 실볼트 66건에서 `ok`·`use`·`다`·영어 문장이 각각 5·4·5·5건을 반환했다.
// 훅은 매 프롬프트마다 이걸 주입하므로, 무관한 주입은 곧 "무시하는 법" 을 가르친다.
func TestConversationalPromptsRecallNothing(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	for _, q := range []string{
		"ok", "use", "다", "네", "고마워", "그래서?", "이거 좀 해줘",
		"can you add a spinner on the login button",
		"오늘 날씨 어때", "please make it a bit more compact",
		"thanks, that works", "무엇을 해야 할까",
	} {
		hits, _, err := Recall(l, c, q, Options{CrossProject: true, Limit: 5, MinScore: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 0 {
			var names []string
			for _, h := range hits {
				names = append(names, h.Note.Stem)
			}
			t.Errorf("%q → %d건 주입 (전부 잡음): %v", q, len(hits), names)
		}
	}
}

// 반대편 — 진짜 질의는 여전히 걸려야 한다. 정밀도를 올리다 회수를 죽이면 안 된다.
//
// 픽스처 볼트는 전부 한국어이므로 한국어 질의로 본다. 한글은 복합어가 붙어 있어
// 부분 매칭이 필수다 — "저장" 이 "저장엔진" 에 걸려야 한다.
func TestRealQueriesStillRecall(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	for _, q := range []string{
		"저장 엔진", "저장엔진", "스키마 단일 테이블", "배포 전략", "로케일 함정",
	} {
		hits, _, err := Recall(l, c, q, Options{CrossProject: true, Limit: 5, MinScore: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) == 0 {
			t.Errorf("%q → 0건 — 정밀도를 올리다 회수를 죽였다", q)
		}
	}
}

// ★ 모든 노트가 갖는 태그는 검색 신호가 아니다.
//
// `capture` 가 전 노트에 `decision` 태그를 붙인다(실볼트 67/67). 그 낱말이 질의에
// 들어오면 **볼트 전체가 headHits ≥ 1** 이 되어 `headHits == 0` 필터가 통째로
// 무력해진다. 불용어에 "결정" 이 들어 있는 것과 같은 이유다.
func TestBoilerplateTagIsNotASearchSignal(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	all, _, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"decision", "decision 뭐가 있었지"} {
		hits, _, err := Recall(l, c, q, Options{CrossProject: true, Limit: 20, MinScore: 1})
		if err != nil {
			t.Fatal(err)
		}
		// **0건이어야 한다.** 모든 노트가 똑같이 걸리는 낱말은 신호가 아니라 잡음이다.
		// "몇 건 미만" 으로 느슨하게 두면, 태그가 다시 head 에 들어와도 일부만
		// 걸리는 바람에 통과해 버린다 — 실제로 그렇게 새어 나갔다(3건/4건).
		if len(hits) != 0 {
			var names []string
			for _, h := range hits {
				names = append(names, h.Note.Stem)
			}
			t.Errorf("%q → %d건 (전체 %d건) — 보일러플레이트 태그가 검색 신호가 됐다: %v",
				q, len(hits), len(all), names)
		}
	}
}
