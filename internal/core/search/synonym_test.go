package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// writeSynonyms 는 픽스처 볼트에 동의어 표를 심는다.
func writeSynonyms(t *testing.T, l *store.Layout, body string) {
	t.Helper()
	p := filepath.Join(l.Vault(), filepath.FromSlash(SynonymPath(l)))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stems(hits []Hit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Note.Stem)
	}
	return out
}

func hasStem(hits []Hit, want string) bool {
	for _, h := range hits {
		if strings.Contains(h.Note.Stem, want) {
			return true
		}
	}
	return false
}

// 표가 없을 때 안 걸리던 낱말이, 표를 심으면 걸린다. 이 기능의 존재 이유다.
func TestSynonymBridgeFindsParaphrase(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	const q = "데이터보관"

	if hits := mustRecall(t, l, c, q, Options{CrossProject: true, Limit: 5, MinScore: 1}); len(hits) != 0 {
		t.Fatalf("표 없이 %q 가 걸렸다 — 픽스처에 그 낱말이 생겼다면 이 테스트를 다시 써야 한다: %v",
			q, stems(hits))
	}

	writeSynonyms(t, l, "# 회수 동의어\n\n설명 줄은 무시된다.\n\n- 데이터보관, 저장엔진\n")

	hits := mustRecall(t, l, c, q, Options{CrossProject: true, Limit: 5, MinScore: 1})
	if !hasStem(hits, "저장엔진") {
		t.Errorf("표를 심었는데 저장엔진 노트가 안 걸린다: %v", stems(hits))
	}
}

// **정확히 맞은 것이 언제나 앞선다.** weightSynonym < weightHead 의 계약이다.
// 뒤집히면 회수 슬롯 3개가 패러프레이즈로 채워져 정확한 답이 탈락한다.
func TestExactHitOutranksSynonymHit(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	// 릴리스방식 → 배포전략(beta 노트의 stem)만 동의어로 잇는다.
	// 저장엔진은 alpha 노트에 그대로 있으므로 정확한 히트다.
	writeSynonyms(t, l, "- 릴리스방식, 배포전략\n")

	hits := mustRecall(t, l, c, "저장엔진 릴리스방식", Options{CrossProject: true, Limit: 5, MinScore: 1})
	if len(hits) < 2 {
		t.Fatalf("둘 다 걸려야 비교가 된다: %v", stems(hits))
	}
	var exact, syn *Hit
	for i := range hits {
		switch {
		case strings.Contains(hits[i].Note.Stem, "저장엔진"):
			if exact == nil {
				exact = &hits[i]
			}
		case strings.Contains(hits[i].Note.Stem, "배포전략"):
			if syn == nil {
				syn = &hits[i]
			}
		}
	}
	if exact == nil || syn == nil {
		t.Fatalf("정확히 맞은 노트와 동의어로 맞은 노트가 둘 다 있어야 한다: %v", stems(hits))
	}
	if exact.Score <= syn.Score {
		t.Errorf("정확한 히트(%d)가 동의어 히트(%d)보다 높아야 한다", exact.Score, syn.Score)
	}
}

// 대화체 질의(키워드 4개 이상)는 히트 둘을 요구한다. 동의어 히트도 그 둘에 센다 —
// 안 세면 패러프레이즈 질의는 정의상 정확한 히트가 0 이라 이 기능이 아무것도 못 한다.
// 반대로 **하나로는 부족하다** — 그게 우연한 매칭을 막는 장치다.
func TestSynonymPassesConversationalGateOnlyWithTwoHits(t *testing.T) {
	const q = "데이터보관 과 영속화 를 어떻게 정했는지 다시 보고 싶다"

	t.Run("둘이면 통과", func(t *testing.T) {
		l, c := fixtureLayoutConfig(t)
		writeSynonyms(t, l, "- 데이터보관, 저장엔진\n- 영속화, 영속성\n")
		if hits := mustRecall(t, l, c, q, Options{CrossProject: true, Limit: 5, MinScore: 1}); !hasStem(hits, "저장엔진") {
			t.Errorf("동의어 히트 둘인데 안 걸린다: %v", stems(hits))
		}
	})

	t.Run("하나면 탈락", func(t *testing.T) {
		l, c := fixtureLayoutConfig(t)
		writeSynonyms(t, l, "- 데이터보관, 저장엔진\n")
		if hits := mustRecall(t, l, c, q, Options{CrossProject: true, Limit: 5, MinScore: 1}); hasStem(hits, "저장엔진") {
			t.Errorf("동의어 히트 하나로 대화체 게이트를 넘었다 — 우연한 매칭을 막는 장치가 뚫린다: %v",
				stems(hits))
		}
	})
}

// **표가 없으면 한 점도 달라지지 않는다.** 켜는 것은 파일을 만드는 행위 하나여야 한다.
func TestNoSynonymFileChangesNothing(t *testing.T) {
	const q = "저장 엔진을 무엇으로 골랐지"
	o := Options{CrossProject: true, Limit: 5, MinScore: 1}

	l1, c1 := fixtureLayoutConfig(t)
	base := mustRecall(t, l1, c1, q, o)

	// 산문만 있는 표 = 묶음 0개. 파일이 아예 없는 것과 같아야 한다.
	l2, c2 := fixtureLayoutConfig(t)
	writeSynonyms(t, l2, "# 제목\n\n아직 아무것도 안 적었다.\n")
	prose := mustRecall(t, l2, c2, q, o)

	if len(base) != len(prose) {
		t.Fatalf("건수가 달라졌다: %d → %d", len(base), len(prose))
	}
	for i := range base {
		if base[i].Note.Stem != prose[i].Note.Stem || base[i].Score != prose[i].Score {
			t.Errorf("%d위가 달라졌다: %s(%d) → %s(%d)",
				i+1, base[i].Note.Stem, base[i].Score, prose[i].Note.Stem, prose[i].Score)
		}
	}
}

// 형제가 여럿 맞아도 질의어 하나는 한 번만 센다 — 표를 크게 쓴 낱말이 점수를
// 독식하면 표가 순위 조작 수단이 된다.
func TestSynonymCountsEachKeywordOnce(t *testing.T) {
	score := func(t *testing.T, table string) int {
		t.Helper()
		l, c := fixtureLayoutConfig(t)
		writeSynonyms(t, l, table)
		hits := mustRecall(t, l, c, "데이터보관", Options{CrossProject: true, Limit: 5, MinScore: 1})
		for _, h := range hits {
			if strings.Contains(h.Note.Stem, "저장엔진") {
				return h.Score
			}
		}
		t.Fatalf("저장엔진 노트가 없다: %v", stems(hits))
		return 0
	}
	one := score(t, "- 데이터보관, 저장엔진\n")
	three := score(t, "- 데이터보관, 저장엔진, 영속성, 임베디드\n")
	if one != three {
		t.Errorf("형제 수가 점수를 바꿨다: 1개=%d, 3개=%d", one, three)
	}
}

// 흔한 낱말이 표에 들어가는 것이 이 설계의 실패 모드다. 거절하고, **거절을 말한다** —
// 조용히 버리면 사람은 고쳤다고 믿고 회수는 그대로다.
func TestSynonymRejectsGenericWordsLoudly(t *testing.T) {
	s := parseSynonyms("- 회수, 불러오기, 결정, 어떻게, 가\n")
	for _, w := range []string{"결정", "어떻게", "가"} {
		if !containsStr(s.Rejected, w) {
			t.Errorf("%q 를 거절했다고 말하지 않았다: %v", w, s.Rejected)
		}
		if len(s.sib[w]) != 0 {
			t.Errorf("%q 가 표에 들어갔다", w)
		}
	}
	if !containsStr(s.sib["회수"], "불러오기") {
		t.Errorf("멀쩡한 낱말까지 버렸다: %v", s.sib)
	}
}

func TestParseSynonyms(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		groups int
		want   map[string][]string
	}{
		{
			name:   "목록이 아닌 줄은 무시한다",
			src:    "# 제목\n\n왜 이렇게 정했는지 설명.\n\n- 회수, 불러오기\n\n> 인용\n",
			groups: 1,
			want:   map[string][]string{"회수": {"불러오기"}},
		},
		{
			name:   "혼자 있는 낱말은 아무것도 안 넓힌다",
			src:    "- 회수\n",
			groups: 0,
		},
		{
			name:   "장식을 벗긴다",
			src:    "- `회수`, **불러오기**, [[검색]]\n",
			groups: 1,
			want:   map[string][]string{"회수": {"불러오기", "검색"}},
		},
		{
			name:   "별표 목록도 받는다",
			src:    "* 손절, 철수\n",
			groups: 1,
			want:   map[string][]string{"손절": {"철수"}},
		},
		{
			name:   "같은 낱말이 두 묶음에 있으면 형제가 합쳐진다",
			src:    "- 회수, 불러오기\n- 회수, 검색\n",
			groups: 2,
			want:   map[string][]string{"회수": {"불러오기", "검색"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := parseSynonyms(tc.src)
			if s.Groups() != tc.groups {
				t.Errorf("묶음 수 = %d, want %d", s.Groups(), tc.groups)
			}
			for k, want := range tc.want {
				got := s.sib[k]
				if len(got) != len(want) {
					t.Fatalf("%q 형제 = %v, want %v", k, got, want)
				}
				for _, w := range want {
					if !containsStr(got, w) {
						t.Errorf("%q 형제에 %q 가 없다: %v", k, w, got)
					}
				}
			}
		})
	}
}

// 파일이 없으면 빈 표를 준다 — 에러가 아니다. 대다수 볼트에 이 파일이 없다.
func TestLoadSynonymsMissingFileIsNotAnError(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	s := LoadSynonyms(l)
	if !s.Empty() {
		t.Errorf("파일이 없는데 표가 비지 않았다: %d묶음", s.Groups())
	}
	if s.hits("아무 텍스트", "아무말") {
		t.Error("빈 표가 히트를 냈다")
	}
}

// nil Layout 에서도 죽지 않는다 — scoreAll 을 직접 부르는 테스트·도구가 있다.
func TestLoadSynonymsNilLayout(t *testing.T) {
	if s := LoadSynonyms(nil); !s.Empty() {
		t.Error("nil Layout 에서 표가 생겼다")
	}
}

// 활용형 질의도 표에 걸린다 — 실측으로 이것이 없으면 문장형 질의가 통째로 새었다.
func TestSynonymMatchesInflectedKeyword(t *testing.T) {
	s := parseSynonyms("- 승부, 경쟁\n- 물량, 대량\n")
	tests := []struct {
		k    string
		want bool
		why  string
	}{
		{"승부", true, "사전형"},
		{"승부하면", true, "어미가 붙은 활용형"},
		{"물량으로", true, "떼지 못한 조사가 붙은 형태"},
		{"승", false, "표의 낱말보다 짧다 — 반대 방향은 안 잇는다"},
		{"대승부", false, "가운데가 아니라 접두만 본다"},
		{"전혀다른말", false, "무관한 낱말"},
	}
	for _, tc := range tests {
		if got := len(s.siblings(tc.k)) > 0; got != tc.want {
			t.Errorf("siblings(%q) 있음=%v, want %v (%s)", tc.k, got, tc.want, tc.why)
		}
	}
}

// 접두가 여럿 걸리면 가장 구체적인(긴) 쪽을 쓴다.
func TestSynonymPrefersLongestPrefix(t *testing.T) {
	s := parseSynonyms("- 종료, 끝\n- 종료조건, 킬스위치\n")
	got := s.siblings("종료조건을")
	if !containsStr(got, "킬스위치") {
		t.Errorf("긴 접두(종료조건)를 못 골랐다: %v", got)
	}
	if containsStr(got, "끝") {
		t.Errorf("짧은 접두(종료)까지 섞었다: %v", got)
	}
}
